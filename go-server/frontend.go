package main

import (
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
)

// mountFrontend 挂载 go:embed 的前端产物，并对非 /api 路径回退 index.html。
func mountFrontend(r chi.Router) {
	staticFS, err := fs.Sub(frontendFiles, "static")
	if err != nil {
		slog.Error("failed to mount embedded frontend", "error", err)
		os.Exit(1)
	}
	indexHTML, err := staticFS.Open("index.html")
	if err != nil {
		slog.Error("embedded frontend missing index.html", "error", err)
		os.Exit(1)
	}
	indexHTML.Close()

	spa := spaHandler(staticFS)
	r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't serve frontend for API routes
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			http.NotFound(w, r)
			return
		}
		spa.ServeHTTP(w, r)
	}))
}
