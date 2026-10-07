package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"clashv/internal/config"
)

// probeFixture 起一对假服务器：controller 恒定返回 status；proxy 模拟
// mihomo 混合端口——收到经代理的请求后按「回环直连规则」转发到控制器，
// 原样带回状态码。返回可直接塞进 config.Settings 的两个端口。
func probeFixture(t *testing.T, status int) (mixedPort, controllerPort int) {
	t.Helper()
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"version":"v1.0.0"}`))
	}))
	t.Cleanup(controller.Close)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequestWithContext(r.Context(), r.Method, controller.URL+r.URL.RequestURI(), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		req.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(proxy.Close)
	p := func(raw string) int {
		_, port, _ := strings.Cut(strings.TrimPrefix(raw, "http://"), ":")
		n, err := strconv.Atoi(port)
		if err != nil {
			t.Fatalf("解析端口失败: %v", err)
		}
		return n
	}
	return p(proxy.URL), p(controller.URL)
}

func TestProbeThroughProxyOK(t *testing.T) {
	mixed, controller := probeFixture(t, http.StatusOK)
	s := config.Settings{MixedPort: mixed, ControllerPort: controller, ControllerSecret: "sec"}
	if err := probeThroughProxy(context.Background(), s); err != nil {
		t.Fatalf("200 响应应视为链路就绪: %v", err)
	}
}

func TestProbeThroughProxyNon200StillReady(t *testing.T) {
	// 401：链路已通，鉴权问题不算「内核不能代理流量」
	mixed, controller := probeFixture(t, http.StatusUnauthorized)
	s := config.Settings{MixedPort: mixed, ControllerPort: controller, ControllerSecret: "wrong"}
	if err := probeThroughProxy(context.Background(), s); err != nil {
		t.Fatalf("任何 HTTP 响应都应视为链路就绪: %v", err)
	}
}

func TestProbeThroughProxyRefused(t *testing.T) {
	// 没有内核在监听：连接拒绝必须报错
	s := config.Settings{MixedPort: 1, ControllerPort: 1, ControllerSecret: "sec"}
	if err := probeThroughProxy(context.Background(), s); err == nil {
		t.Fatal("端口无人监听时探测应失败")
	}
}

func TestPrependPinnedLoopbackRules(t *testing.T) {
	// 回环直连规则必须排在一切规则（含自定义规则）之前，且订阅无 rules
	// 段时也要能注入。
	doc := map[string]any{"rules": []any{"DOMAIN-SUFFIX,x.com,节点选择", "MATCH,DIRECT"}}
	prependPinnedRules(doc)
	got, ok := doc["rules"].([]any)
	if !ok || len(got) != 4 {
		t.Fatalf("合并后应有 4 条规则, got %v", doc["rules"])
	}
	if got[0] != "IP-CIDR,127.0.0.0/8,DIRECT,no-resolve" || got[1] != "IP-CIDR6,::1/128,DIRECT,no-resolve" {
		t.Errorf("回环直连规则应置顶, got %v", got)
	}
	if got[2] != "DOMAIN-SUFFIX,x.com,节点选择" || got[3] != "MATCH,DIRECT" {
		t.Errorf("原有规则应保持原顺序跟在后面, got %v", got)
	}
	empty := map[string]any{}
	prependPinnedRules(empty)
	if got, _ := empty["rules"].([]any); len(got) != len(pinnedLoopbackRules) {
		t.Errorf("无 rules 段时也应注入, got %v", empty["rules"])
	}
}
