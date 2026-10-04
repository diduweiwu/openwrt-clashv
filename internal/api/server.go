// Package api 提供管理界面的 REST API 与前端静态资源。
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clashv/internal/config"
	"clashv/internal/core"
	"clashv/internal/profiles"
	"clashv/internal/web"
)

// deps 聚合各模块，供 handler 使用。
type deps struct {
	cfg      *config.Manager
	prof     *profiles.Manager
	mgr      *core.Manager
	ver      string
	updating atomic.Bool // 批量更新进行中标记（手动全部更新 / 定时任务共用）

	authMu   sync.Mutex              // 保护 authed
	authed   map[string]luciAuthed   // clashv_auth cookie 值 -> 已验证的 LuCI 会话
	ubusOnce sync.Once               // ubus 可用性只探测一次
	ubusOK   bool
}

// luciAuthed 记录一个通过校验的 LuCI 会话：cookie 值与上次 ubus 复验时间。
type luciAuthed struct {
	sid     string
	checked time.Time
}

// authCookieName 是校验通过后下发的本服务 cookie，与 LuCI 的 sysauth 无关。
const authCookieName = "clashv_auth"

// Serve 启动 HTTP 服务（阻塞）。
func Serve(cfg *config.Manager, prof *profiles.Manager, mgr *core.Manager, version string) error {
	d := &deps{cfg: cfg, prof: prof, mgr: mgr, ver: version}
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	// 上次运行可能残留 dnsmasq 转发/防火墙规则（持久化），内核未起时会把
	// LAN DNS 指向死端口、TCP 指向死监听
	go mgr.RemoveTrafficHooks()
	s, err := cfg.Get()
	if err != nil {
		return err
	}
	go d.scheduleLoop()

	mux := http.NewServeMux()

	// 状态与流量
	mux.HandleFunc("GET /api/status", d.handleStatus)
	mux.HandleFunc("GET /api/traffic", d.handleTraffic)
	mux.HandleFunc("GET /api/connections", d.handleConnections)
	mux.HandleFunc("DELETE /api/connections", d.handleConnectionsClose)
	mux.HandleFunc("GET /api/rules", d.handleRules)
	// 自定义规则：置顶并入运行时配置（放 /api/rules 前后皆可，Go 1.22 mux 按最具体路径匹配）
	mux.HandleFunc("GET /api/rules/custom", d.handleCustomRulesGet)
	mux.HandleFunc("PUT /api/rules/custom", d.handleCustomRulesPut)

	// 代理（转发 mihomo 控制接口）
	mux.HandleFunc("GET /api/proxies", d.handleProxies)
	mux.HandleFunc("PUT /api/proxies/{group}", d.handleSelectProxy)
	// 与内核 API 保持一致：单节点测速走 /api/proxies/{name}/delay，整组测速走
	// /api/group/{name}/delay。若整组也挂在 /api/proxies/{x}/delay 下，逐节点
	// 测速的 GET 会被它截走，节点名被当成组名导致内核返回 404 Resource not found。
	mux.HandleFunc("GET /api/proxies/{name}/delay", d.handleProxyDelay)
	mux.HandleFunc("GET /api/group/{name}/delay", d.handleGroupDelay)

	// 订阅
	mux.HandleFunc("GET /api/profiles", d.handleProfileList)
	mux.HandleFunc("POST /api/profiles", d.handleProfileAdd)
	mux.HandleFunc("POST /api/profiles/update_all", d.handleProfilesUpdateAll)
	mux.HandleFunc("GET /api/profiles/schedule", d.handleScheduleGet)
	mux.HandleFunc("PUT /api/profiles/schedule", d.handleSchedulePut)
	mux.HandleFunc("POST /api/profiles/{id}/update", d.handleProfileUpdate)
	mux.HandleFunc("POST /api/profiles/{id}/activate", d.handleProfileActivate)
	mux.HandleFunc("PUT /api/profiles/{id}", d.handleProfileEdit)
	mux.HandleFunc("DELETE /api/profiles/{id}", d.handleProfileDelete)

	// 设置
	mux.HandleFunc("GET /api/settings", d.handleSettingsGet)
	mux.HandleFunc("PUT /api/settings", d.handleSettingsPut)

	// 内核控制
	mux.HandleFunc("POST /api/core/start", d.wrap(d.coreStart))
	mux.HandleFunc("POST /api/core/stop", d.wrap(d.coreStop))
	mux.HandleFunc("POST /api/core/restart", d.wrap(d.coreRestart))
	mux.HandleFunc("GET /api/core/status", d.handleCoreStatus)
	mux.HandleFunc("GET /api/core/latest", d.handleCoreLatest)
	mux.HandleFunc("GET /api/core/config", d.handleCoreConfig)
	mux.HandleFunc("POST /api/core/upgrade", d.handleCoreUpgrade)
	mux.HandleFunc("GET /api/core/mode", d.handleCoreModeGet)
	mux.HandleFunc("PUT /api/core/mode", d.handleCoreModePut)

	// 插件自更新
	mux.HandleFunc("GET /api/plugin/latest", d.handlePluginLatest)
	mux.HandleFunc("POST /api/plugin/upgrade", d.handlePluginUpgrade)
	mux.HandleFunc("POST /api/plugin/reset", d.handlePluginReset)
	mux.HandleFunc("POST /api/service/restart", d.handleServiceRestart)

	// 备份与恢复
	mux.HandleFunc("GET /api/backup/list", d.handleBackupList)
	mux.HandleFunc("POST /api/backup/create", d.handleBackupCreate)
	mux.HandleFunc("POST /api/backup/restore", d.handleBackupRestore)
	mux.HandleFunc("DELETE /api/backup/{name}", d.handleBackupDelete)

	// 升级进度（内核/插件共用）
	mux.HandleFunc("GET /api/upgrade/progress", d.handleUpgradeProgress)

	// 日志（内核/插件）
	mux.HandleFunc("GET /api/logs", d.handleLogs)

	// 前端静态资源（SPA 回退到 index.html）
	dist := web.Dist()
	loadIndex(dist)
	fs := http.FileServerFS(dist)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" && path != "index.html" {
			if f, err := dist.Open(path); err == nil {
				f.Close()
				fs.ServeHTTP(w, r)
				return
			}
		}
		// SPA 路由回退
		serveIndex(w, r)
	})

	addr := fmt.Sprintf(":%d", s.UIPort)
	slog.Info("clashv 管理服务已启动", "addr", addr, "mode", map[bool]string{true: "uci", false: "file"}[cfg.IsOpenWrt()])
	server := &http.Server{Addr: addr, Handler: d.auth(mux), ReadHeaderTimeout: 10 * time.Second}
	return server.ListenAndServe()
}

