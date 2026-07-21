package server

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
)

const (
	mimeM3U = "application/vnd.apple.mpegurl"
	mimeXML = "application/xml; charset=utf-8"
)

// PlaylistFile serves GET /{provider}.m3u and GET /{provider}.xml by reading the
// cache. Go's ServeMux wildcards must end at '}' so we match /{file} and strip
// the suffix. This single-segment pattern also catches SPA routes (e.g. /guide)
// on a hard reload, so non-playlist paths are delegated to fallback (the embedded
// UI). A nil fallback yields 404.
func PlaylistFile(cc *cache.Cache, fallback http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file := r.PathValue("file")
		switch {
		case strings.HasSuffix(file, ".m3u"):
			data, err := cc.ReadM3U(model.ProviderID(strings.TrimSuffix(file, ".m3u")))
			serveCache(w, data, err, mimeM3U)
		case strings.HasSuffix(file, ".xml"):
			data, err := cc.ReadXMLTV(model.ProviderID(strings.TrimSuffix(file, ".xml")))
			serveCache(w, data, err, mimeXML)
		case fallback != nil:
			fallback.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	}
}

// AggregatePlaylist serves GET /playlist.m3u (all providers, namespaced ids).
func AggregatePlaylist(cc *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := cc.ReadAggregateM3U()
		serveCache(w, data, err, mimeM3U)
	}
}

// AggregateGuide serves GET /epg.xml (all providers, namespaced ids).
func AggregateGuide(cc *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := cc.ReadAggregateXMLTV()
		serveCache(w, data, err, mimeXML)
	}
}

func serveCache[T ~[]byte](w http.ResponseWriter, data T, err error, contentType string) {
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write([]byte(data))
}
