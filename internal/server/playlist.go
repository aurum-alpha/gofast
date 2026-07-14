package server

import (
	"net/http"
	"strings"

	"github.com/j27-aurum/gofast/internal/snapshot"
)

// PlaylistFile serves GET /{provider}.m3u and GET /{provider}.xml from the snapshot store.
// Go's ServeMux wildcards must end at '}' so we match /{file} and strip the suffix.
func PlaylistFile(store *snapshot.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file := r.PathValue("file")
		switch {
		case strings.HasSuffix(file, ".m3u"):
			writeM3U(w, store, strings.TrimSuffix(file, ".m3u"))
		case strings.HasSuffix(file, ".xml"):
			writeXML(w, store, strings.TrimSuffix(file, ".xml"))
		default:
			http.NotFound(w, r)
		}
	}
}

func writeM3U(w http.ResponseWriter, store *snapshot.Store, id string) {
	if id == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	snap, ok := store.Get(id)
	if !ok || len(snap.M3U) == 0 {
		http.Error(w, "playlist not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	_, _ = w.Write(snap.M3U)
}

func writeXML(w http.ResponseWriter, store *snapshot.Store, id string) {
	if id == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	snap, ok := store.Get(id)
	if !ok || len(snap.XML) == 0 {
		http.Error(w, "guide not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(snap.XML)
}
