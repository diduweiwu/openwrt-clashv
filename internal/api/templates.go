package api

// 配置模板 API：列表/内容/新增/修改/删除。
// 内置模板（白名单/黑名单）只读（PUT/DELETE 403）；被手动节点订阅引用的
// 模板不允许删除，防止那些订阅更新时无从生成。

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"clashv/internal/templates"
)

func (d *deps) handleTemplateList(w http.ResponseWriter, r *http.Request) {
	list, err := d.tpl.List()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if list == nil {
		list = []templates.Template{}
	}
	writeJSON(w, 200, map[string]any{"templates": list})
}

func (d *deps) handleTemplateContent(w http.ResponseWriter, r *http.Request) {
	data, err := d.tpl.Content(r.PathValue("name"))
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	writeJSON(w, 200, map[string]any{"content": string(data)})
}

func (d *deps) handleTemplateCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20+64<<10)).Decode(&body); err != nil {
		writeErr(w, 400, errStr("请求体不是合法 JSON"))
		return
	}
	if err := d.tpl.Create(body.Name, body.Content); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"name": body.Name})
}

func (d *deps) handleTemplateUpdate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20+64<<10)).Decode(&body); err != nil {
		writeErr(w, 400, errStr("请求体不是合法 JSON"))
		return
	}
	if err := d.tpl.Update(name, body.Content); err != nil {
		writeTemplateErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"name": name})
}

func (d *deps) handleTemplateDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// 被手动节点订阅引用中的模板拒绝删除（报出订阅名，提示先处理）
	if list, err := d.prof.List(); err == nil {
		var usedBy []string
		for _, p := range list {
			if p.Source == "nodes" && p.Template == name {
				usedBy = append(usedBy, "「"+p.Name+"」")
			}
		}
		if len(usedBy) > 0 {
			writeErr(w, 409, errStr("模板正被订阅 "+joinNames(usedBy)+" 使用，请先删除对应订阅或改用其他模板"))
			return
		}
	}
	if err := d.tpl.Delete(name); err != nil {
		writeTemplateErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func writeTemplateErr(w http.ResponseWriter, err error) {
	if errors.Is(err, templates.ErrProtected) {
		writeErr(w, 403, err)
		return
	}
	writeErr(w, 400, err)
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += "、"
		}
		out += n
	}
	return out
}
