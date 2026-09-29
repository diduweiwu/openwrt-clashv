# openclash-air OpenWrt 包构建文件
#
# 用法（在 OpenWrt SDK 根目录）:
#   cp -r openclash-air package/openclash-air          # 整个项目目录放进 SDK 的 package/
#   make package/openclash-air/compile V=s
#
# 平台说明：PKGARCH:=all —— 一个 ipk/apk 通用所有平台。
# 包内附带 7 种架构的 Go 二进制（bin/openclash-air-*），/usr/bin/openclash-air
# 启动器按设备 DISTRIB_ARCH 选择执行，postinst 会删除其他架构节省空间。
# 二进制由 `./scripts/build-linux.sh` 或 GitHub Actions 预先构建。

include $(TOPDIR)/rules.mk

PKG_NAME:=openclash-air
PKG_VERSION:=0.1.0
PKG_RELEASE:=1
PKG_MAINTAINER:=Nier
PKG_LICENSE:=MIT

include $(INCLUDE_DIR)/package.mk

define Package/openclash-air
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Simple mihomo (Clash Meta) manager for OpenWrt
  URL:=https://github.com/nier/openclash-air
  DEPENDS:=+ca-bundle +kmod-tun
  PKGARCH:=all
endef

define Package/openclash-air/description
  轻量级 mihomo 管理插件：Go 后端 + 内嵌 Web 界面。
  单包全平台（内置多架构二进制，按设备自动选择）。
  支持订阅管理、节点切换、TUN 全局接管、内核/插件一键更新。
  LuCI「服务 → openclash-air」或 http://路由器:9097 进入界面。
endef

define Package/openclash-air/conffiles
/etc/config/openclash-air
/etc/openclash-air
endef

define Build/Compile
	# Go 二进制与前端由外部预构建（scripts/build-linux.sh），SDK 只负责打包
endef

define Package/openclash-air/install
	$(INSTALL_DIR) $(1)/usr/lib/openclash-air/bin
	$(INSTALL_BIN) ./bin/openclash-air-arm64 ./bin/openclash-air-armv7 ./bin/openclash-air-mips \
		./bin/openclash-air-mipsle ./bin/openclash-air-amd64 ./bin/openclash-air-riscv64 \
		./bin/openclash-air-loong64 $(1)/usr/lib/openclash-air/bin/
	$(INSTALL_DIR) $(1)/usr/bin
	$(INSTALL_BIN) ./openwrt/root/usr/bin/openclash-air $(1)/usr/bin/openclash-air
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./openwrt/root/etc/init.d/openclash-air $(1)/etc/init.d/openclash-air
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./openwrt/root/etc/config/openclash-air $(1)/etc/config/openclash-air
	$(INSTALL_DIR) $(1)/etc/uci-defaults
	$(INSTALL_BIN) ./openwrt/root/etc/uci-defaults/99-openclash-air $(1)/etc/uci-defaults/99-openclash-air
	$(INSTALL_DIR) $(1)/usr/share/luci/menu.d
	$(INSTALL_DATA) ./openwrt/root/usr/share/luci/menu.d/luci-app-openclash-air.json \
		$(1)/usr/share/luci/menu.d/luci-app-openclash-air.json
	$(INSTALL_DIR) $(1)/usr/share/rpcd/acl.d
	$(INSTALL_DATA) ./openwrt/root/usr/share/rpcd/acl.d/luci-app-openclash-air.json \
		$(1)/usr/share/rpcd/acl.d/luci-app-openclash-air.json
	$(INSTALL_DIR) $(1)/www/luci-static/resources/view/openclash-air
	$(INSTALL_DATA) ./openwrt/root/www/luci-static/resources/view/openclash-air/app.js \
		$(1)/www/luci-static/resources/view/openclash-air/app.js
	$(INSTALL_DIR) $(1)/etc/openclash-air/profiles
endef

# 安装后只保留当前架构的二进制（all 包内置 7 种架构，省下 ~45MB 空间）
define Package/openclash-air/postinst
#!/bin/sh
[ "$${IPKG_NO_SCRIPT}" = "1" ] && exit 0
DISTRIB_ARCH=""
[ -r /etc/openwrt_release ] && . /etc/openwrt_release 2>/dev/null
case "$$DISTRIB_ARCH" in
	aarch64*|arm64*)  KEEP=arm64 ;;
	arm*)             KEEP=armv7 ;;
	mipsel*)          KEEP=mipsle ;;
	mips*)            KEEP=mips ;;
	x86_64|amd64)     KEEP=amd64 ;;
	riscv64)          KEEP=riscv64 ;;
	loongarch64)      KEEP=loong64 ;;
	*)                KEEP="" ;;
esac
if [ -n "$$KEEP" ] && [ -d /usr/lib/openclash-air/bin ]; then
	find /usr/lib/openclash-air/bin -type f ! -name "openclash-air-$$KEEP" -delete
	echo "openclash-air: 已保留 $$KEEP 架构内核二进制"
fi
exit 0
endef

$(eval $(call BuildPackage,openclash-air))
