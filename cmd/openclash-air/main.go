// openclash-air: OpenWrt mihomo 管理插件
//
// 子命令:
//
//	openclash-air run      启动管理服务（默认）
//	openclash-air version  打印版本
package main

import (
	"fmt"
	"os"

	"openclash-air/internal/api"
	"openclash-air/internal/config"
	"openclash-air/internal/core"
	"openclash-air/internal/profiles"
)

var Version = "dev"

func usage() {
	fmt.Fprintf(os.Stderr, `openclash-air %s — OpenWrt mihomo 管理插件

用法:
  openclash-air run [-dev]   启动管理服务（默认子命令）
  openclash-air version      打印版本

选项:
  -dev    开发模式：使用文件存储(~/.openclash-air)而非 UCI，前端从 web/dist 读取
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
	prof := profiles.New(cfg)
	mgr := core.NewManager(cfg, prof, Version)

	if err := api.Serve(cfg, prof, mgr, Version); err != nil {
		fmt.Fprintln(os.Stderr, "openclash-air:", err)
		os.Exit(1)
	}
}
