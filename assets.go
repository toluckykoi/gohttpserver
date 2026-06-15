package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets
var assetsFS embed.FS

// Assets contains legacy project assets (templates, static files for video-player, ipa-install).
var Assets = http.FS(assetsFS)

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
