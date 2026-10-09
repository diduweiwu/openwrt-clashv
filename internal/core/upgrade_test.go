package core

import "testing"

// Release 资产匹配：8 个包全靠包名区分架构，通用版的规则绝不能误吃精简版。
func TestPluginAssetRe(t *testing.T) {
	cases := []struct {
		pkg, format, file string
		want              bool
	}{
		{"luci-app-clashv-arm64", "ipk", "luci-app-clashv-arm64_0.1.12-r10_all.ipk", true},
		{"luci-app-clashv-arm64", "apk", "luci-app-clashv-arm64-0.1.12-r10_all.apk", true},
		{"luci-app-clashv", "ipk", "luci-app-clashv_0.1.12-r10_all.ipk", true},
		{"luci-app-clashv", "apk", "luci-app-clashv-0.1.12-r10_all.apk", true},
		{"luci-app-clashv", "ipk", "luci-app-clashv-arm64_0.1.12-r10_all.ipk", false},
		{"luci-app-clashv", "apk", "luci-app-clashv-arm64-0.1.12-r10_all.apk", false},
		{"luci-app-clashv-arm64", "ipk", "luci-app-clashv-arm64-0.1.12-r10_all.apk", false},
		{"luci-app-clashv-arm64", "ipk", "luci-app-clashv-amd64_0.1.12-r10_all.ipk", false},
		{"luci-app-clashv-arm64", "ipk", "mihomo-linux-arm64-v1.19.2.gz", false},
	}
	for _, c := range cases {
		if got := pluginAssetRe(c.pkg, c.format).MatchString(c.file); got != c.want {
			t.Errorf("pluginAssetRe(%q,%q) match %q = %v, want %v", c.pkg, c.format, c.file, got, c.want)
		}
	}
}

// 与 openwrt/Makefile postinst 的 KEEP 映射保持一致。
func TestOpenwrtPkgArch(t *testing.T) {
	cases := map[string]string{
		"aarch64_cortex-a53": "arm64",
		"aarch64_generic":    "arm64",
		"arm_cortex-a7":      "armv7",
		"arm_arm926ej-s":     "armv7",
		"mipsel_24kc":        "mipsle",
		"mips_24kc":          "mips",
		"mips64_mips64r2":    "", // 64 位 MIPS 不在支持列表，退通用版
		"x86_64":             "amd64",
		"riscv64":            "riscv64",
		"loongarch64":        "loong64",
		"powerpc_8540":       "",
		"":                   "",
	}
	for arch, want := range cases {
		if got := openwrtPkgArch(arch); got != want {
			t.Errorf("openwrtPkgArch(%q) = %q, want %q", arch, got, want)
		}
	}
}
