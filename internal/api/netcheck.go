// 网络体检接口：出口 IP 查询（/api/ipinfo）与网站解锁测试（/api/unlock/test）。
//
// 两个接口都只在本机发起出站请求：直连用默认 client（不经内核），「代理出口」
// 与「解锁测试」统一经本机混合端口（127.0.0.1:MixedPort）走内核代理——路由器
// 本机直连不经过透明代理/TUN，直连结果即国内真实出口 IP。
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// browserUA 浏览器 User-Agent：ip.sb 的边缘节点会拦截常见 HTTP 库的默认 UA
// （官方文档明确提示需自定义），部分站点的 Cloudflare 防护页也对非浏览器 UA 返回 403，
// 因此出口 IP 查询与解锁测试统一携带浏览器 UA。
const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

// ipSource 是一个出口 IP 查询源：URL + 从 JSON 响应里摘字段的方法。
// 逐源兜底：海外源（ip.sb）可能被墙，直连场景自动落到国内可达的 ip-api.com。
type ipSource struct {
	name string
	url  string
	parse func(data []byte) (ip, country, city, isp string, err error)
}

// ipSources 出口 IP 查询源列表，按序尝试直到成功。海外源经代理失败时逐级落到
// 纯文本端点（只回 IP，胜在轻量）与国内直连可达的 ip-api.com（免费版仅 http）。
var ipSources = []ipSource{
	{
		name: "ip.sb",
		url:  "https://api.ip.sb/geoip",
		parse: func(b []byte) (string, string, string, string, error) {
			var v struct {
				IP          string `json:"ip"`
				Country     string `json:"country"`
				CountryCode string `json:"country_code"`
				City        string `json:"city"`
				ISP         string `json:"isp"`
			}
			if err := json.Unmarshal(b, &v); err != nil {
				return "", "", "", "", err
			}
			if v.IP == "" {
				return "", "", "", "", fmt.Errorf("响应缺少 ip 字段")
			}
			return v.IP, v.CountryCode, v.City, v.ISP, nil
		},
	},
	{
		name:  "ip.sb/ip",
		url:   "https://api.ip.sb/ip",
		parse: parsePlainIP,
	},
	{
		name:  "ipify",
		url:   "https://api.ipify.org",
		parse: parsePlainIP,
	},
	{
		name: "ip-api.com",
		url:  "http://ip-api.com/json/?lang=zh-CN&fields=status,query,country,city,isp",
		parse: func(b []byte) (string, string, string, string, error) {
			var v struct {
				Status  string `json:"status"`
				Query   string `json:"query"`
				Country string `json:"country"`
				City    string `json:"city"`
				ISP     string `json:"isp"`
			}
			if err := json.Unmarshal(b, &v); err != nil {
				return "", "", "", "", err
			}
			if v.Status != "success" || v.Query == "" {
				return "", "", "", "", fmt.Errorf("查询失败")
			}
			return v.Query, v.Country, v.City, v.ISP, nil
		},
	},
}

// parsePlainIP 解析纯文本 IP 端点（api.ip.sb/ip、api.ipify.org）：响应体就是 IP 本身。
// 只回 IP 不带地理信息（国家/城市/运营商留空，前端位置行显示 —）。
func parsePlainIP(b []byte) (string, string, string, string, error) {
	ip := strings.TrimSpace(string(b))
	if net.ParseIP(ip) == nil {
		return "", "", "", "", fmt.Errorf("响应不是合法 IP")
	}
	return ip, "", "", "", nil
}

// ipEntry 单个出口的查询结果（直连或代理各一份）。
type ipEntry struct {
	OK      bool   `json:"ok"`      // 是否至少有一个源查询成功
	IP      string `json:"ip"`      // 出口 IP
	Country string `json:"country"` // 国家/地区（国内源为中文，海外源为国家代码）
	City    string `json:"city"`    // 城市
	ISP     string `json:"isp"`     // 运营商
	Source  string `json:"source"`  // 命中的查询源
	Error   string `json:"error"`   // 全部源都失败时的错误信息
}

// ipInfoCache 缓存最近一次查询结果，60 秒内重复请求直接返回，
// 避免首页与详情弹窗反复打查询源；refresh 参数可强制绕过。
type ipInfoCache struct {
	mu      sync.Mutex
	at      time.Time
	direct  ipEntry
	proxy   ipEntry
	proxyOK bool // 代理侧是否查到过（内核未运行时为 false，运行后应重查）
}

var ipCache ipInfoCache

// queryIPEntry 依次尝试各查询源，返回第一个成功的结果；全部失败时把各源原因
// 逐个拼进 Error（如「ip.sb: HTTP 502；ip.sb/ip: …」），方便详情弹窗定位是哪个源挂了。
func queryIPEntry(hc *http.Client) ipEntry {
	var errs []string
	for _, src := range ipSources {
		entry, err := queryOneSource(hc, src)
		if err == nil {
			return entry
		}
		errs = append(errs, src.name+": "+err.Error())
		slog.Debug("出口 IP 查询源失败", "source", src.name, "err", err)
	}
	return ipEntry{OK: false, Error: strings.Join(errs, "；")}
}

