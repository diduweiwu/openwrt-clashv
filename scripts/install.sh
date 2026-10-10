#!/bin/sh
# ======================================================================
# clashv —— OpenWrt 一键安装脚本
#
# 在 OpenWrt 路由器的 SSH 终端里执行，自动完成：
#   1. 检测设备架构是否受支持（读 DISTRIB_ARCH 映射到架构精简包）
#   2. 检测依赖：root 权限、包管理器（opkg/apk）、下载工具、ca-bundle/kmod-tun
#   3. 询问是否经 gh-proxy.com 加速下载 GitHub 资源（默认：是），
#      从 GitHub Release 拉取与设备匹配的插件包
#   4. 执行安装（apk 系统自动带 --allow-untrusted，自建包未签名必须加）
#
# 用法:
#   sh install.sh [选项]
#
# 选项:
#   -y, --yes        非交互执行，所有确认按默认值（加速代理按「是」处理）
#       --no-proxy   不走加速代理，直连 GitHub
#       --proxy URL  自定义加速前缀（默认 https://gh-proxy.com）
#       --repo REPO  插件所在仓库 owner/repo（默认 diduweiwu/openwrt-clashv）
#       --tag TAG    安装指定版本（如 v0.1.24），默认最新 Release
#       --token TOK  GitHub 访问令牌（私有仓库用），默认读环境变量 GITHUB_TOKEN
#       --pkg NAME   强制指定包名（如 luci-app-clashv 通用版）
#       --force      架构映射失败仍继续（安装通用版，但可能无法运行）
#   -h, --help       显示本帮助
#
# 示例:
#   sh install.sh                          # 交互式安装最新版
#   sh install.sh -y                       # 非交互，加速代理按默认「是」
#   sh install.sh --no-proxy --tag v0.1.24 # 直连下载指定版本
#   GITHUB_TOKEN=ghp_xxx sh install.sh     # 私有仓库：经环境变量传令牌
#
# 退出码: 0 成功；1 前置检测/下载/安装失败
# ======================================================================
set -u

# ---------- 默认配置（与插件设置里的默认值保持一致） ----------
REPO="diduweiwu/openwrt-clashv"   # 插件 Release 所在仓库
PROXY="https://gh-proxy.com"      # GitHub 下载加速前缀；置空 = 直连
TAG=""                            # 指定版本，空 = 最新 Release
TOKEN="${GITHUB_TOKEN:-}"         # GitHub 访问令牌（私有仓库）
FORCE_PKG=""                      # 强制指定的包名
FORCE=0                           # 1 = 架构映射失败仍继续
ASSUME_YES=0                      # 1 = 非交互，确认项按默认值
WGET_AUTH_OK=0                    # 1 = wget 支持 --header（GNU wget；busybox 版不支持）

PKG_BASE="luci-app-clashv"        # 插件包名前缀
# 通用版 + 7 个架构精简版（与 internal/core/upgrade.go 的 pluginPkgNames 一致）
PKG_NAMES="$PKG_BASE $PKG_BASE-arm64 $PKG_BASE-armv7 $PKG_BASE-mips $PKG_BASE-mipsle $PKG_BASE-amd64 $PKG_BASE-riscv64 $PKG_BASE-loong64"
OLD_PKG="clashv"                  # 首个发布版的旧包名，与新包互斥需先卸载
TMP_DIR="/tmp"                    # 下载暂存目录

# ---------- 输出工具 ----------
# 终端才上色，管道/重定向输出保持纯文本
if [ -t 1 ] && [ -n "${TERM:-}" ] && [ "$TERM" != dumb ]; then
  C_OK="\033[32m"; C_WARN="\033[33m"; C_ERR="\033[31m"; C_DIM="\033[2m"; C_OFF="\033[0m"
else
  C_OK=""; C_WARN=""; C_ERR=""; C_DIM=""; C_OFF=""
fi

# info 打印进度信息。
# 示例: info "下载 xxx.ipk"
info() { printf "${C_OK}==>${C_OFF} %s\n" "$*"; }

# warn 打印警告（不中断流程）。
# 示例: warn "软件源刷新失败，将尝试继续"
warn() { printf "${C_WARN}==> 警告:${C_OFF} %s\n" "$*"; }

# die 打印错误并退出脚本（退出码 1）。
# 示例: die "设备架构不受支持"
die() { printf "${C_ERR}==> 错误:${C_OFF} %s\n" "$*" >&2; exit 1; }

