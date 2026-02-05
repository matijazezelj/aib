package ui

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var staticFS embed.FS

// StaticFiles returns the embedded static file system for the web UI.
func StaticFiles() fs.FS {
	sub, _ := fs.Sub(staticFS, "static")
	return sub
}
