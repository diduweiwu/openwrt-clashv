package core

import (
	"testing"

	"gopkg.in/yaml.v3"

	"clashv/internal/config"
)

func TestRuleTarget(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"MATCH,节点选择", "节点选择"},
		{"MATCH,DIRECT", ""},
		{"DOMAIN-SUFFIX,google.com,🚀 节点选择", "🚀 节点选择"},
		{"GEOIP,CN,DIRECT", ""},
		{"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve", ""}, // 尾随选项不是目标
		{"IP-CIDR,192.168.0.0/16,MyGroup,src", "MyGroup"},
		{"RULE-SET,gfw,自动选择", "自动选择"},
		{"AND,((DOMAIN,baidu.com),(NETWORK,UDP)),DIRECT", ""},
		{"REJECT", ""}, // 无目标段
		{"", ""},
	}
	for _, c := range cases {
		if got := ruleTarget(c.in); got != c.want {
			t.Errorf("ruleTarget(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func docOf(t *testing.T, s string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("yaml 解析失败: %v", err)
	}
	return doc
}

const noRulesSub = `
proxies:
  - name: "HK-01"
    type: socks5
    server: 127.0.0.1
    port: 1080
proxy-groups:
  - name: "节点选择"
    type: select
    proxies: [HK-01]
`

func TestRulesEmpty(t *testing.T) {
	if !rulesEmpty(docOf(t, noRulesSub)) {
		t.Error("无 rules 段应视为空")
	}
	if !rulesEmpty(docOf(t, noRulesSub+"\nrules: []\n")) {
		t.Error("空 rules 列表应视为空")
	}
	if rulesEmpty(docOf(t, noRulesSub+"\nrules: [\"MATCH,节点选择\"]\n")) {
		t.Error("有规则不应视为空")
	}
}

func TestRulesRouteAnyProxy(t *testing.T) {
	// 全直连规则 = 视同无规则
	if rulesRouteAnyProxy(docOf(t, noRulesSub+"\nrules: [\"GEOIP,CN,DIRECT\",\"MATCH,DIRECT\"]\n")) {
		t.Error("全 DIRECT/REJECT 规则不应算导向代理")
	}
	// 有任一条指向组 = 有代理路由
	if !rulesRouteAnyProxy(docOf(t, noRulesSub+"\nrules: [\"DOMAIN-SUFFIX,google.com,节点选择\",\"MATCH,DIRECT\"]\n")) {
		t.Error("含组目标的规则应算导向代理")
	}
}

func TestMergeCustomRules(t *testing.T) {
	doc := docOf(t, noRulesSub+"\nrules: [\"GEOIP,CN,DIRECT\",\"MATCH,DIRECT\"]\n")
	mergeCustomRules(doc, []string{"DOMAIN-SUFFIX,x.com,节点选择", "IP-CIDR,10.0.0.0/8,DIRECT,no-resolve"})
	got, ok := doc["rules"].([]any)
	if !ok || len(got) != 4 {
		t.Fatalf("合并后应有 4 条规则, got %v", doc["rules"])
	}
	if got[0] != "DOMAIN-SUFFIX,x.com,节点选择" || got[1] != "IP-CIDR,10.0.0.0/8,DIRECT,no-resolve" {
		t.Errorf("自定义规则应置顶, got %v", got)
	}
	if got[2] != "GEOIP,CN,DIRECT" || got[3] != "MATCH,DIRECT" {
		t.Errorf("订阅规则应保持原顺序跟在后面, got %v", got)
	}
}

func TestTunDnsHijackFollowsToggles(t *testing.T) {
	cases := []struct {
		v4, v6 bool
		want   []any
	}{
		{true, true, []any{"any:53"}},
		{true, false, []any{"0.0.0.0:53"}},
		{false, true, []any{"[::]:53"}},
		{false, false, nil},
	}
	for _, c := range cases {
		overlay := managedOverlay(config.Settings{DNSHijackIPv4: c.v4, DNSHijackIPv6: c.v6}, "")
		tun, _ := overlay["tun"].(map[string]any)
		got, _ := tun["dns-hijack"].([]any)
		if len(got) != len(c.want) {
			t.Errorf("v4=%v v6=%v dns-hijack = %v, want %v", c.v4, c.v6, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("v4=%v v6=%v dns-hijack[%d] = %v, want %v", c.v4, c.v6, i, got[i], c.want[i])
			}
		}
	}
}

func TestFirstProxyTarget(t *testing.T) {
	if got := firstProxyTarget(docOf(t, noRulesSub)); got != "节点选择" {
		t.Errorf("firstProxyTarget = %q, want 节点选择", got)
	}
	if got := firstProxyTarget(docOf(t, "proxies:\n  - name: 香港节点\n    type: socks5\n    server: 1.2.3.4\n    port: 1\n")); got != "香港节点" {
		t.Errorf("无组时应取首个节点名, got %q", got)
	}
}