# has 判断命令是否存在。
# 示例: has curl && echo "有 curl"
has() { command -v "$1" >/dev/null 2>&1; }

# usage 打印帮助信息。
# 示例: install.sh --help
usage() {
  cat <<'USAGE'
用法: sh install.sh [选项]

选项:
  -y, --yes        非交互执行，所有确认按默认值（加速代理按「是」处理）
      --no-proxy   不走加速代理，直连 GitHub
      --proxy URL  自定义加速前缀（默认 https://gh-proxy.com）
      --repo REPO  插件所在仓库 owner/repo（默认 diduweiwu/openwrt-clashv）
      --tag TAG    安装指定版本（如 v0.1.24），默认最新 Release
      --token TOK  GitHub 访问令牌（私有仓库用），默认读环境变量 GITHUB_TOKEN
      --pkg NAME   强制指定包名（如 luci-app-clashv 通用版）
      --force      架构映射失败仍继续（安装通用版，但可能无法运行）
  -h, --help       显示本帮助

示例:
  sh install.sh                          # 交互式安装最新版
  sh install.sh -y                       # 非交互，加速代理按默认「是」
  sh install.sh --no-proxy --tag v0.1.24 # 直连下载指定版本
  GITHUB_TOKEN=ghp_xxx sh install.sh     # 私有仓库：经环境变量传令牌
USAGE
}

# ======================================================================
# 环境检测
# ======================================================================

# detect_format 探测 OpenWrt 包管理器，确定包格式。
# apk 系（OpenWrt 24.10+/snapshot）优先 apkg/apk 出 apk 包；
# 否则 opkg 系（22.03/23.05）出 ipk 包。设置全局 FORMAT 与 PKG_BIN。
# 返回: 找不到任何包管理器时返回 1。
# 示例: detect_format && echo "$FORMAT"   # 输出 apk 或 ipk
detect_format() {
  for b in apkg apk; do
    if has "$b"; then FORMAT=apk; PKG_BIN=$b; return 0; fi
  done
  if has opkg; then FORMAT=ipk; PKG_BIN=opkg; return 0; fi
  return 1
}

# distrib_arch 读取设备架构（/etc/openwrt_release 的 DISTRIB_ARCH，
# 如 aarch64_cortex-a53），读不到输出空串。值可能带单引号（23.05 实测）
# 或双引号，一并剥掉。
# 示例: arch=$(distrib_arch)
distrib_arch() {
  [ -f /etc/openwrt_release ] || return 0
  sed -n "s/^DISTRIB_ARCH=[\"']\{0,1\}\([^\"']*\)[\"']\{0,1\}.*/\1/p" /etc/openwrt_release | head -n 1
}

# map_arch 把 DISTRIB_ARCH 映射到架构精简包名后缀，与插件启动器/后端
# openwrtPkgArch 同一套规则；映射不上输出空串（表示架构不受支持）。
# 参数: $1 = DISTRIB_ARCH 值
# 返回: 经 stdout 输出 arm64/armv7/mips/mipsle/amd64/riscv64/loong64 或空
# 示例: map_arch "aarch64_cortex-a53"   # 输出 arm64
map_arch() {
  case "$1" in
    aarch64*|arm64*)   echo arm64 ;;
    arm*)              echo armv7 ;;
    mips64*)           echo "" ;;        # 64 位 MIPS 无对应 Go 构建，跑不了
    mipsel*)           echo mipsle ;;
    mips*)             echo mips ;;
    x86_64|amd64)      echo amd64 ;;
    riscv64)           echo riscv64 ;;
    loongarch64)       echo loong64 ;;
    *)                 echo "" ;;
  esac
}

# check_env 前置检测：OpenWrt 系统、root、包管理器、下载工具、设备架构。
# 结果存全局 FORMAT/PKG_BIN/DOWNLOADER/DARCH/SHORT，失败直接 die。
# 示例: check_env
check_env() {
  [ -f /etc/openwrt_release ] || die "未检测到 OpenWrt（缺少 /etc/openwrt_release），本脚本只能在 OpenWrt 上运行"
  [ "$(id -u)" = "0" ] || die "请以 root 运行（安装包需要 root 权限）：sh install.sh"
  detect_format || die "未找到包管理器（opkg/apk 均不存在），无法安装"
  if has curl; then DOWNLOADER=curl
  elif has wget; then
    DOWNLOADER=wget
    # busybox wget 不认 --header（GNU wget 才有）；带令牌的请求必须能加头
    if wget --help 2>&1 | grep -q -- '--header'; then WGET_AUTH_OK=1; else WGET_AUTH_OK=0; fi
  else die "没有可用的下载工具（curl/wget），请先安装：opkg install curl"
  fi
  DARCH=$(distrib_arch)
  SHORT=$(map_arch "${DARCH:-}")
}

