package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// controllerClient 是 mihomo external-controller 的最小客户端。
// 仅封装本插件用到的接口；全部指向 127.0.0.1，不对外暴露。
type controllerClient struct {
	mu     sync.Mutex
	base   string
	secret string
	hc     *http.Client
}

func (c *controllerClient) setEndpoint(port int, secret string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.base = fmt.Sprintf("http://127.0.0.1:%d", port)
	c.secret = secret
	// Proxy: nil 强制直连 —— 绝不让系统代理环境变量劫持发往本机内核的请求。
	// DisableKeepAlives: 每次请求新建连接 —— 若内核重启瞬间有旧监听残留，
	// 复用的 keep-alive 连接会把后续所有请求送到错误的实例且永不自愈。
	c.hc = &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
	}
}

func (c *controllerClient) endpoint() (string, *http.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hc == nil {
		c.hc = &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
		}
	}
	return c.base, c.hc
}

func (c *controllerClient) do(ctx context.Context, method, path string, body any, out any) error {
	base, hc := c.endpoint()
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("内核控制接口不可达: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("内核接口 %s %s: HTTP %d %s", method, path, resp.StatusCode, string(msg))
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out)
	}
	return nil
}

type coreVersion struct {
	Version string `json:"version"`
	Meta    bool   `json:"meta"`
}

func (c *controllerClient) version(ctx context.Context) (coreVersion, error) {
	var v coreVersion
	err := c.do(ctx, http.MethodGet, "/version", nil, &v)
	return v, err
}

// ConnItem 是内核的一条活动连接（mihomo /connections 单条记录）。
type ConnItem struct {
	ID          string   `json:"id"`
	Upload      int64    `json:"upload"`
	Download    int64    `json:"download"`
	Start       string   `json:"start"` // RFC3339
	Chains      []string `json:"chains"`
	Rule        string   `json:"rule"`
	RulePayload string   `json:"rulePayload"`
	Metadata    struct {
		Network         string `json:"network"`
		Type            string `json:"type"`
		SourceIP        string `json:"sourceIP"`
		SourcePort      string `json:"sourcePort"`
		DestinationIP   string `json:"destinationIP"`
		DestinationPort string `json:"destinationPort"`
		Host            string `json:"host"`
	} `json:"metadata"`
}

// Target 展示用的访问目标：优先域名，否则 IP:端口。
func (c ConnItem) Target() string {
	if h := c.Metadata.Host; h != "" {
		return h
	}
	return c.Metadata.DestinationIP + ":" + c.Metadata.DestinationPort
}

type connSnapshot struct {
	UploadTotal   int64      `json:"uploadTotal"`
	DownloadTotal int64      `json:"downloadTotal"`
	Connections   []ConnItem `json:"connections"`
}

func (c *controllerClient) connections(ctx context.Context) (connSnapshot, error) {
	var s connSnapshot
	err := c.do(ctx, http.MethodGet, "/connections", nil, &s)
	return s, err
}

// RuleItem 是内核加载的一条路由规则（mihomo /rules）。
type RuleItem struct {
	Type    string `json:"type"`    // 如 DOMAIN-SUFFIX / RULE-SET / MATCH
	Payload string `json:"payload"` // 规则内容，如 google.com
	Proxy   string `json:"proxy"`   // 命中后走的目标（节点/组/DIRECT/REJECT）
	Size    int    `json:"size"`    // RULE-SET 的规则条数，其他为 0
}

func (c *controllerClient) rules(ctx context.Context) ([]RuleItem, error) {
	var out struct {
		Rules []RuleItem `json:"rules"`
	}
	err := c.do(ctx, http.MethodGet, "/rules", nil, &out)
	return out.Rules, err
}

// closeAllConnections 断开内核当前全部活动连接。
func (c *controllerClient) closeAllConnections(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/connections", nil, nil)
}

// proxies 返回 /proxies 的完整数据（mihomo 会把 map 键按字典序输出，组内节点
// 顺序由 all 数组保留，即订阅中的真实顺序）。
func (c *controllerClient) proxies(ctx context.Context) (map[string]any, error) {
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/proxies", nil, &raw); err != nil {
		return nil, err
	}
	var list map[string]any
	if data, ok := raw["proxies"]; ok {
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, err
		}
	}
	return list, nil
}

func (c *controllerClient) selectProxy(ctx context.Context, group, name string) error {
	return c.do(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), map[string]string{"name": name}, nil)
}

// delayResult 解析测速结果：成功是 {"节点": 毫秒}，失败可能是 {"message"/"msg": "..."}
func decodeDelay(data []byte) (map[string]int, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(raw))
	for k, v := range raw {
		switch n := v.(type) {
		case float64:
			out[k] = int(n)
		case string:
			if k == "message" || k == "msg" {
				return nil, errStr(n)
			}
		}
	}
	return out, nil
}

type strErr string

func (e strErr) Error() string { return string(e) }

func errStr(s string) error { return strErr(s) }

func (c *controllerClient) groupDelay(ctx context.Context, group, testURL string, timeoutMS int) (map[string]int, error) {
	path := fmt.Sprintf("/group/%s/delay?url=%s&timeout=%d",
		url.PathEscape(group), url.QueryEscape(testURL), timeoutMS)
	base, hc := c.endpoint()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("内核控制接口不可达: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("测速失败: HTTP %d %s", resp.StatusCode, string(data))
	}
	return decodeDelay(data)
}

func (c *controllerClient) proxyDelay(ctx context.Context, name, testURL string, timeoutMS int) (map[string]int, error) {
	path := fmt.Sprintf("/proxies/%s/delay?url=%s&timeout=%d",
		url.PathEscape(name), url.QueryEscape(testURL), timeoutMS)
	base, hc := c.endpoint()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("内核控制接口不可达: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("测速失败: HTTP %d %s", resp.StatusCode, string(data))
	}
	return decodeDelay(data)
}
