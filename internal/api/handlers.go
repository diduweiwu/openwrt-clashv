package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"clashv/internal/config"
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
	writeJSON(w, 200, map[string]any{
		"running":         d.mgr.Running(),
		"pid":             d.mgr.PID(),
		"uptime":          uptime,
		"started_at":      startedAt.Unix(),
		"core":            cs,
		"plugin_version":  d.ver,
		"profile":         profileName,
		"ui_port":         s.UIPort,
		"mixed_port":      s.MixedPort,
		"tun":             s.TUN,
		"allow_lan":       s.AllowLAN,
		"dns":             s.DNS,
		"auto_update":     s.AutoUpdateHours,
		"openwrt":         d.cfg.IsOpenWrt(),
		"token_required":  s.Token != "",
	})
}

func (d *deps) handleTraffic(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, d.mgr.Traffic())
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
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		writeErr(w, 400, errStr("缺少 url 字段"))
		return
	}
	p, err := d.prof.Add(body.Name, body.URL)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	// 首个订阅自动激活
	s, _ := d.cfg.Get()
	if s.ActiveProfile == "" {
		_ = d.cfg.Update(func(u *config.Settings) { u.ActiveProfile = p.ID })
	}
	writeJSON(w, 200, p)
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

	// 内核相关设置变化且内核在运行 → 自动重启生效
	coreChanged := old.MixedPort != s.MixedPort || old.AllowLAN != s.AllowLAN ||
		old.TUN != s.TUN || old.TUNStack != s.TUNStack || old.DNS != s.DNS ||
		old.ControllerPort != s.ControllerPort
	restarted := false
	var restartErr string
	if coreChanged && d.mgr.Running() {
		if err := d.mgr.Restart(); err != nil {
			restartErr = err.Error()
		} else {
			restarted = true
		}
	}
	// 仅 DNS 劫持模式变化 → 不必重启内核，立即切换
	if old.DNSHijack != s.DNSHijack && !coreChanged {
		if d.mgr.Running() && s.DNS && s.DNSHijack != "off" {
			go d.mgr.ApplyDNSHijack(s)
		} else {
			go d.mgr.RemoveDNSHijack()
		}
	}
	writeJSON(w, 200, map[string]any{
		"settings":    s,
		"restarted":   restarted,
		"error":       restartErr,
		"need_reload": old.UIPort != s.UIPort || old.Token != s.Token,
	})
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

// handleUpgradeProgress 查询当前升级任务（内核/插件）的下载进度。
func (d *deps) handleUpgradeProgress(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, d.mgr.Progress())
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

// normalizeVer 去掉版本号前缀 v，便于比较。
func normalizeVer(v string) string {
	if len(v) > 0 && v[0] == 'v' {
		return v[1:]
	}
	return v
}

// ---- 插件 ----

func (d *deps) handlePluginLatest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	latest, err := d.mgr.LatestPlugin(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"current":    d.ver,
		"latest":     latest,
		"has_update": d.ver == "dev" || normalizeVer(latest) != normalizeVer(d.ver),
	})
}

func (d *deps) handlePluginUpgrade(w http.ResponseWriter, r *http.Request) {
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
