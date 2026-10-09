// Package subconv 把常见的节点分享链接转换为 mihomo 的 proxies 配置项。
//
// 支持：ss / ssr / vmess / vless / trojan / hysteria2(hy2) / tuic / anytls，
// 以及整段 base64 编码的订阅内容（机场常见的纯 base64 返回）。每行一条链接，
// 单行解析失败不影响其余节点——失败原因（带行号）随返回值交给调用方提示。
//
// 输出结构体字段顺序即 YAML 输出顺序（name/type/server/port 在前），用
// yaml.v3 按 2 空格缩进逐节点编码后，由 templates.Render 拼进模板的
// proxies 占位段。
package subconv

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Node 是一条转换后的 mihomo 代理节点。字段顺序决定 YAML 键顺序：
// 通用字段在前，各协议专属字段按协议排列（omitempty 省略未用到的）。
type Node struct {
	Name   string `yaml:"name"`
	Type   string `yaml:"type"`
	Server string `yaml:"server"`
	Port   int    `yaml:"port"`
	UDP    bool   `yaml:"udp,omitempty"`

	// ss / ssr
	Cipher        string     `yaml:"cipher,omitempty"`
	Password      string     `yaml:"password,omitempty"`
	Protocol      string     `yaml:"protocol,omitempty"`
	ProtocolParam string     `yaml:"protocol-param,omitempty"`
	Obfs          string     `yaml:"obfs,omitempty"`
	ObfsParam     string     `yaml:"obfs-param,omitempty"`
	Plugin        string     `yaml:"plugin,omitempty"`
	PluginOpts    *PluginOpt `yaml:"plugin-opts,omitempty"`

	// vmess / vless
	UUID              string      `yaml:"uuid,omitempty"`
	AlterID           int         `yaml:"alterId,omitempty"`
	Network           string      `yaml:"network,omitempty"`
	TLS               bool        `yaml:"tls,omitempty"`
	ServerName        string      `yaml:"servername,omitempty"`
	Flow              string      `yaml:"flow,omitempty"`
	ClientFingerprint string      `yaml:"client-fingerprint,omitempty"`
	RealityOpts       *RealityOpt `yaml:"reality-opts,omitempty"`

	// 传输层选项（vless/vmess/trojan 的 ws / grpc / h2）
	WSOpts   *WSOpts   `yaml:"ws-opts,omitempty"`
	H2Opts   *H2Opts   `yaml:"h2-opts,omitempty"`
	GrpcOpts *GrpcOpts `yaml:"grpc-opts,omitempty"`

	// trojan / hysteria2 / tuic
	SNI            string   `yaml:"sni,omitempty"`
	SkipCertVerify bool     `yaml:"skip-cert-verify,omitempty"`
	ALPN           []string `yaml:"alpn,omitempty"`
	Up             string   `yaml:"up,omitempty"`
	Down           string   `yaml:"down,omitempty"`
	ObfsPassword   string   `yaml:"obfs-password,omitempty"`
	// tuic
	CongestionController string `yaml:"congestion-controller,omitempty"`
	UDPRelayMode         string `yaml:"udp-relay-mode,omitempty"`
	ReduceRTT            bool   `yaml:"reduce-rtt,omitempty"`
}

// PluginOpt 覆盖 ss 的 obfs 与 v2ray-plugin 两种插件参数。
type PluginOpt struct {
	Mode string `yaml:"mode,omitempty"`
	Host string `yaml:"host,omitempty"`
	Path string `yaml:"path,omitempty"`
	TLS  bool   `yaml:"tls,omitempty"`
}

type RealityOpt struct {
	PublicKey string `yaml:"public-key,omitempty"`
	ShortID   string `yaml:"short-id,omitempty"`
}

