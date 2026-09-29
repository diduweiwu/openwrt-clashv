#!/bin/sh
# 本地 Docker 构建：跑与 GitHub Actions 完全相同的流水线（scripts/ci-build.sh）。
#
# 产物输出到 build/<target>/（不入 git）。首次运行会构建本地构建镜像并下载
# SDK（约 200MB+，缓存在 docker 卷 clashv-sdkcache），之后会快很多。
#
# 用法:
#   ./scripts/build-local-docker.sh            # 构建 ipk + apk
#   ./scripts/build-local-docker.sh ipk        # 只构建 ipk（opkg 系统 22.03/23.05）
#   ./scripts/build-local-docker.sh apk        # 只构建 apk（OpenWrt 24.10+/snapshot）
#   ./scripts/build-local-docker.sh --shell    # 进入构建容器手动排查
#
# 说明:
#   - 容器固定 linux/amd64，与 GitHub Actions 运行器架构一致（OrbStack 走 Rosetta）。
#   - GOPROXY / NPM_CONFIG_REGISTRY 会透传进容器；npm 默认走 npmmirror，
#     国内网络下才能正常拉包，需要时自行覆盖。
set -e
cd "$(dirname "$0")/.."

IMAGE=clashv-buildenv
PLATFORM=linux/amd64
SDK_VOLUME=clashv-sdkcache

if ! docker info >/dev/null 2>&1; then
  echo "ERROR: Docker 不可用。OrbStack 用户请先启动 OrbStack 后重试。" >&2
  exit 1
fi

MODE="${1:-all}"
case "$MODE" in
  ipk|apk) TARGETS="$MODE" ;;
  all) TARGETS="ipk apk" ;;
  --shell) ;;
  *) echo "用法: $0 [ipk|apk|all|--shell]" >&2; exit 1 ;;
esac

# 构建本地构建镜像（有 docker 层缓存，改 Dockerfile 或首次运行时才会重装依赖）
docker build --platform "$PLATFORM" -f scripts/docker/Dockerfile.build -t "$IMAGE" .

run_build_env() {
  docker run --rm \
    --platform "$PLATFORM" \
    -v "$(pwd):/work" -w /work \
    -v "$SDK_VOLUME:/sdkcache" \
    -v clashv-npmcache:/home/builder/.npm \
    -v clashv-gocache:/home/builder/.cache/go-build \
    -v clashv-gomodcache:/home/builder/go/pkg/mod \
    -e "SDK_CACHE_DIR=/sdkcache" \
    -e "GOPROXY=${GOPROXY:-}" \
    -e "NPM_CONFIG_REGISTRY=${NPM_CONFIG_REGISTRY:-https://registry.npmmirror.com}" \
    "$@"
}

if [ "$MODE" = "--shell" ]; then
  exec run_build_env -it "$IMAGE" bash
fi

mkdir -p build
for t in $TARGETS; do
  echo ""
  echo "==================== 构建 $t ===================="
  run_build_env \
    -e "TARGET=$t" \
    -e "OUT_DIR=/work/build/$t" \
    "$IMAGE" ./scripts/ci-build.sh
done

echo ""
echo "==> 本地构建完成，产物:"
ls -lh build/
