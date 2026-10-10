package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"clashv/internal/config"
	"clashv/internal/core"
	"clashv/internal/profiles"
)

// writeJSON 统一 JSON 响应。
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// wrap 简化无参数 handler 的错误处理。
func (d *deps) wrap(fn func(ctx context.Context) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := fn(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

// requireRunning 内核未运行时统一报错。
func (d *deps) requireRunning(w http.ResponseWriter) bool {
	if !d.mgr.Running() {
		writeErr(w, http.StatusServiceUnavailable, errCoreNotRunning)
		return false
	}
	return true
}

var errCoreNotRunning = errStr("内核未运行")

type strErr string

func (e strErr) Error() string { return string(e) }

func errStr(s string) error { return strErr(s) }

// ---- 状态 ----

func (d *deps) handleStatus(w http.ResponseWriter, r *http.Request) {
	s, err := d.cfg.Get()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	// 自动清理已失效的激活订阅（文件被外部删除等情形）
	if s.ActiveProfile != "" {
		if _, err := d.prof.Get(s.ActiveProfile); err != nil {
			_ = d.cfg.Update(func(u *config.Settings) { u.ActiveProfile = "" })
			s.ActiveProfile = ""
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	cs := d.mgr.CoreStatus(ctx)
	profileName := ""
	if s.ActiveProfile != "" {
		if p, err := d.prof.Get(s.ActiveProfile); err == nil {
			profileName = p.Name
		}
	}
	startedAt := d.mgr.StartedAt()
	var uptime int64
	if !startedAt.IsZero() {
		uptime = int64(time.Since(startedAt).Seconds())
	}
	// CPU 占用率：两次 status 轮询间的差值；非 Linux（本机开发）为 nil，前端隐藏
	var cpu any
	if v, ok := core.CPUPercent(); ok {
		cpu = v
	}
	// 内存细分：内核进程 RSS + 插件自身 RSS + 系统总内存（总内存仅 Linux 有值）
	memory := map[string]float64{
		"core_mb":   d.mgr.CoreRSSMB(),
		"plugin_mb": core.ReadRSSMB(os.Getpid()),
		"total_mb":  core.TotalMemMB(),
	}
	writeJSON(w, 200, map[string]any{
		"running":        d.mgr.Running(),
		"starting":       d.mgr.Starting(),
		"pid":            d.mgr.PID(),
		"uptime":         uptime,
		"started_at":     startedAt.Unix(),
		"cpu":            cpu,
		"core":           cs,
		"memory":         memory,
		"plugin_version": d.ver,
		"profile":        profileName,
		"ui_port":        s.UIPort,
		"mixed_port":     s.MixedPort,
		"tun":            s.TUN,
		"tun_stack":      s.TUNStack,
		"dns":            s.DNS,
		"dns_mode":       s.DNSMode,
		"mode":           config.NormalizeCoreMode(s.CoreMode),
		"openwrt":        d.cfg.IsOpenWrt(),
		"token_required": s.Token != "",
	})
}

func (d *deps) handleTraffic(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, d.mgr.Traffic())
}

// handleCoreModeGet 返回出站模式（持久化值，与运行中内核保持一致）。
func (d *deps) handleCoreModeGet(w http.ResponseWriter, r *http.Request) {
	s, err := d.cfg.Get()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, 200, map[string]string{"mode": config.NormalizeCoreMode(s.CoreMode)})
}

// handleCoreModePut 切换出站模式：运行中的内核立即 PATCH 生效，并持久化到
// 设置（下次启动的合成配置沿用该模式，不因重启回退到 rule）。
func (d *deps) handleCoreModePut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, errStr("请求体不是合法 JSON"))
		return
	}
	mode := config.NormalizeCoreMode(body.Mode)
	if d.mgr.Running() {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		if err := d.mgr.SetMode(ctx, mode); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
	}
	if err := d.cfg.Update(func(u *config.Settings) { u.CoreMode = mode }); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, 200, map[string]string{"mode": mode})
}

