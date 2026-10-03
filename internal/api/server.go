// Package api 提供管理界面的 REST API 与前端静态资源。
package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
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
}

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

// auth 管理界面访问令牌校验：设置了 Token 时，除本机访问外都要求 X-Clashv-Token。
func (d *deps) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, err := d.cfg.Get()
		if err == nil && s.Token != "" && !isLocal(r) {
			if r.Header.Get("X-Clashv-Token") != s.Token {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "访问令牌错误，请在登录框输入设置中配置的令牌"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
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
