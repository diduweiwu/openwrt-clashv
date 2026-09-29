// Package web 内嵌前端构建产物（web/dist）。
// 仓库中 dist 只有占位文件；正式构建流程为 `make web` 后 `make build`。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// Dist 返回前端文件系统（根为 dist/）。
func Dist() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return embedded
	}
	return sub
}
