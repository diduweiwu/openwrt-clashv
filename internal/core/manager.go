// Package core 负责 mihomo 内核的完整生命周期：
// 生成运行时配置、启动/停止进程、与 external-controller 通信、流量采样、内核与插件自更新。
package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"clashv/internal/config"
	"clashv/internal/profiles"
)

// Traffic 是一次流量采样快照。
type Traffic struct {
	Up          int64   `json:"up"`          // 当前上传速率 B/s
	Down        int64   `json:"down"`        // 当前下载速率 B/s
	UpTotal     int64   `json:"up_total"`    // 累计上传
	DownTotal   int64   `json:"down_total"`  // 累计下载
	Connections int     `json:"connections"` // 活动连接数
	MemoryMB    float64 `json:"memory_mb"`   // 内核进程内存占用
}

// Manager 管理 mihomo 进程与其控制接口。
type Manager struct {
	cfg           *config.Manager
	prof          *profiles.Manager
	PluginVersion string

	mu        sync.Mutex
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	startedAt time.Time
	exited    chan struct{} // 内核进程退出时关闭

	running atomic.Bool

	hc *controllerClient

	progMu sync.Mutex
	prog   UpgradeProgress // 当前升级任务进度（同一时间至多一个）

	tmu      sync.Mutex
	traffic  Traffic
	conns    []ConnItem // 最近一次轮询的活动连接快照
	lastTot  [2]int64
	lastTime time.Time
	polling  atomic.Bool
}

// NewManager 创建内核管理器。
func NewManager(cfg *config.Manager, prof *profiles.Manager, pluginVersion string) *Manager {
	return &Manager{
		cfg:           cfg,
		prof:          prof,
		PluginVersion: pluginVersion,
		hc:            &controllerClient{},
	}
}

// Running 报告内核是否在运行。
func (m *Manager) Running() bool { return m.running.Load() }

// PID 返回内核进程号，未运行为 0。
func (m *Manager) PID() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil && m.cmd.Process != nil && m.running.Load() {
		return m.cmd.Process.Pid
	}
	return 0
}

// Start 拉起 mihomo：合成运行时配置 → 启动进程 → 等待控制接口就绪 → 开始流量采样。
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running.Load() {
		return nil
	}
	s, err := m.cfg.Get()
	if err != nil {
		return err
	}
	if err := m.cfg.EnsureDirs(); err != nil {
		return err
	}
	corePath := m.cfg.CorePath()
	if st, err := os.Stat(corePath); err != nil || st.IsDir() {
		return fmt.Errorf("内核未安装（%s 不存在），请到「设置 → 内核」下载", corePath)
	}
	if s.ControllerSecret == "" {
		sec, err := randomSecret()
		if err != nil {
			return err
		}
		s.ControllerSecret = sec
		if err := m.cfg.Update(func(u *config.Settings) { u.ControllerSecret = sec }); err != nil {
			return err
		}
	}
	active := s.ActiveProfile
	list, _ := m.prof.List()
	if active != "" {
		if _, err := m.prof.Get(active); err != nil {
			active = "" // 记录的订阅已被删除，回退到最新一份
		}
	}
	if active == "" && len(list) > 0 {
		active = list[0].ID
	}
	if active == "" {
		return errors.New("还没有可用订阅，请先到「订阅」页添加")
	}
	if err := m.buildRuntimeConfig(active, s); err != nil {
		return err
	}

	rotateLog(m.cfg.LogDir()+"/core.log", 8<<20)
	logFile, err := os.OpenFile(m.cfg.LogDir()+"/core.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.Command(corePath, "-d", m.cfg.Home(), "-f", m.cfg.RuntimeConfigPath())
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = sysProcAttr()
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("启动内核失败: %w", err)
	}
	m.cmd = cmd
	m.cancel = cancel
	m.startedAt = time.Now()
	m.exited = make(chan struct{})
	m.running.Store(true)
	m.resetTraffic()

	// 进程退出回收
	go func() {
		_ = cmd.Wait()
		close(m.exited)
		m.running.Store(false)
		m.polling.Store(false)
		slog.Info("mihomo 进程已退出")
	}()

	// 等待控制接口就绪
	m.hc.setEndpoint(s.ControllerPort, s.ControllerSecret)
	deadline := time.Now().Add(8 * time.Second)
	for {
		if ctx.Err() != nil {
			return errors.New("内核启动中止")
		}
		if !m.running.Load() {
			return errors.New("内核启动后立即退出，请查看日志（工作目录 logs/core.log）")
		}
		if _, err := m.hc.version(ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("内核控制接口 8 秒内未就绪，请查看日志（工作目录 logs/core.log）")
		}
		time.Sleep(300 * time.Millisecond)
	}
	m.startPolling(ctx)
	// DNS 劫持 + TCP 透明代理在内核就绪后异步套用
	go m.ApplyTrafficHooks(s)
	slog.Info("mihomo 已启动", "pid", cmd.Process.Pid, "profile", active)
	return nil
}

