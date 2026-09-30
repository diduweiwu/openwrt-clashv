package tz

import (
	"testing"
	"time"
)

func TestParsePOSIXTZ(t *testing.T) {
	cases := []struct {
		in      string
		wantOK  bool
		wantOff int // 期望的 UTC 偏移秒数
	}{
		{"CST-8", true, 8 * 3600},
		{"CST-8:00", true, 8 * 3600},
		{"<UTC+8>8", true, -8 * 3600}, // 尖括号名，POSIX 正偏移=以西
		{"EST5EDT,M3.2.0,M11.1.0", true, -5 * 3600},
		{"UTC0", true, 0},
		{"HKT-8:30", true, 8*3600 + 1800},
		{"", false, 0},
		{"XX", false, 0},    // 名字太短
		{"CST-x", false, 0}, // 偏移非法
	}
	for _, c := range cases {
		loc, ok := parsePOSIXTZ(c.in)
		if ok != c.wantOK {
			t.Errorf("parsePOSIXTZ(%q) ok=%v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		_, off := time.Now().In(loc).Zone()
		if off != c.wantOff {
			t.Errorf("parsePOSIXTZ(%q) offset=%d, want %d", c.in, off, c.wantOff)
		}
	}
}

func TestIANAFromPOSIX(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"CST-8", "Etc/GMT-8"},                  // 中国：UTC+8
		{"<+08>-8", "Etc/GMT-8"},                // 尖括号名，POSIX 负偏移=以东 8 小时
		{"<+08>8", "Etc/GMT+8"},                 // POSIX 正偏移=以西
		{"UTC0", "UTC"},                         // 零偏移
		{"EST5EDT,M3.2.0,M11.1.0", "Etc/GMT+5"}, // 含夏令时规则，取标准时间近似
		{"HKT-8:30", ""},                        // 非整小时偏移无法映射
		{"CST-x", ""},                           // 非法
	}
	for _, c := range cases {
		if got := ianaFromPOSIX(c.in); got != c.want {
			t.Errorf("ianaFromPOSIX(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLoadLocationEmbedded(t *testing.T) {
	// 内嵌 tzdata：无系统 zoneinfo 的设备上 IANA 名也要能加载
	if _, err := time.LoadLocation("Asia/Shanghai"); err != nil {
		t.Fatalf("LoadLocation(Asia/Shanghai) 失败: %v", err)
	}
}
