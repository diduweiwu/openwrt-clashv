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

// ipGeo 单个查询源的完整地理信息：字段缺失留空，由解析函数尽力填充。
type ipGeo struct {
	IP          string // 出口 IP
	Country     string // 国家/地区（ip-api 为中文名，ip.sb 为英文国名）
	CountryCode string // 国家/地区代码（前端转国旗 emoji）
	City        string // 城市
	ISP         string // 运营商
	Org         string // 所属组织
	ASN         string // 自治域号，形如 AS9808
	ASNOrg      string // 自治域组织
	Continent   string // 大洲（已转中文）
	Timezone    string // 时区，如 Asia/Shanghai
	Lat         string // 纬度
	Lon         string // 经度
}

// ipSource 是一个出口 IP 查询源：URL + 从响应里摘字段的方法。
// 逐源兜底：海外源（ip.sb）可能被墙，直连场景自动落到国内可达的 ip-api.com。
type ipSource struct {
	name  string
	url   string
	parse func(data []byte) (*ipGeo, error)
}

// continentCN 大洲代码 → 中文名（详情弹窗展示用）
var continentCN = map[string]string{
	"AS": "亚洲", "EU": "欧洲", "NA": "北美洲", "SA": "南美洲",
	"AF": "非洲", "OC": "大洋洲", "AN": "南极洲",
}

