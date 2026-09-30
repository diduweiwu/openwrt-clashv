# clashv OpenWrt 包构建文件
#
# 用法（在 OpenWrt SDK 根目录）:
#   cp -r clashv package/clashv          # 整个项目目录放进 SDK 的 package/
#   make package/clashv/compile V=s
#
# 产物结构：一次编译产出 8 个包 ——
#   luci-app-clashv              通用版（PKGARCH:=all，内置 7 种架构二进制，装哪个设备都行）
#   luci-app-clashv-<arch>       架构精简版 ×7（只内置对应架构的二进制，包体约为通用版的 1/7）
#
# 全部包 PKGARCH:=all（安装器不校验设备架构，靠包名区分），互相 CONFLICTS，
# 同一台设备只能装一个，避免覆盖同名文件。
# 包名带 luci-app- 前缀表明这是 OpenWrt LuCI 插件包；插件自身的运行时文件
# （/usr/bin/clashv、/usr/lib/clashv 等）命名保持不变，升级互不影响。
# 二进制由 `./scripts/build-linux.sh` 或 GitHub Actions 预先构建。

include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-clashv
PKG_VERSION:=0.1.0
PKG_RELEASE:=5
PKG_MAINTAINER:=Nier
PKG_LICENSE:=MIT

include $(INCLUDE_DIR)/package.mk

# 与 scripts/build-linux.sh 的产物名一一对应（运行时二进制仍叫 clashv-<arch>）
CLASHV_ARCHES:=arm64 armv7 mips mipsle amd64 riscv64 loong64

CLASHV_ALL_CONFLICTS:=$(foreach a,$(CLASHV_ARCHES),luci-app-clashv-$(a))
# $(1) = 本包架构；返回除本架构外的全部精简包名
clashv_other_arches=$(foreach a,$(filter-out $(1),$(CLASHV_ARCHES)),luci-app-clashv-$(a))
# 首个发布版包名叫 clashv（无前缀），声明冲突避免新旧包文件互相覆盖
CLASHV_LEGACY:=clashv

define Package/luci-app-clashv
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Simple mihomo (Clash Meta) manager for OpenWrt
  URL:=https://github.com/nier/clashv
  DEPENDS:=+ca-bundle +kmod-tun
  CONFLICTS:=$(CLASHV_ALL_CONFLICTS) $(CLASHV_LEGACY)
  PKGARCH:=all
endef

define Package/luci-app-clashv/description
  轻量级 mihomo 管理插件：Go 后端 + 内嵌 Web 界面。
  通用版：内置 7 种架构二进制（arm64/armv7/mips/mipsle/amd64/riscv64/loong64），
  安装后按设备架构自动选择、删除其余。不确定设备架构时选这个。
  支持订阅管理、节点切换、TUN 全局接管、内核/插件一键更新。
  LuCI「服务 → clashv」或 http://路由器:9097 进入界面。
endef

define Package/luci-app-clashv/conffiles
/etc/config/clashv
/etc/clashv
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

define Package/luci-app-clashv/install
	$(call ClashvCommonInstall,$(1))
	$(INSTALL_BIN) ./bin/clashv-arm64 ./bin/clashv-armv7 ./bin/clashv-mips \
		./bin/clashv-mipsle ./bin/clashv-amd64 ./bin/clashv-riscv64 \
		./bin/clashv-loong64 $(1)/usr/lib/clashv/bin/
endef

# 安装后只保留当前架构的二进制（通用版内置 7 种架构，省下 ~40MB 空间；
# 架构精简版只装进了一个二进制，此循环自然跳过）
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

define Package/luci-app-clashv/postinst
$(ClashvPostinst)
endef

# ---- 架构精简版包 ----
# $(1) = 架构名（与 bin/clashv-<arch> 及包名后缀一致）。
# 包内仅含该架构的二进制；$$(1) 是框架回填的安装目录，须延迟展开；
# postinst 同理延迟一层展开，避免 $$(shell 变量) 在 eval 时被降级吃掉。
define GenArchPkg
define Package/luci-app-clashv-$(1)
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Simple mihomo (Clash Meta) manager for OpenWrt ($(1) build)
  URL:=https://github.com/nier/clashv
  DEPENDS:=+ca-bundle +kmod-tun
  CONFLICTS:=luci-app-clashv $(call clashv_other_arches,$(1)) $(CLASHV_LEGACY)
  PKGARCH:=all
endef

define Package/luci-app-clashv-$(1)/description
  轻量级 mihomo 管理插件（$(1) 架构精简版，仅内置单一架构二进制，包体约为通用版的 1/7）。
  与通用版 luci-app-clashv 及其他架构精简版互斥。不确定设备架构时请安装通用版 luci-app-clashv。
endef

define Package/luci-app-clashv-$(1)/conffiles
/etc/config/clashv
/etc/clashv
endef

define Package/luci-app-clashv-$(1)/install
$(call ClashvCommonInstall,$$(1))
$(INSTALL_BIN) ./bin/clashv-$(1) $$(1)/usr/lib/clashv/bin/
endef

define Package/luci-app-clashv-$(1)/postinst
$$(ClashvPostinst)
endef
endef

$(foreach a,$(CLASHV_ARCHES),$(eval $(call GenArchPkg,$(a))))

$(eval $(call BuildPackage,luci-app-clashv))
$(foreach a,$(CLASHV_ARCHES),$(eval $(call BuildPackage,luci-app-clashv-$(a))))
