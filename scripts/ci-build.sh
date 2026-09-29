#!/bin/sh
# 构建流水线：GitHub Actions 与本地 Docker 构建共用，保证两边流程一致。
#
# CI  入口: .github/workflows/compile_packages.yml 的 Compile job（按 STAGE 分步调用）
# 本地入口: scripts/build-local-docker.sh（一次跑完所有阶段）
#
# 用法: TARGET=ipk|apk [STAGE=frontend|go|sdk|package|all] ./scripts/ci-build.sh
#
# 环境变量:
#   TARGET        构建目标: ipk（22.03 SDK）或 apk（snapshot SDK），必填
#   STAGE         只跑某个阶段（CI 分步用，日志按步展示）；默认 all 顺序全部执行
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
PKG_NAME=openclash-air

STAGE="${STAGE:-all}"
case "$STAGE" in
  frontend|go|sdk|package|all) ;;
  *) echo "ERROR: STAGE 必须是 frontend|go|sdk|package|all，当前为 $STAGE" >&2; exit 1 ;;
esac

VERSION="${VERSION:-$(grep 'PKG_VERSION:=' ./openwrt/Makefile | awk -F '=' '{print $2}')}"
if [ -z "$VERSION" ]; then
  echo "ERROR: 无法从 openwrt/Makefile 读取 PKG_VERSION" >&2
  exit 1
fi
echo "==> openclash-air $VERSION (target: $TARGET, stage: $STAGE)"

# ---------- 前端构建 ----------
# 副本目录里构建：npm 会装平台相关的二进制依赖（如 esbuild），在容器里装出的是
# linux 版，直接装进宿主机挂载目录会和 darwin 版 node_modules 冲突
if [ "$STAGE" = "frontend" ] || [ "$STAGE" = "all" ]; then
  echo "==> [frontend] 构建前端 (Vite)"
  WEB_BUILD=$(mktemp -d)
  trap 'rm -rf "$WEB_BUILD"' EXIT
  rsync -a --exclude node_modules --exclude dist web/ "$WEB_BUILD/"
  (cd "$WEB_BUILD" && npm install --no-fund --no-audit && npm run build)
  rm -rf internal/web/dist/assets
  mkdir -p internal/web/dist
  cp -r "$WEB_BUILD"/dist/* internal/web/dist/
fi

# ---------- Go 二进制 ----------
if [ "$STAGE" = "go" ] || [ "$STAGE" = "all" ]; then
  echo "==> [go] 构建 Go 二进制 (7 arch)"
  ./scripts/build-linux.sh bin "$VERSION"
fi

SDK_CACHE_DIR="${SDK_CACHE_DIR:-$REPO_ROOT/tmp}"
OUT_DIR="${OUT_DIR:-$REPO_ROOT/tmp/out}"

# ---------- OpenWrt SDK ----------
if [ "$STAGE" = "sdk" ] || [ "$STAGE" = "all" ]; then
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
    echo "==> [sdk] 下载 OpenWrt SDK ($TARGET)"
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
      rm -f SNAPSDK.tar
      rm -rf "$SDK_NAME"
      # 解压出的目录固定叫 openwrt-sdk-*，直接 glob；不要用 tar tf 读
      # 整个 1.4GB tarball 拿目录名（慢，且 head 提前退出会报无害的
      # "tar: stdout: write error"）
      mv openwrt-sdk-* "$SDK_NAME"
    fi
  else
    echo "==> [sdk] 复用缓存的 SDK: $SDK_CACHE_DIR/$SDK_NAME"
  fi
fi

# ---------- 编译打包 ----------
if [ "$STAGE" = "package" ] || [ "$STAGE" = "all" ]; then
  if [ "$TARGET" = "ipk" ]; then SDK_NAME=SDK; else SDK_NAME=SNAPSDK; fi
  SDK="$SDK_CACHE_DIR/$SDK_NAME"

  echo "==> [package] 拷贝包源码进 SDK"
  PKG_SRC="$SDK/package/$PKG_NAME"
  mkdir -p "$PKG_SRC"
  # 整个仓库（含刚构建的 bin/ 与前端产物）进入包目录；
  # --delete 保证 SDK 缓存复用时不会残留已删除的旧源文件
  rsync -a --delete \
    --exclude .git --exclude tmp --exclude build --exclude .devdata \
    --exclude .DS_Store --exclude web/node_modules \
    "$REPO_ROOT/" "$PKG_SRC/"
  # 包 Makefile 放到包目录根（SDK 约定）
  cp "$PKG_SRC/openwrt/Makefile" "$PKG_SRC/Makefile"
  # 嵌套的 openwrt/Makefile 与包根 Makefile 内容相同，SDK 元数据扫描会把两份
  # 都当成包定义，删掉包内这份（install 引用的 openwrt/root/ 等文件保留）
  rm -f "$PKG_SRC/openwrt/Makefile"
  # 清掉上次构建残留在 SDK bin/ 里的旧产物，避免不同版本号混入
  rm -f "$SDK/bin/openclash-air_*.ipk" "$SDK/bin/openclash-air-*.apk"

  echo "==> [package] 编译包 (make package/$PKG_NAME/compile)"
  # 与 OpenClash 的打包逻辑一致：包正确注册（BuildPackage 宏）后，SDK 模式下
  # make defconfig 会把所有包默认置为 m，无需手动写入 CONFIG
  cd "$SDK"
  make defconfig
  grep -q "^CONFIG_PACKAGE_$PKG_NAME=m" .config || {
    echo "ERROR: $PKG_NAME 未能进入 SDK .config（多半是 DEPENDS 在该 SDK 中缺失）" >&2
    exit 1
  }
  # SDK 默认把全部 1200+ 个 kmod 置为 m，而本包依赖的 kmod-tun 由
  # package/kernel/linux 一个 Makefile 生成——依赖链会触发整个 kernel 包
  # 编译（=m 的 kmod 全编，数十分钟到数小时甚至 OOM）。只保留 kmod-tun
  # 及其依赖，其余全部关掉
  sed -i 's/^CONFIG_PACKAGE_kmod-\([^=]*\)=m/# CONFIG_PACKAGE_kmod-\1 is not set/' .config
  echo "CONFIG_PACKAGE_kmod-tun=m" >> .config
  make defconfig
  echo "==> 选中的 kmod: $(grep -c '^CONFIG_PACKAGE_kmod-.*=m' .config) 个"
  grep '^CONFIG_PACKAGE_kmod-.*=m' .config | head
  make package/$PKG_NAME/compile V=s

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
fi
