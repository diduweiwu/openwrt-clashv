package api

import (
	"io/fs"
	"net/http"
)

// serveIndex 输出内嵌的 index.html，用于前端 history 路由回退。
// idxHTML 在 Serve 启动时由 server.go 填充。
func serveIndex(w http.ResponseWriter, r *http.Request) {
	if idxHTML == nil {
		http.Error(w, "frontend not built", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(idxHTML)
}

var idxHTML []byte

// loadIndex 启动时预读内嵌 index.html。
func loadIndex(dist fs.FS) {
	data, err := fs.ReadFile(dist, "index.html")
	if err == nil {
		idxHTML = data
	}
}
