package config

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// UCI 布局（/etc/config/clashv）:
//
//	config clashv 'main'
//	    option enabled '1'
//	    option core_autostart '1'
//	    option ui_port '9097'
//	    option mixed_port '7890'
//	    option controller_port '9090'
//	    option controller_secret ''
//	    option token ''
//	    option allow_lan '0'
//	    option tun '0'
//	    option tun_stack 'mixed'
//	    option dns '1'
//	    option auto_update '12'
//	    option core_path ''
//	    option core_arch ''
//	    option dns_hijack 'firewall'
//	    option workdir ''
//	    option plugin_repo 'nier/clashv'
//	    option download_proxy 'https://gh-proxy.com'
//	    option active_profile ''

const uciSection = "clashv.main"

func uciGet(key string) (string, bool) {
	out, err := exec.Command("uci", "-q", "get", key).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func (m *Manager) loadUCI(s *Settings) error {
	// 全部「存在才覆盖」：UCI 里缺省的 option 保留 Defaults() 的值
	getOK := func(opt string) (string, bool) {
		return uciGet(uciSection + "." + opt)
	}
	isOn := func(opt string) bool {
		v, _ := getOK(opt)
		return v == "1" || v == "true" || v == "on"
	}
	setStr := func(dst *string, opt string) {
		if v, ok := getOK(opt); ok {
			*dst = v
		}
	}
	setInt := func(dst *int, opt string) {
		if v, ok := getOK(opt); ok {
			*dst = atoi(v, *dst)
		}
	}

	if _, ok := getOK("enabled"); ok {
		s.Enabled = isOn("enabled")
	}
	if _, ok := getOK("core_autostart"); ok {
		s.CoreAutostart = isOn("core_autostart")
	}
	setInt(&s.UIPort, "ui_port")
	setInt(&s.MixedPort, "mixed_port")
	setInt(&s.ControllerPort, "controller_port")
	setStr(&s.ControllerSecret, "controller_secret")
	setStr(&s.Token, "token")
	if _, ok := getOK("allow_lan"); ok {
		s.AllowLAN = isOn("allow_lan")
	}
	if _, ok := getOK("tun"); ok {
		s.TUN = isOn("tun")
	}
	setStr(&s.TUNStack, "tun_stack")
	if _, ok := getOK("dns"); ok {
		s.DNS = isOn("dns")
	}
	setInt(&s.AutoUpdateHours, "auto_update")
	setStr(&s.CorePath, "core_path")
	setStr(&s.CoreArch, "core_arch")
	setStr(&s.DNSHijack, "dns_hijack")
	setStr(&s.WorkDir, "workdir")
	setStr(&s.PluginRepo, "plugin_repo")
	setStr(&s.DownloadProxy, "download_proxy")
	setStr(&s.ActiveProfile, "active_profile")
	return nil
}

// saveUCI 通过 `uci batch` 一次性写出全部 option，再 commit。
// 批量写入避免多次 fork；字符串值中的单引号已在 normalize 中剔除。
func (m *Manager) saveUCI(s *Settings) error {
	onOff := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	var b bytes.Buffer
	line := func(format string, a ...any) {
		fmt.Fprintf(&b, format+"\n", a...)
	}
	// 首次写入时 section 可能不存在
	if _, ok := uciGet(uciSection + ".ui_port"); !ok {
		line("set %s=clashv", uciSection)
	}
	line("set %s.enabled='%s'", uciSection, onOff(s.Enabled))
	line("set %s.core_autostart='%s'", uciSection, onOff(s.CoreAutostart))
	line("set %s.ui_port='%d'", uciSection, s.UIPort)
	line("set %s.mixed_port='%d'", uciSection, s.MixedPort)
	line("set %s.controller_port='%d'", uciSection, s.ControllerPort)
	line("set %s.controller_secret='%s'", uciSection, s.ControllerSecret)
	line("set %s.token='%s'", uciSection, s.Token)
	line("set %s.allow_lan='%s'", uciSection, onOff(s.AllowLAN))
	line("set %s.tun='%s'", uciSection, onOff(s.TUN))
	line("set %s.tun_stack='%s'", uciSection, s.TUNStack)
	line("set %s.dns='%s'", uciSection, onOff(s.DNS))
	line("set %s.auto_update='%d'", uciSection, s.AutoUpdateHours)
	line("set %s.core_path='%s'", uciSection, s.CorePath)
	line("set %s.core_arch='%s'", uciSection, s.CoreArch)
	line("set %s.dns_hijack='%s'", uciSection, s.DNSHijack)
	line("set %s.workdir='%s'", uciSection, s.WorkDir)
	line("set %s.plugin_repo='%s'", uciSection, s.PluginRepo)
	line("set %s.download_proxy='%s'", uciSection, s.DownloadProxy)
	line("set %s.active_profile='%s'", uciSection, s.ActiveProfile)
	line("commit clashv")

	cmd := exec.Command("uci", "batch")
	cmd.Stdin = &b
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("uci batch: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
