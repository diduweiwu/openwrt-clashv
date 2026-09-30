// clashv: OpenWrt mihomo 管理插件
//
// 子命令:
//
//	clashv run      启动管理服务（默认）
//	clashv version  打印版本
package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"clashv/internal/api"
	"clashv/internal/config"
	"clashv/internal/core"
	"clashv/internal/profiles"
)

var Version = "dev"

func usage() {
	fmt.Fprintf(os.Stderr, `clashv %s — OpenWrt mihomo 管理插件

用法:
  clashv run [-dev]   启动管理服务（默认子命令）
  clashv version      打印版本

选项:
  -dev    开发模式：使用文件存储(~/.clashv)而非 UCI，前端从 web/dist 读取
`, Version)
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println(Version)
			return
		case "-h", "--help", "help":
			usage()
			return
		}
	}

	dev := false
	args := []string{}
	for _, a := range os.Args[1:] {
		if a == "-dev" {
			dev = true
		} else {
			args = append(args, a)
		}
	}
	if len(args) > 0 && args[0] != "run" {
		usage()
		os.Exit(2)
	}

	cfg := config.New(dev)
	// 尽早建目录并接文件日志，后续 slog 才能落盘
	_ = cfg.EnsureDirs()
	setupLogger(cfg)
	prof := profiles.New(cfg)
	mgr := core.NewManager(cfg, prof, Version)
	// 日志占用常驻清理：运行中按大小轮转 + 过期旧档删除
	go core.LogJanitor(cfg)
	// 开机自启内核：路由器重启后 procd 只拉起本服务，内核不会自己跑起来。
	// 按状态记忆恢复：重启前内核在运行才自动拉起（见 Manager markCoreState）。
	go autoStartCore(cfg, prof, mgr)

	if err := api.Serve(cfg, prof, mgr, Version); err != nil {
		fmt.Fprintln(os.Stderr, "clashv:", err)
		os.Exit(1)
	}
}

// autoStartCore 在服务启动后按运行状态记忆自动拉起内核：重启前内核在运行
// （core.state == running）才恢复，手动停过的不动。前置不满足（内核还没
// 下载、没有任何订阅）时静默跳过；启动失败只记日志，不影响插件本身运行。
func autoStartCore(cfg *config.Manager, prof *profiles.Manager, mgr *core.Manager) {
	// init 脚本 START=99 已在开机尾声，这里再留几秒让网络/防火墙稳定
	time.Sleep(3 * time.Second)
	if !mgr.CoreWasRunning() {
		return
	}
	if _, err := os.Stat(cfg.CorePath()); err != nil {
		return // 内核尚未下载
	}
	if list, _ := prof.List(); len(list) == 0 {
		return // 尚未添加订阅
	}
	if err := mgr.Start(); err != nil {
		slog.Warn("内核自动恢复启动失败，请到界面手动启动", "err", err)
		return
	}
	slog.Info("内核已随服务自动恢复运行")
}
