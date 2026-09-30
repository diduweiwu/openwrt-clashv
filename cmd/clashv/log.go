package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"clashv/internal/config"
	"clashv/internal/tz"
)

// setupLogger 把 slog 输出同时写到 stderr（procd → syslog）和
// <workdir>/logs/clashv.log（界面「日志」页读取）。超过 8MB 时启动轮转。
//
// OpenWrt 通常没有 zoneinfo，Go 拿不到本地时区会按 UTC 记日志；
// 这里解析 /etc/TZ、uci zonename 等兜底，让日志时间与系统一致。
func setupLogger(cfg *config.Manager) {
	time.Local = tz.Resolve()
	logPath := filepath.Join(cfg.LogDir(), "clashv.log")
	rotateLog(logPath, 8<<20)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		// 文件打不开（如只读 fs）时退回纯 stderr，不影响服务
		return
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, f), nil)))
}

// rotateLog 超过 max 字节时把当前日志挪到 .old（仅保留一份旧档）。
func rotateLog(path string, max int64) {
	st, err := os.Stat(path)
	if err != nil || st.Size() < max {
		return
	}
	_ = os.Remove(path + ".old")
	_ = os.Rename(path, path+".old")
}
