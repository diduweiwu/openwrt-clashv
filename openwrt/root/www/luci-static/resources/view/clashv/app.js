'use strict';
'require view';
'require uci';
'require dom';

/*
 * 探测 LuCI 主题的页面背景色，返回 "r,g,b" 字符串；全部取不到返回空串。
 * 候选元素从视口最外层向内：html / body 是绝大多数主题写页面底色的地方；
 * #main / main / #maincontent / .container / #app 兜住「body 透明、
 * 背景画在主容器上」的主题（如 ImmortalWrt 系），只看 body/html 会取空。
 * 每个候选分两轮看：先纯色（backgroundColor，非透明即命中），后渐变
 * （backgroundImage 里第一个 rgb 色，Argon 类渐变主题兜底；纯色优先于
 * 渐变，避免装饰性渐变盖过真实底色）。
 */
function themeBgColor() {
	var sels = [document.documentElement, document.body,
		'#main', 'main', '#maincontent', '.container', '#app'];
	var els = [];
	for (var i = 0; i < sels.length; i++) {
		var el = (typeof sels[i] === 'string') ? document.querySelector(sels[i]) : sels[i];
		if (el) els.push(el);
	}
	for (var j = 0; j < els.length; j++) {
		var c = (window.getComputedStyle(els[j]).backgroundColor || '')
			.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)(?:,\s*([\d.]+))?\)/);
		if (c && c[4] !== '0')  // alpha 0（全透明）取不到色，跳过
			return c[1] + ',' + c[2] + ',' + c[3];
	}
	for (var k = 0; k < els.length; k++) {
		var g = (window.getComputedStyle(els[k]).backgroundImage || '')
			.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/);
		if (g) return g[1] + ',' + g[2] + ',' + g[3];
	}
	return '';
}

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
		 * 主题背景取不到纯色时省略参数，界面回退到透明让位方案（html.embedded）。
		 */
		var luciBg = themeBgColor();
		if (luciBg)
			src += (src.indexOf('?') >= 0 ? '&' : '?') + 'luci_bg=' + luciBg;
		var frame = E('iframe', {
			src: src,
			/* 放行系统级全屏：日志页「全屏」用 Fullscreen API 盖满整个浏览器窗口，
			   不放行时只能退化为 iframe 区域内铺满（allow 是现代标准写法，
			   allowfullscreen 兼容旧内核，两个都留） */
			allow: 'fullscreen',
			allowfullscreen: 'true',
			/* 底色与插件页同色（拿不到主题色时透明）：避免加载间隙闪出突兀色块 */
			style: 'width:100%;border:0;border-radius:8px;display:block;background:' +
				(luciBg ? 'rgb(' + luciBg + ')' : 'transparent')
		});

		/*
		 * 页脚随文档流：部分主题（移动端尤甚）把页脚钉在视口底部，叠加 iframe
		 * 撑满剩余高度后，「Powered by LuCI…」版本信息永远贴底常驻；钉住的
		 * 改回文档流尾随（与其他 LuCI 页一致），未钉住的随文档落在 iframe 下方
		 * （首屏外，滚动 LuCI 页面可见）。
		 */
		document.head.appendChild(E('style', { type: 'text/css' },
			'footer, .footer, #footer { position: static !important; bottom: auto !important; }'));

		/*
		 * 让 iframe 撑满视口剩余高度：滚动只发生在界面内部，LuCI 页面本身
		 * 基本不滚（页脚留在文档流末尾，滚 LuCI 页面才能看到，与其他页面一致）。
		 */
		var fit = function () {
			var top = frame.getBoundingClientRect().top;
			var h = Math.max(360, window.innerHeight - top - 8);
			frame.style.height = h + 'px';
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