// Stop 停止 mihomo 进程。
func (m *Manager) Stop() error {
	// 先撤 DNS 劫持与透明代理规则，让 LAN 解析和转发立刻回退直连，再停内核
	m.RemoveTrafficHooks()
	m.mu.Lock()
	cmd, cancel := m.cmd, m.cancel
	m.mu.Unlock()
	m.running.Store(false)
	m.polling.Store(false)
	if cmd == nil || cmd.Process == nil {
		if cancel != nil {
			cancel()
		}
		return nil
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-m.exited:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-m.exited
	}
	if cancel != nil {
		cancel()
	}
	slog.Info("mihomo 已停止")
	return nil
}

// Restart 重启内核（会重新合成运行时配置）。
func (m *Manager) Restart() error {
	if err := m.Stop(); err != nil {
		return err
	}
	return m.Start()
}

// StartedAt 返回本次内核启动时间；未运行则为零值。
func (m *Manager) StartedAt() time.Time {
	if !m.running.Load() {
		return time.Time{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startedAt
}

// ---- 流量采样 ----

// Traffic 返回最新流量快照。
func (m *Manager) Traffic() Traffic {
	m.tmu.Lock()
	defer m.tmu.Unlock()
	return m.traffic
}

func (m *Manager) resetTraffic() {
	m.tmu.Lock()
	defer m.tmu.Unlock()
	m.traffic = Traffic{}
	m.conns = nil
	m.lastTot = [2]int64{}
	m.lastTime = time.Time{}
}

// Connections 返回内核当前活动连接快照（随流量每秒轮询刷新）；内核未运行为空。
func (m *Manager) Connections() []ConnItem {
	m.tmu.Lock()
	defer m.tmu.Unlock()
	out := make([]ConnItem, len(m.conns))
	copy(out, m.conns)
	return out
}

// startPolling 每秒轮询一次 /connections 计算速率，并采样内核内存。
func (m *Manager) startPolling(ctx context.Context) {
	if m.polling.Swap(true) {
		return
	}
	go func() {
		defer m.polling.Store(false)
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		var emaUp, emaDown float64
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if !m.running.Load() {
					return
				}
				snap, err := m.hc.connections(ctx)
				if err != nil {
					continue
				}
				now := time.Now()
				m.tmu.Lock()
				if !m.lastTime.IsZero() {
					dt := now.Sub(m.lastTime).Seconds()
					if dt > 0.2 {
						du := float64(snap.UploadTotal-m.lastTot[0]) / dt
						dd := float64(snap.DownloadTotal-m.lastTot[1]) / dt
						if du >= 0 {
							emaUp = emaUp*0.5 + du*0.5
						}
						if dd >= 0 {
							emaDown = emaDown*0.5 + dd*0.5
						}
					}
				}
				m.lastTot = [2]int64{snap.UploadTotal, snap.DownloadTotal}
				m.lastTime = now
				m.traffic = Traffic{
					Up:          int64(emaUp),
					Down:        int64(emaDown),
					UpTotal:     snap.UploadTotal,
					DownTotal:   snap.DownloadTotal,
					Connections: len(snap.Connections),
					MemoryMB:    m.processRSSMB(),
				}
				// 连接快照上限 1000 条，防止异常大量连接拖垮路由器内存
				m.conns = snap.Connections
				if len(m.conns) > 1000 {
					m.conns = m.conns[:1000]
				}
				m.tmu.Unlock()
			}
		}
	}()
}

// processRSSMB 读取内核进程 RSS（MB）。Linux 直接读 /proc，其余平台用 ps。
func (m *Manager) processRSSMB() float64 {
	pid := m.PID()
	if pid == 0 {
		return 0
	}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				kb, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(line, "VmRSS:")), 64)
				return kb / 1024.0
			}
		}
		return 0
	}
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	kb, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return kb / 1024.0
}

// sysProcAttr 目前无需平台特化；保留钩子便于日后加 Pdeathsig 等。
func sysProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{} }

// ---- mihomo 控制接口转发 ----

// Proxies 返回全部代理与代理组。
func (m *Manager) Proxies(ctx context.Context) (map[string]any, error) {
	return m.hc.proxies(ctx)
}

// SelectProxy 切换选择型代理组的选中节点。
func (m *Manager) SelectProxy(ctx context.Context, group, name string) error {
	return m.hc.selectProxy(ctx, group, name)
}

// GroupDelay 对整组节点并发测延迟，返回 节点名→毫秒。
func (m *Manager) GroupDelay(ctx context.Context, group, testURL string, timeoutMS int) (map[string]int, error) {
	return m.hc.groupDelay(ctx, group, testURL, timeoutMS)
}

// ProxyDelay 对单个节点测延迟。
func (m *Manager) ProxyDelay(ctx context.Context, name, testURL string, timeoutMS int) (map[string]int, error) {
	return m.hc.proxyDelay(ctx, name, testURL, timeoutMS)
}

func randomSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
