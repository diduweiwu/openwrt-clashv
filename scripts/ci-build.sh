#!/bin/sh
# 构建流水线：GitHub Actions 与本地 Docker 构建共用，保证两边流程一致。
#
# CI  入口: .github/workflows/compile_packages.yml 的 Compile job
# 本地入口: scripts/build-local-docker.sh（Docker/OrbStack）
#
# 用法: TARGET=ipk|apk ./scripts/ci-build.sh
#
# 环境变量:
#   TARGET        构建目标: ipk（22.03 SDK）或 apk（snapshot SDK），必填
#   VERSION       包版本号，默认读 openwrt/Makefile 的 PKG_VERSION
#   SDK_URL       覆盖 ipk 目标的 SDK 下载地址（默认与 CI 相同）
#   SDK_CACHE_DIR SDK 下载/解压目录，默认 ./tmp（本地构建可指向持久缓存卷）
#   OUT_DIR       产物输出目录，默认 ./tmp/out
#
# 前置依赖（CI 由 Install Dependencies 步骤提供；本地由构建镜像提供）:
#   node/npm、go、rsync、zstd、curl、tar、make、gcc 等 OpenWrt SDK 编译前置
set -e
cd "$(dirname "$0")/.."
REPO_ROOT=$(pwd)

TARGET="${TARGET:?请设置 TARGET=ipk 或 apk}"
case "$TARGET" in
  ipk|apk) ;;
  *) echo "ERROR: TARGET 必须是 ipk 或 apk，当前为 $TARGET" >&2; exit 1 ;;
esac

VERSION="${VERSION:-$(grep 'PKG_VERSION:=' ./openwrt/Makefile | awk -F '=' '{print $2}')}"
if [ -z "$VERSION" ]; then
  echo "ERROR: 无法从 openwrt/Makefile 读取 PKG_VERSION" >&2
  exit 1
fi
echo "==> openclash-air $VERSION ($TARGET)"

