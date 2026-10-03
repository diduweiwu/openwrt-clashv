package core

// 节点域名优选：机场节点的 server 常为域名，内核自行解析后只会固定用其中
// 一个 IP。这里定时把域名的全部 A/AAAA 记录解析出来，对每个 IP 做 TCP 握手
// 测速，把最快的 IP 直接写进运行时配置的 server 字段。
//
// 安全边界（宁可不用，不可用坏）：
//   - 只对 TCP 型协议做测速；hysteria/tuic 等 QUIC 族 TCP 多半不通，测不出
//     优劣，一律保持域名由内核自己解析；
//   - TLS 节点必须带显式 sni/servername 才允许改写——server 换成 IP 后 SNI
//     会跟着变，没有显式 SNI 的节点（证书按域名校验、CDN 按 SNI 分流）会被
//     改坏，这类节点保留域名；
//   - 某个 IP 都拨不通时保留域名，优选失败永远不劣于不优选。

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	probeTimeout = 2 * time.Second // 单个 IP 的 TCP 拨号超时
	probeBudget  = 4 * time.Second // 单个域名「解析+全部 IP 测速」的总预算
	maxProbeIPs  = 8               // 解析结果过多时只测前几个（v4 优先）
)

// tcpProbeTypes 允许 TCP 握手测速的协议白名单。
var tcpProbeTypes = map[string]bool{
	"ss": true, "vmess": true, "vless": true, "trojan": true,
	"socks5": true, "http": true, "snell": true, "anytls": true, "ssh": true,
}

// probeEntry 是一次优选测速的结果缓存。
type probeEntry struct {
	ip string // 最快 IP，空串表示该域名当前没有可拨通的 IP
	at time.Time
}

// optState 是节点优选的运行记忆：applied 记录当前运行时配置里每个
// 「域名|端口」实际写入的 IP（空串=保留域名），供定时重测判断是否有变化。
type optState struct {
	mu         sync.Mutex
	applied    map[string]string
	probeCache map[string]probeEntry
}

// serverKey 是测速缓存的键。
func serverKey(host, port string) string { return host + "|" + port }

// probeBestServers 遍历 doc["proxies"]，对每个可优化的「域名+端口」组合
// 测出最快 IP，返回 域名|端口 → IP（测不出为空串）。ttl 内的结果走缓存。
func (m *Manager) probeBestServers(doc map[string]any, ttl time.Duration) map[string]string {
	type target struct{ host, port string }
	seen := map[string]bool{}
	var targets []target
	for _, pm := range proxyMaps(doc) {
		host, port, ok := optimizableServer(pm)
		if !ok {
			continue
		}
		key := serverKey(host, port)
		if seen[key] {
			continue
		}
		seen[key] = true
		targets = append(targets, target{host, port})
	}
	if len(targets) == 0 {
		return map[string]string{}
	}
	var wg sync.WaitGroup
	best := make(map[string]string, len(targets))
	for _, t := range targets {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			best[serverKey(t.host, t.port)] = m.bestForServer(t.host, t.port, ttl)
		}(t)
	}
	wg.Wait()
	for key, ip := range best {
		host, port := splitServerKey(key)
		if ip == "" {
			slog.Info("节点域名优选未测出可用 IP，保留域名由内核解析", "server", host, "port", port)
			continue
		}
		slog.Info("节点域名优选完成", "server", host, "port", port, "ip", ip)
	}
	return best
}

// applyBestServers 把测速结果写进 doc 的 proxies，并记录到 applied；
// 返回实际被改写的节点数（server 从域名换成了 IP）。
func (m *Manager) applyBestServers(doc map[string]any, best map[string]string) int {
	next := make(map[string]string, len(best))
	changed := 0
	for _, pm := range proxyMaps(doc) {
		host, port, ok := optimizableServer(pm)
		if !ok {
			continue
		}
		key := serverKey(host, port)
		ip := best[key]
		next[key] = ip
		if ip == "" || ip == pm["server"] {
			continue
		}
		pm["server"] = ip
		changed++
	}
	m.opt.mu.Lock()
	m.opt.applied = next
	m.opt.mu.Unlock()
	return changed
}

// reprobeApplied 强制重测当前已应用的域名集合（绕过缓存），返回最新结果。
func (m *Manager) reprobeApplied() map[string]string {
	m.opt.mu.Lock()
	keys := make([]string, 0, len(m.opt.applied))
	for k := range m.opt.applied {
		keys = append(keys, k)
	}
	m.opt.mu.Unlock()
	best := make(map[string]string, len(keys))
	var wg sync.WaitGroup
	for _, key := range keys {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			host, port := splitServerKey(key)
			best[key] = m.probeServerNow(host, port)
		}(key)
	}
	wg.Wait()
	return best
}

// OptimizeTick 供定时循环调用：重测已应用的域名，结果有变化时重启内核生效。
// 返回是否发生了重启。
func (m *Manager) OptimizeTick() bool {
	if !m.Running() {
		return false
	}
	best := m.reprobeApplied()
	if !m.appliedDiffers(best) {
		slog.Info("节点 IP 优选复测完成，结果无变化")
		return false
	}
	slog.Info("节点 IP 优选测得更优 IP，重载内核生效")
	if err := m.Restart(); err != nil {
		slog.Warn("节点 IP 优选重载内核失败", "err", err)
		return false
	}
	return true
}

func (m *Manager) appliedDiffers(best map[string]string) bool {
	m.opt.mu.Lock()
	defer m.opt.mu.Unlock()
	if len(best) != len(m.opt.applied) {
		return true
	}
	for k, v := range best {
		if m.opt.applied[k] != v {
			return true
		}
	}
	return false
}

