package core

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CPU 占用率采样：读 /proc/stat，计算两次调用之间的繁忙占比。
// 采样挂在 /api/status 的轮询节奏上（App.vue 每 5s 一次），无独立 goroutine。

var (
	cpuMu       sync.Mutex
	cpuPrevBusy float64
	cpuPrevTot  float64
	cpuPrevAt   time.Time
	cpuLast     float64
)

// CPUPercent 返回自上次调用以来的系统 CPU 占用率（0-100，含其他进程）。
// 首次调用无从计算差值返回 0；非 Linux（无 /proc/stat，如本机开发环境）
// 返回 false，前端据此隐藏该项。
func CPUPercent() (float64, bool) {
	if runtime.GOOS != "linux" {
		return 0, false
	}
	busy, total, ok := readCPUTimes()
	if !ok {
		return 0, false
	}
	cpuMu.Lock()
	defer cpuMu.Unlock()
	if cpuPrevAt.IsZero() || total <= cpuPrevTot {
		// 首次采样或计数回退（休眠恢复等）：只记基线，本次返回上次结果
		cpuPrevBusy, cpuPrevTot, cpuPrevAt = busy, total, time.Now()
		return cpuLast, true
	}
	dt := time.Since(cpuPrevAt).Seconds()
	if dt > 0 {
		cpuLast = (busy - cpuPrevBusy) / (total - cpuPrevTot) * 100
		if cpuLast < 0 {
			cpuLast = 0
		} else if cpuLast > 100 {
			cpuLast = 100
		}
	}
	cpuPrevBusy, cpuPrevTot, cpuPrevAt = busy, total, time.Now()
	return cpuLast, true
}

// readCPUTimes 解析 /proc/stat 首行：
// "cpu  user nice system idle iowait irq softirq steal steal_guest ..."
// 繁忙 = 总量 - 空闲(idle+iowait)。
func readCPUTimes() (busy, total float64, ok bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	line, _, found := strings.Cut(string(data), "\n")
	if !found {
		return 0, 0, false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "cpu" {
		return 0, 0, false
	}
	var idle float64
	for i, f := range fields[1:] {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return 0, 0, false
		}
		total += v
		// 字段序：user nice system idle iowait ...，idle=第4个(+iowait第5个)
		if i == 3 || i == 4 {
			idle += v
		}
	}
	return total - idle, total, true
}