// auth 管理界面访问鉴权，两层独立开关：
//  1) 访问令牌：设置了 Token 时，除本机访问外都要求 X-Clashv-Token；
//  2) OpenWrt 登录校验（luci_auth，默认开）：非本机请求需持有有效的 LuCI 登录会话。
//     LuCI 入口页会把 rpcd 会话 id 以 luci_sid 参数带进 iframe，校验通过后下发本服务
//     自己的 clashv_auth cookie（后续请求凭 cookie，sid 每 2 分钟经 ubus 复验）；
//     直接访问 路由器IP:9097 而未登录 LuCI 时返回 401。非 OpenWrt 环境（无 ubus）自动关闭。
func (d *deps) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, err := d.cfg.Get()
		if err == nil && !isLocal(r) {
			if s.Token != "" && r.Header.Get("X-Clashv-Token") != s.Token {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "访问令牌错误，请在登录框输入设置中配置的令牌"})
				return
			}
			if s.LuciAuth && d.cfg.IsOpenWrt() && d.hasUBUS() && !d.luciAuthed(w, r) {
				d.denyUnauthorized(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// luciAuthed 判断请求是否已通过 LuCI 登录校验：携带有效的 clashv_auth cookie，
// 或当场通过 URL 里的 luci_sid 校验并换发 cookie。
func (d *deps) luciAuthed(w http.ResponseWriter, r *http.Request) bool {
	d.authMu.Lock()
	if d.authed == nil {
		d.authed = make(map[string]luciAuthed)
	}
	d.authMu.Unlock()

	if c, err := r.Cookie(authCookieName); err == nil && c.Value != "" && d.sessionValid(c.Value) {
		return true
	}
	sid := r.URL.Query().Get("luci_sid")
	if sid == "" || !validLuciSession(sid) {
		return false
	}
	val := make([]byte, 16)
	if _, err := rand.Read(val); err != nil {
		return false
	}
	token := hex.EncodeToString(val)
	d.authMu.Lock()
	d.authed[token] = luciAuthed{sid: sid, checked: time.Now()}
	d.authMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: authCookieName, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 43200, // 12h，超时后靠 ubus 复验兜底
	})
	return true
}

