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
# 注意：8 个包全部显式书写 —— 不要改成 $(eval $(call 模板)) 生成，SDK 的
# 全量打包模式下嵌套 define 模板会生成坏文本（missing separator，CI 已踩坑）。
# 二进制由 `./scripts/build-linux.sh` 或 GitHub Actions 预先构建。

include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-clashv
PKG_VERSION:=0.1.4
PKG_RELEASE:=8
PKG_MAINTAINER:=Nier
PKG_LICENSE:=MIT

include $(INCLUDE_DIR)/package.mk

define Package/luci-app-clashv
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Simple mihomo (Clash Meta) manager for OpenWrt
  URL:=https://github.com/nier/clashv
  DEPENDS:=+ca-bundle +kmod-tun
  CONFLICTS:=luci-app-clashv-arm64 luci-app-clashv-armv7 luci-app-clashv-mips luci-app-clashv-mipsle luci-app-clashv-amd64 luci-app-clashv-riscv64 luci-app-clashv-loong64 clashv
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

# Go 二进制与前端由外部预构建（scripts/build-linux.sh），SDK 只负责打包。
# 必须显式置空：缺省时 SDK 会对 build_dir 里的源码跑默认 make 直接报错。
define Build/Compile
endef

# 除内核二进制外的公共文件（$1 = 目标目录）；$$(call) 展开发生在打包脚本
# 生成阶段，纯文本替换，与 SDK 元数据解析无关，可安全共享
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
	# 前端内嵌在插件二进制里：升级后旧进程仍运行旧二进制，界面不会更新，
	# 必须重启服务加载新 UI（内核由 clashv 自管，core.state 会让它自动拉起）
	if [ -x /etc/init.d/clashv ] && /etc/init.d/clashv enabled; then
		/etc/init.d/clashv restart 2>/dev/null
		echo "clashv: 服务已重启以加载新版本"
	fi
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

# ---- 以下 7 个架构精简版包，除二进制与 CONFLICTS 外内容一致，全部显式书写 ----

define Package/luci-app-clashv-arm64
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Simple mihomo (Clash Meta) manager for OpenWrt (arm64 build)
  URL:=https://github.com/nier/clashv
  DEPENDS:=+ca-bundle +kmod-tun
  CONFLICTS:=luci-app-clashv luci-app-clashv-armv7 luci-app-clashv-mips luci-app-clashv-mipsle luci-app-clashv-amd64 luci-app-clashv-riscv64 luci-app-clashv-loong64 clashv
  PKGARCH:=all
endef

define Package/luci-app-clashv-arm64/description
  轻量级 mihomo 管理插件（arm64 架构精简版，仅内置单一架构二进制，包体约为通用版的 1/7）。
  与通用版及其他架构精简版互斥。不确定设备架构时请安装通用版 luci-app-clashv。
endef

define Package/luci-app-clashv-arm64/conffiles
/etc/config/clashv
/etc/clashv
endef

define Package/luci-app-clashv-arm64/install
	$(call ClashvCommonInstall,$(1))
	$(INSTALL_BIN) ./bin/clashv-arm64 $(1)/usr/lib/clashv/bin/
endef

define Package/luci-app-clashv-arm64/postinst
$(ClashvPostinst)
endef

define Package/luci-app-clashv-armv7
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Simple mihomo (Clash Meta) manager for OpenWrt (armv7 build)
  URL:=https://github.com/nier/clashv
  DEPENDS:=+ca-bundle +kmod-tun
  CONFLICTS:=luci-app-clashv luci-app-clashv-arm64 luci-app-clashv-mips luci-app-clashv-mipsle luci-app-clashv-amd64 luci-app-clashv-riscv64 luci-app-clashv-loong64 clashv
  PKGARCH:=all
endef

define Package/luci-app-clashv-armv7/description
  轻量级 mihomo 管理插件（armv7 架构精简版，仅内置单一架构二进制，包体约为通用版的 1/7）。
  与通用版及其他架构精简版互斥。不确定设备架构时请安装通用版 luci-app-clashv。
endef

define Package/luci-app-clashv-armv7/conffiles
/etc/config/clashv
/etc/clashv
endef

define Package/luci-app-clashv-armv7/install
	$(call ClashvCommonInstall,$(1))
	$(INSTALL_BIN) ./bin/clashv-armv7 $(1)/usr/lib/clashv/bin/
endef

define Package/luci-app-clashv-armv7/postinst
$(ClashvPostinst)
endef

define Package/luci-app-clashv-mips
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Simple mihomo (Clash Meta) manager for OpenWrt (mips build)
  URL:=https://github.com/nier/clashv
  DEPENDS:=+ca-bundle +kmod-tun
  CONFLICTS:=luci-app-clashv luci-app-clashv-arm64 luci-app-clashv-armv7 luci-app-clashv-mipsle luci-app-clashv-amd64 luci-app-clashv-riscv64 luci-app-clashv-loong64 clashv
  PKGARCH:=all
endef

define Package/luci-app-clashv-mips/description
  轻量级 mihomo 管理插件（mips 大端架构精简版，仅内置单一架构二进制，包体约为通用版的 1/7）。
  与通用版及其他架构精简版互斥。不确定设备架构时请安装通用版 luci-app-clashv。
endef

define Package/luci-app-clashv-mips/conffiles
/etc/config/clashv
/etc/clashv
endef

define Package/luci-app-clashv-mips/install
	$(call ClashvCommonInstall,$(1))
	$(INSTALL_BIN) ./bin/clashv-mips $(1)/usr/lib/clashv/bin/
endef

