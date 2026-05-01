package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed static/*
var embeddedFiles embed.FS

var staticFS, _ = fs.Sub(embeddedFiles, "static")
var indexHTML, _ = fs.ReadFile(staticFS, "index.html")

func Handler() http.Handler {
	fileServer := http.FileServer(http.FS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "." || clean == "" {
			serveIndex(w)
			return
		}
		if _, err := fs.Stat(staticFS, clean); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		if strings.Contains(path.Base(clean), ".") {
			http.NotFound(w, r)
			return
		}
		serveIndex(w)
	})
}

func serveIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}