// sessionValid 校验已换发的 cookie： sid 在 2 分钟内验过直接放行（界面每秒轮询，
// 不能每个请求都 fork ubus），过期则重新调 ubus 验证 LuCI 会话仍存活。
func (d *deps) sessionValid(token string) bool {
	d.authMu.Lock()
	e, ok := d.authed[token]
	if ok && time.Since(e.checked) < 2*time.Minute {
		d.authMu.Unlock()
		return true
	}
	d.authMu.Unlock()
	if !ok {
		return false
	}
	alive := validLuciSession(e.sid)
	d.authMu.Lock()
	if alive {
		d.authed[token] = luciAuthed{sid: e.sid, checked: time.Now()}
	} else {
		delete(d.authed, token) // LuCI 会话已失效，放行资格一并吊销
	}
	d.authMu.Unlock()
	return alive
}

// validLuciSession 用 ubus 校验 rpcd 会话 id 是否为有效的 LuCI 登录会话：
// 有效时返回体含 username 字段，无效（未登录/已过期/伪造）时 ubus 报 Not found。
func validLuciSession(sid string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ubus", "call", "session", "get",
		fmt.Sprintf(`{"ubus_rpc_session":%q}`, sid)).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), `"username"`)
}

// hasUBUS 探测 ubus 命令是否存在（OpenWrt 必有；macOS 开发机没有，跳过登录校验）。
func (d *deps) hasUBUS() bool {
	d.ubusOnce.Do(func() {
		_, err := exec.LookPath("ubus")
		d.ubusOK = err == nil
	})
	return d.ubusOK
}

// denyUnauthorized 未通过 OpenWrt 登录校验：API 回 JSON 供前端提示；
// 页面请求直接回一个自带样式的 401 页（此时静态资源也未放行，不能依赖 SPA）。
func (d *deps) denyUnauthorized(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "请先登录 OpenWrt 管理后台（LuCI），再从菜单进入 ClashV；或登录后刷新本页",
			"auth":  "luci",
		})
		return
	}
	luciHost := r.Host
	if i := hostRuneIndex(luciHost, ':'); i > 0 { // 去掉端口，LuCI 在 80/443
		luciHost = luciHost[:i]
	}
	luciURL := "http://" + luciHost + "/cgi-bin/luci/"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(w, `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>需要登录 OpenWrt</title></head>
<body style="margin:0;display:flex;align-items:center;justify-content:center;min-height:100vh;background:#14161c;color:#e8eaf0;font-family:system-ui,-apple-system,'PingFang SC','Microsoft YaHei',sans-serif">
<div style="max-width:420px;padding:32px;text-align:center;line-height:1.7">
<div style="font-size:40px">🔒</div>
<h1 style="font-size:18px;margin:12px 0 8px">需要登录 OpenWrt 管理后台</h1>
<p style="font-size:13.5px;color:#9aa0ae;margin:0 0 20px">ClashV 界面已开启登录校验：<br>请先登录 LuCI，再从菜单进入 ClashV，或登录后刷新本页。</p>
<a href="%s" style="display:inline-block;padding:9px 22px;border-radius:8px;background:#5b5bd6;color:#fff;text-decoration:none;font-size:13.5px">前往 LuCI 登录</a>
</div></body></html>`, luciURL)
}

func isLocal(r *http.Request) bool {
	host := r.RemoteAddr
	if i := hostRuneIndex(host, ':'); i > 0 {
		host = host[:i]
	}
	return host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

func hostRuneIndex(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// scheduleLoop 每 30 秒检查一次是否到达订阅定时更新时间。
// 命中条件：今天星期在配置内、当前时间落在「到点起 10 分钟」窗口内、今天还没跑过。
// 窗口外（如白天）重启服务不补跑，避免意外触发全量更新。
func (d *deps) scheduleLoop() {
	lastDate := ""
	for {
		time.Sleep(30 * time.Second)
		s, err := d.cfg.Get()
		if err != nil || s.AutoUpdateDays == "" {
			continue
		}
		now := time.Now()
		due := false
		for _, day := range config.ParseWeekdays(s.AutoUpdateDays) {
			if int(now.Weekday()) == day {
				due = true
				break
			}
		}
		if !due {
			continue
		}
		hh, mm, ok := config.ParseHHMM(s.AutoUpdateTime)
		if !ok {
			continue
		}
		sched := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
		if now.Before(sched) || now.After(sched.Add(10*time.Minute)) {
			continue
		}
		today := now.Format("2006-01-02")
		if today == lastDate {
			continue
		}
		lastDate = today
		slog.Info("到达订阅定时更新时间，开始更新全部订阅", "time", s.AutoUpdateTime)
		if _, restarted, restartErr := d.updateAllProfiles(); restartErr != "" {
			slog.Warn("订阅定时更新未完成", "err", restartErr)
		} else if restarted {
			slog.Info("订阅定时更新完成，内核已重载")
		}
	}
}