// bestForServer 取某域名+端口的最快 IP，ttl 内复用缓存，过期则重新测速。
func (m *Manager) bestForServer(host, port string, ttl time.Duration) string {
	key := serverKey(host, port)
	m.opt.mu.Lock()
	e, ok := m.opt.probeCache[key]
	m.opt.mu.Unlock()
	if ok && time.Since(e.at) < ttl {
		return e.ip
	}
	ip := m.probeServerNow(host, port)
	m.opt.mu.Lock()
	m.opt.probeCache[key] = probeEntry{ip: ip, at: time.Now()}
	m.opt.mu.Unlock()
	return ip
}

// probeServerNow 解析域名的全部 IP 并并发 TCP 握手测速，返回最快 IP
// （全部失败返回空串）。
func (m *Manager) probeServerNow(host, port string) string {
	ctx, cancel := context.WithTimeout(context.Background(), probeBudget)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return ""
	}
	// v4 优先：路由器代理路径通常只有 v4，v6 放前面会白占测速名额
	sort.Slice(addrs, func(i, j int) bool { return addrs[i].Is4() && !addrs[j].Is4() })
	if len(addrs) > maxProbeIPs {
		addrs = addrs[:maxProbeIPs]
	}
	portNum, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return ""
	}
	type result struct {
		ip string
		ms int64
	}
	results := make(chan result, len(addrs))
	for _, a := range addrs {
		go func(a netip.Addr) {
			start := time.Now()
			d := net.Dialer{Timeout: probeTimeout}
			c, err := d.DialContext(ctx, "tcp", netip.AddrPortFrom(a, uint16(portNum)).String())
			if err != nil {
				results <- result{}
				return
			}
			_ = c.Close()
			results <- result{ip: a.String(), ms: time.Since(start).Milliseconds()}
		}(a)
	}
	best, bestMS := "", int64(-1)
	for range addrs {
		r := <-results
		if r.ip != "" && (bestMS < 0 || r.ms < bestMS) {
			best, bestMS = r.ip, r.ms
		}
	}
	if best != "" {
		slog.Info("节点域名测速结果", "server", host, "port", port, "best", best, "rtt_ms", bestMS, "candidates", len(addrs))
	}
	return best
}

// proxyMaps 取 doc["proxies"] 里的节点 map 列表（缺失/类型不符返回空）。
func proxyMaps(doc map[string]any) []map[string]any {
	list, ok := doc["proxies"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		if pm, ok := p.(map[string]any); ok {
			out = append(out, pm)
		}
	}
	return out
}

// optimizableServer 判断节点是否允许做 IP 优选，允许时返回 域名/端口。
// 硬性条件：协议在 TCP 白名单内、server 是域名、端口合法、无 dialer-proxy、
// 用 TLS 时必须有显式 sni/servername（server 换 IP 后 SNI 不能跟着变）。
func optimizableServer(pm map[string]any) (string, string, bool) {
	typ, _ := pm["type"].(string)
	if !tcpProbeTypes[typ] {
		return "", "", false
	}
	if _, chained := pm["dialer-proxy"]; chained {
		return "", "", false
	}
	host, _ := pm["server"].(string)
	if host == "" || net.ParseIP(host) != nil || !containsDot(host) {
		return "", "", false // 已是 IP / 内网单标签名：不参与
	}
	port, ok := portOf(pm)
	if !ok {
		return "", "", false
	}
	if !tlsServerNameSafe(pm) {
		return "", "", false
	}
	return host, strconv.Itoa(port), true
}

// tlsServerNameSafe 报告「server 换成 IP 后 TLS 语义不变」：
//   - 未启用 TLS（或本协议不涉 TLS）：true；
//   - 启用 TLS：必须有显式 sni/servername，否则 SNI 会退化成 server 本身，
//     换成 IP 后证书校验/CDN 分流都可能被改坏，这类节点不改写。
func tlsServerNameSafe(pm map[string]any) bool {
	truthy := func(v any) bool {
		b, ok := v.(bool)
		return ok && b
	}
	str := func(v any) string { s, _ := v.(string); return s }
	switch pm["type"] {
	case "trojan", "anytls":
		return str(pm["sni"]) != ""
	case "vmess", "vless":
		if !truthy(pm["tls"]) {
			return true
		}
		return str(pm["servername"]) != ""
	case "socks5", "http":
		if !truthy(pm["tls"]) {
			return true
		}
		return str(pm["sni"]) != ""
	case "ss":
		// shadowsocks 本体无 TLS；挂 shadowtls 插件时由 plugin-opts.sni 负责
		opts, ok := pm["plugin-opts"].(map[string]any)
		if !ok {
			return true
		}
		if truthy(opts["tls"]) {
			return str(opts["sni"]) != ""
		}
		return true
	}
	return false
}

// portOf 取节点端口，兼容 yaml 解析出的 int / float64 / 字符串。
func portOf(pm map[string]any) (int, bool) {
	switch v := pm["port"].(type) {
	case int:
		return v, v > 0 && v < 65536
	case int64:
		return int(v), v > 0 && v < 65536
	case float64:
		return int(v), v > 0 && v < 65536
	case string:
		n, err := strconv.Atoi(v)
		return n, err == nil && n > 0 && n < 65536
	}
	return 0, false
}

func containsDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}

// splitServerKey 拆回 host/port（仅用于日志展示）。
func splitServerKey(key string) (host, port string) {
	host, port, _ = strings.Cut(key, "|")
	return host, port
}

// resetApplied 清空优选记录（关闭开关后调用，运行时配置回到纯域名）。
func (m *Manager) resetApplied() {
	m.opt.mu.Lock()
	m.opt.applied = map[string]string{}
	m.opt.mu.Unlock()
}
