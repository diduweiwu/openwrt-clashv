// Package tz 解析系统本地时区。
//
// OpenWrt 不装 zoneinfo 时，Go 只认 TZ 环境变量与 /etc/localtime，而 OpenWrt
// 把时区存成 POSIX TZ 字符串（/etc/TZ，如 "CST-8"）和 uci 的 zonename
// （如 "Asia/Shanghai"），Go 都读不到，日志时间会退回 UTC。这里按
// zonename → /etc/TZ(POSIX) → TZ 环境变量的顺序兜底。
package tz

import (
	"os"
	"os/exec"
	"strings"
	"time"
)

// Resolve 返回最优的本地时区；解析不出时返回 time.Local（可能就是 UTC）。
func Resolve() *time.Location {
	if v := strings.TrimSpace(os.Getenv("TZ")); v != "" {
		if loc, err := time.LoadLocation(v); err == nil {
			return loc
		}
		if loc, ok := parsePOSIXTZ(v); ok {
			return loc
		}
	}
	// TZ 未设置时 Go 已按 /etc/localtime 初始化过 time.Local；
	// 偏移非 0 说明确实拿到了本地时区
	if _, off := time.Now().Zone(); off != 0 {
		return time.Local
	}
	if name := uciZoneName(); name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	if v := etcTZ(); v != "" {
		if loc, ok := parsePOSIXTZ(v); ok {
			return loc
		}
	}
	return time.Local
}

// EnvValue 返回适合传给子进程的 TZ 值：优先 zoneinfo 名（子进程装了
// zoneinfo 就能精确到 DST），否则 /etc/TZ 原文（busybox 工具能识别）。
func EnvValue() string {
	if name := uciZoneName(); name != "" {
		if _, err := time.LoadLocation(name); err == nil {
			return name
		}
	}
	return etcTZ()
}

func uciZoneName() string {
	out, err := exec.Command("uci", "-q", "get", "system.@system[0].zonename").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func etcTZ() string {
	data, err := os.ReadFile("/etc/TZ")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// parsePOSIXTZ 解析 POSIX TZ 字符串的 "标准名偏移" 部分，如 "CST-8"、
// "<UTC+8>8"、"EST5EDT,M3.2.0,M11.1.0"。只取标准时区偏移（夏令时规则
// 忽略——中国无夏令时；有 DST 的地区会常年按标准时间显示，可接受）。
// 注意 POSIX 偏移方向与直觉相反：正数表示格林威治以西，"CST-8" = UTC+8。
func parsePOSIXTZ(s string) (*time.Location, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	name := ""
	// <...> 括起来的名字
	if strings.HasPrefix(s, "<") {
		end := strings.Index(s, ">")
		if end < 0 {
			return nil, false
		}
		name = s[1:end]
		s = s[end+1:]
	} else {
		i := 0
		for i < len(s) && ((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
			i++
		}
		if i < 3 {
			return nil, false
		}
		name = s[:i]
		s = s[i:]
	}
	// 偏移 [-+]hh[:mm[:ss]]，后面可能紧跟夏令时名与规则（EST5EDT,M3.2.0,…），
	// 数字段读到非数字即停，剩余部分忽略
	sign := 1
	if len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	readNum := func(maxLen int) (int, bool) {
		n := 0
		i := 0
		for i < len(s) && i < maxLen && s[i] >= '0' && s[i] <= '9' {
			n = n*10 + int(s[i]-'0')
			i++
		}
		if i == 0 {
			return 0, false
		}
		s = s[i:]
		return n, true
	}
	hh, ok := readNum(3)
	if !ok || hh > 24 {
		return nil, false
	}
	secs := hh * 3600
	if strings.HasPrefix(s, ":") {
		s = s[1:]
		mm, ok := readNum(2)
		if !ok || mm > 59 {
			return nil, false
		}
		secs += mm * 60
		if strings.HasPrefix(s, ":") {
			s = s[1:]
			ss, ok := readNum(2)
			if !ok || ss > 59 {
				return nil, false
			}
			secs += ss
		}
	}
	if name == "" {
		name = "LOCAL"
	}
	return time.FixedZone(name, sign*-secs), true
}
