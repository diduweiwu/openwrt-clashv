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

set -e
cd "$(dirname "$0")/.."

OUT="${1:-bin}"
VERSION="${2:-dev}"
export PATH="$HOME/.local/go/bin:$PATH"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export CGO_ENABLED=0

TARGETS="
linux:arm64::
linux:arm:GOARM=7:armv7
linux:mips::
linux:mipsle::
linux:amd64::
linux:riscv64::
linux:loong64::
"

mkdir -p "$OUT"

# 前端产物缺失时先构建（正式打包需要；CI 会显式构建）
if [ ! -d web/dist/assets ] && command -v npm >/dev/null 2>&1; then
	echo "==> 构建前端"
	(cd web && npm install --no-fund --no-audit && npm run build)
	rm -rf internal/web/dist/assets
	cp -r web/dist/* internal/web/dist/
fi

LDFLAGS="-s -w -X main.Version=$VERSION"

echo "$TARGETS" | while IFS=: read -r goos goarch extra suffix; do
	[ -z "$goarch" ] && continue
	name="$goarch"
	[ -n "$suffix" ] && name="$suffix"
	echo "==> ${goos}/${goarch} ${extra} -> clashv-$name"
	# shellcheck disable=SC2086
	env GOOS="$goos" GOARCH="$goarch" $extra \
		go build -trimpath -ldflags "$LDFLAGS" \
		-o "$OUT/clashv-$name" ./cmd/clashv
done

echo "==> 完成:"
ls -lh "$OUT"/clashv-*