type WSOpts struct {
	Path    string            `yaml:"path,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

type H2Opts struct {
	Host []string `yaml:"host,omitempty"`
	Path string   `yaml:"path,omitempty"`
}

type GrpcOpts struct {
	ServiceName string `yaml:"grpc-service-name,omitempty"`
}

// Fail 是一条解析失败的记录（行号从 1 起）。
type Fail struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// ParseMustErr 把失败明细聚合成一条用户可读错误（空切片返回 nil）。
func ParseMustErr(failed []Fail) error {
	if len(failed) == 0 {
		return errors.New("没有可识别的节点")
	}
	var reasons []string
	for i, f := range failed {
		if i == 3 {
			break
		}
		reasons = append(reasons, fmt.Sprintf("第%d行 %s", f.Line, f.Reason))
	}
	msg := "没有可识别的节点：" + strings.Join(reasons, "；")
	if len(failed) > 3 {
		msg += fmt.Sprintf("等共 %d 行", len(failed))
	}
	return errors.New(msg)
}

// 支持的分享链接协议前缀。
const schemes = "ss ssr vmess vless trojan hysteria2 hy2 tuic anytls"

// Parse 解析多行节点文本，返回转换成功的节点与逐行失败原因。
// 输入里没有任何链接前缀时，先按整段 base64 订阅内容解码再逐行解析。
func Parse(input string) (nodes []Node, failed []Fail) {
	lines := splitLines(input)
	if !anyKnownScheme(lines) {
		if dec, err := b64Decode(input); err == nil && strings.Contains(string(dec), "://") {
			lines = splitLines(string(dec))
		}
	}
	for i, line := range lines {
		n, err := parseLine(line)
		if err != nil {
			if !IsSkipLine(err) { // 空行/注释行静默跳过
				failed = append(failed, Fail{Line: i + 1, Reason: err.Error()})
			}
			continue
		}
		nodes = append(nodes, n)
	}
	dedupeNames(nodes)
	return nodes, failed
}

func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

func anyKnownScheme(lines []string) bool {
	for _, l := range lines {
		for _, s := range strings.Fields(schemes) {
			if strings.HasPrefix(strings.TrimSpace(l), s+"://") {
				return true
			}
		}
	}
	return false
}

func parseLine(line string) (Node, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
		return Node{}, errSkip // 空行/注释行静默跳过
	}
	switch {
	case strings.HasPrefix(line, "ss://"):
		return parseSS(line[len("ss://"):])
	case strings.HasPrefix(line, "ssr://"):
		return parseSSR(line[len("ssr://"):])
	case strings.HasPrefix(line, "vmess://"):
		return parseVMess(line[len("vmess://"):])
	case strings.HasPrefix(line, "vless://"):
		return parseVLESS(line[len("vless://"):])
	case strings.HasPrefix(line, "trojan://"):
		return parseTrojan(line[len("trojan://"):])
	case strings.HasPrefix(line, "hysteria2://"):
		return parseHy2(line[len("hysteria2://"):])
	case strings.HasPrefix(line, "hy2://"):
		return parseHy2(line[len("hy2://"):])
	case strings.HasPrefix(line, "hysteria://"):
		return Node{}, errors.New("暂不支持 hysteria(v1) 链接，请改用 hysteria2:// 或经订阅链接添加")
	case strings.HasPrefix(line, "tuic://"):
		return parseTUIC(line[len("tuic://"):])
	case strings.HasPrefix(line, "anytls://"):
		return parseAnyTLS(line[len("anytls://"):])
	default:
		return Node{}, fmt.Errorf("无法识别的链接（支持：%s）", schemes)
	}
}

// errSkip 标记「无需报错」的跳过行。
var errSkip = errors.New("skip")

// IsSkipLine 报告失败记录是否只是空行/注释等应静默跳过的行。
func IsSkipLine(err error) bool { return errors.Is(err, errSkip) }

// ---- ss ----
// 两种格式：SIP002（ss://base64(method:pass)@host:port#name）与
// 旧版整段 base64（ss://base64(method:pass@host:port)#name）。
func parseSS(s string) (Node, error) {
	main, name := splitFragment(s)
	var query url.Values
	if i := strings.IndexByte(main, '?'); i >= 0 {
		var err error
		if query, err = url.ParseQuery(main[i+1:]); err != nil {
			return Node{}, errors.New("参数解析失败")
		}
		main = strings.TrimSuffix(main[:i], "/")
	}
	var method, pass, host string
	var port int
	var err error
	if strings.Contains(main, "@") {
		at := strings.LastIndexByte(main, '@')
		userinfo, hostport := main[:at], main[at+1:]
		if strings.Contains(userinfo, ":") { // 明文 method:pass（可能被 URL 编码）
			if unesc, uerr := url.QueryUnescape(userinfo); uerr == nil {
				userinfo = unesc
			}
			method, pass, err = splitColon(userinfo)
			if err != nil {
				return Node{}, err
			}
		} else {
			dec, derr := b64Decode(userinfo)
			if derr != nil {
				return Node{}, errors.New("用户信息不是合法的 base64")
			}
			if method, pass, err = splitColon(string(dec)); err != nil {
				return Node{}, err
			}
		}
		if host, port, err = splitHostPort(hostport); err != nil {
			return Node{}, err
		}
	} else {
		dec, derr := b64Decode(main)
		if derr != nil {
			return Node{}, errors.New("不是合法的 ss 链接")
		}
		full := string(dec)
		at := strings.LastIndexByte(full, '@')
		if at < 0 {
			return Node{}, errors.New("不是合法的 ss 链接")
		}
		if method, pass, err = splitColon(full[:at]); err != nil {
			return Node{}, err
		}
		if host, port, err = splitHostPort(full[at+1:]); err != nil {
			return Node{}, err
		}
	}
	n := Node{Name: name, Type: "ss", Server: host, Port: port, Cipher: method, Password: pass, UDP: true}
	applySSPlugin(&n, query.Get("plugin"))
	return n, nil
}

// applySSPlugin 解析 SIP002 的 plugin 参数（百分号编码，分号分隔）。
// obfs-local/simple-obfs → plugin obfs；v2ray-plugin → plugin v2ray-plugin。
func applySSPlugin(n *Node, plugin string) {
	if plugin == "" {
		return
	}
	if unesc, err := url.QueryUnescape(plugin); err == nil {
		plugin = unesc
	}
	parts := strings.Split(plugin, ";")
	name := parts[0]
	kv := map[string]string{}
	for _, p := range parts[1:] {
		if k, v, ok := strings.Cut(p, "="); ok {
			kv[k] = v
		}
	}
	switch name {
	case "obfs", "simple-obfs", "obfs-local":
		mode := kv["obfs"]
		if mode == "" {
			mode = "http"
		}
		n.Plugin = "obfs"
		n.PluginOpts = &PluginOpt{Mode: mode, Host: kv["obfs-host"]}
	case "v2ray-plugin":
		n.Plugin = "v2ray-plugin"
		mode := kv["mode"]
		if mode == "" {
			mode = "websocket"
		}
		n.PluginOpts = &PluginOpt{Mode: mode, Host: kv["host"], Path: kv["path"], TLS: kv["tls"] == "true"}
	}
}

// ---- ssr ----
// ssr://base64( host:port:protocol:method:obfs:base64(pass) /?obfsparam=..&protoparam=..&remarks=.. )
func parseSSR(s string) (Node, error) {
	dec, err := b64Decode(s)
	if err != nil {
		return Node{}, errors.New("不是合法的 ssr 链接")
	}
	main := string(dec)
	query := url.Values{}
	if i := strings.IndexByte(main, '?'); i >= 0 { // 分隔符规范是 "/?"，部分实现只给 "?"；main 是 base64 不含 '?'，取第一个即安全
		if query, err = url.ParseQuery(strings.TrimLeft(main[i:], "?/")); err != nil {
			return Node{}, errors.New("ssr 参数解析失败")
		}
		main = main[:i]
	}
	parts := strings.Split(main, ":")
	if len(parts) < 6 {
		return Node{}, errors.New("不是合法的 ssr 链接")
	}
	// IPv6 主机自带冒号：固定取最后 5 段为参数，剩余全部并回主机
	n6 := len(parts)
	host := strings.Join(parts[:n6-5], ":")
	port, err := strconv.Atoi(parts[n6-5])
	if err != nil {
		return Node{}, errors.New("ssr 端口无效")
	}
	passB64, err := b64Decode(parts[n6-1])
	if err != nil {
		return Node{}, errors.New("ssr 密码不是合法的 base64")
	}
	n := Node{
		Name: b64Str(query.Get("remarks")), Type: "ssr", Server: host, Port: port,
		Cipher: parts[n6-3], Password: string(passB64), UDP: true,
		Protocol: orDefault(parts[n6-4], "origin"),
		Obfs:     orDefault(parts[n6-2], "plain"),
	}
	n.ProtocolParam = b64Str(query.Get("protoparam"))
	n.ObfsParam = b64Str(query.Get("obfsparam"))
	return n, nil
}

// ---- vmess ----
// vmess://base64(JSON)，字段见 v2rayN 分享格式；个别机场不编码直接给 JSON。
func parseVMess(s string) (Node, error) {
	var info struct {
		Ps   string `json:"ps"`
		Add  string `json:"add"`
		Port any    `json:"port"`
		ID   string `json:"id"`
		Aid  any    `json:"aid"`
		Scy  string `json:"scy"`
		Net  string `json:"net"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
		SNI  string `json:"sni"`
		ALPN string `json:"alpn"`
		FP   string `json:"fp"`
	}
	raw := []byte(s)
	if !json.Valid(raw) {
		dec, err := b64Decode(s)
		if err != nil {
			return Node{}, errors.New("不是合法的 vmess 链接")
		}
		raw = dec
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		return Node{}, errors.New("vmess 内容不是合法的分享 JSON")
	}
	port, err := portOf(info.Port)
	if err != nil {
		return Node{}, err
	}
	if info.Add == "" || info.ID == "" {
		return Node{}, errors.New("vmess 缺少服务器或用户 ID")
	}
	n := Node{
		Name: info.Ps, Type: "vmess", Server: info.Add, Port: port,
		UUID: info.ID, Cipher: orDefault(info.Scy, "auto"), UDP: true,
		TLS: info.TLS == "tls", ServerName: info.SNI,
	}
	if v, ok := info.Aid.(string); ok {
		n.AlterID, _ = strconv.Atoi(strings.TrimSpace(v))
	} else if v, ok := info.Aid.(float64); ok {
		n.AlterID = int(v)
	}
	applyTransport(&n, info.Net, info.Host, info.Path)
	if info.ALPN != "" {
		n.ALPN = splitCSV(info.ALPN)
	}
	n.ClientFingerprint = info.FP
	return n, nil
}

