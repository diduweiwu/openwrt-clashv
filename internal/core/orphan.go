package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"clashv/internal/config"
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

// waitCoreServing 确认内核的服务端口已在接受连接且代理链路真实可用：
// 先逐个 TCP 拨测 mixed（HTTP/SOCKS 混合代理）、redir（透明代理重定向入口）
// 与内核 DNS 端口（mihomo 对 dns.listen 同时提供 TCP/UDP 服务）——控制器
// 就绪≠服务就绪，某个监听没起来时 mihomo 只在日志里报错并继续运行；端口
// 全通后再通过混合端口真实代理一个 HTTP 请求到本机控制器，验证
// 监听接受 → 隧道分发 → 规则匹配 → 出站拨号 → 响应返回整条链路。探测
// 目标是本机回环地址，运行时配置已置顶固定直连规则，因此结果不依赖
// 互联网与节点健康——只有内核自己真的能代理流量才算通过。
// 例外：出站为全局模式时内核绕过规则表把全部流量送 GLOBAL 组，回环
// 探测会被送往远程节点而误判失败，此时只做端口拨测。超时仍不通则报错
// （调用方会杀掉刚拉起的内核）。
func waitCoreServing(ctx context.Context, s config.Settings) error {
	ports := []string{fmt.Sprintf("127.0.0.1:%d", s.MixedPort), "127.0.0.1:" + redirPort}
	if s.DNS {
		ports = append(ports, "127.0.0.1:"+dnsListenPort)
	}
	global := config.NormalizeCoreMode(s.CoreMode) == "global"
	deadline := time.Now().Add(10 * time.Second)
	var lastAddr string
	var lastErr error
	for {
		reachable := true
		for _, addr := range ports {
			conn, err := (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext(ctx, "tcp", addr)
			if err != nil {
				lastAddr, lastErr = addr, err
				reachable = false
				break
			}
			_ = conn.Close()
		}
		if reachable {
			if global {
				return nil
			}
			if err := probeThroughProxy(ctx, s); err == nil {
				return nil
			} else {
				lastAddr, lastErr = fmt.Sprintf("127.0.0.1:%d", s.MixedPort), err
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("代理链路探测未通过（%s: %w）", lastAddr, lastErr)
		}
		select {
		case <-ctx.Done():
			return errors.New("内核启动中止")
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// probeThroughProxy 经混合端口真实代理一次 HTTP 请求访问本机控制器
// /version。能带回任何 HTTP 响应（含 401/5xx）即证明整条代理链路已通，
// 状态码属于控制器鉴权/自身问题，不是「内核不能代理流量」。
func probeThroughProxy(ctx context.Context, s config.Settings) error {
	target := fmt.Sprintf("http://127.0.0.1:%d/version", s.ControllerPort)
	proxyURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", s.MixedPort))
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			Proxy:             http.ProxyURL(proxyURL),
			DisableKeepAlives: true,
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.ControllerSecret)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	_ = resp.Body.Close()
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