// handleConnections 返回当前活动连接快照（随内核每秒轮询刷新）。
// poll_error 非空表示轮询内核失败——列表为空时应展示该错误而非「暂无连接」。
// up/down_total 是内核本次启动以来的累计流量，与 /api/traffic 同源。
func (d *deps) handleConnections(w http.ResponseWriter, r *http.Request) {
	items := []core.ConnItem{}
	var pollErr string
	if d.mgr.Running() {
		items = d.mgr.Connections()
		pollErr = d.mgr.PollError()
	}
	tr := d.mgr.Traffic()
	writeJSON(w, 200, map[string]any{
		"items":      items,
		"poll_error": pollErr,
		"up_total":   tr.UpTotal,
		"down_total": tr.DownTotal,
	})
}

// handleConnectionsClose 断开内核当前全部活动连接。
func (d *deps) handleConnectionsClose(w http.ResponseWriter, r *http.Request) {
	if !d.requireRunning(w) {
		return
	}
	if err := d.mgr.CloseAllConnections(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleRules 返回内核实际加载的路由规则列表。
func (d *deps) handleRules(w http.ResponseWriter, r *http.Request) {
	if !d.requireRunning(w) {
		return
	}
	rules, err := d.mgr.Rules(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if rules == nil {
		rules = []core.RuleItem{}
	}
	writeJSON(w, 200, map[string]any{"rules": rules})
}

// handleCustomRulesGet 返回用户自定义规则（合成后的 clash 规则串，置顶并入运行时配置）。
func (d *deps) handleCustomRulesGet(w http.ResponseWriter, r *http.Request) {
	rules := d.cfg.CustomRules()
	if rules == nil {
		rules = []string{}
	}
	writeJSON(w, 200, map[string]any{"rules": rules})
}

// handleCustomRulesPut 覆写用户自定义规则；内核在运行时自动重启加载新规则。
func (d *deps) handleCustomRulesPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rules []string `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, errStr("请求体不是合法的规则 JSON"))
		return
	}
	if err := d.cfg.SetCustomRules(body.Rules); err != nil {
		writeErr(w, 500, err)
		return
	}
	restarted := false
	var restartErr string
	if d.mgr.Running() {
		if err := d.mgr.Restart(); err != nil {
			restartErr = err.Error()
		} else {
			restarted = true
		}
	}
	writeJSON(w, 200, map[string]any{"ok": restartErr == "", "restarted": restarted, "error": restartErr})
}

// ---- 代理 ----

func (d *deps) handleProxies(w http.ResponseWriter, r *http.Request) {
	if !d.requireRunning(w) {
		return
	}
	list, err := d.mgr.Proxies(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, 200, map[string]any{"proxies": list})
}

func (d *deps) handleSelectProxy(w http.ResponseWriter, r *http.Request) {
	if !d.requireRunning(w) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErr(w, 400, errStr("缺少 name 字段"))
		return
	}
	if err := d.mgr.SelectProxy(r.Context(), r.PathValue("group"), body.Name); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (d *deps) handleGroupDelay(w http.ResponseWriter, r *http.Request) {
	if !d.requireRunning(w) {
		return
	}
	q := r.URL.Query()
	timeout, _ := strconv.Atoi(q.Get("timeout"))
	if timeout <= 0 || timeout > 10000 {
		timeout = 5000
	}
	testURL := q.Get("url")
	if testURL == "" {
		testURL = "https://www.gstatic.com/generate_204"
	}
	out, err := d.mgr.GroupDelay(r.Context(), r.PathValue("group"), testURL, timeout)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, 200, out)
}

func (d *deps) handleProxyDelay(w http.ResponseWriter, r *http.Request) {
	if !d.requireRunning(w) {
		return
	}
	q := r.URL.Query()
	timeout, _ := strconv.Atoi(q.Get("timeout"))
	if timeout <= 0 || timeout > 10000 {
		timeout = 5000
	}
	testURL := q.Get("url")
	if testURL == "" {
		testURL = "https://www.gstatic.com/generate_204"
	}
	out, err := d.mgr.ProxyDelay(r.Context(), r.PathValue("name"), testURL, timeout)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, 200, out)
}

// ---- 订阅 ----

func (d *deps) handleProfileList(w http.ResponseWriter, r *http.Request) {
	list, err := d.prof.List()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	s, _ := d.cfg.Get()
	if list == nil {
		list = []profiles.Profile{}
	}
	writeJSON(w, 200, map[string]any{"profiles": list, "active": s.ActiveProfile})
}

func (d *deps) handleProfileAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		UA       string `json:"ua"`
		Template string `json:"template"` // 手动节点方式：注入用的模板名
		Nodes    string `json:"nodes"`    // 手动节点方式：多行节点链接/base64
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, errStr("请求体不是合法 JSON"))
		return
	}
	// 手动节点方式：转换节点注入模板生成订阅，无需 URL
	if body.Nodes != "" {
		p, skipped, err := d.prof.AddNodes(body.Name, body.Template, body.Nodes)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		d.afterProfileAdded(p.ID)
		writeJSON(w, 200, addResp{Profile: p, Skipped: skipped})
		return
	}
	if body.URL == "" {
		writeErr(w, 400, errStr("缺少 url 字段"))
		return
	}
	p, err := d.prof.Add(body.Name, body.URL, body.UA)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	// 记住本次使用的 UA（含内置），下次打开弹窗可继续使用
	if body.UA != "" {
		_ = d.cfg.Update(func(u *config.Settings) { u.CustomUA = body.UA })
	}
	d.afterProfileAdded(p.ID)
	writeJSON(w, 200, addResp{Profile: p})
}

// afterProfileAdded 首个订阅自动激活。
func (d *deps) afterProfileAdded(id string) {
	s, _ := d.cfg.Get()
	if s.ActiveProfile == "" {
		_ = d.cfg.Update(func(u *config.Settings) { u.ActiveProfile = id })
	}
}

// addResp 订阅添加结果：内嵌 Profile 字段平铺输出，skipped 为节点
// 转换中解析失败被跳过的行提示（URL 订阅恒为空且不输出）。
type addResp struct {
	profiles.Profile
	Skipped []string `json:"skipped,omitempty"`
}

// handleCoreConfig 返回当前合成给 mihomo 的运行时配置（config.yaml）。
func (d *deps) handleCoreConfig(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(d.cfg.RuntimeConfigPath())
	if err != nil {
		writeErr(w, 404, errStr("运行时配置不存在，内核启动后生成"))
		return
	}
	writeJSON(w, 200, map[string]any{"content": string(data)})
}

// handleProfileContentGet 返回订阅原文件内容（本地手工编辑用）。
func (d *deps) handleProfileContentGet(w http.ResponseWriter, r *http.Request) {
	data, err := d.prof.Content(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	writeJSON(w, 200, map[string]any{"content": string(data)})
}

// handleProfileContentPut 保存本地编辑后的订阅原文件；不自动重启内核，由前端询问。
func (d *deps) handleProfileContentPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 10<<20+64<<10)).Decode(&body); err != nil {
		writeErr(w, 400, errStr("请求体不是合法 JSON"))
		return
	}
	if err := d.prof.SaveContent(r.PathValue("id"), []byte(body.Content)); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"saved": true})
}

func (d *deps) handleProfileUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := d.prof.Update(id)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	// 更新的是当前激活订阅且内核在跑 → 重启生效
	s, _ := d.cfg.Get()
	restarted := false
	if s.ActiveProfile == id && d.mgr.Running() {
		if err := d.mgr.Restart(); err == nil {
			restarted = true
		}
	}
	writeJSON(w, 200, map[string]any{"profile": p, "restarted": restarted})
}

func (d *deps) handleProfileActivate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := d.prof.Get(id); err != nil {
		writeErr(w, 404, err)
		return
	}
	if err := d.cfg.Update(func(u *config.Settings) { u.ActiveProfile = id }); err != nil {
		writeErr(w, 500, err)
		return
	}
	running := d.mgr.Running()
	var startErr string
	if running {
		if err := d.mgr.Restart(); err != nil {
			startErr = err.Error()
		}
	}
	writeJSON(w, 200, map[string]any{"ok": startErr == "", "error": startErr, "restarted": running})
}

// handleProfileEdit 修改订阅（名称/地址/UA）；地址或 UA 变化时自动重新下载，
// 该订阅处于激活且内核在跑时自动重启生效。
func (d *deps) handleProfileEdit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		UA       string `json:"ua"`
		Template string `json:"template"` // 手动节点订阅：模板名
		Nodes    string `json:"nodes"`    // 手动节点订阅：节点文本（留空沿用）
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, errStr("请求体不是合法 JSON"))
		return
	}
	// 手动节点订阅：改名/换模板/改节点，内容有变重新生成
	if cur, err := d.prof.Get(id); err == nil && cur.Source == "nodes" {
		p, changed, skipped, err := d.prof.EditNodes(id, body.Name, body.Template, body.Nodes)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		s, _ := d.cfg.Get()
		restarted := false
		if changed && s.ActiveProfile == id && d.mgr.Running() {
			if err := d.mgr.Restart(); err == nil {
				restarted = true
			}
		}
		writeJSON(w, 200, map[string]any{"profile": p, "redownloaded": changed, "restarted": restarted, "skipped": skipped})
		return
	}
	p, redownloaded, err := d.prof.Edit(id, body.Name, body.URL, body.UA)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	s, _ := d.cfg.Get()
	restarted := false
	if redownloaded && s.ActiveProfile == id && d.mgr.Running() {
		if err := d.mgr.Restart(); err == nil {
			restarted = true
		}
	}
	writeJSON(w, 200, map[string]any{"profile": p, "redownloaded": redownloaded, "restarted": restarted})
}

func (d *deps) handleProfileDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, _ := d.cfg.Get()
	if s.ActiveProfile == id {
		_ = d.cfg.Update(func(u *config.Settings) { u.ActiveProfile = "" })
		if d.mgr.Running() {
			_ = d.mgr.Stop()
		}
	}
	if err := d.prof.Delete(id); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// subUpdResult 是单个订阅的批量更新结果。
type subUpdResult struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// updateAllProfiles 依次更新全部订阅；激活订阅有变化且内核在跑时只重启一次。
// updating 原子标记防止定时任务与手动「全部更新」并发重跑。
func (d *deps) updateAllProfiles() (results []subUpdResult, restarted bool, restartErr string) {
	if !d.updating.CompareAndSwap(false, true) {
		return nil, false, "已有更新任务在进行中"
	}
	defer d.updating.Store(false)

	list, err := d.prof.List()
	if err != nil {
		return nil, false, err.Error()
	}
	s, _ := d.cfg.Get()
	activeUpdated := false
	for _, p := range list {
		res := subUpdResult{ID: p.ID, Name: p.Name}
		if _, err := d.prof.Update(p.ID); err != nil {
			res.Error = err.Error()
			slog.Warn("订阅更新失败", "name", p.Name, "err", err)
		} else {
			res.OK = true
			slog.Info("订阅已更新", "name", p.Name)
			if p.ID == s.ActiveProfile {
				activeUpdated = true
			}
		}
		results = append(results, res)
	}
	if activeUpdated && d.mgr.Running() {
		if err := d.mgr.Restart(); err != nil {
			restartErr = err.Error()
			slog.Warn("订阅更新后重启内核失败", "err", err)
		} else {
			restarted = true
		}
	}
	return results, restarted, restartErr
}

// handleProfilesUpdateAll 一键更新全部订阅。
func (d *deps) handleProfilesUpdateAll(w http.ResponseWriter, r *http.Request) {
	results, restarted, restartErr := d.updateAllProfiles()
	if results == nil && restartErr != "" {
		writeErr(w, http.StatusConflict, errStr(restartErr))
		return
	}
	okN, failN := 0, 0
	for _, res := range results {
		if res.OK {
			okN++
		} else {
			failN++
		}
	}
	writeJSON(w, 200, map[string]any{
		"results":    results,
		"ok_count":   okN,
		"fail_count": failN,
		"restarted":  restarted,
		"error":      restartErr,
	})
}

// handleScheduleGet 返回订阅定时更新配置（星期列表 + 时间点）。
func (d *deps) handleScheduleGet(w http.ResponseWriter, r *http.Request) {
	s, err := d.cfg.Get()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeSchedule(w, s.AutoUpdateDays, s.AutoUpdateTime)
}

// handleSchedulePut 保存订阅定时更新配置；启用时要求至少选一天且时间合法。
func (d *deps) handleSchedulePut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool   `json:"enabled"`
		Days    []int  `json:"days"`
		Time    string `json:"time"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, errStr("请求体不是合法 JSON"))
		return
	}
	days := config.EncodeWeekdays(body.Days)
	if body.Enabled {
		if days == "" {
			writeErr(w, 400, errStr("请至少选择一个星期"))
			return
		}
		if _, _, ok := config.ParseHHMM(body.Time); !ok {
			writeErr(w, 400, errStr("时间格式应为 HH:mm"))
			return
		}
	}
	if err := d.cfg.Update(func(u *config.Settings) {
		u.AutoUpdateDays = days
		if _, _, ok := config.ParseHHMM(body.Time); ok {
			u.AutoUpdateTime = body.Time
		}
	}); err != nil {
		writeErr(w, 500, err)
		return
	}
	s, _ := d.cfg.Get()
	slog.Info("订阅定时更新配置已保存", "enabled", s.AutoUpdateDays != "", "days", s.AutoUpdateDays, "time", s.AutoUpdateTime)
	writeSchedule(w, s.AutoUpdateDays, s.AutoUpdateTime)
}

