package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"clashv/internal/config"
	"clashv/internal/core"
	"clashv/internal/tz"
)

// setupLogger 把 slog 输出同时写到 stderr（procd → syslog）和
// <workdir>/logs/clashv.log（界面「日志」页读取）。超过 8MB 时启动轮转，
// 运行期间的持续轮转由 core.LogJanitor 负责。
//
// OpenWrt 通常没有 zoneinfo，Go 拿不到本地时区会按 UTC 记日志；
// 这里解析 /etc/TZ、uci zonename 等兜底，让日志时间与系统一致。
func setupLogger(cfg *config.Manager) {
	time.Local = tz.Resolve()
	logPath := filepath.Join(cfg.LogDir(), "clashv.log")
	core.RotateAtOpen(logPath, core.LogMaxBytes)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		// 文件打不开（如只读 fs）时退回纯 stderr，不影响服务
		return
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, f), &slog.HandlerOptions{
		// 时间用「年-月-日 时:分:秒.毫秒」，替代 slog 默认的 RFC3339
		// （time=2026-09-30T12:52:33.180+08:00），与内核日志在界面上的
		// 展示格式保持一致
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				if t, ok := a.Value.Any().(time.Time); ok {
					a.Value = slog.StringValue(t.Format("2006-01-02 15:04:05.000"))
				}
			}
			return a
		},
	})))
}