// parseGeoip 解析 ip.sb /geoip 的 JSON 响应（免费版自带 ASN/组织/时区/坐标全套字段）。
func parseGeoip(b []byte) (*ipGeo, error) {
	var v struct {
		IP              string  `json:"ip"`
		Country         string  `json:"country"`
		CountryCode     string  `json:"country_code"`
		City            string  `json:"city"`
		ISP             string  `json:"isp"`
		Organization    string  `json:"organization"`
		ASN             int     `json:"asn"`
		ASNOrganization string  `json:"asn_organization"`
		ContinentCode   string  `json:"continent_code"`
		Timezone        string  `json:"timezone"`
		Latitude        float64 `json:"latitude"`
		Longitude       float64 `json:"longitude"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if v.IP == "" {
		return nil, fmt.Errorf("响应缺少 ip 字段")
	}
	g := &ipGeo{
		IP: v.IP, Country: v.Country, CountryCode: v.CountryCode, City: v.City,
		ISP: v.ISP, Org: v.Organization, ASNOrg: v.ASNOrganization,
		Timezone: v.Timezone, Continent: continentCN[v.ContinentCode],
	}
	if v.ASN > 0 {
		g.ASN = "AS" + strconv.Itoa(v.ASN)
	}
	if v.Latitude != 0 || v.Longitude != 0 {
		g.Lat = strconv.FormatFloat(v.Latitude, 'f', -1, 64)
		g.Lon = strconv.FormatFloat(v.Longitude, 'f', -1, 64)
	}
	return g, nil
}

// parseIPApi 解析 ip-api.com 的 JSON 响应（免费版 http，as 字段形如「AS9808 中国移动」需拆分）。
func parseIPApi(b []byte) (*ipGeo, error) {
	var v struct {
		Status      string  `json:"status"`
		Query       string  `json:"query"`
		Country     string  `json:"country"`
		CountryCode string  `json:"countryCode"`
		City        string  `json:"city"`
		ISP         string  `json:"isp"`
		Org         string  `json:"org"`
		AS          string  `json:"as"`
		Continent   string  `json:"continent"`
		Timezone    string  `json:"timezone"`
		Lat         float64 `json:"lat"`
		Lon         float64 `json:"lon"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if v.Status != "success" || v.Query == "" {
		return nil, fmt.Errorf("查询失败")
	}
	g := &ipGeo{
		IP: v.Query, Country: v.Country, CountryCode: v.CountryCode, City: v.City,
		ISP: v.ISP, Org: v.Org, Timezone: v.Timezone, Continent: v.Continent,
	}
	// as 形如「AS9808 中国移动…」：首个空格前是自治域号，其余并入自治域组织
	if fields := strings.SplitN(strings.TrimSpace(v.AS), " ", 2); len(fields) > 0 && fields[0] != "" {
		g.ASN = fields[0]
		if len(fields) > 1 {
			g.ASNOrg = fields[1]
		}
	}
	if v.Lat != 0 || v.Lon != 0 {
		g.Lat = strconv.FormatFloat(v.Lat, 'f', -1, 64)
		g.Lon = strconv.FormatFloat(v.Lon, 'f', -1, 64)
	}
	return g, nil
}

// parsePlain 解析纯文本 IP 端点（api.ip.sb/ip、api-ipv6.ip.sb/ip、api.ipify.org）：
// 响应体就是 IP 本身，不带地理信息（其余字段留空）。
func parsePlain(b []byte) (*ipGeo, error) {
	ip := strings.TrimSpace(string(b))
	if net.ParseIP(ip) == nil {
		return nil, fmt.Errorf("响应不是合法 IP")
	}
	return &ipGeo{IP: ip}, nil
}

// parseIP38 解析 ip38.cn 的 get_ip_info.php 响应：{"ip":"…","region":"中国 四川 成都 移动"}。
// region 为空格分隔的归属描述：首段国家、末段运营商（≥3 段时）、中间各段合并为
// 省/市描述；段数不足时尽力填充。仅能识别「中国」的国家代码（国旗 emoji 用），
// 其余字段（ASN/经纬度等）该源不提供，留空由弹窗按空值显示。
func parseIP38(b []byte) (*ipGeo, error) {
	var v struct {
		IP     string `json:"ip"`
		Region string `json:"region"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if net.ParseIP(strings.TrimSpace(v.IP)) == nil {
		return nil, fmt.Errorf("响应缺少合法 ip 字段")
	}
	g := &ipGeo{IP: strings.TrimSpace(v.IP)}
	parts := strings.Fields(v.Region)
	if len(parts) > 0 {
		g.Country = parts[0]
		if g.Country == "中国" {
			g.CountryCode = "CN"
		}
	}
	if len(parts) >= 3 {
		g.ISP = parts[len(parts)-1]
		g.City = strings.Join(parts[1:len(parts)-1], " ")
	} else if len(parts) == 2 {
		g.City = parts[1]
	}
	return g, nil
}

// ipSources 代理出口查询源列表（经混合端口、强制走代理），按序尝试直到成功。
var ipSources = []ipSource{
	{name: "ip.sb", url: "https://api.ip.sb/geoip", parse: parseGeoip},
	{name: "ip.sb/ip", url: "https://api.ip.sb/ip", parse: parsePlain},
	{name: "ipify", url: "https://api.ipify.org", parse: parsePlain},
	{
		name:  "ip-api.com",
		url:   "http://ip-api.com/json/?lang=zh-CN&fields=status,query,country,countryCode,city,isp,org,as,timezone,lat,lon,continent",
		parse: parseIPApi,
	},
}

// directSources 直连出口查询源：国内直连可达的端点，ip38.cn 优先（一次同时
// 返回 IP 与归属地；边缘节点拦截非浏览器 UA，queryOneSource 统一带 browserUA
// 已规避，实测无 UA 请求 403）。国外源在 TUN/透明代理接管本机流量时会被内核
// 按规则分流走代理，查到的不是直连出口；国内源在任何分流规则下都命中 DIRECT，
// 拿到的才是真实直连出口。ip38 失败时落到纯文本源 + geoLookup 反查的老两步。
var directSources = []ipSource{
	{name: "ip38.cn", url: "https://api.ip38.com/get_ip_info.php", parse: parseIP38},
	{name: "nipw.cn", url: "https://4.wsmdn.top", parse: parsePlain},
	{name: "3322", url: "http://ip.3322.net", parse: parsePlain},
}

// geoLookup 反查指定 IP 的地理信息（归属只取决于参数 IP，与查询自身走哪条链路
// 无关）：ip.sb geoip/{ip} 优先（字段全），ip-api.com/{ip} 兜底（免费版 http）。
// 全部失败返回 nil，调用方保留仅含 IP 的结果。
func geoLookup(hc *http.Client, ip string) *ipGeo {
	sources := []ipSource{
		{name: "ip.sb", url: "https://api.ip.sb/geoip/" + ip, parse: parseGeoip},
		{
			name:  "ip-api.com",
			url:   "http://ip-api.com/json/" + ip + "?lang=zh-CN&fields=status,query,country,countryCode,city,isp,org,as,timezone,lat,lon,continent",
			parse: parseIPApi,
		},
	}
	for _, src := range sources {
		req, err := http.NewRequest("GET", src.url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", browserUA)
		resp, err := hc.Do(req)
		if err != nil {
			continue
		}
		data, err := func() ([]byte, error) {
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			return io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		}()
		if err != nil {
			continue
		}
		if g, err := src.parse(data); err == nil {
			return g
		}
	}
	return nil
}

// queryDirectEntry 查询真实直连出口。ip38.cn 源一次同时返回 IP 与中文归属地，
// 直接采用（不反查——geoLookup 走国外源 ip.sb，正是要替换掉的依赖）；纯文本源
// （nipw/3322）只回显裸 IP，才反查地理补全（失败仅缺地理字段，IP 仍有效）。
func queryDirectEntry() ipEntry {
	entry := queryIPEntry(&http.Client{Timeout: 8 * time.Second}, directSources...)
	if !entry.OK || entry.Country != "" {
		return entry
	}
	if g := geoLookup(&http.Client{Timeout: 8 * time.Second}, entry.IP); g != nil {
		return entryFromGeo(g, entry.Source)
	}
	return entry
}

// ipEntry 单个出口的查询结果（直连或代理各一份），扩展字段供详情弹窗展示。
type ipEntry struct {
	OK      bool   `json:"ok"`      // 是否至少有一个源查询成功
	IP      string `json:"ip"`      // 出口 IP
	Country string `json:"country"` // 国家/地区（国内源为中文，海外源为英文国名或代码）
	City    string `json:"city"`    // 城市
	ISP     string `json:"isp"`     // 运营商
	Source  string `json:"source"`  // 命中的查询源
	Error   string `json:"error"`   // 全部源都失败时的逐源错误汇总

	// 扩展地理字段（查询源不支持或纯文本源时留空，弹窗里空值行显示 —）
	CountryCode string `json:"countryCode,omitempty"` // 国家/地区代码（国旗 emoji）
	Org         string `json:"org,omitempty"`         // 所属组织
	ASN         string `json:"asn,omitempty"`         // 自治域号，形如 AS9808
	ASNOrg      string `json:"asnOrg,omitempty"`      // 自治域组织
	Continent   string `json:"continent,omitempty"`   // 大洲
	Timezone    string `json:"timezone,omitempty"`    // 时区
	Lat         string `json:"lat,omitempty"`         // 纬度
	Lon         string `json:"lon,omitempty"`         // 经度
}

// entryFromGeo 由查询源的解析结果构造成功态 ipEntry。
func entryFromGeo(g *ipGeo, source string) ipEntry {
	return ipEntry{
		OK: true, IP: g.IP, Country: g.Country, City: g.City, ISP: g.ISP, Source: source,
		CountryCode: g.CountryCode, Org: g.Org, ASN: g.ASN, ASNOrg: g.ASNOrg,
		Continent: g.Continent, Timezone: g.Timezone, Lat: g.Lat, Lon: g.Lon,
	}
}

// ipInfoCache 缓存最近一次查询结果，60 秒内重复请求直接返回，
// 避免首页与详情弹窗反复打查询源；refresh 参数可强制绕过。
type ipInfoCache struct {
	mu      sync.Mutex
	at      time.Time
	direct  ipEntry
	proxy   ipEntry
	ipv6    ipEntry // 本机 IPv6 直连出网探测（与 direct 同为直连侧，仅测可达性）
	proxyOK bool    // 代理侧是否查到过（内核未运行时为 false，运行后应重查）
}

var ipCache ipInfoCache

// queryIPEntry 依次尝试查询源，返回第一个成功的结果；全部失败时把各源原因
// 逐个拼进 Error（如「ip.sb: HTTP 502；ip.sb/ip: …」），方便详情弹窗定位是哪个源挂了。
// sources 不传时用默认出口 IP 源（IPv6 探测等场景可传入自定义源列表）。
func queryIPEntry(hc *http.Client, sources ...ipSource) ipEntry {
	if len(sources) == 0 {
		sources = ipSources
	}
	var errs []string
	for _, src := range sources {
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
	g, err := src.parse(data)
	if err != nil {
		return ipEntry{}, err
	}
	return entryFromGeo(g, src.name), nil
}

// handleIPInfo GET /api/ipinfo：查询国内直连出口、代理出口与本机 IPv6 出网探测。
// 参数 refresh=1 时绕过 60 秒缓存强制重查。
// 返回 {direct: ipEntry, proxy: ipEntry, ipv6: ipEntry, ts: 采样时刻Unix秒}。
// 内核未运行时 proxy 侧连接会被拒绝，返回 ok=false 的占位结果；
// ipv6 为直连侧探测（请求纯 IPv6 端点，可达即本机支持 IPv6 出网）。
func (d *deps) handleIPInfo(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"

	ipCache.mu.Lock()
	cached := !refresh && time.Since(ipCache.at) < 60*time.Second
	if cached {
		out := map[string]any{
			"direct": ipCache.direct,
			"proxy":  ipCache.proxy,
			"ipv6":   ipCache.ipv6,
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
	// 直连、代理、IPv6 探测并发查询，都回来后一起落缓存
	var directEntry, proxyEntry, ipv6Entry ipEntry
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		directEntry = queryDirectEntry()
	}()
	go func() {
		defer wg.Done()
		proxyEntry = d.queryViaProxy(s.MixedPort)
	}()
	go func() {
		defer wg.Done()
		ipv6Entry = queryIPEntry(&http.Client{Timeout: 8 * time.Second}, ipv6Sources...)
	}()
	wg.Wait()

	ipCache.mu.Lock()
	ipCache.at = time.Now()
	ipCache.direct = directEntry
	ipCache.proxy = proxyEntry
	ipCache.ipv6 = ipv6Entry
	ts := ipCache.at.Unix()
	ipCache.mu.Unlock()

	writeJSON(w, 200, map[string]any{"direct": directEntry, "proxy": proxyEntry, "ipv6": ipv6Entry, "ts": ts})
}

// handleIPGeoDirect GET /api/ipgeo/direct：只查国内直连出口（ip38.cn 一次返回
// IP 与归属地），供浏览器端检测模式的直连行调用——api.ip38.com 的 CORS 只
// 放行 www.ip38.com 自己，浏览器页面无法直接 fetch，经本后端转发即可；后端
// 直连国内站在任何分流规则下都走 DIRECT，与直连行语义一致。每次实时查询
// 不走缓存（前端已有 60 秒轻缓存兜频率）。
//
// 返回：ipEntry 结构；失败 502 {"error": "..."}。
func (d *deps) handleIPGeoDirect(w http.ResponseWriter, r *http.Request) {
	entry := queryDirectEntry()
	if !entry.OK {
		writeErr(w, 502, fmt.Errorf("%s", entry.Error))
		return
	}
	writeJSON(w, 200, entry)
}

// ipv6Sources IPv6 出网探测源：域名只有 AAAA 记录，本机无 IPv6 时连接直接失败；
// nipw.cn 的 6.wsmdn.top 为国内直连探测（TUN/分流下也走 DIRECT），海外源兜底
var ipv6Sources = []ipSource{
	{name: "nipw.cn", url: "https://6.wsmdn.top", parse: parsePlain},
	{name: "ipv6.ip.sb", url: "https://api-ipv6.ip.sb/ip", parse: parsePlain},
	{name: "ipv6.ipify", url: "https://api6.ipify.org", parse: parsePlain},
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