func writeSchedule(w http.ResponseWriter, days, tm string) {
	writeJSON(w, 200, map[string]any{
		"enabled": days != "",
		"days":    config.ParseWeekdays(days),
		"time":    tm,
	})
}

// ---- 设置 ----

func (d *deps) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	s, err := d.cfg.Get()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, s)
}

func (d *deps) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var body config.Settings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, errStr("请求体不是合法的设置 JSON"))
		return
	}
	old, _ := d.cfg.Get()
	err := d.cfg.Update(func(u *config.Settings) {
		*u = body
		// 不允许通过该接口改动的东西
		u.ActiveProfile = old.ActiveProfile
		if u.ControllerSecret == "" {
			u.ControllerSecret = old.ControllerSecret
		}
	})
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	s, _ := d.cfg.Get()

	// 内核相关设置变化且内核在运行 → 自动重启生效。
	// DNS 劫持族（v4/v6）在 TUN 下写进 yaml 的 dns-hijack，也必须重启；
	// 非 TUN 只影响防火墙规则，走下方钩子重建即可。
	hijackChanged := old.DNSHijack != s.DNSHijack ||
		old.DNSHijackIPv4 != s.DNSHijackIPv4 || old.DNSHijackIPv6 != s.DNSHijackIPv6
	coreChanged := old.MixedPort != s.MixedPort ||
		old.TUN != s.TUN || old.TUNStack != s.TUNStack || old.DNS != s.DNS ||
		old.DNSMode != s.DNSMode || old.CoreMemLimit != s.CoreMemLimit ||
		old.ControllerPort != s.ControllerPort || old.CoreChannel != s.CoreChannel ||
		(hijackChanged && s.TUN)
	// 渠道切换后目标渠道还没有内核文件时不能重启（启动会失败），保持现状
	// 等用户下载完成（下载成功后内核在运行中会自动重启到新渠道）
	channelMissing := old.CoreChannel != s.CoreChannel && !coreChannelInstalled(d.cfg.CorePath())
	restarted := false
	var restartErr string
	if coreChanged && !channelMissing && d.mgr.Running() {
		if err := d.mgr.Restart(); err != nil {
			restartErr = err.Error()
		} else {
			restarted = true
		}
	}
	// 仅 DNS 劫持变化 → 不必重启内核，立即切换
	if hijackChanged && !coreChanged {
		if d.mgr.Running() {
			go d.mgr.ApplyTrafficHooks(s)
		} else {
			// 内核没跑时规则绝不能留着：透明代理指向死端口会断 LAN 上网
			go d.mgr.RemoveTrafficHooks()
		}
	}
	writeJSON(w, 200, map[string]any{
		"settings":             s,
		"restarted":            restarted,
		"error":                restartErr,
		"need_reload":          old.UIPort != s.UIPort || old.Token != s.Token,
		"core_channel_missing": channelMissing,
	})
}

