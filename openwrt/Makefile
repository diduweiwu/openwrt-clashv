# clashv OpenWrt 包构建文件
#
# 用法（在 OpenWrt SDK 根目录）:
#   cp -r clashv package/clashv          # 整个项目目录放进 SDK 的 package/
#   make package/clashv/compile V=s
#
# 单包全平台：PKGARCH:=all，内置 7 种架构的 Go 二进制（arm64/armv7/mips/
# mipsle/amd64/riscv64/loong64），安装后由 /usr/bin/clashv 启动器按设备
# DISTRIB_ARCH 自动选择执行，postinst 删除其余架构节省安装空间。
# CONFLICTS 声明了首个发布版的旧包名 clashv，升级前需先卸载旧包。
# 注意：不要拆分架构子包，也不要改回 $(eval $(call 模板)) 生成 —— SDK 全量
# 打包模式下嵌套 define 模板会生成坏文本（missing separator，CI 已踩坑）。
# 二进制由 `./scripts/build-linux.sh` 或 GitHub Actions 预先构建。

include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-clashv
PKG_VERSION:=0.1.0
PKG_RELEASE:=6
PKG_MAINTAINER:=Nier
PKG_LICENSE:=MIT

include $(INCLUDE_DIR)/package.mk

define Package/luci-app-clashv
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Simple mihomo (Clash Meta) manager for OpenWrt
  URL:=https://github.com/nier/clashv
  DEPENDS:=+ca-bundle +kmod-tun
  CONFLICTS:=clashv
  PKGARCH:=all
endef

define Package/luci-app-clashv/description
  轻量级 mihomo 管理插件：Go 后端 + 内嵌 Web 界面。
  单包全平台：内置 7 种架构二进制（arm64/armv7/mips/mipsle/amd64/riscv64/loong64），
  安装后按设备架构自动选择、删除其余，任意设备直接安装。
  支持订阅管理、节点切换、TUN 全局接管、内核/插件一键更新。
  LuCI「服务 → clashv」或 http://路由器:9097 进入界面。
endef

define Package/luci-app-clashv/conffiles
/etc/config/clashv
/etc/clashv
endef

# Go 二进制与前端由外部预构建（scripts/build-linux.sh），SDK 只负责打包。
# 必须显式置空：缺省时 SDK 会对 build_dir 里的源码跑默认 make 直接报错。
define Build/Compile
endef

# 除内核二进制外的公共文件（$1 = 目标目录）
define ClashvCommonInstall
	$(INSTALL_DIR) $(1)/usr/lib/clashv/bin
	$(INSTALL_DIR) $(1)/usr/bin
	$(INSTALL_BIN) ./openwrt/root/usr/bin/clashv $(1)/usr/bin/clashv
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./openwrt/root/etc/init.d/clashv $(1)/etc/init.d/clashv
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./openwrt/root/etc/config/clashv $(1)/etc/config/clashv
	$(INSTALL_DIR) $(1)/etc/uci-defaults
	$(INSTALL_BIN) ./openwrt/root/etc/uci-defaults/99-clashv $(1)/etc/uci-defaults/99-clashv
	$(INSTALL_DIR) $(1)/usr/share/luci/menu.d
	$(INSTALL_DATA) ./openwrt/root/usr/share/luci/menu.d/luci-app-clashv.json \
		$(1)/usr/share/luci/menu.d/luci-app-clashv.json
	$(INSTALL_DIR) $(1)/usr/share/rpcd/acl.d
	$(INSTALL_DATA) ./openwrt/root/usr/share/rpcd/acl.d/luci-app-clashv.json \
		$(1)/usr/share/rpcd/acl.d/luci-app-clashv.json
	$(INSTALL_DIR) $(1)/www/luci-static/resources/view/clashv
	$(INSTALL_DATA) ./openwrt/root/www/luci-static/resources/view/clashv/app.js \
		$(1)/www/luci-static/resources/view/clashv/app.js
	$(INSTALL_DIR) $(1)/etc/clashv/profiles
endef

# 安装后只保留当前架构的二进制（包内置 7 种架构，省下 ~40MB 空间）
define ClashvPostinst
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
if [ -n "$$KEEP" ] && [ -d /usr/lib/clashv/bin ]; then
	# 部分固件的 BusyBox find 无 -delete/-exec，用纯 shell 循环保证兼容
	for f in /usr/lib/clashv/bin/clashv-*; do
		[ "$$f" = "/usr/lib/clashv/bin/clashv-$$KEEP" ] || rm -f "$$f"
	done
	echo "clashv: 已保留 $$KEEP 架构内核二进制"
fi
# 刷新 LuCI 菜单缓存并重载 rpcd ACL，否则「服务」菜单不会出现
[ -n "$${IPKG_INSTROOT}" ] || {
	rm -f /tmp/luci-indexcache* 2>/dev/null
	/etc/init.d/rpcd reload 2>/dev/null || killall -HUP rpcd 2>/dev/null
}
exit 0
endef

define Package/luci-app-clashv/install
	$(call ClashvCommonInstall,$(1))
	$(INSTALL_BIN) ./bin/clashv-arm64 ./bin/clashv-armv7 ./bin/clashv-mips \
		./bin/clashv-mipsle ./bin/clashv-amd64 ./bin/clashv-riscv64 \
		./bin/clashv-loong64 $(1)/usr/lib/clashv/bin/
endef

define Package/luci-app-clashv/postinst
$(ClashvPostinst)
endef

$(eval $(call BuildPackage,luci-app-clashv))