// queryOneSource 请求单个查询源并解析（响应上限 64KB，超时由 client 控制）。
func queryOneSource(hc *http.Client, src ipSource) (ipEntry, error) {
	req, err := http.NewRequest("GET", src.url, nil)
	if err != nil {
		return ipEntry{}, err
	}
	req.Header.Set("User-Agent", browserUA)
	resp, err := hc.Do(req)
	if err != nil {
		return ipEntry{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ipEntry{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return ipEntry{}, err
	}
	ip, country, city, isp, err := src.parse(data)
	if err != nil {
		return ipEntry{}, err
	}
	return ipEntry{OK: true, IP: ip, Country: country, City: city, ISP: isp, Source: src.name}, nil
}

// handleIPInfo GET /api/ipinfo：查询国内直连出口与代理出口的 IP 信息。
// 参数 refresh=1 时绕过 60 秒缓存强制重查。
// 返回 {direct: ipEntry, proxy: ipEntry, ts: 采样时刻Unix秒}。
// 内核未运行时 proxy 侧连接会被拒绝，返回 ok=false 的占位结果。
func (d *deps) handleIPInfo(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"

	ipCache.mu.Lock()
	cached := !refresh && time.Since(ipCache.at) < 60*time.Second
	if cached {
		out := map[string]any{
			"direct": ipCache.direct,
			"proxy":  ipCache.proxy,
			"ts":     ipCache.at.Unix(),
		}
		ipCache.mu.Unlock()
		writeJSON(w, 200, out)
		return
	}
	ipCache.mu.Unlock()

	s, err := d.cfg.Get()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	// 直连与代理并发查询，两边都回来后一起落缓存
	var directEntry, proxyEntry ipEntry
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		directEntry = queryIPEntry(&http.Client{Timeout: 8 * time.Second})
	}()
	go func() {
		defer wg.Done()
		proxyEntry = d.queryViaProxy(s.MixedPort)
	}()
	wg.Wait()

	ipCache.mu.Lock()
	ipCache.at = time.Now()
	ipCache.direct = directEntry
	ipCache.proxy = proxyEntry
	ts := ipCache.at.Unix()
	ipCache.mu.Unlock()

	writeJSON(w, 200, map[string]any{"direct": directEntry, "proxy": proxyEntry, "ts": ts})
}

// queryViaProxy 经本机混合端口查询代理出口 IP（内核未运行时返回失败占位）。
func (d *deps) queryViaProxy(mixedPort int) ipEntry {
	if mixedPort <= 0 {
		return ipEntry{OK: false, Error: "内核未配置代理端口"}
	}
	proxyURL := &url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(mixedPort)}
	hc := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
	return queryIPEntry(hc)
}

// unlockSite 单个解锁测试站点（请求体）。
type unlockSite struct {
	Name string `json:"name"` // 展示名称
	URL  string `json:"url"`  // 测试地址
}

// unlockResult 单个站点测试结果。ok=true 表示经代理可正常访问（HTTP 2xx/3xx）；
// 403/404/451 等状态码原样返回 code，由前端标注为「不支持」（如 Netflix 区域封锁
// 对无版权影片返回 404）。
type unlockResult struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	OK   bool   `json:"ok"`   // 经代理访问是否成功（2xx/3xx）
	Code int    `json:"code"` // HTTP 状态码，传输层失败为 0
	MS   int64  `json:"ms"`   // 耗时（毫秒）
	Err  string `json:"err"`  // 失败原因
}

// handleUnlockTest POST /api/unlock/test：并发经代理端口测试站点可达性。
// 请求体 {"sites":[{"name":"Netflix","url":"https://..."}]}，最多 50 个；
// 返回 {"results":[unlockResult]}。测试全程强制走代理（解锁语义 = 代理出口可访问）。
func (d *deps) handleUnlockTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Sites []unlockSite `json:"sites"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil {
		writeErr(w, 400, fmt.Errorf("请求体解析失败: %w", err))
		return
	}
	if len(body.Sites) == 0 {
		writeErr(w, 400, fmt.Errorf("没有可测试的站点"))
		return
	}
	if len(body.Sites) > 50 {
		body.Sites = body.Sites[:50]
	}

	s, err := d.cfg.Get()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	proxyURL := &url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(s.MixedPort)}
	hc := &http.Client{
		Timeout: 12 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyURL(proxyURL),
			TLSHandshakeTimeout:   8 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		},
	}

	results := make([]unlockResult, len(body.Sites))
	sem := make(chan struct{}, 6) // 并发上限，防止一次全测把内核连接打满
	var wg sync.WaitGroup
	for i, site := range body.Sites {
		wg.Add(1)
		go func(i int, site unlockSite) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = testSite(r.Context(), hc, site)
		}(i, site)
	}
	wg.Wait()

	writeJSON(w, 200, map[string]any{"results": results})
}

// testSite 经代理请求单个站点：状态码 2xx/3xx 视为解锁，4xx/5xx 原样带回，
// 传输层失败（超时/重置）code=0、err 带原因。响应体不读取（只判状态码，上限 4KB 防连接悬挂）。
func testSite(ctx context.Context, hc *http.Client, site unlockSite) unlockResult {
	res := unlockResult{Name: site.Name, URL: site.URL}
	if site.URL == "" {
		res.Err = "地址为空"
		return res
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", site.URL, nil)
	if err != nil {
		res.Err = "地址无效"
		return res
	}
	// 浏览器 UA：部分站点（如 Cloudflare 防护页）对非浏览器 UA 直接 403，会误报不支持
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	resp, err := hc.Do(req)
	res.MS = time.Since(start).Milliseconds()
	if err != nil {
		res.Err = "连接失败：" + err.Error()
		return res
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 4<<10)
	res.Code = resp.StatusCode
	res.OK = resp.StatusCode >= 200 && resp.StatusCode < 400
	if !res.OK {
		res.Err = "HTTP " + strconv.Itoa(resp.StatusCode)
	}
	return res
}