# show_env 打印检测到的环境信息。
# 示例: show_env
show_env() {
  desc=$(sed -n "s/^DISTRIB_DESCRIPTION=[\"']\{0,1\}\([^\"']*\)[\"']\{0,1\}.*/\1/p" /etc/openwrt_release | head -n 1)
  info "检测环境"
  printf "    %-14s %s\n" "系统:" "${desc:-OpenWrt}"
  printf "    %-14s %s (%s 包)\n" "包管理器:" "$PKG_BIN" "$FORMAT"
  printf "    %-14s %s\n" "下载工具:" "$DOWNLOADER"
  printf "    %-14s %s\n" "设备架构:" "${DARCH:-未知}"
  if [ -n "$SHORT" ]; then
    printf "    %-14s %s -> ${C_OK}%s${C_OFF} 精简包\n" "架构映射:" "$DARCH" "$SHORT"
  else
    printf "    ${C_DIM}%-14s%s${C_OFF}\n" "架构映射:" "未映射（不确定是否可运行）"
  fi
}

# check_arch 校验架构是否受支持；不支持时默认终止，--force 才继续。
# 示例: check_arch
check_arch() {
  [ -n "$SHORT" ] && return 0
  if [ "$FORCE" = "1" ]; then
    warn "架构 ${DARCH:-未知} 不在支持列表（--force 已跳过检查），通用版可能无法运行"
    return 0
  fi
  die "设备架构 ${DARCH:-未知} 不受支持。支持列表：aarch64/arm、mipsel/mips、x86_64、riscv64、loongarch64。确认要强行尝试请加 --force"
}

# ======================================================================
# 安装包选择
# ======================================================================

# pkg_installed 判断指定包是否已安装。
# 参数: $1 = 包名
# 返回: 已安装返回 0，未安装返回 1
# 示例: pkg_installed ca-bundle || opkg install ca-bundle
pkg_installed() {
  if [ "$FORMAT" = apk ]; then
    $PKG_BIN info -e "$1" >/dev/null 2>&1
  else
    opkg list-installed 2>/dev/null | grep -q "^$1 "
  fi
}

# installed_pkg 查找已安装的 clashv 插件包名（含旧包名优先级最低，
# 见 old_pkg_installed）。升级时原地替换同一变体——通用版与精简版互为
# CONFLICTS，装错变体会被包管理器拒绝。
# 返回: 经 stdout 输出包名，没有则输出空。
# 示例: name=$(installed_pkg)
installed_pkg() {
  for p in $PKG_NAMES; do
    if pkg_installed "$p"; then echo "$p"; return 0; fi
  done
}

# old_pkg_installed 判断是否还装着旧包名 clashv（首个发布版），
# 它与新包文件冲突，必须先卸载。
# 返回: 已安装返回 0
# 示例: old_pkg_installed && opkg remove clashv
old_pkg_installed() { pkg_installed "$OLD_PKG"; }

# pick_pkg 选定要安装的包名，存全局 PKG。
# 优先级：--pkg 指定 > 已装变体 > 架构精简版 > 通用版；
# 发现已装旧包 clashv 时先询问/直接卸载（-y 按默认「是」）。
# 示例: pick_pkg
pick_pkg() {
  src=""
  if [ -n "$FORCE_PKG" ]; then
    PKG=$FORCE_PKG; src="--pkg 指定"
  else
    PKG=$(installed_pkg)
    if [ -n "$PKG" ]; then
      src="已装，升级该变体"
    elif [ -n "$SHORT" ]; then
      PKG="$PKG_BASE-$SHORT"
    else
      PKG=$PKG_BASE
    fi
  fi
  printf "    %-14s %s%s\n" "安装包:" "$PKG" "$([ -n "$src" ] && printf "（%s）" "$src")"
  if old_pkg_installed; then
    if [ "$ASSUME_YES" = "1" ]; then
      ans=""
    else
      printf "检测到旧版包 %s，与新包文件冲突，是否先卸载？[Y/n] " "$OLD_PKG"
      read ans
    fi
    case "$ans" in
      n*|N*) die "请先手动卸载旧包：$([ "$FORMAT" = apk ] && echo "$PKG_BIN del" || echo "opkg remove") $OLD_PKG 后重试" ;;
    esac
    info "卸载旧包 $OLD_PKG"
    if [ "$FORMAT" = apk ]; then $PKG_BIN del "$OLD_PKG" || warn "旧包卸载失败，继续尝试安装"
    else opkg remove "$OLD_PKG" || warn "旧包卸载失败，继续尝试安装"; fi
  fi
}

