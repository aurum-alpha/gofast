package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// Built by `npm run build` in web/ (Vite outDir).
//
//go:embed all:dist
var distFS embed.FS

// Handler serves the React SPA from the embedded Vite build.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(sub, path); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
			// Hashed assets under assets/ should 404 if missing, not fall back to index.
			if strings.HasPrefix(path, "assets/") {
				http.NotFound(w, r)
				return
			}
		}
		// SPA fallback: client-side routes.
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
