package core

import "testing"

func TestOptimizableServer(t *testing.T) {
	node := func(kv map[string]any) map[string]any {
		base := map[string]any{"type": "ss", "server": "example.com", "port": 443}
		for k, v := range kv {
			base[k] = v
		}
		return base
	}
	cases := []struct {
		name string
		node map[string]any
		want bool
	}{
		{"ss 纯文本域名", node(nil), true},
		{"已是 IP", node(map[string]any{"server": "1.2.3.4"}), false},
		{"单标签名", node(map[string]any{"server": "localhost"}), false},
		{"端口字符串", node(map[string]any{"port": "8443"}), true},
		{"端口非法", node(map[string]any{"port": 0}), false},
		{"dialer-proxy 链式", node(map[string]any{"dialer-proxy": "前置"}), false},
		{"UDP 协议 hysteria2", node(map[string]any{"type": "hysteria2", "sni": "example.com"}), false},
		{"vless 带 servername", node(map[string]any{"type": "vless", "tls": true, "servername": "example.com"}), true},
		{"vless TLS 无 servername", node(map[string]any{"type": "vless", "tls": true}), false},
		{"vless 明文", node(map[string]any{"type": "vless"}), true},
		{"vmess TLS 带 servername", node(map[string]any{"type": "vmess", "tls": true, "servername": "cdn.example.com"}), true},
		{"trojan 带 sni", node(map[string]any{"type": "trojan", "sni": "example.com"}), true},
		{"trojan 无 sni", node(map[string]any{"type": "trojan"}), false},
		{"ss shadowtls 带 sni", node(map[string]any{"plugin": "shadowtls", "plugin-opts": map[string]any{"tls": true, "sni": "example.com"}}), true},
		{"ss shadowtls 无 sni", node(map[string]any{"plugin": "shadowtls", "plugin-opts": map[string]any{"tls": true}}), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			host, port, ok := optimizableServer(c.node)
			if ok != c.want {
				t.Fatalf("optimizable = %v, want %v (host=%q port=%q)", ok, c.want, host, port)
			}
		})
	}
}

func TestProxyMapsAndPortOf(t *testing.T) {
	doc := map[string]any{
		"proxies": []any{
			map[string]any{"name": "a", "port": 1},
			"bad-item",
			map[string]any{"name": "b", "port": "2"},
		},
		"rules": []any{"MATCH,DIRECT"},
	}
	maps := proxyMaps(doc)
	if len(maps) != 2 {
		t.Fatalf("proxyMaps len = %d, want 2", len(maps))
	}
	if p, ok := portOf(maps[1]); !ok || p != 2 {
		t.Fatalf("portOf string = %d,%v", p, ok)
	}
	if _, ok := portOf(map[string]any{"port": nil}); ok {
		t.Fatal("portOf nil 应为 false")
	}
}