// ---- vless / trojan / hysteria2 / tuic / anytls：URI 形态，共用解析 ----

func parseVLESS(s string) (Node, error) {
	u, err := url.Parse("vless://" + s)
	if err != nil {
		return Node{}, errors.New("不是合法的 vless 链接")
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Node{}, err
	}
	q := u.Query()
	if enc := q.Get("encryption"); enc != "" && enc != "none" {
		return Node{}, errors.New("vless encryption 仅支持 none")
	}
	security := q.Get("security")
	n := Node{
		Name: u.Fragment, Type: "vless", Server: host, Port: port,
		UUID: u.User.Username(), UDP: true,
		Network: normNetwork(q.Get("type")),
		TLS:     security == "tls" || security == "reality" || security == "xtls",
		Flow:    q.Get("flow"),
	}
	if sni := q.Get("sni"); sni != "" {
		n.ServerName = sni
	}
	if security == "reality" {
		n.RealityOpts = &RealityOpt{PublicKey: q.Get("pbk"), ShortID: q.Get("sid")}
		if n.ClientFingerprint == "" {
			n.ClientFingerprint = "chrome" // mihomo Reality 必须带 utls 指纹
		}
	}
	n.ClientFingerprint = orDefault(q.Get("fp"), n.ClientFingerprint)
	applyTransport(&n, n.Network, q.Get("host"), q.Get("path"))
	if a := q.Get("alpn"); a != "" {
		n.ALPN = splitCSV(a)
	}
	n.SkipCertVerify = truthy(q, "allowInsecure") || truthy(q, "insecure")
	return n, nil
}

