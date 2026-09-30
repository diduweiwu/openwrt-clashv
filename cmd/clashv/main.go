// clashv: OpenWrt mihomo 管理插件
//
// 子命令:
//
//	clashv run      启动管理服务（默认）
//	clashv version  打印版本
package main

import (
	"fmt"
	"os"

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

	if err := api.Serve(cfg, prof, mgr, Version); err != nil {
		fmt.Fprintln(os.Stderr, "clashv:", err)
		os.Exit(1)
	}
}
