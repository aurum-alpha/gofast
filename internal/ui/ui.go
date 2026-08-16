package ui

import (
	"io/fs"
	"net/http"
	"os"
	"strings"
)

// Handler serves the React SPA from dir, which holds the built Vite output
// (index.html plus hashed assets/).
//
// The assets are shipped in the container image rather than compiled into the
// binary. The deploy artifact for this project is a container, so an embed
// bought nothing: it coupled `go build` to a completed UI build — the Go
// packages would not compile until vite had run — while the image still had to
// be built either way. Copying dist into the image keeps the two builds
// independent and makes what is being served inspectable in a running
// container.
func Handler(dir string) http.Handler {
	return handlerFor(os.DirFS(dir))
}

// handlerFor is split out so tests can supply an in-memory filesystem instead
// of needing a real UI build on disk.
func handlerFor(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(fsys, path); err == nil {
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