// coreChannelInstalled 检查指定路径的内核二进制是否存在且为普通文件。
func coreChannelInstalled(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

// ---- 内核控制 ----

func (d *deps) coreStart(ctx context.Context) (any, error) {
	if err := d.mgr.Start(); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (d *deps) coreStop(ctx context.Context) (any, error) {
	if err := d.mgr.Stop(); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (d *deps) coreRestart(ctx context.Context) (any, error) {
	if err := d.mgr.Restart(); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (d *deps) handleCoreStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	writeJSON(w, 200, d.mgr.CoreStatus(ctx))
}

func (d *deps) handleCoreLatest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cur := d.mgr.CoreStatus(ctx)
	latest, err := d.mgr.LatestCore(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"current":    cur.Version,
		"latest":     latest,
		"has_update": normalizeVer(latest) != normalizeVer(cur.Version) || !cur.Installed,
	})
}

func (d *deps) handleCoreUpgrade(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	version, err := d.mgr.UpgradeCore(ctx)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"version": version})
}

// maxCoreUploadSize 手动上传内核的大小上限，与 core 包下载上限（maxAssetSize）同值。
const maxCoreUploadSize = 256 << 20

// handleCoreUpload 手动上传内核（multipart/form-data，字段名 file）。内容支持
// 原始二进制或 mihomo Release 的 .gz 压缩包（后端按文件头识别）；安装到哪个
// 渠道跟随当前设置的 core_channel，文件本身是 Release 还是 Alpha 不校验。
// 校验（试运行）通过才替换现有内核，失败不影响现有内核。返回 {version}。
func (d *deps) handleCoreUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCoreUploadSize)
	file, _, err := r.FormFile("file")
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeErr(w, http.StatusBadRequest, errStr("文件过大，请上传 256MB 以内的内核文件"))
			return
		}
		writeErr(w, http.StatusBadRequest, errStr("请用 multipart/form-data 的 file 字段携带内核文件"))
		return
	}
	defer file.Close()
	version, err := d.mgr.UploadCore(r.Context(), file)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, 200, map[string]any{"version": version})
}

