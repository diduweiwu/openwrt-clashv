# clashv 顶层 Makefile
#
# 常用目标:
#   make web          构建前端 (Vite → web/dist)
#   make build        构建本机二进制（内嵌前端）→ bin/clashv
#   make dev          开发模式跑后端（文件存储，配合 npm run dev 热更新前端）
#   make linux        交叉编译全部 OpenWrt 目标架构 → bin/clashv-linux-*
#   make test-sub     起一个本地测试订阅服务器 (127.0.0.1:8899/sub.yaml)
#   make clean        清理构建产物
#
# 发版不走 make：唯一入口是 ./scripts/release.sh（升版本号+提交+打 tag+推送触发 CI）

VERSION ?= 0.2.0
GO      ?= $(HOME)/.local/go/bin/go
export GOPROXY ?= https://goproxy.cn,direct

.PHONY: all web build dev linux test-sub clean

all: build

web:
	cd web && npm install --no-fund --no-audit && npm run build
	rm -rf internal/web/dist/assets
	cp -r web/dist/* internal/web/dist/

build: web
	mkdir -p bin
	$(GO) build -trimpath -ldflags "-s -w -X main.Version=$(VERSION)" -o bin/clashv ./cmd/clashv

dev:
	$(GO) run ./cmd/clashv run -dev

linux:
	./scripts/build-linux.sh bin $(VERSION)

test-sub:
	@mkdir -p .devdata && printf 'proxies:\n  - name: "HK-01"\n    type: socks5\n    server: 127.0.0.1\n    port: 1080\n  - name: "JP-01"\n    type: socks5\n    server: 127.0.0.1\n    port: 1081\nproxy-groups:\n  - name: "节点选择"\n    type: select\n    proxies:\n      - 自动选择\n      - HK-01\n      - JP-01\n  - name: "自动选择"\n    type: url-test\n    proxies: [HK-01, JP-01]\n    url: https://www.gstatic.com/generate_204\n    interval: 300\nrules:\n  - MATCH,节点选择\n' > .devdata/sub.yaml
	@cd .devdata && python3 -m http.server 8899

clean:
	rm -rf bin web/dist internal/web/dist/assets .devdata