# ======================================================================
# 依赖
# ======================================================================

# setup_deps 刷新软件源并补齐插件包的依赖（ca-bundle、kmod-tun）。
# 软件源刷新失败只警告（依赖可能已装）；依赖装失败也只警告——固件把
# tun 编进内核时源里没有 kmod-tun 也能跑，真正的硬失败留给安装步。
# 示例: setup_deps
setup_deps() {
  mkdir -p /var/lock   # 极简固件/容器环境可能缺失，opkg 锁文件需要它
  info "刷新软件源（$PKG_BIN update）"
  if [ "$FORMAT" = apk ]; then
    $PKG_BIN update || warn "apk update 失败，依赖可能装不上（检查 /etc/apk/repositories）"
  else
    opkg update || warn "opkg update 失败，依赖可能装不上（检查 /etc/opkg/distfeeds.conf）"
  fi
  for dep in ca-bundle kmod-tun; do
    if pkg_installed "$dep"; then
      printf "    %-12s 已就绪\n" "$dep"
    else
      printf "    %-12s 安装中..." "$dep"
      if [ "$FORMAT" = apk ]; then
        if $PKG_BIN add "$dep" >/dev/null 2>&1; then echo " 完成"; else echo " ${C_WARN}失败${C_OFF}（若安装报缺依赖请手动处理）"; fi
      else
        if opkg install "$dep" >/dev/null 2>&1; then echo " 完成"; else echo " ${C_WARN}失败${C_OFF}（若安装报缺依赖请手动处理）"; fi
      fi
    fi
  done
}

# ======================================================================
# 下载
# ======================================================================

# ask_proxy 询问是否经加速前缀下载 GitHub 资源，结果存全局 USE_PROXY。
# 命令行已指定（--proxy/--no-proxy）或 -y 时跳过询问；交互默认「是」。
# 示例: ask_proxy
ask_proxy() {
  USE_PROXY=0
  [ -n "$PROXY" ] && USE_PROXY=1
  [ "$ASSUME_YES" = "1" ] && return 0
  if [ "$USE_PROXY" = "1" ] && [ -z "${PROXY_GIVEN:-}" ]; then
    printf "是否经 %s 加速下载 GitHub 资源？[Y/n] " "$PROXY"
    read ans
    case "$ans" in n*|N*) USE_PROXY=0 ;; esac
  fi
}

# proxied 给 GitHub 链接套加速前缀（格式与插件后端 proxiedURL 一致）。
# 参数: $1 = 原始 github.com / api.github.com 链接
# 示例: proxied "https://github.com/x/y/releases/download/..." 
proxied() {
  if [ "$USE_PROXY" = "1" ] && [ -n "$PROXY" ]; then
    echo "$PROXY/$1"
  else
    echo "$1"
  fi
}

# http_fetch 下载 URL 到文件（$2 为空输出到 stdout）；$3 非空时携带
# Bearer 令牌。curl 优先（-f 让 4xx/5xx 直接失败）；wget 分支只用 busybox
# wget 也认的参数（无 -t 重试选项），加 --no-check-certificate 兜底没装
# ca-bundle 的固件。
# 参数: $1 = URL  $2 = 输出文件（空 = stdout）  $3 = 是否带令牌（非空=是）
# 返回: 下载失败返回非 0
# 示例: http_fetch "https://github.com/..." /tmp/a.ipk ""
http_fetch() {
  url=$1 out=$2 mode=${3:-}
  if [ "$DOWNLOADER" = curl ]; then
    if [ -n "$mode" ]; then
      curl -fSL --connect-timeout 20 --retry 2 -H "Authorization: Bearer $TOKEN" -o "$out" "$url"
    else
      curl -fSL --connect-timeout 20 --retry 2 -o "$out" "$url"
    fi
  else
    [ -n "$out" ] || out=-   # busybox wget 输出 stdout 用 -O -
    if [ -n "$mode" ]; then
      [ "$WGET_AUTH_OK" = "1" ] || die "当前 wget 是 busybox 版、不支持 --header，带令牌下载需要 curl：opkg install curl"
      wget -T 20 --no-check-certificate --header "Authorization: Bearer $TOKEN" -O "$out" "$url"
    else
      wget -T 20 --no-check-certificate -O "$out" "$url"
    fi
  fi
}

