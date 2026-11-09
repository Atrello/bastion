// Package web is the panel's web app, built by Vite into dist/ and embedded into the panel's binary, so
// the panel is one file to run.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Build identifies this build of the web app: it changes whenever the app does (index.html names the
// app's files by their contents). The API sends it with every answer, so an open page can tell when the
// panel has been updated and offer to reload.
func Build() string {
	index, _ := dist.ReadFile("dist/index.html")
	sum := sha256.Sum256(index)
	return hex.EncodeToString(sum[:6])
}

// Handler serves the web app. Any address that isn't a file gets index.html, so pages like
// /stacks/3/immich work when opened directly (the app's router takes it from there).
func Handler() http.Handler {
	files, _ := fs.Sub(dist, "dist")
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		index = []byte("The web app hasn't been built: run make build.")
	}
	static := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if info, err := fs.Stat(files, name); err == nil && !info.IsDir() && name != "index.html" {
			if strings.HasPrefix(name, "assets/") {
				// Vite names these by their content, so they never change.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			static.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}