func parseTrojan(s string) (Node, error) {
	u, err := url.Parse("trojan://" + s)
	if err != nil {
		return Node{}, errors.New("不是合法的 trojan 链接")
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Node{}, err
	}
	q := u.Query()
	pass := u.User.Username()
	if pw, ok := u.User.Password(); ok && pw != "" {
		pass += ":" + pw
	}
	if pass == "" {
		return Node{}, errors.New("trojan 缺少密码")
	}
	n := Node{
		Name: u.Fragment, Type: "trojan", Server: host, Port: port,
		Password: pass, UDP: true,
		SNI:               orDefault(q.Get("sni"), q.Get("peer")),
		Network:           normNetwork(q.Get("type")),
		ClientFingerprint: q.Get("fp"),
	}
	applyTransport(&n, n.Network, q.Get("host"), q.Get("path"))
	if a := q.Get("alpn"); a != "" {
		n.ALPN = splitCSV(a)
	}
	n.SkipCertVerify = truthy(q, "allowInsecure") || truthy(q, "insecure")
	return n, nil
}

func parseHy2(s string) (Node, error) {
	u, err := url.Parse("hysteria2://" + s)
	if err != nil {
		return Node{}, errors.New("不是合法的 hysteria2 链接")
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Node{}, err
	}
	q := u.Query()
	auth := u.User.Username()
	if pw, ok := u.User.Password(); ok && pw != "" {
		auth += ":" + pw
	}
	if auth == "" {
		return Node{}, errors.New("hysteria2 缺少认证密码")
	}
	n := Node{
		Name: u.Fragment, Type: "hysteria2", Server: host, Port: port,
		Password: auth, UDP: true,
		SNI: q.Get("sni"), Up: q.Get("up"), Down: q.Get("down"),
	}
	if obfs := q.Get("obfs"); obfs != "" {
		n.Obfs = obfs
		n.ObfsPassword = q.Get("obfs-password")
	}
	if a := q.Get("alpn"); a != "" {
		n.ALPN = splitCSV(a)
	}
	n.SkipCertVerify = truthy(q, "allowInsecure") || truthy(q, "insecure")
	return n, nil
}