# api_get 请求 GitHub API 把响应打到 stdout。带令牌时必须直连（加速
# 代理服务器不带凭据，私有仓库必 404）；无令牌直连优先、加速前缀兜底，
# 与插件后端 fetchGitHub 的顺序一致。
# 参数: $1 = API 地址
# 返回: 两个地址都失败返回 1
# 示例: json=$(api_get "https://api.github.com/repos/x/y/releases/latest")
api_get() {
  if [ -n "$TOKEN" ]; then
    http_fetch "$1" "" auth
    return $?
  fi
  if http_fetch "$1" "" ""; then return 0; fi
  warn "API 直连失败，改经加速前缀重试"
  http_fetch "$(proxied "$1")" "" ""
}

# json_str 从 GitHub API 的 JSON 里提取第一个字符串字段值。
# 不依赖 jq（OpenWrt 默认没有）：按 "字段": "值" 模式 grep。
# 参数: $1 = 字段名  $2 = JSON 文本
# 示例: tag=$(json_str tag_name "$json")
json_str() {
  printf '%s' "$2" | grep -o "\"$1\": *\"[^\"]*\"" | head -n 1 | sed 's/^.*: *"//; s/"$//'
}

# resolve_asset 查询 Release 并定位设备匹配的安装包资产。
# 全局产出 RELEASE_TAG/ASSET/ASSET_NAME。资产名模式与后端 pluginAssetRe
# 一致：包名后必须紧跟数字开头的版本段，避免通用版误匹配架构精简版
# （luci-app-clashv_0.1.0-1... 不会吃掉 luci-app-clashv-arm64_...）。
# 示例: resolve_asset
resolve_asset() {
  info "查询 Release（$REPO${TAG:+，指定 $TAG}）"
  api="https://api.github.com/repos/$REPO/releases/latest"
  [ -n "$TAG" ] && api="https://api.github.com/repos/$REPO/releases/tags/$TAG"
  JSON=$(api_get "$api") || die "查询 GitHub Release 失败：网络不可达/触发限流（无令牌每小时 60 次）/仓库还没有发布 Release。可稍后重试、--token 提升限额，或到 https://github.com/$REPO/releases 确认"
  RELEASE_TAG=$(json_str tag_name "$JSON")
  [ -n "$RELEASE_TAG" ] || die "GitHub 响应异常（$(json_str message "$JSON" | cut -c1-60)）。若为限流请稍后重试或 --token"
  ASSET=$(pick_asset_url) || die "Release $RELEASE_TAG 里没有找到 $PKG 的 $FORMAT 包（可用 --pkg luci-app-clashv 换通用版试试）"
  ASSET_NAME=${ASSET##*/}
  printf "    %-14s %s\n" "版本:" "$RELEASE_TAG"
  printf "    %-14s %s\n" "安装包:" "$ASSET_NAME"
}

# pick_asset_url 在 Release 资产列表里找 $PKG 对应的下载地址。
# 返回: 经 stdout 输出 URL；找不到返回 1
# 示例: url=$(pick_asset_url)
pick_asset_url() {
  urls=$(printf '%s' "$JSON" | grep -o '"browser_download_url": *"[^"]*"' | sed 's/^.*: *"//; s/"$//')
  for u in $urls; do
    name=${u##*/}
    case "$FORMAT:$name" in
      ipk:"$PKG"_[0-9]*.ipk) echo "$u"; return 0 ;;
      apk:"$PKG"-[0-9]*.apk) echo "$u"; return 0 ;;
    esac
  done
  return 1
}