// handleUpgradeProgress 查询当前升级任务（内核/插件）的下载进度。
func (d *deps) handleUpgradeProgress(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, d.mgr.Progress())
}

// handleUpgradeCancel 取消当前进行中的升级任务，下载循环随即中断。
func (d *deps) handleUpgradeCancel(w http.ResponseWriter, r *http.Request) {
	if !d.mgr.CancelUpgrade() {
		writeErr(w, 409, errors.New("当前没有进行中的升级任务"))
		return
	}
	writeJSON(w, 200, map[string]bool{"cancelled": true})
}

// handleLogs 返回日志文件尾部内容。kind=core|plugin，bytes 限制返回大小。
func (d *deps) handleLogs(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	name := "core.log"
	if kind == "plugin" {
		name = "clashv.log"
	}
	maxBytes := int64(128 << 10)
	if n, err := strconv.ParseInt(r.URL.Query().Get("bytes"), 10, 64); err == nil && n > 0 && n <= 512<<10 {
		maxBytes = n
	}
	path := filepath.Join(d.cfg.LogDir(), name)
	f, err := os.Open(path)
	if err != nil {
		writeJSON(w, 200, map[string]any{"content": "", "size": 0, "exists": false})
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	size := st.Size()
	// 只读尾部 maxBytes，避免大文件全量进内存
	if size > maxBytes {
		_, _ = f.Seek(size-maxBytes, io.SeekStart)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes))
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"content":   string(data),
		"size":      size,
		"exists":    true,
		"truncated": size > maxBytes,
	})
}

