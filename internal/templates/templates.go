// Package templates 管理「配置模板」：存放在 <workdir>/templates/<名字>.yaml。
//
// 模板用于「手动粘贴节点」方式添加订阅——模板提供 rules / proxy-groups 等
// 骨架，其中 proxies 占位段与 __ALL_PROXIES__ 占位符由转换后的节点替换
// （见 render.go）。内置模板（白名单/黑名单，界面名字旁有「内置」标记）在启动时播种，不可修改删除。
package templates

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"clashv/internal/config"
)

// DefaultName 手动节点添加未指定模板时的默认选择。
const DefaultName = "白名单"

// builtins 是内置模板（名字即文件名，内容逐字保留，用户模板不得占用其名字）。
var builtins = []struct{ Name, Content string }{
	{DefaultName, builtinWhitelistContent},
	{"黑名单", builtinBlacklistContent},
}

// IsBuiltin 报告名字是否为内置模板。
func IsBuiltin(name string) bool {
	for _, b := range builtins {
		if b.Name == name {
			return true
		}
	}
	return false
}

// ErrProtected 内置模板的保护错误。
var ErrProtected = errors.New("内置模板不可修改或删除")

// Template 是一条模板的元数据。
type Template struct {
	Name      string `json:"name"`
	Builtin   bool   `json:"builtin"`
	UpdatedAt int64  `json:"updated_at"`
	Size      int64  `json:"size"`
}

// Manager 管理模板文件。
type Manager struct {
	cfg *config.Manager
}

// New 创建模板管理器（轻量对象，可按需多处构造）。
func New(cfg *config.Manager) *Manager { return &Manager{cfg: cfg} }

func (m *Manager) dir() string             { return filepath.Join(m.cfg.Home(), "templates") }
func (m *Manager) path(name string) string { return filepath.Join(m.dir(), name+".yaml") }