# download_asset 把安装包下载到 /tmp 并做基本校验。候选顺序：有令牌走
# 直连（代理不带凭据）；否则加速前缀优先、直连兜底，与后端下载顺序一致。
# 全局产出 FILE（本地文件路径）。
# 示例: download_asset
download_asset() {
  info "下载 $ASSET_NAME（$([ "$USE_PROXY" = 1 ] && [ -n "$PROXY" ] && [ -z "$TOKEN" ] && echo "经 $PROXY 加速" || echo "直连")）"
  FILE="$TMP_DIR/$ASSET_NAME"
  ok=0
  if [ -n "$TOKEN" ]; then
    rm -f "$FILE" && http_fetch "$ASSET" "$FILE" auth && ok=1
  else
    if [ "$USE_PROXY" = 1 ] && [ -n "$PROXY" ]; then
      rm -f "$FILE" && http_fetch "$(proxied "$ASSET")" "$FILE" "" && ok=1
    fi
    if [ "$ok" = "0" ]; then
      [ "$USE_PROXY" = 1 ] && warn "加速下载失败，改直连重试"
      rm -f "$FILE" && http_fetch "$ASSET" "$FILE" "" && ok=1
    fi
  fi
  [ "$ok" = "1" ] || die "下载失败：网络不可达或文件不存在。可试 --no-proxy 直连，或手动下载后阅读 README 手动安装"
  size=$(wc -c < "$FILE")
  [ "$size" -ge 1024 ] || die "下载内容异常（仅 $size 字节），多半是代理/网络返回了错误页"
  printf "    %-14s %s\n" "大小:" "$(awk -v s="$size" 'BEGIN{printf "%.1f MB", s/1048576}')"
}

# ======================================================================
# 安装
# ======================================================================

# do_install 执行安装。apk 系统必须带 --allow-untrusted：自建包未经
# OpenWrt 签名，不带该参数会报 UNTRUSTED signature 拒装。
# 示例: do_install
do_install() {
  info "安装 $ASSET_NAME"
  if [ "$FORMAT" = apk ]; then
    $PKG_BIN add --allow-untrusted "$FILE" || die "apk 安装失败（缺依赖时先确认软件源里有 ca-bundle/kmod-tun）"
  else
    opkg install "$FILE" || die "opkg 安装失败（缺依赖时先确认软件源里有 ca-bundle/kmod-tun；提示与已装包冲突时按提示卸载后重试）"
  fi
  rm -f "$FILE"
}

# finish 打印安装完成后的使用指引。
# 示例: finish
finish() {
  printf "${C_OK}==> 安装完成${C_OFF}\n\n"
  echo "后续步骤："
  echo "  1. LuCI → 服务 → ClashV 进入管理界面（未登录会先到路由器登录页）"
  echo "  2. 订阅页添加并启用订阅"
  echo "  3. 设置 → 内核 一键下载 mihomo 内核，然后回首页启动"
  echo "  4. 直接访问 http://<路由器IP>:9097 会自动跳转到插件页面"
}

# ======================================================================
# 参数解析与入口
# ======================================================================

# parse_args 解析命令行参数并存入全局配置变量。
# 示例: parse_args --no-proxy -y
parse_args() {
  PROXY_GIVEN=""
  while [ $# -gt 0 ]; do
    case "$1" in
      -y|--yes)     ASSUME_YES=1 ;;
      --no-proxy)   PROXY=""; PROXY_GIVEN=1 ;;
      --proxy)      [ $# -ge 2 ] || die "--proxy 需要一个 URL 参数"; PROXY=$2; PROXY_GIVEN=1; shift ;;
      --repo)       [ $# -ge 2 ] || die "--repo 需要 owner/repo 参数"; REPO=$2; shift ;;
      --tag)        [ $# -ge 2 ] || die "--tag 需要版本参数（如 v0.1.24）"; TAG=$2; shift ;;
      --token)      [ $# -ge 2 ] || die "--token 需要令牌参数"; TOKEN=$2; shift ;;
      --pkg)        [ $# -ge 2 ] || die "--pkg 需要包名参数"; FORCE_PKG=$2; shift ;;
      --force)      FORCE=1 ;;
      -h|--help)    usage; exit 0 ;;
      *)            die "未知参数: $1（--help 查看用法）" ;;
    esac
    shift
  done
}

# main 主流程：环境检测 → 架构校验 → 选包 → 依赖 → 代理确认 →
# 查询/下载 → 安装。
# 示例: install.sh -y
main() {
  parse_args "$@"
  check_env
  show_env
  check_arch
  info "选择安装包"
  pick_pkg
  setup_deps
  info "下载设置"
  ask_proxy
  resolve_asset
  download_asset
  do_install
  finish
}

main "$@"