# ---------- 1. 前端构建 ----------
# 副本目录里构建：npm 会装平台相关的二进制依赖（如 esbuild），在容器里装出的是
# linux 版，直接装进宿主机挂载目录会和 darwin 版 node_modules 冲突
echo "==> [1/5] 构建前端 (Vite)"
WEB_BUILD=$(mktemp -d)
trap 'rm -rf "$WEB_BUILD"' EXIT
rsync -a --exclude node_modules --exclude dist web/ "$WEB_BUILD/"
(cd "$WEB_BUILD" && npm install --no-fund --no-audit && npm run build)
rm -rf internal/web/dist/assets
mkdir -p internal/web/dist
cp -r "$WEB_BUILD"/dist/* internal/web/dist/

# ---------- 2. Go 二进制 ----------
echo "==> [2/5] 构建 Go 二进制 (7 arch)"
./scripts/build-linux.sh bin "$VERSION"

# ---------- 3. OpenWrt SDK ----------
SDK_CACHE_DIR="${SDK_CACHE_DIR:-$REPO_ROOT/tmp}"
OUT_DIR="${OUT_DIR:-$REPO_ROOT/tmp/out}"
mkdir -p "$SDK_CACHE_DIR"
cd "$SDK_CACHE_DIR"

if [ "$TARGET" = "ipk" ]; then
  # 22.03 SDK：opkg 系统可用（22.03/23.05 全部）
  SDK_NAME=SDK
else
  # snapshot SDK：apk 系统可用（OpenWrt 24.10+/snapshot）
  SDK_NAME=SNAPSDK
fi

if [ ! -d "$SDK_NAME" ]; then
  echo "==> [3/5] 下载 OpenWrt SDK ($TARGET)"
  if [ "$TARGET" = "ipk" ]; then
    curl -SLfk --connect-timeout 30 --retry 3 \
      "${SDK_URL:-https://downloads.openwrt.org/releases/22.03.7/targets/x86/64/openwrt-sdk-22.03.7-x86-64_gcc-11.2.0_musl.Linux-x86_64.tar.xz}" \
      -o ./SDK.tar.xz
    tar xf SDK.tar.xz
    rm -f SDK.tar.xz
    rm -rf "$SDK_NAME"
    mv openwrt-sdk-* "$SDK_NAME"
  else
    BASE_URL="https://downloads.openwrt.org/snapshots/targets/x86/64/"
    SDK_TARBALL=$(curl -sf "$BASE_URL" | grep -oE 'openwrt-sdk-x86-64[^"]+\.tar\.zst' | head -n 1)
    if ! curl -SLfk --connect-timeout 30 --retry 3 "$BASE_URL/$SDK_TARBALL" -o ./SNAPSDK.tar.zst; then
      BASE_URL2="https://mirrors.pku.edu.cn/files/openwrt/snapshots/targets/x86/64/"
      SDK_TARBALL=$(curl -sf "$BASE_URL2" | grep -oE 'openwrt-sdk-x86-64[^"]+\.tar\.zst' | head -n 1)
      curl -SLfk --connect-timeout 30 --retry 3 "$BASE_URL2/$SDK_TARBALL" -o ./SNAPSDK.tar.zst
    fi
    zstd -d SNAPSDK.tar.zst
    rm -f SNAPSDK.tar.zst
    tar xf SNAPSDK.tar
    SDK_DIR=$(tar tf SNAPSDK.tar | head -n 1 | cut -d/ -f1)
    rm -f SNAPSDK.tar
    rm -rf "$SDK_NAME"
    mv "$SDK_DIR" "$SDK_NAME"
  fi
else
  echo "==> [3/5] 复用缓存的 SDK: $SDK_CACHE_DIR/$SDK_NAME"
fi

# ---------- 4. 拷贝包源码 ----------
echo "==> [4/5] 拷贝包源码进 SDK"
SDK="$SDK_CACHE_DIR/$SDK_NAME"
PKG_SRC="$SDK/package/openclash-air"
mkdir -p "$PKG_SRC"
# 整个仓库（含刚构建的 bin/ 与前端产物）进入包目录；
# --delete 保证 SDK 缓存复用时不会残留已删除的旧源文件
rsync -a --delete \
  --exclude .git --exclude tmp --exclude build --exclude .devdata \
  --exclude .DS_Store --exclude web/node_modules \
  "$REPO_ROOT/" "$PKG_SRC/"
# 包 Makefile 放到包目录根（SDK 约定）
cp "$PKG_SRC/openwrt/Makefile" "$PKG_SRC/Makefile"
# 清掉上次构建残留在 SDK bin/ 里的旧产物，避免不同版本号混入
rm -f "$SDK/bin/openclash-air_*.ipk" "$SDK/bin/openclash-air-*.apk"

# ---------- 5. 编译包并收集产物 ----------
echo "==> [5/5] 编译包 (make package/openclash-air/compile)"
cd "$SDK"
# defconfig 不会自动选中第三方包；不显式打开，package/xxx/compile 目标不会生成
echo "CONFIG_PACKAGE_openclash-air=m" >> .config
make defconfig
grep -q "^CONFIG_PACKAGE_openclash-air=m" .config || {
  echo "ERROR: openclash-air 未能进入 SDK .config（多半是 DEPENDS 在该 SDK 中缺失）" >&2
  exit 1
}
make package/openclash-air/compile V=s

case "$TARGET" in
  ipk) PATTERN='openclash-air_*.ipk' ;;
  apk) PATTERN='openclash-air-*.apk' ;;
esac
rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"
find bin -name "$PATTERN" -exec cp {} "$OUT_DIR/" \;
ls "$OUT_DIR"/* >/dev/null 2>&1 || {
  echo "ERROR: bin/ 下未找到匹配 $PATTERN 的产物" >&2
  exit 1
}
echo "==> 产物:"
ls -lh "$OUT_DIR"
