package core

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"clashv/internal/config"
)

// buildRuntimeConfig 把「订阅原文 + clashv 托管的基础设置」合成为
// <workdir>/config.yaml 交给 mihomo 加载。
//
// 合成规则：以订阅内容为底，托管键覆盖其上——端口、控制器、TUN、DNS、
// store-selected 等始终由本插件管理，订阅里的同名键会被忽略，
// proxies / proxy-groups / rules / providers 等业务段原样保留。
func (m *Manager) buildRuntimeConfig(activeProfile string, s config.Settings) error {
	p, err := m.prof.Get(activeProfile)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(m.prof.Path(p.ID))
	if err != nil {
		return fmt.Errorf("读取订阅文件失败: %w", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("订阅文件不是有效的 YAML: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	for k, v := range managedOverlay(s) {
		doc[k] = v
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	path := m.cfg.RuntimeConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// managedOverlay 返回本插件托管的 mihomo 基础配置。
func managedOverlay(s config.Settings) map[string]any {
	m := map[string]any{
		"mixed-port": s.MixedPort,
		// 路由器插件固定允许 LAN：透明代理把流量 REDIRECT 到本机端口，
		// 以及局域网设备直连混合端口，都要求监听 0.0.0.0（127.0.0.1 会拒收）
		"allow-lan":           true,
		"bind-address":        "*",
		"mode":                "rule",
		"log-level":           "info",
		"unified-delay":       true,
		"tcp-concurrent":      true,
		"find-process-mode":   "off", // 路由器 CPU 弱，跳过每连接的进程匹配
		"external-controller": fmt.Sprintf("127.0.0.1:%d", s.ControllerPort),
		"secret":              s.ControllerSecret,
		// 透明代理（redirect 模式）监听端口：防火墙把 LAN 的 TCP 重定向到这里，
		// 与 hijack.go 的 redirPort 保持一致
		"redir-port": 7892,
		"profile":    map[string]any{"store-selected": true},
		"tun": map[string]any{
			"enable":                s.TUN,
			"stack":                 s.TUNStack,
			"auto-route":            s.TUN,
			"auto-detect-interface": true,
			"dns-hijack":            []any{"any:53"},
		},
	}
	if s.DNS {
		m["dns"] = map[string]any{
			"enable":        true,
			"listen":        "0.0.0.0:1053",
			"ipv6":          false,
			"enhanced-mode": "fake-ip",
			"fake-ip-range": "198.18.0.1/16",
			// NTP/系统连通性检查走真实 IP，避免假 IP 干扰时间同步等
			"fake-ip-filter": []any{
				"*.lan", "+.local", "+.market.xiaomi.com",
				"+.msftconnecttest.com", "+.msftncsi.com",
				"time.windows.com", "time.nist.gov", "*.ntp.org",
				"time.apple.com", "time.asia.apple.com", "time1.cloud.tencent.com",
			},
			"default-nameserver": []any{"223.5.5.5", "119.29.29.29"},
			"nameserver":         []any{"https://doh.pub/dns-query", "https://dns.alidns.com/dns-query"},
		}
	}
	return m
}
