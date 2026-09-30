package core

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"clashv/internal/config"
)

// 流量接管（旁路由/网关模式的关键），两部分必须配套：
//
//  1. DNS 劫持：LAN 的 53 端口查询交给内核 DNS（1053，fake-ip 或
//     redir-host 模式由设置决定），否则域名解析被污染，google 等解析
//     到错误 IP。方式二选一（dns_hijack 设置）：
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
const fwChain = "CLASHV_DNS"

// reservedNets 保留网段：目标是这些地址的流量不走透明代理（LAN 内部、
// 路由器自身端口等直连），fake-ip 网段不在此列、必须被重定向。
var reservedNets = []string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
	"169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16",
	"224.0.0.0/4", "240.0.0.0/4", "255.255.255.255/32",
}

// binDirs OpenWrt 的系统命令目录；procd 等场景下 PATH 可能被精简，找不到时逐个兜底。
var binDirs = []string{"/usr/sbin", "/sbin", "/usr/bin", "/bin"}

// findBin 定位系统命令：先 LookPath，再扫 OpenWrt 常见 sbin 目录。
func findBin(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	for _, dir := range binDirs {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return p, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// run 执行系统命令（自动定位可执行文件），错误信息带命令原文。
func run(name string, args ...string) error {
	bin, err := findBin(name)
	if err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		return trimErr(append([]string{name}, args...), out, err)
	}
	return nil
}

// try 尽力执行、失败静默，只用于清理场景。
func try(name string, args ...string) { _ = run(name, args...) }

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

// ApplyTrafficHooks 按设置套用 DNS 劫持与 TCP 透明代理（幂等，重复调用安全）。
func (m *Manager) ApplyTrafficHooks(s config.Settings) {
	if !m.cfg.IsOpenWrt() {
		return
	}
	// 启动流程与设置保存都会异步调进来，防火墙重建必须串行
	m.hookMu.Lock()
	defer m.hookMu.Unlock()

	// 先清掉旧规则再按当前设置重建，避免模式切换后残留
	m.clearFirewallRules()

	// TCP 透明代理与 DNS 劫持（firewall 模式）合并为一次防火墙重建：
	// 分两次各自建表会互相干扰（表/链重复创建、残留半套规则）
	dnsFirewall := s.DNS && s.DNSHijack == "firewall"
	tcpRedirect := !s.TUN
	if s.TUN {
		slog.Info("TUN 模式由内核 auto-route 接管流量，跳过防火墙重定向")
	}
	if dnsFirewall || tcpRedirect {
		if err := applyFirewallRules(dnsFirewall, tcpRedirect); err != nil {
			slog.Warn("防火墙接管启用失败（DNS 劫持/TCP 透明代理）",
				"dns", dnsFirewall, "tcp_redirect", tcpRedirect, "err", err)
		} else {
			if tcpRedirect {
				slog.Info("TCP 透明代理已启用", "listen_port", redirPort)
			}
			if dnsFirewall {
				slog.Info("DNS 劫持已启用", "mode", "firewall", "listen_port", dnsListenPort)
			}
		}
	}

	// DNS 劫持（dnsmasq 模式）与撤销
	switch {
	case s.DNS && s.DNSHijack == "dnsmasq":
		if err := applyDnsmasqForward(true); err != nil {
			slog.Warn("DNS 劫持启用失败", "mode", "dnsmasq", "err", err)
		} else {
			slog.Info("DNS 劫持已启用", "mode", "dnsmasq", "listen_port", dnsListenPort)
		}
	case !s.DNS || s.DNSHijack == "off":
		if err := applyDnsmasqForward(false); err != nil {
			slog.Warn("DNS 劫持撤销失败（dnsmasq）", "err", err)
		}
	}
}

// RemoveTrafficHooks 撤销全部流量接管规则（幂等）。内核停止、设置变更与插件启动时调用。
func (m *Manager) RemoveTrafficHooks() {
	if !m.cfg.IsOpenWrt() {
		return
	}
	m.hookMu.Lock()
	defer m.hookMu.Unlock()
	m.clearFirewallRules()
	if err := applyDnsmasqForward(false); err != nil {
		slog.Warn("DNS 劫持撤销失败（dnsmasq）", "err", err)
	}
}

// clearFirewallRules 清空自建的 nft 表与 iptables 链（不存在时静默）。
func (m *Manager) clearFirewallRules() {
	try("nft", "delete", "table", "inet", nftTable)
	try("iptables", "-t", "nat", "-D", "PREROUTING", "-j", fwChain)
	try("iptables", "-t", "nat", "-F", fwChain)
	try("iptables", "-t", "nat", "-X", fwChain)
}

// applyFirewallRules 用防火墙规则实现 DNS 劫持（dns=true）与 TCP 透明代理
// （redirect=true），两者可同时启用。优先 nft（OpenWrt 22.03+ / fw4），
// 失败回退 iptables；两者都失败时必须把 nft 的原始错误一并带出，
// 否则回退路径会掩盖真正的失败原因（只有 iptables 缺失这一条）。
func applyFirewallRules(dns, redirect bool) error {
	if !dns && !redirect {
		return nil
	}
	nftErr := nftRules(dns, redirect)
	if nftErr != nil {
		// 重建是幂等的；nft/内核不旧时规则写法不该失败，多为瞬时冲突
		// （fw4 并发提交 netlink 等），稍候重试一次再谈回退
		time.Sleep(300 * time.Millisecond)
		nftErr = nftRules(dns, redirect)
	}
	if nftErr == nil {
		return nil
	}
	iptErr := iptablesRules(dns, redirect)
	if iptErr == nil {
		slog.Warn("nft 规则失败，已回退 iptables", "err", nftErr)
		return nil
	}
	return fmt.Errorf("nft 失败: %v；iptables 失败: %v", nftErr, iptErr)
}

// nftRules 用 nft 实现与 iptables 版同构的规则：nat 基础链只挂一个 jump，
// 具体规则放在普通链 CLASHV_DNS 里，保留网段逐条 return——不使用匿名集合
// （interval 集合在部分老内核上会被拒绝，且报错难定位）。
func nftRules(dns, redirect bool) error {
	// 先删再建：部分旧版 nft 对已存在的表/链执行 add 会报错
	try("nft", "delete", "table", "inet", nftTable)
	if err := run("nft", "add", "table", "inet", nftTable); err != nil {
		return err
	}
	if err := run("nft", "add", "chain", "inet", nftTable, "prerouting",
		"{ type nat hook prerouting priority dstnat; policy accept; }"); err != nil {
		return err
	}
	if err := run("nft", "add", "chain", "inet", nftTable, fwChain); err != nil {
		return err
	}
	if err := run("nft", "add", "rule", "inet", nftTable, "prerouting",
		"jump", fwChain); err != nil {
		return err
	}
	if dns {
		if err := run("nft", "add", "rule", "inet", nftTable, fwChain,
			"udp", "dport", "53", "redirect", "to", ":"+dnsListenPort); err != nil {
			return err
		}
		if err := run("nft", "add", "rule", "inet", nftTable, fwChain,
			"tcp", "dport", "53", "redirect", "to", ":"+dnsListenPort); err != nil {
			return err
		}
	}
	if redirect {
		// 保留网段直连；注意规则顺序：先放行保留网段，再兜底重定向其余 TCP
		for _, net := range reservedNets {
			if err := run("nft", "add", "rule", "inet", nftTable, fwChain,
				"ip", "daddr", net, "return"); err != nil {
				return err
			}
		}
		// ip protocol 限定 IPv4，与 iptables nat 表语义一致
		if err := run("nft", "add", "rule", "inet", nftTable, fwChain,
			"ip", "protocol", "tcp", "redirect", "to", ":"+redirPort); err != nil {
			return err
		}
	}
	return nil
}

func iptablesRules(dns, redirect bool) error {
	ipt, err := findBin("iptables")
	if err != nil {
		return fmt.Errorf("iptables 不可用（fw4 系统需安装 iptables-nft，或排查 nft 后端失败原因）: %w", err)
	}
	runIpt := func(args ...string) error {
		out, err := exec.Command(ipt, args...).CombinedOutput()
		if err != nil {
			return trimErr(append([]string{"iptables"}, args...), out, err)
		}
		return nil
	}
	// 自建链 + PREROUTING 跳转，撤销时删链即可，不碰别人的规则
	_ = runIpt("-t", "nat", "-N", fwChain)
	if dns {
		if err := runIpt("-t", "nat", "-A", fwChain,
			"-p", "udp", "--dport", "53", "-j", "REDIRECT", "--to-ports", dnsListenPort); err != nil {
			return err
		}
		if err := runIpt("-t", "nat", "-A", fwChain,
			"-p", "tcp", "--dport", "53", "-j", "REDIRECT", "--to-ports", dnsListenPort); err != nil {
			return err
		}
	}
	if redirect {
		for _, net := range reservedNets {
			if err := runIpt("-t", "nat", "-A", fwChain, "-d", net, "-j", "RETURN"); err != nil {
				return err
			}
		}
		if err := runIpt("-t", "nat", "-A", fwChain,
			"-p", "tcp", "-j", "REDIRECT", "--to-ports", redirPort); err != nil {
			return err
		}
	}
	// 跳转规则已存在则不重复加
	if err := runIpt("-t", "nat", "-C", "PREROUTING", "-j", fwChain); err == nil {
		return nil
	}
	return runIpt("-t", "nat", "-A", "PREROUTING", "-j", fwChain)
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
