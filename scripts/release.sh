#!/usr/bin/env bash
# clashv 一键发版：自动升级版本号 → 提交 → 打 tag → 推送
#
# 用法:
#   ./scripts/release.sh            # patch +0.0.1（默认）
#   ./scripts/release.sh minor      # +0.1.0
#   ./scripts/release.sh major      # +1.0.0
#   ./scripts/release.sh 0.2.0      # 指定版本号
#   ./scripts/release.sh --dry-run  # 只预览要做什么，不改任何文件
#
# 会同步更新 4 处版本号（当前值必须一致）：
#   openwrt/Makefile  PKG_VERSION   ← CI 读取，Release tag 的来源
#   Makefile          VERSION       ← 本机二进制 -X main.Version
#   web/package.json  version
#   web/package-lock.json version
# 推送 tag v<版本> 触发 GitHub Actions 打包并发布 Release。
# 注意：脚本含中文文案，bash 在 UTF-8 locale 下会把紧挨变量的中文字符
# 当成变量名的一部分，所以所有变量引用一律写 ${VAR} 带花括号的形式。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

die() { echo "✗ $*" >&2; exit 1; }

# ---------- 参数解析 ----------
DRY_RUN=0
BUMP=patch
for arg in "$@"; do
  case "${arg}" in
    --dry-run) DRY_RUN=1 ;;
    major|minor|patch) BUMP="${arg}" ;;
    -h|--help) awk 'NR==1 {next} /^#/ {sub(/^# ?/,""); print; next} {exit}' "$0"; exit 0 ;;
    *)
      echo "${arg}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' \
        || die "无法识别的参数「${arg}」（可用：major/minor/patch、x.y.z、--dry-run）"
      BUMP="${arg}" ;;
  esac
done

# ---------- 前置检查 ----------
[ -n "$(git branch --show-current)" ] || die "当前处于分离 HEAD 状态，先切到分支"
[ -z "$(git status --porcelain)" ] || { echo "✗ 工作区有未提交改动，先处理掉再发版：" >&2; git status --short >&2; exit 1; }

OLD="$(grep -E '^PKG_VERSION:=' openwrt/Makefile | head -1 | cut -d= -f2)"
[ -n "${OLD}" ] || die "无法从 openwrt/Makefile 读取 PKG_VERSION"

case "${BUMP}" in
  major|minor|patch)
    IFS='.' read -r A B C <<< "${OLD}"
    case "${BUMP}" in
      major) NEW="$((A + 1)).0.0" ;;
      minor) NEW="${A}.$((B + 1)).0" ;;
      patch) NEW="${A}.${B}.$((C + 1))" ;;
    esac ;;
  *) NEW="${BUMP}" ;;
esac
[ "${NEW}" != "${OLD}" ] || die "新版本号与当前相同（${OLD}）"

git rev-parse -q --verify "refs/tags/v${NEW}" >/dev/null \
  && die "本地已存在 tag v${NEW}"
git ls-remote --tags origin "refs/tags/v${NEW}" | grep -q . \
  && die "远端已存在 tag v${NEW}"

# ---------- 计划 ----------
echo "版本号: ${OLD} → ${NEW}（${BUMP}）"
echo "提交:   chore: 版本号升至 v${NEW}"
echo "标签:   v${NEW}"
echo "推送:   $(git branch --show-current) + v${NEW} → $(git remote get-url origin)"
if [ "${DRY_RUN}" = 1 ]; then
  echo "（dry-run，未做任何改动）"
  exit 0
fi

# ---------- 改版本号 ----------
# 兼容 macOS/BSD sed（-i ''）与 GNU sed（-i）
sed_edit() {
  if sed --version >/dev/null 2>&1; then sed -i "$2" "$1"; else sed -i '' "$2" "$1"; fi
}

# lock 文件里有各依赖包自己的 version 字段，动它之前先确认旧版本号恰好出现 2 次
# （根条目 + packages."" 条目），次数不对说明撞上了依赖的版本号，不能盲替
LOCK_HITS="$(grep -c "\"version\": \"${OLD}\"" web/package-lock.json || true)"
[ "${LOCK_HITS}" = "2" ] || die "web/package-lock.json 中 version:${OLD} 出现 ${LOCK_HITS} 次（预期 2），请手动检查"
[ "$(grep -c "\"version\": \"${OLD}\"" web/package.json || true)" = "1" ] \
  || die "web/package.json 中旧版本号出现次数异常，请手动检查"
# 新版本号也可能恰好是某个依赖的版本（如 vdirs@0.1.8），校验只看「净增 2 次」
NEW_HITS_BEFORE="$(grep -c "\"version\": \"${NEW}\"" web/package-lock.json || true)"

sed_edit openwrt/Makefile       "s/^PKG_VERSION:=.*/PKG_VERSION:=${NEW}/"
sed_edit Makefile               "s/^VERSION ?= .*/VERSION ?= ${NEW}/"
sed_edit web/package.json       "s/^\(  \"version\": \"\)\(${OLD}\)\(\",\)$/\1${NEW}\3/"
sed_edit web/package-lock.json  "s/^\( *\"version\": \"\)\(${OLD}\)\(\",\)$/\1${NEW}\3/"

# ---------- 校验 ----------
grep -q "PKG_VERSION:=${NEW}"  openwrt/Makefile      || die "openwrt/Makefile 版本号未更新"
grep -q "^VERSION ?= ${NEW}"   Makefile              || die "Makefile 版本号未更新"
grep -q "\"version\": \"${NEW}\"" web/package.json   || die "web/package.json 版本号未更新"
NEW_HITS_AFTER="$(grep -c "\"version\": \"${NEW}\"" web/package-lock.json || true)"
[ "$(( ${NEW_HITS_AFTER} - ${NEW_HITS_BEFORE} ))" = "2" ] \
  || die "web/package-lock.json 版本号未正确更新（${NEW} 净增 ${NEW_HITS_AFTER}−${NEW_HITS_BEFORE}≠2）"
if grep -q "\"version\": \"${OLD}\"" web/package.json web/package-lock.json; then
  die "仍有旧版本号残留"
fi
echo "✓ 4 处版本号已更新为 ${NEW}"

# ---------- 提交 + 打 tag + 推送 ----------
git add openwrt/Makefile Makefile web/package.json web/package-lock.json
git commit -m "chore: 版本号升至 v${NEW}"
git tag "v${NEW}"
git push origin "$(git branch --show-current)" "v${NEW}"

REMOTE_URL="$(git remote get-url origin | sed -E 's#^git@github.com:#https://github.com/#; s#\.git$##')"
echo "✓ 已推送 tag v${NEW}，GitHub Actions 开始打包: ${REMOTE_URL}/actions"