define Package/luci-app-clashv-mips/postinst
$(ClashvPostinst)
endef

define Package/luci-app-clashv-mipsle
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Simple mihomo (Clash Meta) manager for OpenWrt (mipsle build)
  URL:=https://github.com/nier/clashv
  DEPENDS:=+ca-bundle +kmod-tun
  CONFLICTS:=luci-app-clashv luci-app-clashv-arm64 luci-app-clashv-armv7 luci-app-clashv-mips luci-app-clashv-amd64 luci-app-clashv-riscv64 luci-app-clashv-loong64 clashv
  PKGARCH:=all
endef

define Package/luci-app-clashv-mipsle/description
  轻量级 mihomo 管理插件（mipsle 小端架构精简版，仅内置单一架构二进制，包体约为通用版的 1/7）。
  与通用版及其他架构精简版互斥。不确定设备架构时请安装通用版 luci-app-clashv。
endef

define Package/luci-app-clashv-mipsle/conffiles
/etc/config/clashv
/etc/clashv
endef

define Package/luci-app-clashv-mipsle/install
	$(call ClashvCommonInstall,$(1))
	$(INSTALL_BIN) ./bin/clashv-mipsle $(1)/usr/lib/clashv/bin/
endef

define Package/luci-app-clashv-mipsle/postinst
$(ClashvPostinst)
endef

define Package/luci-app-clashv-amd64
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Simple mihomo (Clash Meta) manager for OpenWrt (amd64 build)
  URL:=https://github.com/nier/clashv
  DEPENDS:=+ca-bundle +kmod-tun
  CONFLICTS:=luci-app-clashv luci-app-clashv-arm64 luci-app-clashv-armv7 luci-app-clashv-mips luci-app-clashv-mipsle luci-app-clashv-riscv64 luci-app-clashv-loong64 clashv
  PKGARCH:=all
endef

define Package/luci-app-clashv-amd64/description
  轻量级 mihomo 管理插件（amd64/x86-64 架构精简版，仅内置单一架构二进制，包体约为通用版的 1/7）。
  与通用版及其他架构精简版互斥。不确定设备架构时请安装通用版 luci-app-clashv。
endef

define Package/luci-app-clashv-amd64/conffiles
/etc/config/clashv
/etc/clashv
endef

define Package/luci-app-clashv-amd64/install
	$(call ClashvCommonInstall,$(1))
	$(INSTALL_BIN) ./bin/clashv-amd64 $(1)/usr/lib/clashv/bin/
endef

define Package/luci-app-clashv-amd64/postinst
$(ClashvPostinst)
endef

define Package/luci-app-clashv-riscv64
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Simple mihomo (Clash Meta) manager for OpenWrt (riscv64 build)
  URL:=https://github.com/nier/clashv
  DEPENDS:=+ca-bundle +kmod-tun
  CONFLICTS:=luci-app-clashv luci-app-clashv-arm64 luci-app-clashv-armv7 luci-app-clashv-mips luci-app-clashv-mipsle luci-app-clashv-amd64 luci-app-clashv-loong64 clashv
  PKGARCH:=all
endef

define Package/luci-app-clashv-riscv64/description
  轻量级 mihomo 管理插件（riscv64 架构精简版，仅内置单一架构二进制，包体约为通用版的 1/7）。
  与通用版及其他架构精简版互斥。不确定设备架构时请安装通用版 luci-app-clashv。
endef

define Package/luci-app-clashv-riscv64/conffiles
/etc/config/clashv
/etc/clashv
endef

define Package/luci-app-clashv-riscv64/install
	$(call ClashvCommonInstall,$(1))
	$(INSTALL_BIN) ./bin/clashv-riscv64 $(1)/usr/lib/clashv/bin/
endef

define Package/luci-app-clashv-riscv64/postinst
$(ClashvPostinst)
endef

define Package/luci-app-clashv-loong64
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Simple mihomo (Clash Meta) manager for OpenWrt (loong64 build)
  URL:=https://github.com/nier/clashv
  DEPENDS:=+ca-bundle +kmod-tun
  CONFLICTS:=luci-app-clashv luci-app-clashv-arm64 luci-app-clashv-armv7 luci-app-clashv-mips luci-app-clashv-mipsle luci-app-clashv-amd64 luci-app-clashv-riscv64 clashv
  PKGARCH:=all
endef

define Package/luci-app-clashv-loong64/description
  轻量级 mihomo 管理插件（loong64 架构精简版，仅内置单一架构二进制，包体约为通用版的 1/7）。
  与通用版及其他架构精简版互斥。不确定设备架构时请安装通用版 luci-app-clashv。
endef

define Package/luci-app-clashv-loong64/conffiles
/etc/config/clashv
/etc/clashv
endef

define Package/luci-app-clashv-loong64/install
	$(call ClashvCommonInstall,$(1))
	$(INSTALL_BIN) ./bin/clashv-loong64 $(1)/usr/lib/clashv/bin/
endef

define Package/luci-app-clashv-loong64/postinst
$(ClashvPostinst)
endef

$(eval $(call BuildPackage,luci-app-clashv))
$(eval $(call BuildPackage,luci-app-clashv-arm64))
$(eval $(call BuildPackage,luci-app-clashv-armv7))
$(eval $(call BuildPackage,luci-app-clashv-mips))
$(eval $(call BuildPackage,luci-app-clashv-mipsle))
$(eval $(call BuildPackage,luci-app-clashv-amd64))
$(eval $(call BuildPackage,luci-app-clashv-riscv64))
$(eval $(call BuildPackage,luci-app-clashv-loong64))
