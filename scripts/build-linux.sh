#!/bin/sh
# 构建 clashv 的 Linux 二进制
#
# 用法: ./scripts/build-linux.sh [输出目录] [版本号]
#
# 产物命名（通用架构名，供 openwrt 打包与启动器按设备架构选择）:
#   clashv-arm64     aarch64 全系（cortex-a53/a72/generic…）
#   clashv-armv7     armv6/v7（cortex-a7/a9…，armv6 设备可尝试）
#   clashv-mips      big-endian 软浮点（mips_24kc 等）
#   clashv-mipsle    little-endian 软浮点（mipsel_24kc 等）
#   clashv-amd64     x86_64（GOAMD64=v1 最大兼容）
#   clashv-riscv64   riscv64_generic
#   clashv-loong64   loongarch64_generic
# 另产 darwin 双架构裸二进制（不进 OpenWrt 包，随 Release 上传）：本地 mac
# 开发时可在界面上测插件自更新（裸二进制替换路径），见 scripts/dev.sh。

set -e
cd "$(dirname "$0")/.."

OUT="${1:-bin}"
VERSION="${2:-dev}"
export PATH="$HOME/.local/go/bin:$PATH"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export CGO_ENABLED=0

mkdir -p "$OUT"

# 前端产物缺失时先构建（正式打包需要；CI 会显式构建）
if [ ! -d web/dist/assets ] && command -v npm >/dev/null 2>&1; then
	echo "==> 构建前端"
	(cd web && npm install --no-fund --no-audit && npm run build)
	rm -rf internal/web/dist/assets
	cp -r web/dist/* internal/web/dist/
fi

LDFLAGS="-s -w -X main.Version=$VERSION"

# 9 个二进制并行编译（4 核 CI runner 上串行 ~90s → 并行 ~30s；go build 缓存并发安全）。
# 各架构输出重定向到独立日志，成功后删掉，失败时统一倒出，避免并行输出穿插难读
build_arch() {
	arch_name=$4
	[ -n "$arch_name" ] || arch_name=$2
	echo "==> $1/$2 $3 -> clashv-$arch_name"
	# shellcheck disable=SC2086
	env GOOS="$1" GOARCH="$2" $3 \
		go build -trimpath -ldflags "$LDFLAGS" \
		-o "$OUT/clashv-$arch_name" ./cmd/clashv \
		>"$OUT/.build-$arch_name.log" 2>&1
}

rc=0
pids=""
build_arch linux arm64 "" "" & pids="$pids $!"
build_arch linux arm GOARM=7 armv7 & pids="$pids $!"
build_arch linux mips "" "" & pids="$pids $!"
build_arch linux mipsle "" "" & pids="$pids $!"
build_arch linux amd64 "" "" & pids="$pids $!"
build_arch linux riscv64 "" "" & pids="$pids $!"
build_arch linux loong64 "" "" & pids="$pids $!"
# darwin：第 4 参必须显式给，否则回落 GOARCH 会与 linux 的 clashv-arm64/amd64 撞名
build_arch darwin arm64 "" darwin-arm64 & pids="$pids $!"
build_arch darwin amd64 "" darwin-amd64 & pids="$pids $!"
for p in $pids; do
	wait "$p" || rc=1
done
if [ "$rc" -ne 0 ]; then
	echo "ERROR: Go 构建失败，各架构日志:"
	cat "$OUT"/.build-*.log
	exit 1
fi
rm -f "$OUT"/.build-*.log

echo "==> 完成:"
ls -lh "$OUT"/clashv-*
