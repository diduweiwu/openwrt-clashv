package core

import (
	"log/slog"
	"os/exec"
	"strings"

	"clashv/internal/config"
)

// DNS 劫持（旁路由/网关模式的关键）：
//
// LAN 客户端的 DNS 查询默认由 OpenWrt 自带 dnsmasq（53 端口）用运营商上游应答，
// 结果被污染，导致 google 等域名解析到假 IP、即使流量走内核也路由错误。
// 参照 OpenClash 提供两种接管方式：
//
//	firewall —— 防火墙转发：nft/iptables 把 LAN 的 53 端口流量重定向到内核 DNS（1053）；
//	dnsmasq  —— dnsmasq 转发：把 dnsmasq 的上游改成 127.0.0.1#1053。
//
// 两者都只在 OpenWrt 且内核 DNS 开启时生效；内核停止时撤销，避免 LAN 断网。

// dnsListenPort 必须与 managedOverlay 中 dns.listen 保持一致。
const dnsListenPort = "1053"

const nftTable = "clashv_dns"

// ApplyDNSHijack 按设置启用 DNS 劫持（幂等，重复调用安全）。
func (m *Manager) ApplyDNSHijack(s config.Settings) {
	if !m.cfg.IsOpenWrt() || s.DNSHijack == "off" || !s.DNS {
		return
	}
	// 先清掉旧规则，避免模式切换后残留
	m.removeFirewallRules()
	var err error
	switch s.DNSHijack {
	case "firewall":
		err = applyFirewallHijack()
	case "dnsmasq":
		err = applyDnsmasqForward(true)
	}
	if err != nil {
		slog.Warn("DNS 劫持启用失败", "mode", s.DNSHijack, "err", err)
	} else {
		slog.Info("DNS 劫持已启用", "mode", s.DNSHijack, "listen_port", dnsListenPort)
	}
}

// RemoveDNSHijack 撤销全部 DNS 劫持（幂等）。内核停止、设置变更与插件启动时调用。
func (m *Manager) RemoveDNSHijack() {
	if !m.cfg.IsOpenWrt() {
		return
	}
	m.removeFirewallRules()
	if err := applyDnsmasqForward(false); err != nil {
		slog.Warn("DNS 劫持撤销失败（dnsmasq）", "err", err)
	}
}

// removeFirewallRules 只清防火墙规则（nft 表 / iptables 链）。
func (m *Manager) removeFirewallRules() {
	_ = exec.Command("nft", "delete", "table", "inet", nftTable).Run()
	_ = exec.Command("iptables", "-t", "nat", "-D", "PREROUTING", "-j", "CLASHV_DNS").Run()
	_ = exec.Command("iptables", "-t", "nat", "-F", "CLASHV_DNS").Run()
	_ = exec.Command("iptables", "-t", "nat", "-X", "CLASHV_DNS").Run()
}

func run(args ...string) error {
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return trimErr(args, out, err)
	}
	return nil
}

func trimErr(args []string, out []byte, err error) error {
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	}
	return &cmdError{cmd: strings.Join(args, " "), msg: msg}
}

type cmdError struct {
	cmd string
	msg string
}

func (e *cmdError) Error() string { return e.cmd + ": " + e.msg }

// applyFirewallHijack 防火墙转发：优先 nft（OpenWrt 22.03+ / fw4），失败回退 iptables。
func applyFirewallHijack() error {
	if err := nftHijack(); err == nil {
		return nil
	}
	return iptablesHijack()
}

func nftHijack() error {
	if _, err := exec.Command("nft", "list", "table", "inet", nftTable).Output(); err == nil {
		return nil // 已存在视为已生效（apply 前已先 delete，不会走到这里）
	}
	if err := run("nft", "add", "table", "inet", nftTable); err != nil {
		return err
	}
	if err := run("nft", "add", "chain", "inet", nftTable, "prerouting",
		"{ type nat hook prerouting priority dstnat; policy accept; }"); err != nil {
		return err
	}
	if err := run("nft", "add", "rule", "inet", nftTable, "prerouting",
		"udp", "dport", "53", "redirect", "to", ":"+dnsListenPort); err != nil {
		return err
	}
	return run("nft", "add", "rule", "inet", nftTable, "prerouting",
		"tcp", "dport", "53", "redirect", "to", ":"+dnsListenPort)
}

func iptablesHijack() error {
	// 自建链 + PREROUTING 跳转，撤销时删链即可，不碰别人的规则
	_ = exec.Command("iptables", "-t", "nat", "-N", "CLASHV_DNS").Run()
	if err := run("iptables", "-t", "nat", "-A", "CLASHV_DNS",
		"-p", "udp", "--dport", "53", "-j", "REDIRECT", "--to-ports", dnsListenPort); err != nil {
		return err
	}
	if err := run("iptables", "-t", "nat", "-A", "CLASHV_DNS",
		"-p", "tcp", "--dport", "53", "-j", "REDIRECT", "--to-ports", dnsListenPort); err != nil {
		return err
	}
	// 跳转规则已存在则不重复加
	if err := exec.Command("iptables", "-t", "nat", "-C", "PREROUTING", "-j", "CLASHV_DNS").Run(); err == nil {
		return nil
	}
	return run("iptables", "-t", "nat", "-A", "PREROUTING", "-j", "CLASHV_DNS")
}

// applyDnsmasqForward 把 dnsmasq 的上游切到（或切离）内核 DNS。
// 已是目标状态时不重启 dnsmasq，避免启停流程反复打断 LAN 解析。
func applyDnsmasqForward(on bool) error {
	server := "127.0.0.1#" + dnsListenPort
	out, err := exec.Command("uci", "-q", "show", "dhcp.@dnsmasq[0]").Output()
	enabled := err == nil && strings.Contains(string(out), "='"+server+"'")
	if on && enabled {
		return nil
	}
	if !on && !enabled {
		return nil
	}
	if !on {
		_ = exec.Command("uci", "-q", "del_list", "dhcp.@dnsmasq[0].server="+server).Run()
		_ = exec.Command("uci", "set", "dhcp.@dnsmasq[0].noresolv=0").Run()
	} else {
		// 幂等：先删再加
		_ = exec.Command("uci", "-q", "del_list", "dhcp.@dnsmasq[0].server="+server).Run()
		if err := run("uci", "add_list", "dhcp.@dnsmasq[0].server="+server); err != nil {
			return err
		}
		if err := run("uci", "set", "dhcp.@dnsmasq[0].noresolv=1"); err != nil {
			return err
		}
	}
	if err := run("uci", "commit", "dhcp"); err != nil {
		return err
	}
	return run("/etc/init.d/dnsmasq", "restart")
}
