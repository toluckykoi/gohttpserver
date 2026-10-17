package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed frontend/dist
var frontendDistFS embed.FS

// FrontendAssets 是内嵌的 Vue 3 前端构建产物。
var FrontendAssets = http.FS(func() fs.FS {
	sub, err := fs.Sub(frontendDistFS, "frontend/dist")
	if err != nil {
		panic(err)
	}
	return sub
}())
