'use strict';
'require view';
'require uci';
'require dom';

/*
 * 预估 LuCI 主题的页面背景色，返回 "r,g,b" 字符串；全部取不到返回空串。
 * 只作为 iframe 首 paint 的预估（避免加载闪色）：候选元素从视口最外层
 * 向内，html / body 是多数主题写页面底色的地方；#main / main /
 * #maincontent / .container / #app 兜住「body 透明、背景画在主容器上」
 * 的主题。固定候选对第三方主题可能取错（更外层元素先命中）或取空，
 * iframe 挂载后会用 chainBgColor() 沿真实祖先链实测校正。
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
 * 沿 iframe 实际挂载位置的祖先链合成页面可见底色，返回 [r,g,b] 数组；
 * 整条链全透明返回 null。
 * 与 themeBgColor 的固定候选不同：iframe 周围露出的颜色只由它的祖先链
 * 决定，不管主题把底色画在哪一层（html/body/内容容器），只要在链上就
 * 取得到，对任意第三方主题成立。从最外层（html）向内逐层做 alpha 合成：
 * 不透明背景直接盖底（下层再深的色也被盖住），半透明背景按 alpha 与
 * 当前底混合——链上层叠后的结果即用户真正看到的颜色。
 * 参数 frame：已挂载的 iframe 元素；返回 [r,g,b] 或 null。
 * 示例：body 白底 #fff、内容容器灰底 #e9ecef 时，固定候选会先命中白，
 * 本函数沿 html→body→容器 合成后返回 [233,236,239]（灰，即可见底色）。
 */
function chainBgColor(frame) {
	var chain = [];
	for (var el = frame.parentElement; el; el = el.parentElement)
		chain.push(el);
	var base = null;
	for (var i = chain.length - 1; i >= 0; i--) {
		var m = (window.getComputedStyle(chain[i]).backgroundColor || '')
			.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)(?:,\s*([\d.]+))?\)/);
		if (!m) continue;
		var a = (m[4] === undefined) ? 1 : parseFloat(m[4]);
		if (a >= 1) {
			base = [+m[1], +m[2], +m[3]];
		} else if (a > 0) {
			/* 半透明：叠在当前底上混合；链外（画布）默认白 */
			var b = base || [255, 255, 255];
			base = [
				Math.round(m[1] * a + b[0] * (1 - a)),
				Math.round(m[2] * a + b[1] * (1 - a)),
				Math.round(m[3] * a + b[2] * (1 - a))
			];
		}
	}
	return base;
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
	 * 挂载/加载后沿真实祖先链实测主题底色并校正：
	 * - frame 自身底色随实测色随时更新（加载间隙/圆角外不闪错色）；
	 * - 实测色与 URL 预估不同（或预估为空）且 iframe 已加载时，经
	 *   postMessage(clashv_luci_bg) 通知界面重铺底色并重判深浅；
	 * - 监听界面的 clashv_bg_query：界面启动主动查询一次，兜住预估为空
	 *   或消息早于界面监听注册的时序。
	 */
	var bgNotified = false;
	var frameLoaded = false;
	/*
	 * 「LuCI 内嵌透明背景」开关（存放在插件界面侧 localStorage，默认关）：
	 * 开 = iframe 元素底色透明 + 插件页自身不铺底，透出 LuCI 主题的真实背景
	 * （纯色/渐变/背景图都成立）；关 = 恢复实测色铺底（取色融合方案）。
	 * 开关状态经 clashv_luci_transparent 消息从界面同步过来（插件启动与
	 * 用户切换时各发一次）；fit()/load 里反复调用的 syncBg 也要按它跳过
	 * iframe 自身铺底，否则透明开着会被实测色盖回去。透明开启时祖先链
	 * 实测与校正消息照常进行——界面深浅判断仍依赖实测底色亮度。
	 */
	var transparentOn = false;
	var syncBg = function () {
		var rgb = chainBgColor(frame);
		if (!rgb) return;
		var s = rgb.join(',');
		if (!transparentOn) frame.style.background = 'rgb(' + s + ')';
		if (bgNotified || s === luciBg || !frameLoaded) return;
		bgNotified = true;
		try {
			frame.contentWindow.postMessage(
				{ type: 'clashv_luci_bg', rgb: s }, '*');
		} catch (e) { /* 跨源异常兜底，load 后会再同步 */ }
	};
	frame.addEventListener('load', function () {
		frameLoaded = true;
		syncBg();
	});
	window.addEventListener('message', function (ev) {
		if (ev.source !== frame.contentWindow) return;
		var d = ev.data;
		if (!d) return;
		if (d.type === 'clashv_luci_transparent') {
			transparentOn = !!d.on;
			if (transparentOn) frame.style.background = 'transparent';
			else syncBg();  // 关掉后恢复实测色铺底（bgNotified 已置位时只改底色不再发消息）
			return;
		}
		if (d.type !== 'clashv_bg_query') return;
		frameLoaded = true;
		bgNotified = false;
		syncBg();
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
			syncBg();  // 已挂载时顺带实测校正底色（幂等）
		};

		requestAnimationFrame(fit);
		setTimeout(fit, 50);
		setTimeout(fit, 400);  // 部分主题脚本异步重排容器，晚一拍再实测一次
		frame.addEventListener('load', fit);
		window.addEventListener('resize', fit);

		return E('div', { 'class': 'cbi-map' }, [frame]);
	},
	handleSave: null,
	handleSaveApply: null,
	handleReset: null,
});
