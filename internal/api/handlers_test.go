package api

import "testing"

func TestPluginHasUpdate(t *testing.T) {
	cases := []struct {
		ver             string
		curID, latestID int64
		latestTag       string
		want            bool
		wantBy          string
	}{
		{"0.1.12", 100, 200, "v0.1.13", true, "id"},  // 线上更新
		{"0.1.12", 200, 200, "v0.1.12", false, "id"}, // 同一个 Release
		{"0.1.13", 300, 200, "v0.1.12", false, "id"}, // 本地比线上新：不提示（ID 分得清方向）
		{"0.1.12", 0, 200, "v0.1.13", true, "ver"},   // 本地 Release 查不到 → 版本号兜底
		{"0.1.13", 0, 200, "v0.1.12", false, "ver"},  // 兜底也能分方向：本地更新不提示
		{"dev", 0, 0, "", true, "dev"},
	}
	for _, c := range cases {
		got, by := pluginHasUpdate(c.ver, c.curID, c.latestID, c.latestTag)
		if got != c.want || by != c.wantBy {
			t.Errorf("pluginHasUpdate(%q,%d,%d,%q) = %v,%v, want %v,%v",
				c.ver, c.curID, c.latestID, c.latestTag, got, by, c.want, c.wantBy)
		}
	}
}

// 拆段数字比较：字符串序会得出 0.1.2 > 0.1.13 的错误结论。
func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.2", "0.1.13", -1},  // 字典序误判的反例
		{"0.1.13", "0.1.2", 1},   //
		{"0.1.10", "0.1.9", 1},   //
		{"0.1.12", "0.1.12", 0},  //
		{"v0.1.12", "0.1.12", 0}, // v 前缀容忍
		{"1.0", "1.0.0", 0},      // 段数不足补 0
		{"1.0.1", "1.0", 1},      //
		{"0.2", "0.1.99", 1},     // 高位段优先
		{"1.0-rc1", "1.0.0", 0},  // 非数字段取前导数字（0 == 0）
		{"abc", "0.0.1", -1},     // 全非数字视为 0
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