// handleLogsClear 清空指定日志（kind=core|plugin）。用截断而非删除：两个日志的
// 写入方都持有 O_APPEND fd（内核日志由本插件接管内核 stdout，插件日志是本进程
// 自写），截断后下次写入自动从文件头继续，与 LogJanitor 的 copytruncate 轮转
// 是同一安全模型，内核运行中也可执行。
func (d *deps) handleLogsClear(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "core" && kind != "plugin" {
		writeErr(w, 400, errStr("kind 必须是 core 或 plugin"))
		return
	}
	name := "core.log"
	if kind == "plugin" {
		name = "clashv.log"
	}
	path := filepath.Join(d.cfg.LogDir(), name)
	if err := os.Truncate(path, 0); err != nil && !os.IsNotExist(err) {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// normalizeVer 去掉版本号前缀 v，便于比较。
func normalizeVer(v string) string {
	if len(v) > 0 && v[0] == 'v' {
		return v[1:]
	}
	return v
}

// ---- 插件 ----

func (d *deps) handlePluginLatest(w http.ResponseWriter, r *http.Request) {
	// 比最新 Release 多查一次当前版本的 tags 接口，超时相应放宽
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rel, err := d.mgr.LatestPluginRelease(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	curID := int64(0)
	if d.ver != "dev" {
		curID = d.mgr.InstalledPluginReleaseID(ctx, d.ver)
	}
	hasUpdate, by := pluginHasUpdate(d.ver, curID, rel.ID, rel.TagName)
	writeJSON(w, 200, map[string]any{
		"current":    d.ver,
		"latest":     rel.TagName,
		"current_id": curID,
		"latest_id":  rel.ID,
		"compare_by": by, // id / tag / dev：has_update 的判定依据，设备上排查用
		"has_update": hasUpdate,
		// 当前设备将下载的 ipk/apk 包（非 OpenWrt 为 null）
		"pkg": d.mgr.PluginPackagePlan(),
	})
}

// pluginHasUpdate 判断插件是否可更新。优先比较 Release 数字 ID——GitHub 的
// Release ID 按创建顺序单调递增，能分清新旧方向、不受版本号格式影响；本地
// 版本的 Release 查不到（自编译版本、tag 没发 Release）时退回版本号拆段比较。
func pluginHasUpdate(ver string, curID, latestID int64, latestTag string) (bool, string) {
	if ver == "dev" {
		return true, "dev"
	}
	if curID > 0 && latestID > 0 {
		return latestID > curID, "id"
	}
	return compareVersions(latestTag, ver) > 0, "ver"
}

// compareVersions 版本号按点拆段转数字比较，返回 -1/0/1：字符串直接比会出现
// "0.1.2" > "0.1.13" 的字典序误判，必须拆段。取每段前导数字（"rc1" 视为 0），
// 段数不足补 0（1.0 与 1.0.0 相等），前缀 v 容忍。
func compareVersions(a, b string) int {
	as, bs := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	seg := func(s []string, i int) string {
		if i < len(s) {
			return s[i]
		}
		return ""
	}
	num := func(s string) int64 {
		var n int64
		for _, c := range s {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int64(c-'0')
		}
		return n
	}
	for i := 0; i < len(as) || i < len(bs); i++ {
		av, bv := num(seg(as, i)), num(seg(bs, i))
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		}
	}
	return 0
}

func (d *deps) handlePluginUpgrade(w http.ResponseWriter, r *http.Request) {
	if d.cfg.IsOpenWrt() {
		// OpenWrt 走 ipk/apk 包流程：接口只负责下载到 /tmp，响应返回后再在
		// 后台安装——安装末尾 postinst 会重启服务杀掉本进程，响应必须先发出。
		// 结果靠前端等服务失联再恢复来确认，安装失败记录在升级进度里。
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
		defer cancel()
		version, pkg, file, err := d.mgr.PreparePluginUpgrade(ctx)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		go func() {
			if err := d.mgr.InstallDownloadedPlugin(pkg, file); err != nil {
				slog.Warn("插件在线更新安装失败", "pkg", pkg.Name, "err", err)
			}
		}()
		writeJSON(w, 200, map[string]any{"version": version, "need_restart": false, "pkg": pkg})
		return
	}
	// 非 OpenWrt：下载裸二进制替换自身，需手动重启服务
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	version, needRestart, err := d.mgr.UpgradePlugin(ctx)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"version": version, "need_restart": needRestart})
}

