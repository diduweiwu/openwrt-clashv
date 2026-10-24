'use strict';
'require view';
'require uci';
'require dom';

/*
 * LuCI 入口页：把 clashv 管理界面以 iframe 内嵌到 LuCI。
 * 实际界面由 clashv 服务（Go）直接提供，LuCI 只做一个导航入口。
 */
return L.view.extend({
	render: function () {
		var port = '9097';
		var host = window.location.hostname;
		/*
		 * 把当前 LuCI 会话 id 传给 clashv：界面开启「OpenWrt 登录校验」时，
		 * clashv 用它经 ubus 验证确实是已登录的 LuCI 会话后才放行；
		 * 直接访问 路由器IP:9097 会被 302 跳回本页（未登录 LuCI 时是登录页）。
		 * 全 0 表示未登录，不带参数。
		 */
		var src = 'http://' + host + ':' + port + '/';
		var sid = (L.env && L.env.sessionid) || '';
		if (sid && sid !== '00000000000000000000000000000000')
			src += '?luci_sid=' + encodeURIComponent(sid);
		/*
		 * 把 LuCI 主题的背景色传给 clashv（luci_bg=r,g,b）：界面用同色铺页面底，
		 * 视觉上融入当前主题（深浅由底色亮度自动判断，「跟随系统」时优先跟随 LuCI）。
		 * 主题背景是图片/渐变（如 Argon）时拿不到有意义的纯色就省略参数，
		 * 界面回退到透明让位方案（html.embedded）。body 上取不到再看 html。
		 */
		var luciBg = (function () {
			var els = [document.body, document.documentElement];
			for (var i = 0; i < els.length; i++) {
				var c = window.getComputedStyle(els[i]).backgroundColor || '';
				var m = c.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)(?:,\s*([\d.]+))?\)/);
				if (m && m[4] !== '0')  // alpha 0（全透明）取不到色，跳过
					return m[1] + ',' + m[2] + ',' + m[3];
			}
			return '';
		})();
		if (luciBg)
			src += (src.indexOf('?') >= 0 ? '&' : '?') + 'luci_bg=' + luciBg;
		var frame = E('iframe', {
			src: src,
			/* 底色与插件页同色（拿不到主题色时透明）：避免加载间隙闪出突兀色块 */
			style: 'width:100%;border:0;border-radius:8px;display:block;background:' +
				(luciBg ? 'rgb(' + luciBg + ')' : 'transparent')
		});

		/*
		 * 让 iframe 恰好撑满视口剩余高度：LuCI 页面本身不再溢出滚动，
		 * 左侧菜单保持固定，滚动只发生在界面内部
		 * （界面内容不满一屏时内部也不出滚动条，由界面自身的 overflow-y:auto 保证）。
		 */
		var fit = function () {
			var top = frame.getBoundingClientRect().top;
			var h = Math.max(360, window.innerHeight - top - 8);
			frame.style.height = h + 'px';
			/* 主题页脚/边距等额外高度导致外层仍溢出时，按实际溢出量收敛一次 */
			var over = document.documentElement.scrollHeight - window.innerHeight;
			if (over > 0)
				frame.style.height = Math.max(360, h - over) + 'px';
		};

		requestAnimationFrame(fit);
		setTimeout(fit, 50);
		frame.addEventListener('load', fit);
		window.addEventListener('resize', fit);

		return E('div', { 'class': 'cbi-map' }, [frame]);
	},
	handleSave: null,
	handleSaveApply: null,
	handleReset: null,
});