func parseTUIC(s string) (Node, error) {
	u, err := url.Parse("tuic://" + s)
	if err != nil {
		return Node{}, errors.New("不是合法的 tuic 链接")
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Node{}, err
	}
	q := u.Query()
	pass, _ := u.User.Password()
	n := Node{
		Name: u.Fragment, Type: "tuic", Server: host, Port: port,
		UUID: u.User.Username(), Password: pass, UDP: true,
		SNI: q.Get("sni"),
	}
	if n.UUID == "" {
		return Node{}, errors.New("tuic 缺少 UUID")
	}
	n.CongestionController = q.Get("congestion_control")
	n.UDPRelayMode = q.Get("udp_relay_mode")
	n.ReduceRTT = truthy(q, "reduce_rtt")
	if a := q.Get("alpn"); a != "" {
		n.ALPN = splitCSV(a)
	}
	n.SkipCertVerify = truthy(q, "allow_insecure") || truthy(q, "allowInsecure") || truthy(q, "insecure")
	return n, nil
}

func parseAnyTLS(s string) (Node, error) {
	u, err := url.Parse("anytls://" + s)
	if err != nil {
		return Node{}, errors.New("不是合法的 anytls 链接")
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Node{}, err
	}
	q := u.Query()
	pass := u.User.Username()
	if pw, ok := u.User.Password(); ok && pw != "" {
		pass += ":" + pw
	}
	if pass == "" {
		return Node{}, errors.New("anytls 缺少密码")
	}
	n := Node{
		Name: u.Fragment, Type: "anytls", Server: host, Port: port,
		Password: pass, UDP: true,
		SNI:               q.Get("sni"),
		ClientFingerprint: q.Get("fp"),
	}
	n.SkipCertVerify = truthy(q, "allowInsecure") || truthy(q, "insecure")
	return n, nil
}