func (d *deps) handleServiceRestart(w http.ResponseWriter, r *http.Request) {
	if err := d.mgr.RestartService(); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- 恢复出厂 ----

// handlePluginReset 把除订阅配置（含当前激活项）外的全部数据恢复到安装初始
// 状态：停内核、清自定义规则/运行状态文件/日志，设置回默认值。
// 内核二进制与订阅文件是用户资产，不删。
func (d *deps) handlePluginReset(w http.ResponseWriter, r *http.Request) {
	// 先停内核（Stop 内部会撤 DNS 劫持/透明代理钩子），失败不阻断重置
	if err := d.mgr.Stop(); err != nil {
		slog.Warn("恢复出厂：停止内核失败，继续重置", "err", err)
	}
	// 运行状态与内核缓存（core.state 运行记忆 / cache.db 选中节点与 fakeip
	// 映射）、旧运行时配置：下次启动全部按默认重新生成
	for _, f := range []string{"core.state", "cache.db", "config.yaml"} {
		if err := os.Remove(filepath.Join(d.cfg.Home(), f)); err != nil && !os.IsNotExist(err) {
			slog.Warn("恢复出厂：清理状态文件失败", "file", f, "err", err)
		}
	}
	// 自定义规则
	if err := os.Remove(d.cfg.CustomRulesPath()); err != nil && !os.IsNotExist(err) {
		slog.Warn("恢复出厂：删除自定义规则失败", "err", err)
	}
	// 日志（诊断数据一并清空）
	if entries, err := os.ReadDir(d.cfg.LogDir()); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
				continue
			}
			if err := os.Remove(filepath.Join(d.cfg.LogDir(), e.Name())); err != nil {
				slog.Warn("恢复出厂：清理日志失败", "file", e.Name(), "err", err)
			}
		}
	}
	// 设置回默认；订阅选择与数据目录保留
	if err := d.cfg.Update(func(u *config.Settings) {
		keepProfile, keepWorkdir := u.ActiveProfile, u.WorkDir
		*u = config.Defaults()
		u.ActiveProfile = keepProfile
		u.WorkDir = keepWorkdir
	}); err != nil {
		writeErr(w, 500, err)
		return
	}
	slog.Info("已恢复出厂设置（订阅配置保留）")
	s, _ := d.cfg.Get()
	writeJSON(w, 200, map[string]any{"ok": true, "settings": s})
}

// ---- 备份与恢复 ----

func (d *deps) handleBackupList(w http.ResponseWriter, r *http.Request) {
	list, err := d.mgr.ListBackups()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"backups": list})
}

func (d *deps) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, errStr("请求体不是合法的 JSON"))
		return
	}
	info, err := d.mgr.CreateBackup(body.Name)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "backup": info})
}

func (d *deps) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, errStr("请求体不是合法的 JSON"))
		return
	}
	restarted, err := d.mgr.RestoreBackup(body.Name)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	slog.Info("备份恢复完成", "name", body.Name, "restarted", restarted)
	s, _ := d.cfg.Get()
	writeJSON(w, 200, map[string]any{"ok": true, "restarted": restarted, "settings": s})
}

func (d *deps) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	if err := d.mgr.DeleteBackup(r.PathValue("name")); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
