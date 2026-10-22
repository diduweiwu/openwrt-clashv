// watch.go 实现断网自动重启（看门狗）：后台定时拨测用户配置的检测地址，
// 内核运行中检测不通则自动重启内核尝试恢复；连续失败期间的重启次数达到
// 设置上限后不再重启，直到某次检测恢复连通才重新计数。
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	watchInterval = 30 * time.Second // 拨测间隔：每 30 秒检测一次
	watchTimeout  = 10 * time.Second // 单次拨测超时
	watchGrace    = 2 * time.Minute  // 自动重启后的静默期：内核就绪与网络恢复都需要时间，期间不检测，防止误判连跳计数
)

// watchState 汇总断网守护的运行状态（内存态，重启服务即清零，不落盘）。
// fails/reboots/limitHit 三者构成一个「故障周期」：检测一旦恢复连通即整体清零。
type watchState struct {
	mu         sync.Mutex
	lastCheck  int64 // 最近一次拨测的 Unix 时间戳（秒），0 为尚未检测过
	lastOK     bool  // 最近一次拨测是否连通
	fails      int   // 当前故障周期内连续失败次数
	reboots    int   // 当前故障周期内已自动重启内核次数
	lastReboot int64 // 最近一次自动重启的 Unix 时间戳（秒），0 为从未触发
	limitHit   bool  // 当前故障周期内已达重启上限（true 时不再重启，只记录失败）
}

// recordOK 记录一次成功拨测并清空故障周期；返回清零前是否处于故障周期
//（供主循环决定要不要打一条「已恢复」日志）。
func (w *watchState) recordOK() (wasFailing bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	wasFailing = w.lastCheck > 0 && !w.lastOK
	w.lastCheck = time.Now().Unix()
	w.lastOK = true
	w.fails = 0
	w.reboots = 0
	w.limitHit = false
	return wasFailing
}

// recordFail 记录一次失败拨测，返回本次是否应当执行自动重启与是否已达上限：
// 未达上限则计入一次重启名额并放行，已达上限只累计失败次数。
// round 为当前故障周期内已计数的重启序号（含本次），供日志展示；
// firstLimit 为本次拨测刚触达上限（ true 时主循环记一条日志，之后静默防刷屏）。
func (w *watchState) recordFail(max int) (round int, limit bool, firstLimit bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastCheck = time.Now().Unix()
	w.lastOK = false
	w.fails++
	if w.limitHit || w.reboots >= max {
		firstLimit = !w.limitHit
		w.limitHit = true
		return w.reboots, true, firstLimit
	}
	w.reboots++
	return w.reboots, false, false
}

// markReboot 记录一次已执行的自动重启时间（供静默期判断与状态展示）。
func (w *watchState) markReboot() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastReboot = time.Now().Unix()
}

// cooling 报告当前是否处于自动重启后的静默期。
func (w *watchState) cooling() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastReboot > 0 && time.Since(time.Unix(w.lastReboot, 0)) < watchGrace
}

// snapshot 返回状态快照（map 形式，直接并入 /api/status 响应）。
func (w *watchState) snapshot() map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	return map[string]any{
		"last_check":  w.lastCheck,
		"last_ok":     w.lastOK,
		"fails":       w.fails,
		"reboots":     w.reboots,
		"last_reboot": w.lastReboot,
		"limit_hit":   w.limitHit,
	}
}

// proxyClient 构造经本机混合端口的拨测 client：检测请求必须经内核代理发出，
// 测的才是「通过代理能否上网」——透明代理模式只接管局域网转发流量，路由器
// 本机直连不经过内核（TUN 才接管本机），直连 google.com 这类地址永远不通。
// 每 30 秒才构造一次，开销可忽略；内核未运行时连接被拒绝，照常视为失败。
func (d *deps) proxyClient() *http.Client {
	port := 7890
	if s, err := d.cfg.Get(); err == nil && s.MixedPort > 0 {
		port = s.MixedPort
	}
	return &http.Client{
		Timeout: watchTimeout,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}),
		},
	}
}

// probeURL 对检测地址做一次 GET 拨测（client 决定直连或经代理）。2xx/3xx 视为
// 连通；超时、DNS/TLS 错误、4xx/5xx 一律视为不通。响应体只读一小段即丢弃，
// 避免大响应拖慢检测。
func probeURL(client *http.Client, url string) bool {
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// handleWatchTest 手动拨测：设置页「检测」按钮验证检测地址当前是否可达，
// 与后台守护走同一条经代理路径。内核未运行时代理端口连接失败，返回失败并附提示。
func (d *deps) handleWatchTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, errStr("请求体不是合法 JSON"))
		return
	}
	raw := strings.TrimSpace(body.URL)
	if raw == "" {
		writeErr(w, 400, errStr("缺少检测地址"))
		return
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	start := time.Now()
	ok := probeURL(d.proxyClient(), raw)
	resp := map[string]any{"ok": ok, "ms": time.Since(start).Milliseconds()}
	if !ok {
		resp["error"] = "地址当前不可访问（内核未运行、代理链路不通或地址无效）"
	}
	writeJSON(w, 200, resp)
}

// watchLoop 断网守护主循环：每 30 秒经内核代理拨测一次检测地址。仅在开关
// 打开、地址已配置且内核运行中时工作；自动重启后的静默期（2 分钟）内跳过
// 检测，给内核就绪与网络恢复留时间。拨测失败按设置的重启次数上限自动重启内核。
func (d *deps) watchLoop() {
	for {
		time.Sleep(watchInterval)
		s, err := d.cfg.Get()
		if err != nil || !s.AutoRebootEnabled {
			continue
		}
		url := strings.TrimSpace(s.AutoRebootURL)
		if url == "" {
			continue
		}
		// 内核未运行/启动中时不检测也不计数：守护只负责「运行中掉线」的恢复
		if !d.mgr.Running() || d.mgr.Starting() || d.watch.cooling() {
			continue
		}
		if probeURL(d.proxyClient(), url) {
			if d.watch.recordOK() {
				slog.Info("断网守护：检测地址已恢复连通，重启计数清零", "url", url)
			}
			continue
		}
		round, limit, firstLimit := d.watch.recordFail(s.AutoRebootMax)
		if limit {
			// 已达上限：只在刚触达上限的那一刻记一条日志，之后静默
			if firstLimit {
				slog.Warn("断网守护：检测地址仍不通，已达自动重启次数上限，本轮不再重启",
					"url", url, "reboots", round, "max", s.AutoRebootMax)
			}
			continue
		}
		slog.Warn("断网守护：检测地址访问失败，自动重启内核尝试恢复",
			"url", url, "round", round, "max", s.AutoRebootMax)
		if err := d.mgr.Restart(); err != nil {
			slog.Error("断网守护：内核重启失败", "err", err)
		}
		d.watch.markReboot()
	}
}
