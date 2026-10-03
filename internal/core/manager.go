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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"clashv/internal/config"
	"clashv/internal/profiles"
	"clashv/internal/tz"
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
	// starting 表示内核进程已拉起但控制接口尚未就绪（启动窗口最长 15s）。
	// 期间 Running() 仍为 false：没就绪就不能算「运行中」，否则界面在
	// 重启窗口里显示运行中、启动失败时还会跳回已停止，误导用户。
	starting atomic.Bool

	hc *controllerClient

	progMu sync.Mutex
	prog   UpgradeProgress // 当前升级任务进度（同一时间至多一个）

	hookMu sync.Mutex // 防火墙接管规则（DNS 劫持/TCP 透明代理）重建串行化

	tmu      sync.Mutex
	traffic  Traffic
	conns    []ConnItem // 最近一次轮询的活动连接快照
	lastTot  [2]int64
	lastTime time.Time
	pollErr  string // 最近一次连接轮询失败的错误（空 = 正常）
	pollLog  string // 上次已告警的错误，用于去重
	polling  atomic.Bool

	opt optState // 节点域名优选：已应用结果与测速缓存
}

// NewManager 创建内核管理器。
func NewManager(cfg *config.Manager, prof *profiles.Manager, pluginVersion string) *Manager {
	return &Manager{
		cfg:           cfg,
		prof:          prof,
		PluginVersion: pluginVersion,
		hc:            &controllerClient{},
		opt: optState{
			applied:    map[string]string{},
			probeCache: map[string]probeEntry{},
		},
	}
}

// Running 报告内核是否在运行（进程存活且控制接口已就绪）。
func (m *Manager) Running() bool { return m.running.Load() }

// Starting 报告内核是否处于启动中（进程已拉起、控制接口未就绪）。
func (m *Manager) Starting() bool { return m.starting.Load() }

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
	if m.running.Load() || m.starting.Load() {
		return nil
	}
	// 任意返回路径都要清掉启动中标记
	defer m.starting.Store(false)
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

	// 清理上次异常残留的孤儿内核（procd respawn、Start 中途失败等都会留下）。
	// 孤儿会占住控制/redir/DNS 端口，让新内核绑不上、本插件与旧实例对话：
	// 表现为「代理能用但连接列表为空、测速全失败」。按内核二进制路径精确匹配，
	// 不会误杀 OpenClash 等其他插件的内核。
	if killed := sweepOrphanCores(corePath); killed > 0 {
		slog.Warn("已清理残留的内核进程", "count", killed, "path", corePath)
	}

	RotateAtOpen(filepath.Join(m.cfg.LogDir(), "core.log"), LogMaxBytes)
	logFile, err := os.OpenFile(m.cfg.LogDir()+"/core.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	// 记录本次启动前内核日志的末尾位置，就绪后检查这段日志里的端口绑定错误
	logOffset := int64(0)
	if st, err := logFile.Stat(); err == nil {
		logOffset = st.Size()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.Command(corePath, "-d", m.cfg.Home(), "-f", m.cfg.RuntimeConfigPath())
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// 透传 TZ，让内核日志也用本地时区（OpenWrt 的 /etc/TZ 由本插件解析后同步）；
	// 设置了内存软上限时注入 GOMEMLIMIT，内核接近上限会加大 GC 力度压住 RSS
	cmd.Env = os.Environ()
	if tzVal := tz.EnvValue(); tzVal != "" {
		cmd.Env = append(cmd.Env, "TZ="+tzVal)
	}
	if s.CoreMemLimit > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("GOMEMLIMIT=%dMiB", s.CoreMemLimit))
	}
	cmd.SysProcAttr = sysProcAttr()
	m.starting.Store(true)
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("启动内核失败: %w", err)
	}
	m.cmd = cmd
	m.cancel = cancel
	m.startedAt = time.Now()
	m.exited = make(chan struct{})
	m.resetTraffic()

	// 进程退出回收
	go func() {
		_ = cmd.Wait()
		close(m.exited)
		m.running.Store(false)
		m.polling.Store(false)
		m.markCoreState(false) // 无论正常停止还是异常退出，运行记忆都清掉
		slog.Info("mihomo 进程已退出")
	}()

	// 等待控制接口就绪（此时 running 仍为 false，就绪后才置位）
	m.hc.setEndpoint(s.ControllerPort, s.ControllerSecret)
	deadline := time.Now().Add(15 * time.Second)
	for {
		select {
		case <-m.exited:
			return errors.New("内核启动后立即退出，请查看日志（工作目录 logs/core.log）")
		default:
		}
		if ctx.Err() != nil {
			killCoreProcess(cmd, m.exited)
			return errors.New("内核启动中止")
		}
		if _, err := m.hc.version(ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			// 15 秒仍未就绪：多半是配置错误或弱 CPU 加载 geodata 慢。
			// 必须把子进程杀掉，否则它稍后就绪后成为孤儿，占住全部端口。
			killCoreProcess(cmd, m.exited)
			return errors.New("内核控制接口 15 秒内未就绪，已终止内核，请查看日志（工作目录 logs/core.log）")
		}
		time.Sleep(300 * time.Millisecond)
	}
	// 内核「能应答」不代表监听都起来了：端口被占时 mihomo 只在日志里报错并继续运行，
	// 后果是流量根本进不来。这里检查本次启动新增的日志段，把绑定失败变成显式错误。
	time.Sleep(500 * time.Millisecond)
	if err := checkCoreBindErrors(m.cfg.LogDir()+"/core.log", logOffset); err != nil {
		killCoreProcess(cmd, m.exited)
		return fmt.Errorf("内核端口绑定失败: %w", err)
	}
	// 控制器能应答 + 日志无绑定错误，仍不代表服务真的可用：再模拟客户端
	// 真实拨号，确认代理/DNS 监听已在接受连接，全部可用后才置「运行中」。
	// 否则界面提前显示运行中，LAN 设备却连不上代理，误判成「断网」。
	if err := waitCoreServing(ctx, s); err != nil {
		killCoreProcess(cmd, m.exited)
		return fmt.Errorf("内核服务未就绪: %w", err)
	}
	// 就绪：此刻起才算「运行中」
	m.running.Store(true)
	m.markCoreState(true) // 记住运行状态：服务重启/路由器重启后据此自动恢复
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
	// 直接 SIGKILL，不等优雅退出：内核没有必须优雅关停才能落盘的状态——
	// TUN/监听端口随进程消失由内核回收，代理选择/fakeip 走 bbolt 事务即时落盘；
	// 防火墙与 dnsmasq 劫持已在上面先撤。SIGTERM 会被 mihomo 的信号处理器接住
	// 走完整关闭流程，最坏要等 5 秒才兜底 kill，保存设置重启时白白卡住。
	_ = cmd.Process.Kill()
	<-m.exited
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
	m.pollErr = ""
	m.pollLog = ""
}