// EnsureBuiltin 保证全部内置模板存在（文件被手动删掉也能自愈）。
func (m *Manager) EnsureBuiltin() error {
	if err := os.MkdirAll(m.dir(), 0o755); err != nil {
		return err
	}
	var firstErr error
	for _, b := range builtins {
		if _, err := os.Stat(m.path(b.Name)); err == nil {
			continue
		}
		if err := os.WriteFile(m.path(b.Name), []byte(b.Content), 0o644); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// List 返回全部模板：内置在前，其余按名字排序。
func (m *Manager) List() ([]Template, error) {
	_ = m.EnsureBuiltin()
	entries, err := os.ReadDir(m.dir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Template
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		st, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Template{
			Name:      name,
			Builtin:   IsBuiltin(name),
			UpdatedAt: st.ModTime().Unix(),
			Size:      st.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Builtin != out[j].Builtin {
			return out[i].Builtin
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Content 读取模板内容；内置模板缺失时先自愈再读。
func (m *Manager) Content(name string) ([]byte, error) {
	if IsBuiltin(name) {
		_ = m.EnsureBuiltin()
	}
	if !ValidName(name) {
		return nil, fmt.Errorf("非法的模板名")
	}
	data, err := os.ReadFile(m.path(name))
	if err != nil {
		return nil, fmt.Errorf("模板不存在: %s", name)
	}
	return data, nil
}

// Create 新建用户模板。
func (m *Manager) Create(name, content string) error {
	if err := ValidateNameNew(name); err != nil {
		return err
	}
	if err := validateContent(content); err != nil {
		return err
	}
	if _, err := os.Stat(m.path(name)); err == nil {
		return fmt.Errorf("模板「%s」已存在", name)
	}
	if err := os.MkdirAll(m.dir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(m.path(name), []byte(content), 0o644)
}

// Update 覆盖模板内容（内置模板拒绝）。
func (m *Manager) Update(name, content string) error {
	if IsBuiltin(name) {
		return ErrProtected
	}
	if !ValidName(name) {
		return fmt.Errorf("非法的模板名")
	}
	if err := validateContent(content); err != nil {
		return err
	}
	if _, err := os.Stat(m.path(name)); err != nil {
		return fmt.Errorf("模板不存在: %s", name)
	}
	tmp := m.path(name) + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.path(name))
}

// Delete 删除用户模板（内置模板拒绝）。
func (m *Manager) Delete(name string) error {
	if IsBuiltin(name) {
		return ErrProtected
	}
	if !ValidName(name) {
		return fmt.Errorf("非法的模板名")
	}
	if err := os.Remove(m.path(name)); err != nil {
		return fmt.Errorf("模板不存在: %s", name)
	}
	return nil
}

// Has 报告模板是否存在。
func (m *Manager) Has(name string) bool {
	_, err := os.Stat(m.path(name))
	return err == nil
}

// ModTime 返回模板文件修改时间（模板不存在时返回零值）。
func (m *Manager) ModTime(name string) time.Time {
	st, err := os.Stat(m.path(name))
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

// ValidName 报告名字是否可用于既有模板的读取/删除（宽松：只防路径穿越）。
func ValidName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	return !strings.ContainsAny(name, "./\\") && !strings.ContainsAny(name, "\x00")
}

// ValidateNameNew 校验新建模板名：中文/字母/数字/下划线/中划线，1~32 字符，
// 不含点号（防路径穿越），且不得占用内置模板名。
func ValidateNameNew(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("模板名不能为空")
	}
	if IsBuiltin(name) {
		return fmt.Errorf("「%s」是内置模板名，不能使用", name)
	}
	runes := []rune(name)
	if len(runes) > 32 {
		return errors.New("模板名过长（最多 32 字符）")
	}
	for _, r := range runes {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '-' || r == '_' || (r >= 0x4e00 && r <= 0x9fa5)
		if !ok {
			return errors.New("模板名只能包含中文、字母、数字、下划线或中划线")
		}
	}
	return nil
}

// validateContent 校验模板内容：非空、1MB 上限、顶层必须是 YAML 映射、
// 且带 proxies 占位段（节点注入位置）。
func validateContent(content string) error {
	if len(strings.TrimSpace(content)) == 0 {
		return errors.New("模板内容为空")
	}
	if len(content) > 1<<20 {
		return errors.New("模板内容超过 1MB 上限")
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return fmt.Errorf("不是合法的 YAML：%w", err)
	}
	if doc == nil {
		return errors.New("内容不是有效的配置结构（顶层须为键值映射）")
	}
	if _, _, state := findProxiesSection(content); state != proxiesPlaceholder {
		return errors.New(`模板缺少 proxies 占位段（如 "proxies: ~"），节点将无处注入`)
	}
	return nil
}

// builtinWhitelistContent 内置白名单模板（用户不可改，内容即最终真源，逐字保留）：
// 国外域名先经 geosite/proxy 规则分流，国内 IP/域名全直连，兜底 MATCH 走节点选择。
const builtinWhitelistContent = `mode: rule
mixed-port: 7897
allow-lan: false
log-level: warning
external-controller: ''
secret: set-your-secret
unified-delay: false
profile:
  store-selected: true
external-controller-unix: /tmp/verge/verge-mihomo.sock
geo-update-interval: 24
external-controller-cors:
  allow-private-network: true
  allow-origins:
  - tauri://localhost
  - http://tauri.localhost
  - https://yacd.metacubex.one
  - https://metacubex.github.io
  - https://board.zash.run.place
geodata-mode: true
dns:
  enable: true
  listen: :53

  # 开启 IPv6 DNS
  ipv6: true

  # 推荐 fake-ip，分流更准确
  enhanced-mode: fake-ip

  fake-ip-range: 198.18.0.1/16

  fake-ip-filter:
    - '*.lan'
    - '*.local'
    - '*.arpa'
    - 'time.*.com'
    - 'ntp.*.com'
    - '+.market.xiaomi.com'
    - 'localhost.ptlogin2.qq.com'
    - '*.msftncsi.com'
    - 'www.msftconnecttest.com'

  fake-ip-filter-mode: blacklist

  # DNS启动解析服务器
  # 不使用system，避免绕过Clash
  default-nameserver:
    - 223.5.5.5
    - 119.29.29.29

  # 普通域名解析
  # 国内优先
  nameserver:
    - https://dns.alidns.com/dns-query
    - https://doh.pub/dns-query

  # 国外域名 fallback
  fallback:
    - https://cloudflare-dns.com/dns-query
    - https://dns.google/dns-query

  fallback-filter:
    geoip: true
    geoip-code: CN

    # 中国IP不进入fallback
    ipcidr:
      - 240.0.0.0/4
      - 0.0.0.0/32

    # 这些域名强制走国外DNS
    domain:
      - '+.google.com'
      - '+.googleapis.com'
      - '+.youtube.com'
      - '+.facebook.com'
      - '+.twitter.com'
      - '+.x.com'

  # 代理节点域名解析
  proxy-server-nameserver:
    - https://dns.alidns.com/dns-query

  # DNS遵守rules
  respect-rules: true

  # 不使用hosts
  use-hosts: false
  use-system-hosts: false

  # 开启HTTP3 DoH
  prefer-h3: true
geox-url:
  geoip: https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/geoip/release/geoip.dat
  geosite: https://gh-proxy.org/https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat
  mmdb: https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/geoip/release/Country.mmdb
tun:
  enable: false
  stack: mixed
  auto-route: true
  strict-route: false
  auto-detect-interface: true
  dns-hijack:
  - any:53
  device: utun1024
  mtu: 1500

proxies: ~
proxy-groups:
  - name: 节点选择
    type: select
    proxies:
      - 自动选择
      - DIRECT
      - __ALL_PROXIES__

  - name: 自动选择
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    tolerance: 50
    proxies:
      - __ALL_PROXIES__

rule-providers:
  reject:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/reject.txt"
    path: ./ruleset/reject.yaml
    interval: 86400

  icloud:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/icloud.txt"
    path: ./ruleset/icloud.yaml
    interval: 86400

  apple:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/apple.txt"
    path: ./ruleset/apple.yaml
    interval: 86400

  google:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/google.txt"
    path: ./ruleset/google.yaml
    interval: 86400

  proxy:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/proxy.txt"
    path: ./ruleset/proxy.yaml
    interval: 86400

  direct:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/direct.txt"
    path: ./ruleset/direct.yaml
    interval: 86400

  private:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/private.txt"
    path: ./ruleset/private.yaml
    interval: 86400

  gfw:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/gfw.txt"
    path: ./ruleset/gfw.yaml
    interval: 86400

  tld-not-cn:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/tld-not-cn.txt"
    path: ./ruleset/tld-not-cn.yaml
    interval: 86400

  telegramcidr:
    type: http
    behavior: ipcidr
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/telegramcidr.txt"
    path: ./ruleset/telegramcidr.yaml
    interval: 86400

  cncidr:
    type: http
    behavior: ipcidr
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/cncidr.txt"
    path: ./ruleset/cncidr.yaml
    interval: 86400

  lancidr:
    type: http
    behavior: ipcidr
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/lancidr.txt"
    path: ./ruleset/lancidr.yaml
    interval: 86400

  applications:
    type: http
    behavior: classical
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/applications.txt"
    path: ./ruleset/applications.yaml
    interval: 86400

#白名单
rules:
  - RULE-SET,applications,DIRECT
  - DOMAIN,clash.razord.top,DIRECT
  - DOMAIN,yacd.haishan.me,DIRECT
  - RULE-SET,private,DIRECT
  - RULE-SET,reject,REJECT
  - RULE-SET,icloud,DIRECT
  - RULE-SET,apple,DIRECT
  - RULE-SET,google,节点选择
  - RULE-SET,proxy,节点选择
  - RULE-SET,direct,DIRECT
  - RULE-SET,lancidr,DIRECT
  - RULE-SET,cncidr,DIRECT
  - RULE-SET,telegramcidr,节点选择
  - GEOIP,LAN,DIRECT
  - DOMAIN-SUFFIX,xn--ngstr-lra8j.com,节点选择
  - DOMAIN-SUFFIX,services.googleapis.cn,节点选择
  - GEOIP,CN,DIRECT
  - MATCH,节点选择
`

// builtinBlacklistContent 内置黑名单模板：gfw/tld-not-cn/telegram 等黑名单命中才走代理，
// 其余流量（含未匹配）全部 DIRECT（兜底 MATCH,DIRECT）。
const builtinBlacklistContent = `mode: rule
mixed-port: 7897
allow-lan: false
log-level: warning
external-controller: ''
secret: set-your-secret
unified-delay: false
profile:
  store-selected: true
external-controller-unix: /tmp/verge/verge-mihomo.sock
geo-update-interval: 24
external-controller-cors:
  allow-private-network: true
  allow-origins:
  - tauri://localhost
  - http://tauri.localhost
  - https://yacd.metacubex.one
  - https://metacubex.github.io
  - https://board.zash.run.place
geodata-mode: true
dns:
  enable: true
  listen: :53

  # 开启 IPv6 DNS
  ipv6: true

  # 推荐 fake-ip，分流更准确
  enhanced-mode: fake-ip

  fake-ip-range: 198.18.0.1/16

  fake-ip-filter:
    - '*.lan'
    - '*.local'
    - '*.arpa'
    - 'time.*.com'
    - 'ntp.*.com'
    - '+.market.xiaomi.com'
    - 'localhost.ptlogin2.qq.com'
    - '*.msftncsi.com'
    - 'www.msftconnecttest.com'

  fake-ip-filter-mode: blacklist

  # DNS启动解析服务器
  # 不使用system，避免绕过Clash
  default-nameserver:
    - 223.5.5.5
    - 119.29.29.29

  # 普通域名解析
  # 国内优先
  nameserver:
    - https://dns.alidns.com/dns-query
    - https://doh.pub/dns-query

  # 国外域名 fallback
  fallback:
    - https://cloudflare-dns.com/dns-query
    - https://dns.google/dns-query

  fallback-filter:
    geoip: true
    geoip-code: CN

    # 中国IP不进入fallback
    ipcidr:
      - 240.0.0.0/4
      - 0.0.0.0/32

    # 这些域名强制走国外DNS
    domain:
      - '+.google.com'
      - '+.googleapis.com'
      - '+.youtube.com'
      - '+.facebook.com'
      - '+.twitter.com'
      - '+.x.com'

  # 代理节点域名解析
  proxy-server-nameserver:
    - https://dns.alidns.com/dns-query

  # DNS遵守rules
  respect-rules: true

  # 不使用hosts
  use-hosts: false
  use-system-hosts: false

  # 开启HTTP3 DoH
  prefer-h3: true
geox-url:
  geoip: https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/geoip/release/geoip.dat
  geosite: https://gh-proxy.org/https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat
  mmdb: https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/geoip/release/Country.mmdb
tun:
  enable: false
  stack: mixed
  auto-route: true
  strict-route: false
  auto-detect-interface: true
  dns-hijack:
  - any:53
  device: utun1024
  mtu: 1500

proxies: ~
proxy-groups:
  - name: 节点选择
    type: select
    proxies:
      - 自动选择
      - DIRECT
      - __ALL_PROXIES__

  - name: 自动选择
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    tolerance: 50
    proxies:
      - __ALL_PROXIES__

rule-providers:
  reject:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/reject.txt"
    path: ./ruleset/reject.yaml
    interval: 86400

  icloud:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/icloud.txt"
    path: ./ruleset/icloud.yaml
    interval: 86400

  apple:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/apple.txt"
    path: ./ruleset/apple.yaml
    interval: 86400

  google:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/google.txt"
    path: ./ruleset/google.yaml
    interval: 86400

  proxy:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/proxy.txt"
    path: ./ruleset/proxy.yaml
    interval: 86400

  direct:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/direct.txt"
    path: ./ruleset/direct.yaml
    interval: 86400

  private:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/private.txt"
    path: ./ruleset/private.yaml
    interval: 86400

  gfw:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/gfw.txt"
    path: ./ruleset/gfw.yaml
    interval: 86400

  tld-not-cn:
    type: http
    behavior: domain
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/tld-not-cn.txt"
    path: ./ruleset/tld-not-cn.yaml
    interval: 86400

  telegramcidr:
    type: http
    behavior: ipcidr
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/telegramcidr.txt"
    path: ./ruleset/telegramcidr.yaml
    interval: 86400

  cncidr:
    type: http
    behavior: ipcidr
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/cncidr.txt"
    path: ./ruleset/cncidr.yaml
    interval: 86400

  lancidr:
    type: http
    behavior: ipcidr
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/lancidr.txt"
    path: ./ruleset/lancidr.yaml
    interval: 86400

  applications:
    type: http
    behavior: classical
    url: "https://gh-proxy.org/https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/applications.txt"
    path: ./ruleset/applications.yaml
    interval: 86400

#黑名单
rules:
  - RULE-SET,applications,DIRECT
  - DOMAIN,clash.razord.top,DIRECT
  - DOMAIN,yacd.haishan.me,DIRECT
  - RULE-SET,private,DIRECT
  - RULE-SET,reject,REJECT
  - RULE-SET,tld-not-cn,节点选择
  - RULE-SET,gfw,节点选择
  - RULE-SET,telegramcidr,节点选择
  - DOMAIN-SUFFIX,xn--ngstr-lra8j.com,节点选择
  - DOMAIN-SUFFIX,services.googleapis.cn,节点选择
  - MATCH,DIRECT
`
