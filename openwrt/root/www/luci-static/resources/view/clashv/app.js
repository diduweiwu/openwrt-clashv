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
		var frame = E('iframe', {
			src: 'http://' + host + ':' + port + '/',
			style: 'width:100%;border:0;border-radius:8px;min-height:calc(100vh - 70px);background:#14161c',
		});
		return E('div', { 'class': 'cbi-map' }, [frame]);
	},
	handleSave: null,
	handleSaveApply: null,
	handleReset: null,
});
