package core

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// 内核进程残留（孤儿）防护：
//
// procd respawn 插件、Start 中途失败、内核被 SIGKILL 等场景都会留下仍在运行的
// mihomo。孤儿占住 controller/redir/DNS 端口后，新内核绑不上端口但进程照常
// 存活，本插件会从此与旧实例对话——同密钥同配置，接口照样应答，表现为
// 「代理看起来正常，连接列表为空、测速全部失败」。这里的清扫按内核二进制
// 完整路径精确匹配，不会影响 OpenClash 等其他插件自己的内核。

// sweepOrphanCores 杀掉所有正在运行 corePath 这个二进制的进程（不含自身）。
// 返回杀掉的个数。
func sweepOrphanCores(corePath string) int {
	pids := findCorePids(corePath)
	if len(pids) == 0 {
		return 0
	}
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	// 给 3 秒优雅退出，顽固的补 SIGKILL
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(findCorePids(corePath)) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, pid := range findCorePids(corePath) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	// LISTEN 端口随进程退出立即释放；留 200ms 让内核态收敛
	time.Sleep(200 * time.Millisecond)
	return len(pids)
}

// findCorePids 找出正在运行 corePath 的进程号（Linux 读 /proc，
// 其他平台退回 pgrep -f）。
func findCorePids(corePath string) []int {
	if entries, err := os.ReadDir("/proc"); err == nil {
		var pids []int
		self := os.Getpid()
		for _, e := range entries {
			pid, err := strconv.Atoi(e.Name())
			if err != nil || pid == self {
				continue
			}
			if procRunsCore(pid, corePath) {
				pids = append(pids, pid)
			}
		}
		return pids
	}
	return pgrepCore(corePath)
}

// procRunsCore 判断 pid 是否在运行 corePath：优先 exe 软链（二进制升级后
// 会带 " (deleted)" 后缀，需去掉再比），失败退回 cmdline 的 argv[0]。
func procRunsCore(pid int, corePath string) bool {
	if link, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
		if link == corePath || strings.TrimSuffix(link, " (deleted)") == corePath {
			return true
		}
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	argv0, _, _ := bytes.Cut(data, []byte{0})
	return string(argv0) == corePath
}

func pgrepCore(corePath string) []int {
	out, err := exec.Command("pgrep", "-f", corePath).Output()
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if pid, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && pid != self {
			pids = append(pids, pid)
		}
	}
	return pids
}

// checkCoreBindErrors 检查内核本次启动新增日志里的端口绑定错误。
// mihomo 某个监听端口被占时只在日志里报错并继续运行，流量进不来，
// 这里把它变成显式启动错误。
func checkCoreBindErrors(path string, from int64) error {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(f, 256<<10))
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		lo := strings.ToLower(line)
		if strings.Contains(lo, "address already in use") ||
			strings.Contains(lo, "bind: permission denied") ||
			(strings.Contains(lo, "server error") && strings.Contains(lo, "listen")) {
			return errors.New(strings.TrimSpace(line))
		}
	}
	return nil
}

// killCoreProcess 结束刚拉起、但启动流程未完成的子进程（SIGTERM → 5s → SIGKILL），
// 避免留下孤儿。exited 为该进程的退出通知 channel。
func killCoreProcess(cmd *exec.Cmd, exited <-chan struct{}) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-exited
	}
}