// Connections 返回内核当前活动连接快照（随流量每秒轮询刷新）；内核未运行为空。
func (m *Manager) Connections() []ConnItem {
	m.tmu.Lock()
	defer m.tmu.Unlock()
	out := make([]ConnItem, len(m.conns))
	copy(out, m.conns)
	return out
}

// PollError 返回连接轮询最近的错误（空字符串表示正常）。
// 连接列表为空时用它区分「没有连接」和「轮询失败」。
func (m *Manager) PollError() string {
	m.tmu.Lock()
	defer m.tmu.Unlock()
	return m.pollErr
}

func (m *Manager) setPollError(err error) {
	m.tmu.Lock()
	defer m.tmu.Unlock()
	if err == nil {
		m.pollErr = ""
		return
	}
	msg := err.Error()
	m.pollErr = msg
	// 同一类错误只告警一次，避免每秒刷屏
	if msg != m.pollLog {
		m.pollLog = msg
		slog.Warn("内核连接轮询失败，连接列表将保持为空", "err", msg)
	}
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
					m.setPollError(err)
					continue
				}
				m.setPollError(nil)
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
				// 行形如 "VmRSS:  12345 kB"，数字与单位间有空格，只能取首段
				fields := strings.Fields(strings.TrimPrefix(line, "VmRSS:"))
				if len(fields) == 0 {
					return 0
				}
				kb, err := strconv.ParseFloat(fields[0], 64)
				if err != nil {
					return 0
				}
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

// ---- 内核运行状态记忆 ----
//
// 状态写在 <home>/core.state：Start 成功置 "running"，进程退出即清除。
// 插件服务重启（procd respawn）或路由器重启后，autoStartCore 据此判断
// 「重启前内核是否在运行」，是则自动恢复。不用 UCI 存储：内核启停是高频
// 事件，避免反复写 flash 里的 /etc/config。

func (m *Manager) coreStatePath() string {
	return filepath.Join(m.cfg.Home(), "core.state")
}

func (m *Manager) markCoreState(running bool) {
	path := m.coreStatePath()
	if !running {
		_ = os.Remove(path)
		return
	}
	if err := os.WriteFile(path, []byte("running\n"), 0o644); err != nil {
		slog.Warn("内核状态记忆写入失败（重启后不会自动恢复运行）", "err", err)
	}
}

// CoreWasRunning 报告上次记录的内核是否处于运行状态（文件缺失/内容不符为 false）。
// 供服务启动时的自动恢复逻辑调用（见 cmd/clashv autoStartCore）。
func (m *Manager) CoreWasRunning() bool {
	data, err := os.ReadFile(m.coreStatePath())
	return err == nil && strings.TrimSpace(string(data)) == "running"
}

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

// Rules 返回内核当前加载的路由规则（即运行时 config.yaml 合成后的生效结果）。
func (m *Manager) Rules(ctx context.Context) ([]RuleItem, error) {
	return m.hc.rules(ctx)
}

// CloseAllConnections 断开内核当前全部活动连接。
func (m *Manager) CloseAllConnections(ctx context.Context) error {
	return m.hc.closeAllConnections(ctx)
}

// Mode 返回内核当前出站模式（rule / global / direct）。
func (m *Manager) Mode(ctx context.Context) (string, error) {
	return m.hc.mode(ctx)
}

// SetMode 运行时切换出站模式；持久化由 API 层负责写入设置。
func (m *Manager) SetMode(ctx context.Context, mode string) error {
	return m.hc.setMode(ctx, mode)
}

func randomSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
