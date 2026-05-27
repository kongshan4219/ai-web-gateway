package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static/*
var staticFS embed.FS

// staticHandler returns an http.Handler that serves embedded static files
// under /static/ from the static/ directory.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("static: failed to create sub filesystem: " + err.Error())
	}
	return http.FileServer(http.FS(sub))
}

// serveIndex serves the embedded index.html for the dashboard route.
func serveIndex() http.HandlerFunc {
	indexHTML, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		panic("static: index.html not found in embedded FS: " + err.Error())
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(indexHTML)
	}
}

// serveStaticFile handles routes like /static/style.css, /static/app.js.
func serveStaticFile(w http.ResponseWriter, r *http.Request) {
	// Strip the /static/ prefix to get the filename
	// e.g. /static/style.css -> style.css, /static/app.js -> app.js
	name := strings.TrimPrefix(r.URL.Path, "/static/")
	if name == "" || name == r.URL.Path {
		http.NotFound(w, r)
		return
	}

	data, err := staticFS.ReadFile("static/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Set appropriate Content-Type
	switch {
	case strings.HasSuffix(name, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(name, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case strings.HasSuffix(name, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	// Support caching for static assets (1 hour)
	w.Header().Set("Cache-Control", "public, max-age=3600")

	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
