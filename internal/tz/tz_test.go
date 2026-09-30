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
		{"XX", false, 0},  // 名字太短
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
