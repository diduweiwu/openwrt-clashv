// Package config 负责 clashv 自身设置。
//
// OpenWrt 上持久化到 UCI（/etc/config/clashv）；非 OpenWrt 开发环境回退到
// $HOME/.clashv/config.json，其余数据（订阅文件、日志、运行时配置）统一放 WorkDir。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Settings 是全部用户可配置项，字段与 UCI option 一一对应（见 uci.go）。
type Settings struct {
	Enabled          bool   `json:"enabled"`           // 开机启动插件服务
	UIPort           int    `json:"ui_port"`           // 管理界面 / API 端口
	MixedPort        int    `json:"mixed_port"`        // mihomo 混合代理端口
	ControllerPort   int    `json:"controller_port"`   // mihomo external-controller 端口（仅监听 127.0.0.1）
	ControllerSecret string `json:"controller_secret"` // mihomo 控制密钥，留空则首次启动自动生成
	Token            string `json:"token"`             // 管理界面访问令牌，留空表示不启用鉴权
	TUN              bool   `json:"tun"`               // TUN 模式（接管全局流量）；关闭时自动用防火墙做 TCP 透明代理
	TUNStack         string `json:"tun_stack"`         // TUN 协议栈: system / gvisor / mixed
	DNS              bool   `json:"dns"`               // 由 mihomo 接管 DNS（TUN 模式建议开启）
	DNSMode          string `json:"dns_mode"`          // DNS 解析模式: fake-ip / redir-host
	AutoUpdateHours  int    `json:"auto_update"`       // 订阅自动更新间隔（小时），0 为关闭
	CorePath         string `json:"core_path"`         // mihomo 二进制路径
	CoreArch         string `json:"core_arch"`         // 内核下载平台名，留空自动检测（如 linux-arm64）
	CoreMemLimit     int    `json:"core_mem_limit"`    // 内核内存软上限（GOMEMLIMIT，MB），0 为不限制
	CoreMode         string `json:"core_mode"`         // 出站模式: rule / global / direct（卡片切换后持久化，重启仍生效）
	DNSHijack        string `json:"dns_hijack"`        // DNS 劫持模式: firewall / dnsmasq / off（旁路由必开其一）
	CustomUA         string `json:"custom_ua"`         // 上次使用的自定义订阅 User-Agent（记住，下次预填）
	WorkDir          string `json:"workdir"`           // 数据目录：订阅、运行时配置、日志
	PluginRepo       string `json:"plugin_repo"`       // 插件自更新的 GitHub 仓库（owner/repo）
	DownloadProxy    string `json:"download_proxy"`    // GitHub 下载加速前缀（如 https://gh-proxy.com），留空直连
	ActiveProfile    string `json:"active_profile"`    // 当前激活的订阅 ID
}

// Defaults 返回一份带合理默认值的设置副本。
func Defaults() Settings {
	return Settings{
		Enabled:          true,
		UIPort:           9097,
		MixedPort:        7890,
		ControllerPort:   9090,
		ControllerSecret: "",
		Token:            "",
		TUN:              false,
		TUNStack:         "mixed",
		DNS:              true,
		DNSMode:          "fake-ip",
		AutoUpdateHours:  12,
		CorePath:         "",
		CoreArch:         "",
		CoreMemLimit:     0,
		CoreMode:         "rule",
		DNSHijack:        "firewall", // 与 OpenClash 一致：默认防火墙转发 DNS
		CustomUA:         "",
		WorkDir:          "",
		PluginRepo:       "nier/clashv",
		DownloadProxy:    "https://gh-proxy.com",
		ActiveProfile:    "",
	}
}

// Manager 管理设置的加载与保存，线程安全。
type Manager struct {
	mu      sync.Mutex
	dev     bool
	useUCI  bool
	cur     Settings
	loaded  bool
	fileCfg string // dev 模式 JSON 配置路径
}

// New 创建 Manager。dev=true 强制走文件存储（供本机开发）。
func New(dev bool) *Manager {
	m := &Manager{dev: dev}
	m.useUCI = !dev && hasUCI()
	return m
}

func hasUCI() bool {
	_, err := exec.LookPath("uci")
	return err == nil
}

// IsOpenWrt 报告当前是否以 UCI 模式运行。
func (m *Manager) IsOpenWrt() bool { return m.useUCI }

