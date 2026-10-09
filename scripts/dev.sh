#!/bin/sh
# 本地一键开发环境：Go 后端 + 前端 vite 热更新，改代码/联调/测升级不用走 CI 打包。
#
# 用法:
#   ./scripts/dev.sh                # 构建后端并启动（日常开发，改完代码重跑即生效）
#   ./scripts/dev.sh --no-build     # 跳过构建，直接跑 tmp/dev/clashv 现有二进制
#   ./scripts/dev.sh --clean        # 用隔离 HOME（tmp/dev/home）起全新实例，不碰 ~/.clashv
#
# 地址:
#   前端（热更新，改 .vue 立即生效）  http://localhost:5173   （/api 已代理到后端）
#   后端（内嵌 UI，构建时快照）       http://localhost:9097
#
# 升级测试（本地完整走「检查更新→下载→替换→重启」）:
#   1. ./scripts/dev.sh                        起服务（版本号 dev，恒显示可更新）
#   2. 界面里点升级                            下载 Release 资产并替换 tmp/dev/clashv
#   3. Ctrl-C 后 ./scripts/dev.sh --no-build   跑起来的就是升级后的版本
#   注：内核（mihomo）升级本地可完整测试；插件升级测的是裸二进制路径，
#   路由器上的 ipk/apk 安装流程仍需真机验证。
#
# 前置: go 在 ~/.local/go/bin；web/node_modules 缺失时自动 npm install。

set -e
cd "$(dirname "$0")/.."

BUILD=1
NOCLEAN=1
for arg in "$@"; do
	case "$arg" in
	--no-build) BUILD=0 ;;
	--clean) NOCLEAN=0 ;;
	*) echo "未知参数: ${arg}（支持 --no-build / --clean）" >&2; exit 1 ;;
	esac
done

export PATH="$HOME/.local/go/bin:$PATH"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

BIN=tmp/dev/clashv
mkdir -p tmp/dev

if [ -n "$(lsof -nP -iTCP:9097 -sTCP:LISTEN 2>/dev/null)" ]; then
	echo "ERROR: 9097 端口已被占用（上次的服务没停？）：" >&2
	lsof -nP -iTCP:9097 -sTCP:LISTEN >&2
	exit 1
fi

if [ "$BUILD" -eq 1 ]; then
	echo "==> 构建后端 -> $BIN"
	go build -trimpath -ldflags "-s -w -X main.Version=dev" -o "$BIN" ./cmd/clashv
elif [ ! -x "$BIN" ]; then
	echo "==> $BIN 不存在，改为构建"
	go build -trimpath -ldflags "-s -w -X main.Version=dev" -o "$BIN" ./cmd/clashv
else
	# ⚠ ${BIN} 必须加花括号：macOS 的 bash 3.2 下 $VAR 紧跟全角字符会把变量吞空
	echo "==> 使用现有 ${BIN}（--no-build）"
fi

# 前端依赖缺失时安装（npmmirror 兜底，国内环境 npm 默认源可能超时）
if [ ! -d web/node_modules ]; then
	echo "==> 安装前端依赖"
	(cd web && npm install --no-fund --no-audit) || (cd web && npm install --no-fund --no-audit --registry=https://registry.npmmirror.com)
fi

BACK_PID=""
FRONT_PID=""
cleanup() {
	[ -n "$BACK_PID" ] && kill "$BACK_PID" 2>/dev/null
	[ -n "$FRONT_PID" ] && kill "$FRONT_PID" 2>/dev/null
}
trap cleanup EXIT INT TERM

if [ "$NOCLEAN" -eq 0 ]; then
	mkdir -p tmp/dev/home
	echo "==> 启动后端（隔离 HOME=tmp/dev/home）: http://localhost:9097"
	HOME="$PWD/tmp/dev/home" "$BIN" run -dev &
else
	echo "==> 启动后端: http://localhost:9097"
	"$BIN" run -dev &
fi
BACK_PID=$!

echo "==> 启动前端（热更新）: http://localhost:5173"
(cd web && npm run dev) &
FRONT_PID=$!

echo
echo "    开发入口: http://localhost:5173   （后端内嵌 UI 在 :9097）"
echo "    Ctrl-C 退出并停掉两个进程；测升级用 --no-build，见脚本头部说明"
echo
wait
