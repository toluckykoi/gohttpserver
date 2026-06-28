package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed frontend/dist
var frontendDistFS embed.FS

// FrontendAssets contains the Vue 3 frontend build output.
var FrontendAssets = http.FS(func() fs.FS {
	sub, err := fs.Sub(frontendDistFS, "frontend/dist")
	if err != nil {
		panic(err)
	}
	return sub
}())
