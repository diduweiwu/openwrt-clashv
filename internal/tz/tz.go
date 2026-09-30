// Package tz 解析系统本地时区。
//
// OpenWrt 不装 zoneinfo 时，Go 只认 TZ 环境变量与 /etc/localtime，而 OpenWrt
// 把时区存成 POSIX TZ 字符串（/etc/TZ，如 "CST-8"）和 uci 的 zonename
// （如 "Asia/Shanghai"），Go 都读不到，日志时间会退回 UTC。这里按
// zonename → /etc/TZ(POSIX) → TZ 环境变量的顺序兜底。内嵌 tzdata 让
// zonename 在没有系统 zoneinfo 的设备上也能加载。
package tz

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // 内嵌 IANA 时区库（约 +450KB），OpenWrt 无 zoneinfo 时可用
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

// EnvValue 返回适合传给 mihomo 子进程的 TZ 值。
//
// 必须传 IANA 名：Go 的 time 包不解析 POSIX TZ 串（"CST-8" 会被当成
// 无效值静默退回 UTC），这曾是内核日志时间戳停在 UTC 的原因。uci
// zonename 优先（mihomo 同样内嵌 tzdata，能精确解析 IANA 名）；没有
// zonename 时把 /etc/TZ 的 POSIX 偏移映射成等价的 Etc/GMT±N 固定区。
func EnvValue() string {
	if name := uciZoneName(); name != "" {
		return name
	}
	if v := etcTZ(); v != "" {
		return ianaFromPOSIX(v)
	}
	return ""
}

// ianaFromPOSIX 把 POSIX TZ 串映射成等价的 Etc/GMT±N 固定时区名
// （POSIX "CST-8" = UTC+8 = "Etc/GMT-8"）。含夏令时规则或非整小时
// 偏移时返回空（调用方放弃传 TZ，宁缺毋滥——Go 不认 POSIX 串）。
func ianaFromPOSIX(s string) string {
	loc, ok := parsePOSIXTZ(s)
	if !ok {
		return ""
	}
	_, off := time.Now().In(loc).Zone()
	if off%3600 != 0 {
		return ""
	}
	h := off / 3600
	switch {
	case h == 0:
		return "UTC"
	case h > 0:
		return "Etc/GMT-" + strconv.Itoa(h)
	default:
		return "Etc/GMT+" + strconv.Itoa(-h)
	}
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
