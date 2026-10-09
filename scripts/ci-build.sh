#!/bin/sh
# 构建流水线：GitHub Actions 与本地 Docker 构建共用，保证两边流程一致。
#
# CI  入口: .github/workflows/compile_packages.yml 的 Compile job（按 STAGE 分步调用）
# 本地入口: scripts/build-local-docker.sh（一次跑完所有阶段）
#
# 用法: TARGET=ipk|apk [STAGE=frontend|go|sdkname|sdk|package|all] ./scripts/ci-build.sh
#
# 环境变量:
#   TARGET        构建目标: ipk（22.03 SDK）或 apk（snapshot SDK），必填
#   STAGE         只跑某个阶段（CI 分步用，日志按步展示）；sdkname 只解析 SDK
#                 压缩包文件名不下载，供 CI 缓存 key 使用；默认 all 顺序全部执行
#   VERSION       包版本号，默认读 openwrt/Makefile 的 PKG_VERSION
#   SDK_URL       覆盖 ipk 目标的 SDK 下载地址（默认与 CI 相同）
#   SDK_IMAGE_IPK/APK  覆盖/禁用 Docker Hub 的 SDK 镜像（显式设空 = 禁用镜像
#                 模式，回落 tarball 直下；默认 openwrt/sdk:x86-64-22.03.7 / :x86-64-25.12.5）
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
PKG_NAME=luci-app-clashv

STAGE="${STAGE:-all}"
case "$STAGE" in
  frontend|go|sdkname|sdk|package|all) ;;
  *) echo "ERROR: STAGE 必须是 frontend|go|sdk|package|all，当前为 $STAGE" >&2; exit 1 ;;
esac

VERSION="${VERSION:-$(grep 'PKG_VERSION:=' ./openwrt/Makefile | awk -F '=' '{print $2}')}"
if [ -z "$VERSION" ]; then
  echo "ERROR: 无法从 openwrt/Makefile 读取 PKG_VERSION" >&2
  exit 1
fi
echo "==> clashv $VERSION (target: $TARGET, stage: $STAGE)"

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
  # build-linux.sh 以 web/dist/assets 判断前端是否已构建，同步一份过去，
  # 避免 Go 阶段把整个 npm install + vite build 再白跑一遍（约 13 秒）
  rm -rf web/dist
  mkdir -p web/dist
  cp -r "$WEB_BUILD"/dist/. web/dist/
fi

# ---------- Go 二进制 ----------
if [ "$STAGE" = "go" ] || [ "$STAGE" = "all" ]; then
  echo "==> [go] 构建 Go 二进制 (7 linux + 2 darwin)"
  ./scripts/build-linux.sh bin "$VERSION"
fi

SDK_CACHE_DIR="${SDK_CACHE_DIR:-$REPO_ROOT/tmp}"
OUT_DIR="${OUT_DIR:-$REPO_ROOT/tmp/out}"

# ipk 目标用 22.03.7 最终版 SDK（版本已冻结，URL 恒定 → 缓存 key 永不失效）；SDK_URL 可覆盖
IPK_SDK_URL="${SDK_URL:-https://downloads.openwrt.org/releases/22.03.7/targets/x86/64/openwrt-sdk-22.03.7-x86-64_gcc-11.2.0_musl.Linux-x86_64.tar.xz}"

# SDK 优先从 Docker Hub 的 OpenWrt 官方镜像提取（Actions 机房直连 Hub，几十秒拉完；
# 镜像内 SDK 在 /builder，docker cp 拷出即可），拉取失败回落下方 tarball 直下路径。
# ⚠ 只有正式版 tag（vX.Y.Z 触发构建）烘焙了完整 SDK；分支/snapshot tag 是空壳
# （内含 setup.sh，运行时才从 downloads.openwrt.org 现下 284M，等于没绕开慢源）。
#   x86-64-22.03.7  ipk 目标：与上面 tarball 是同一份 22.03.7 SDK
#   x86-64-25.12.5  apk 目标：首个 apk 系稳定版（24.10 仍是 opkg；本包 PKGARCH:=all
#                   仅依赖 ca-bundle+kmod-tun，对 SDK 用 snapshot 还是 25.12 无差别，
#                   且 tag 冻结不再随上游漂移）。显式设空变量可禁用镜像走 tarball。
SDK_IMAGE_IPK="${SDK_IMAGE_IPK-openwrt/sdk:x86-64-22.03.7}"
SDK_IMAGE_APK="${SDK_IMAGE_APK-openwrt/sdk:x86-64-25.12.5}"
sdk_image() {
  case "$TARGET" in
    ipk) echo "$SDK_IMAGE_IPK" ;;
    apk) echo "$SDK_IMAGE_APK" ;;
  esac
}