// Home 返回数据目录（订阅、日志、运行时配置都放这里）。
func (m *Manager) Home() string {
	m.mu.Lock()
	s := m.cur
	m.mu.Unlock()
	if s.WorkDir != "" {
		return s.WorkDir
	}
	if m.useUCI {
		return "/etc/clashv"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clashv")
}

// ProfilesDir 返回订阅文件目录。
func (m *Manager) ProfilesDir() string { return filepath.Join(m.Home(), "profiles") }

// LogDir 返回内核日志目录。
func (m *Manager) LogDir() string { return filepath.Join(m.Home(), "logs") }

// RuntimeConfigPath 返回合成后给 mihomo 使用的运行时配置路径。
func (m *Manager) RuntimeConfigPath() string { return filepath.Join(m.Home(), "config.yaml") }

// CustomRulesPath 返回用户自定义规则文件路径。一行一条 clash 规则，
// 不走 UCI/JSON 设置——规则串可含空格与任意字符，独立纯文本文件最稳妥。
func (m *Manager) CustomRulesPath() string { return filepath.Join(m.Home(), "custom-rules.txt") }

// CustomRules 读取用户自定义规则（一行一条；文件不存在视为空列表）。
func (m *Manager) CustomRules() []string {
	data, err := os.ReadFile(m.CustomRulesPath())
	if err != nil {
		return nil
	}
	return sanitizeRules(strings.Split(string(data), "\n"))
}

// SetCustomRules 覆写用户自定义规则（去空白、去重后原样落盘）。
func (m *Manager) SetCustomRules(rules []string) error {
	clean := sanitizeRules(rules)
	path := m.CustomRulesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(clean, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// sanitizeRules 规整规则列表：修剪首尾空白、剔除空行、按出现顺序去重。
func sanitizeRules(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

// CorePath 返回 mihomo 二进制路径（含默认值推导）。
func (m *Manager) CorePath() string {
	m.mu.Lock()
	p := m.cur.CorePath
	m.mu.Unlock()
	if p != "" {
		return p
	}
	if m.useUCI {
		return "/usr/bin/mihomo"
	}
	return filepath.Join(m.Home(), "bin", "mihomo")
}

// Get 返回当前设置（首次调用时从存储加载，之后走缓存）。
func (m *Manager) Get() (Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loaded {
		return m.cur, nil
	}
	s := Defaults()
	var err error
	if m.useUCI {
		err = m.loadUCI(&s)
	} else {
		err = m.loadFile(&s)
	}
	if err != nil {
		return s, err
	}
	s.normalize()
	m.cur = s
	m.loaded = true
	return s, nil
}

// Update 以 fn 修改设置并持久化；fn 内部对 Settings 的改动会被保存。
func (m *Manager) Update(fn func(s *Settings)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		m.mu.Unlock()
		if _, err := m.Get(); err != nil {
			return err
		}
		m.mu.Lock()
	}
	s := m.cur
	fn(&s)
	s.normalize()
	if m.useUCI {
		if err := m.saveUCI(&s); err != nil {
			return err
		}
	} else if err := m.saveFile(&s); err != nil {
		return err
	}
	m.cur = s
	return nil
}

func (s *Settings) normalize() {
	if s.UIPort <= 0 || s.UIPort > 65535 {
		s.UIPort = 9097
	}
	if s.MixedPort <= 0 || s.MixedPort > 65535 {
		s.MixedPort = 7890
	}
	if s.ControllerPort <= 0 || s.ControllerPort > 65535 {
		s.ControllerPort = 9090
	}
	if s.AutoUpdateHours < 0 {
		s.AutoUpdateHours = 0
	}
	s.TUNStack = strings.TrimSpace(s.TUNStack)
	switch s.TUNStack {
	case "system", "gvisor", "mixed":
	default:
		s.TUNStack = "mixed"
	}
	s.ControllerSecret = strings.ReplaceAll(s.ControllerSecret, "'", "")
	s.Token = strings.ReplaceAll(s.Token, "'", "")
	// 内存软上限：负数视为关闭；给个硬顶防止误填离谱值
	if s.CoreMemLimit < 0 || s.CoreMemLimit > 16384 {
		s.CoreMemLimit = 0
	}
	s.PluginRepo = strings.Trim(s.PluginRepo, "/ ")
	if s.PluginRepo == "" {
		s.PluginRepo = "nier/clashv"
	}
	s.DownloadProxy = strings.TrimSuffix(strings.TrimSpace(s.DownloadProxy), "/")
	s.CustomUA = strings.ReplaceAll(strings.TrimSpace(s.CustomUA), "'", "")
	// DNS 劫持模式白名单；历史配置为空时视为默认防火墙转发
	s.DNSHijack = strings.TrimSpace(s.DNSHijack)
	switch s.DNSHijack {
	case "firewall", "dnsmasq", "off":
	default:
		s.DNSHijack = "firewall"
	}
	// DNS 解析模式白名单；历史配置为空时视为 fake-ip
	s.DNSMode = strings.TrimSpace(s.DNSMode)
	switch s.DNSMode {
	case "fake-ip", "redir-host":
	default:
		s.DNSMode = "fake-ip"
	}
	// 出站模式白名单；历史配置为空时视为规则模式
	switch strings.TrimSpace(s.CoreMode) {
	case "global", "direct":
	case "rule":
	default:
		s.CoreMode = "rule"
	}
}

// NormalizeCoreMode 归一出站模式，非法值回退规则模式（供状态展示与合成配置用）。
func NormalizeCoreMode(m string) string {
	switch m {
	case "global", "direct":
		return m
	}
	return "rule"
}

// EnsureDirs 创建运行所需目录结构。
func (m *Manager) EnsureDirs() error {
	for _, d := range []string{m.Home(), m.ProfilesDir(), m.LogDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s: %w", d, err)
		}
	}
	return nil
}

// ---- dev/文件存储 ----

type fileDoc struct {
	Settings
}

func (m *Manager) fileCfgPath() string {
	if m.fileCfg != "" {
		return m.fileCfg
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clashv", "config.json")
}

func (m *Manager) loadFile(s *Settings) error {
	data, err := os.ReadFile(m.fileCfgPath())
	if os.IsNotExist(err) {
		if s.WorkDir == "" {
			home, _ := os.UserHomeDir()
			s.WorkDir = filepath.Join(home, ".clashv")
		}
		return nil
	}
	if err != nil {
		return err
	}
	// 以当前值（默认值）为底做覆盖式解析：配置里缺失的键保留默认值
	doc := fileDoc{*s}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("解析 %s: %w", m.fileCfgPath(), err)
	}
	*s = doc.Settings
	return nil
}

func (m *Manager) saveFile(s *Settings) error {
	path := m.fileCfgPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(fileDoc{*s}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// atoi是 uci.go 用的小工具：字符串转 int，失败返回 def。
func atoi(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}
