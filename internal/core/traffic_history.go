package core

// 流量历史采样与持久化：内核轮询循环（startPolling）每秒追加一个采样点，
// 内存与磁盘都只保留最近 1 小时。历史由后端持续采样积累，与页面是否打开无关；
// 定期落盘到数据目录（traffic_history.json），服务重启（含插件自升级、procd
// respawn、路由器重启）后自动恢复，界面首屏经 /api/traffic/history 一次性拉取，
// 流量曲线不再因刷新页面或重启插件而清零。

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const (
	// trafficHistKeep 采样保留上限：1 小时 × 1 秒采样 = 3600 点
	trafficHistKeep = 3600
	// trafficHistSave 采样落盘间隔：服务重启最多丢这段时间的尾部采样。
	// 也不宜更频繁——文件约百 KB，写太勤会加重路由器 flash 磨损
	trafficHistSave = 2 * time.Minute
	// trafficHistWindow 采样保留时长：曲线展示窗口即按此值设定（前端 1 小时）
	trafficHistWindow = time.Hour
)

// TrafficSample 是一次流量历史采样。
type TrafficSample struct {
	T    int64 // 采样时刻（毫秒时间戳）
	Up   int64 // 上传速率（B/s）
	Down int64 // 下载速率（B/s）
}

// trafficFile 是落盘/接口共用的紧凑结构：三个平行数组按采样时刻对齐，
// 比对象数组省约一半体积（3600 点约 90KB，减轻 flash 写入量）。
type trafficFile struct {
	T []int64 `json:"t"` // 采样时刻（毫秒时间戳）
	U []int64 `json:"u"` // 上传速率 B/s
	D []int64 `json:"d"` // 下载速率 B/s
}

// trafficHistoryPath 返回流量历史落盘文件路径（数据目录下，与 core.state 同级）。
func (m *Manager) trafficHistoryPath() string {
	return filepath.Join(m.cfg.Home(), "traffic_history.json")
}

// loadTrafficHistoryLocked 首次访问时从磁盘恢复历史（懒加载：不依赖 config
// 的加载顺序，Manager 创建早于配置就绪也不会出问题）。文件缺失视为首次运行；
// 解析失败按损坏处理并忽略已有历史，不影响功能。须持有 histMu。
func (m *Manager) loadTrafficHistoryLocked() {
	if m.histLoaded {
		return
	}
	m.histLoaded = true
	data, err := os.ReadFile(m.trafficHistoryPath())
	if err != nil {
		return // 首次运行/文件缺失：空历史即可
	}
	var f trafficFile
	if err := json.Unmarshal(data, &f); err != nil {
		slog.Warn("流量历史文件损坏，忽略已有历史", "err", err)
		return
	}
	cut := time.Now().Add(-trafficHistWindow).UnixMilli()
	for i := 0; i < len(f.T) && i < len(f.U) && i < len(f.D); i++ {
		if f.T[i] < cut {
			continue // 只恢复最近 1 小时，陈旧文件不回灌
		}
		m.hist = append(m.hist, TrafficSample{T: f.T[i], Up: f.U[i], Down: f.D[i]})
	}
}

// appendTrafficSample 追加一个采样点并按需落盘。由轮询循环每秒调用一次；
// 只保留最近 1 小时（按时间与条数双重裁剪），距上次落盘超过 trafficHistSave
// 时把全量历史写回文件（临时文件 + rename，避免写一半被重启截断）。
//
// 参数 now 为采样时刻，up/down 为该时刻的上下行速率（B/s）。
func (m *Manager) appendTrafficSample(now time.Time, up, down int64) {
	m.histMu.Lock()
	defer m.histMu.Unlock()
	m.loadTrafficHistoryLocked()
	m.hist = append(m.hist, TrafficSample{T: now.UnixMilli(), Up: up, Down: down})
	// 按时间裁剪：丢掉窗口外的旧采样（内核停跑/页面长开后仍只保留 1 小时）
	cut := now.Add(-trafficHistWindow).UnixMilli()
	drop := 0
	for drop < len(m.hist) && m.hist[drop].T < cut {
		drop++
	}
	if drop > 0 {
		m.hist = m.hist[drop:]
	}
	// 按条数兜底裁剪，防御采样节奏异常导致超限
	if len(m.hist) > trafficHistKeep {
		m.hist = m.hist[len(m.hist)-trafficHistKeep:]
	}
	if now.Sub(m.lastHistSave) >= trafficHistSave {
		m.lastHistSave = now
		m.saveTrafficHistoryLocked()
	}
}

// saveTrafficHistoryLocked 把当前历史全量写回磁盘。须持有 histMu。
func (m *Manager) saveTrafficHistoryLocked() {
	f := trafficFile{
		T: make([]int64, 0, len(m.hist)),
		U: make([]int64, 0, len(m.hist)),
		D: make([]int64, 0, len(m.hist)),
	}
	for _, s := range m.hist {
		f.T = append(f.T, s.T)
		f.U = append(f.U, s.Up)
		f.D = append(f.D, s.Down)
	}
	data, err := json.Marshal(f)
	if err != nil {
		return
	}
	path := m.trafficHistoryPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Warn("流量历史落盘失败", "err", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		slog.Warn("流量历史落盘失败", "err", err)
	}
}

// TrafficHistory 返回最近 1 小时的全部采样（平行数组形式），供
// GET /api/traffic/history 一次性下发；前端首屏用它填充流量曲线。
// 内核停止期间没有采样，历史里天然留有时间断档。
func (m *Manager) TrafficHistory() trafficFile {
	m.histMu.Lock()
	defer m.histMu.Unlock()
	m.loadTrafficHistoryLocked()
	f := trafficFile{
		T: make([]int64, 0, len(m.hist)),
		U: make([]int64, 0, len(m.hist)),
		D: make([]int64, 0, len(m.hist)),
	}
	for _, s := range m.hist {
		f.T = append(f.T, s.T)
		f.U = append(f.U, s.Up)
		f.D = append(f.D, s.Down)
	}
	return f
}