# snapshot SDK 源列表（按优先级）：官方源优先，失败回落 TUNA。
# 北大/阿里/南大/上交等国内镜像均未同步 snapshots 目录（实测 404），别再加。
SNAPSHOT_BASE_URLS="https://downloads.openwrt.org/snapshots/targets/x86/64/ https://mirrors.tuna.tsinghua.edu.cn/openwrt/snapshots/targets/x86/64/"

# snapshot SDK 目录列表页里解析压缩包文件名（随上游更新变化）
snapshot_tarball_name() {
  for base in $SNAPSHOT_BASE_URLS; do
    name=$(curl -sf --connect-timeout 30 --retry 3 "$base" | grep -oE 'openwrt-sdk-x86-64[^"]+\.tar\.zst' | head -n 1)
    [ -n "$name" ] && { echo "$name"; return 0; }
  done
  return 1
}

# 大文件下载（SDK 压缩包 100M+）：官方源对大文件偶发 HTTP/2 流中断
# （PROTOCOL_ERROR）与限速，--http1.1 从根上规避 H2 流错误；--retry-all-errors
# 让 92 这类「非瞬态」错误也吃进重试；-C - 断点续传，重试从已下字节继续。
# curl 重试耗尽后外层再整轮重来；换源前由调用方删半截文件，避免两源混拼。
fetch_big() {
  url="$1" out="$2"
  for round in 1 2 3; do
    if curl -SLfk --http1.1 --connect-timeout 30 \
        --retry 4 --retry-all-errors --retry-delay 5 -C - "$url" -o "$out"; then
      return 0
    fi
    rm -f "$out"
  done
  return 1
}

# ---------- 解析 SDK 压缩包文件名（不下载，供 CI 缓存 key 用） ----------
# snapshot SDK 的文件名随上游更新变化，以它作 key 可在上游换 SDK 时自动失效重建
if [ "$STAGE" = "sdkname" ]; then
  # 镜像模式输出镜像 tag 作缓存 key（正式版 tag 冻结不漂移）；tarball 模式输出压缩包名
  if [ "$TARGET" = "ipk" ]; then
    if [ -n "$SDK_IMAGE_IPK" ]; then echo "${SDK_IMAGE_IPK##*:}"; else basename "$IPK_SDK_URL"; fi
  else
    if [ -n "$SDK_IMAGE_APK" ]; then
      echo "${SDK_IMAGE_APK##*:}"
    else
      SDK_TARBALL=$(snapshot_tarball_name)
      [ -n "$SDK_TARBALL" ] || { echo "ERROR: 无法解析 snapshot SDK 文件名" >&2; exit 1; }
      echo "$SDK_TARBALL"
    fi
  fi
