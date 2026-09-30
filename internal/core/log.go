package core

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clashv/internal/config"
)

// LogMaxBytes 单个日志文件的轮转阈值（当前档 + 一份旧档，单文件上限 2×LogMaxBytes）。
const LogMaxBytes = 8 << 20

// oldLogRetention 旧档保留时长：超过即删除，避免长期运行慢慢占满存储。
const oldLogRetention = 7 * 24 * time.Hour

// RotateAtOpen 在（重新）打开日志文件前调用：超过 max 字节时把当前日志挪到
// .old（仅保留一份旧档）。此时写入方尚未打开文件，直接重命名即可。
func RotateAtOpen(path string, max int64) {
	st, err := os.Stat(path)
	if err != nil || st.Size() < max {
		return
	}
	_ = os.Remove(path + ".old")
	_ = os.Rename(path, path+".old")
}

// rotateLive 对正被写入方追加（O_APPEND）的日志做轮转：复制到 .old 后截断原文件。
// 不能用重命名——写入进程的 fd 跟着 inode 走，重命名后日志会一直写进旧档，
// 新文件永远是空的；而 O_APPEND fd 在截断后下次写入会自动从文件头继续。
func rotateLive(path string, max int64) {
	st, err := os.Stat(path)
	if err != nil || st.Size() < max {
		return
	}
	in, err := os.Open(path)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.OpenFile(path+".old", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return // 复制失败不动原文件：宁可超限也不丢日志
	}
	_ = out.Close()
	_ = os.Truncate(path, 0)
}

// cleanOldLogs 删除 logs 目录里超过保留期的 .old 旧档。
func cleanOldLogs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-oldLogRetention)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".old") {
			continue
		}
		if info, err := e.Info(); err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// LogJanitor 常驻清理日志占用：每 10 分钟按大小轮转内核/插件日志（copytruncate，
// 内核运行中也能转），并删除超过保留期的 .old 旧档。阻塞运行，用 go LogJanitor(cfg) 启动。
func LogJanitor(cfg *config.Manager) {
	dir := cfg.LogDir()
	coreLog := filepath.Join(dir, "core.log")
	pluginLog := filepath.Join(dir, "clashv.log")
	cleanOldLogs(dir)
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		rotateLive(coreLog, LogMaxBytes)
		rotateLive(pluginLog, LogMaxBytes)
		cleanOldLogs(dir)
	}
}
