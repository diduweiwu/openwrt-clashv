package core

import (
	"log/slog"
	"os/exec"
	"strings"

	"clashv/internal/config"
)

// 流量接管（旁路由/网关模式的关键），两部分必须配套：
//
//  1. DNS 劫持：LAN 的 53 端口查询交给内核 DNS（1053，fake-ip），
//     否则域名解析被污染，google 等解析到假 IP。方式二选一（dns_hijack 设置）：
//     firewall —— nft/iptables 把 53 端口流量重定向到内核 DNS；
//     dnsmasq  —— 把 dnsmasq 的上游改成 127.0.0.1#1053。
//
//  2. TCP 透明代理：TUN 关闭时，防火墙把 LAN 转发的 TCP 流量 REDIRECT 到
//     内核 redir 端口（7892）。缺了这步，客户端拿到 fake-ip 后 TCP 无路可走，
//     表现为「DNS 正常但网页全部打不开」——与 OpenClash 的 redirect 模式一致。
//     TUN 模式由内核 auto-route 自行接管，无需（也不能重复）加防火墙规则。
//
// 两者只在 OpenWrt 且内核运行时生效；内核停止时全部撤销，避免 LAN 断网。

// dnsListenPort 必须与 managedOverlay 中 dns.listen 保持一致。
const dnsListenPort = "1053"

// redirPort 必须与 managedOverlay 中 redir-port 保持一致。
const redirPort = "7892"

const nftTable = "clashv_dns"
const iptChain = "CLASHV_DNS"

// reservedNets 保留网段：目标是这些地址的流量不走透明代理（LAN 内部、
// 路由器自身端口等直连），fake-ip 网段不在此列、必须被重定向。
var reservedNets = []string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
	"169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16",
	"224.0.0.0/4", "240.0.0.0/4", "255.255.255.255/32",
}

// ApplyTrafficHooks 按设置套用 DNS 劫持与 TCP 透明代理（幂等，重复调用安全）。
func (m *Manager) ApplyTrafficHooks(s config.Settings) {
	if !m.cfg.IsOpenWrt() {
		return
	}
	// 先清掉旧规则再按当前设置重建，避免模式切换后残留
	m.clearFirewallRules()

	// TCP 透明代理
	if s.TUN {
		slog.Info("TUN 模式由内核 auto-route 接管流量，跳过防火墙重定向")
	} else if err := applyFirewallRules(false, true); err != nil {
		slog.Warn("TCP 透明代理启用失败", "err", err)
	} else {
		slog.Info("TCP 透明代理已启用", "listen_port", redirPort)
	}

	// DNS 劫持
	if !s.DNS || s.DNSHijack == "off" {
		if err := applyDnsmasqForward(false); err != nil {
			slog.Warn("DNS 劫持撤销失败（dnsmasq）", "err", err)
		}
		return
	}
	var err error
	switch s.DNSHijack {
	case "firewall":
		err = applyFirewallRules(true, false)
	case "dnsmasq":
		err = applyDnsmasqForward(true)
	}
	if err != nil {
		slog.Warn("DNS 劫持启用失败", "mode", s.DNSHijack, "err", err)
	} else {
		slog.Info("DNS 劫持已启用", "mode", s.DNSHijack, "listen_port", dnsListenPort)
	}
}

// RemoveTrafficHooks 撤销全部流量接管规则（幂等）。内核停止、设置变更与插件启动时调用。
func (m *Manager) RemoveTrafficHooks() {
	if !m.cfg.IsOpenWrt() {
		return
	}
	m.clearFirewallRules()
	if err := applyDnsmasqForward(false); err != nil {
		slog.Warn("DNS 劫持撤销失败（dnsmasq）", "err", err)
	}
}

// clearFirewallRules 清空自建的 nft 表与 iptables 链（不存在时静默）。
func (m *Manager) clearFirewallRules() {
	_ = exec.Command("nft", "delete", "table", "inet", nftTable).Run()
	_ = exec.Command("iptables", "-t", "nat", "-D", "PREROUTING", "-j", iptChain).Run()
	_ = exec.Command("iptables", "-t", "nat", "-F", iptChain).Run()
	_ = exec.Command("iptables", "-t", "nat", "-X", iptChain).Run()
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

// applyFirewallRules 用防火墙规则实现 DNS 劫持（dns=true）与 TCP 透明代理
// （redirect=true），两者可同时启用。优先 nft（OpenWrt 22.03+ / fw4），失败回退 iptables。
func applyFirewallRules(dns, redirect bool) error {
	if err := nftRules(dns, redirect); err == nil {
		return nil
	}
	return iptablesRules(dns, redirect)
}

func nftRules(dns, redirect bool) error {
	if !dns && !redirect {
		return nil
	}
	if err := run("nft", "add", "table", "inet", nftTable); err != nil {
		return err
	}
	if err := run("nft", "add", "chain", "inet", nftTable, "prerouting",
		"{ type nat hook prerouting priority dstnat; policy accept; }"); err != nil {
		return err
	}
	if dns {
		if err := run("nft", "add", "rule", "inet", nftTable, "prerouting",
			"udp", "dport", "53", "redirect", "to", ":"+dnsListenPort); err != nil {
			return err
		}
		if err := run("nft", "add", "rule", "inet", nftTable, "prerouting",
			"tcp", "dport", "53", "redirect", "to", ":"+dnsListenPort); err != nil {
			return err
		}
	}
	if redirect {
		// 保留网段直连；注意规则顺序：先放行保留网段，再兜底重定向其余 TCP
		args := []string{"add", "rule", "inet", nftTable, "prerouting",
			"ip", "daddr", "{" + strings.Join(reservedNets, ",") + "}", "return"}
		if err := run(args...); err != nil {
			return err
		}
		if err := run("nft", "add", "rule", "inet", nftTable, "prerouting",
			"tcp", "dport", "1-65535", "redirect", "to", ":"+redirPort); err != nil {
			return err
		}
	}
	return nil
}

func iptablesRules(dns, redirect bool) error {
	if !dns && !redirect {
		return nil
	}
	// 自建链 + PREROUTING 跳转，撤销时删链即可，不碰别人的规则
	_ = exec.Command("iptables", "-t", "nat", "-N", iptChain).Run()
	if dns {
		if err := run("iptables", "-t", "nat", "-A", iptChain,
			"-p", "udp", "--dport", "53", "-j", "REDIRECT", "--to-ports", dnsListenPort); err != nil {
			return err
		}
		if err := run("iptables", "-t", "nat", "-A", iptChain,
			"-p", "tcp", "--dport", "53", "-j", "REDIRECT", "--to-ports", dnsListenPort); err != nil {
			return err
		}
	}
	if redirect {
		for _, net := range reservedNets {
			if err := run("iptables", "-t", "nat", "-A", iptChain, "-d", net, "-j", "RETURN"); err != nil {
				return err
			}
		}
		if err := run("iptables", "-t", "nat", "-A", iptChain,
			"-p", "tcp", "-j", "REDIRECT", "--to-ports", redirPort); err != nil {
			return err
		}
	}
	// 跳转规则已存在则不重复加
	if err := exec.Command("iptables", "-t", "nat", "-C", "PREROUTING", "-j", iptChain).Run(); err == nil {
		return nil
	}
	return run("iptables", "-t", "nat", "-A", "PREROUTING", "-j", iptChain)
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