fi

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
    echo "==> [sdk] 获取 OpenWrt SDK ($TARGET)"
    SDK_IMG=$(sdk_image)
    extracted=0
    # 首选：Docker Hub 官方镜像直提（Actions 机房秒级拉完，无 openwrt.org 慢源问题）
    if [ -n "$SDK_IMG" ] && command -v docker >/dev/null 2>&1; then
      if docker pull "$SDK_IMG"; then
        echo "==> [sdk] 从镜像提取 SDK: $SDK_IMG"
        cid=$(docker create "$SDK_IMG")
        rm -rf "$SDK_NAME"
        docker cp "$cid":/builder/. "$SDK_NAME"
        docker rm "$cid" >/dev/null
        extracted=1
      else
        echo "==> [sdk] 镜像拉取失败，回落 tarball 直下"
      fi
    fi
    # 回退：tarball 直下（本地无 docker / Hub 不可达时仍可用）
    if [ "$extracted" -eq 0 ]; then
      if [ "$TARGET" = "ipk" ]; then
        fetch_big "$IPK_SDK_URL" ./SDK.tar.xz
        tar xf SDK.tar.xz
        rm -f SDK.tar.xz
        rm -rf "$SDK_NAME"
        mv openwrt-sdk-* "$SDK_NAME"
      else
        # snapshot SDK。换源前删掉上一源留下的半截文件再从头下，
        # 避免两个源各下一半混拼出坏包
        for base in $SNAPSHOT_BASE_URLS; do
          SDK_TARBALL=$(curl -sf --connect-timeout 30 --retry 3 "$base" | grep -oE 'openwrt-sdk-x86-64[^"]+\.tar\.zst' | head -n 1)
          [ -n "$SDK_TARBALL" ] || continue
          rm -f ./SNAPSDK.tar.zst
          fetch_big "$base/$SDK_TARBALL" ./SNAPSDK.tar.zst && break
        done
        [ -f ./SNAPSDK.tar.zst ] || { echo "ERROR: 所有源均下载 snapshot SDK 失败" >&2; exit 1; }
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
    --exclude .DS_Store --exclude web/node_modules --exclude web/dist \
    "$REPO_ROOT/" "$PKG_SRC/"
  # 包 Makefile 放到包目录根（SDK 约定）
  cp "$PKG_SRC/openwrt/Makefile" "$PKG_SRC/Makefile"
  # 嵌套的 openwrt/Makefile 与包根 Makefile 内容相同，SDK 元数据扫描会把两份
  # 都当成包定义，删掉包内这份（install 引用的 openwrt/root/ 等文件保留）
  rm -f "$PKG_SRC/openwrt/Makefile"
  # 清掉上次构建残留在 SDK bin/ 里的旧产物，避免不同版本号混入
  # （现包名 luci-app-clashv*；*clashv* 连历史包名 clashv* 一起清掉；
  # 此时 cwd 还在仓库根，必须用 SDK 绝对路径）
  find "$SDK/bin" \( -name '*clashv*.ipk' -o -name '*clashv*.apk' \) -delete 2>/dev/null || true

  echo "==> [package] 编译包 (make package/$PKG_NAME/compile)"
  # 与 OpenClash 的打包逻辑一致：包正确注册（BuildPackage 宏）后，SDK 模式下
  # make defconfig 会把所有包默认置为 m，无需手动写入 CONFIG
  cd "$SDK"
  make defconfig
  grep -q "^CONFIG_PACKAGE_$PKG_NAME=m" .config || {
    echo "ERROR: $PKG_NAME 未能进入 SDK .config（多半是 DEPENDS 在该 SDK 中缺失）" >&2
    exit 1
  }
  # 本包只依赖 kmod-tun（内核源码里无 DEPENDS，无传递依赖）。SDK 的 Kconfig
  # 默认值把全部 kmod 预置成 =m，逐行剪掉只留依赖链需要的
  sed -i 's/^CONFIG_PACKAGE_kmod-\([^=]*\)=m$/# CONFIG_PACKAGE_kmod-\1 is not set/' .config
  open_kmod() {
    p=$1
    grep -q "^CONFIG_PACKAGE_$p=m" .config && return 0
    echo "CONFIG_PACKAGE_$p=m" >> .config
    # 从 tmp/.packageinfo 取该包 Depends 中的 kmod，递归打开
    for d in $(awk -v pkg="Package: $p" \
      '$0 == pkg { f = 1; next } f && /^Depends:/ { sub(/^Depends: /, ""); print; exit } f && /^$/ { exit }' \
      tmp/.packageinfo); do
      d=$(echo "$d" | tr -d '+' | sed 's/:.*//')
      case "$d" in kmod-*) open_kmod "$d" ;; esac
    done
  }
  open_kmod kmod-tun
  echo "==> 选中的 kmod: $(grep -c '^CONFIG_PACKAGE_kmod-.*=m' .config) 个"
  grep '^CONFIG_PACKAGE_kmod-.*=m' .config
  # .build 是「配置已就绪」的标记：缺它时 package/xxx/compile 前会再跑一次
  # defconfig，SDK 默认值会把下面的 kmod 剪减全部冲掉，所以这里必须补上
  touch tmp/.build
  # ⚠ 不能裸调 make package/.../compile：SDK 顶层有 %:: 兜底规则，任何 make 都
  # 会先跑 conf --defconfig=.config 重算——SDK 默认值把全部 kmod 预置 =m 且
  # defconfig 模式不理会显式 not-set 行，剪减被整个冲回上千个（冷构建重打包
  # 上千个无关 kmod、apk 目标拖到 8 分钟的元凶；round 87 起剪减从未真正生效，
  # 一直靠缓存掩盖）。以 OPENWRT_BUILD=1 直接调第二阶段内层 make，目标已注册、
  # 不再经过兜底规则的 conf，剪减后的 .config 原样生效
  OPENWRT_BUILD=1 make -r package/$PKG_NAME/compile V=s

  case "$TARGET" in
    # 一次产出 8 个包：通用版 luci-app-clashv_<版本>_all.ipk + 7 个架构精简版
    # luci-app-clashv-<arch>（apk 目标同理），PATTERN 一把抓
    ipk) PATTERN='luci-app-clashv*.ipk' ;;
    apk) PATTERN='luci-app-clashv*.apk' ;;
  esac
  rm -rf "$OUT_DIR"
  mkdir -p "$OUT_DIR"
  find bin -name "$PATTERN" -exec cp {} "$OUT_DIR/" \;
  # darwin 裸二进制随 Release 上传（只搭 ipk 目标的车，两个矩阵产物不重复）：
  # 本地 mac 开发时插件自更新走裸二进制路径，见 scripts/dev.sh
  [ "$TARGET" = "ipk" ] && cp bin/clashv-darwin-* "$OUT_DIR"/
  ls "$OUT_DIR"/* >/dev/null 2>&1 || {
    echo "ERROR: bin/ 下未找到匹配 $PATTERN 的产物" >&2
    exit 1
  }
  echo "==> 产物:"
  ls -lh "$OUT_DIR"
fi