// ---- 通用小工具 ----

// applyTransport 按 network 类型填充 ws / h2 / grpc 传输选项；tcp 留空。
func applyTransport(n *Node, network, host, path string) {
	switch network {
	case "ws", "websocket":
		n.Network = "ws"
		w := &WSOpts{Path: orDefault(path, "/")}
		if host != "" {
			w.Headers = map[string]string{"Host": host}
		}
		n.WSOpts = w
	case "h2", "http", "h2c":
		n.Network = "h2"
		n.H2Opts = &H2Opts{Host: splitCSV(host), Path: orDefault(path, "/")}
	case "grpc", "gun":
		n.Network = "grpc"
		n.GrpcOpts = &GrpcOpts{ServiceName: orDefault(path, "")}
	default:
		n.Network = ""
	}
}

func splitFragment(s string) (main, name string) {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		name, _ = url.QueryUnescape(s[i+1:])
		s = s[:i]
	}
	return s, name
}

func splitColon(s string) (a, b string, err error) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return "", "", errors.New("缺少冒号分隔的用户信息")
	}
	return s[:i], s[i+1:], nil
}

func splitHostPort(s string) (string, int, error) {
	s = strings.TrimSuffix(s, "/")
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, fmt.Errorf("地址缺少端口或格式无效（%s）", s)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("端口无效（%s）", portStr)
	}
	if host == "" {
		return "", 0, errors.New("服务器地址为空")
	}
	return host, port, nil
}

func hostPort(u *url.URL) (string, int, error) {
	return splitHostPort(u.Host)
}

func portOf(v any) (int, error) {
	switch t := v.(type) {
	case string:
		p, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil || p < 1 || p > 65535 {
			return 0, fmt.Errorf("端口无效（%s）", t)
		}
		return p, nil
	case float64:
		p := int(t)
		if p < 1 || p > 65535 {
			return 0, errors.New("端口无效")
		}
		return p, nil
	}
	return 0, errors.New("端口无效")
}

// normNetwork 归一传输层名：websocket→ws、http→h2（v2rayN 用 http 表示 h2）、gun→grpc、tcp 留空。
func normNetwork(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "ws", "websocket":
		return "ws"
	case "h2", "http", "h2c":
		return "h2"
	case "grpc", "gun":
		return "grpc"
	}
	return ""
}

func b64Decode(s string) ([]byte, error) {
	s = strings.Join(strings.Fields(s), "")
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	return base64.StdEncoding.DecodeString(s)
}

// b64Str 解码 SSR 参数里的 base64 字段，失败返回原值（部分实现不编码）。
func b64Str(s string) string {
	if s == "" {
		return ""
	}
	if dec, err := b64Decode(s); err == nil && looksText(dec) {
		return string(dec)
	}
	return s
}

func looksText(b []byte) bool {
	for _, c := range b {
		if c < 0x09 || (c > 0x0d && c < 0x20) {
			return false
		}
	}
	return true
}

func truthy(q url.Values, key string) bool {
	switch strings.ToLower(q.Get(key)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// dedupeNames 保证节点名唯一（mihomo 要求）：重名追加序号，空名兜底「节点-N」。
func dedupeNames(nodes []Node) {
	seen := map[string]int{}
	for i := range nodes {
		if strings.TrimSpace(nodes[i].Name) == "" {
			nodes[i].Name = fmt.Sprintf("节点%d", i+1)
		}
		if first, dup := seen[nodes[i].Name]; dup {
			for {
				seen[nodes[i].Name]++
				cand := fmt.Sprintf("%s-%d", nodes[first].Name, seen[nodes[i].Name])
				if _, again := seen[cand]; !again {
					nodes[i].Name = cand
					break
				}
			}
		}
		seen[nodes[i].Name]++
	}
}

// MarshalNode 把单个节点按 2 空格缩进编码为 YAML 文本（多行，无列表横杠）。
func MarshalNode(n Node) (string, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}
