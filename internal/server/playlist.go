package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

const (
	mimeM3U = "application/vnd.apple.mpegurl"
	mimeXML = "application/xml; charset=utf-8"
)

// PlaylistFile serves GET /{provider}.m3u and GET /{provider}.xml by reading the
// cache. Go's ServeMux wildcards must end at '}' so we match /{file} and strip
// the suffix. This single-segment pattern also catches SPA routes (e.g. /guide)
// on a hard reload, so non-playlist paths are delegated to fallback (the embedded
// UI). A nil fallback yields 404. A disabled provider (no live feed in reg) is a
// 404 even when its cached last-good files still exist on disk.
func PlaylistFile(reg *provider.Registry, cc *cache.Cache, fallback http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file := r.PathValue("file")
		switch {
		case strings.HasSuffix(file, ".m3u"):
			id := model.ProviderID(strings.TrimSuffix(file, ".m3u"))
			if _, ok := reg.Feed(id); !ok {
				http.NotFound(w, r)
				return
			}
			data, err := cc.ReadM3U(id)
			serveCache(w, r, data, err, mimeM3U)
		case strings.HasSuffix(file, ".xml"):
			id := model.ProviderID(strings.TrimSuffix(file, ".xml"))
			if _, ok := reg.Feed(id); !ok {
				http.NotFound(w, r)
				return
			}
			data, err := cc.ReadXMLTV(id)
			serveCache(w, r, data, err, mimeXML)
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
		serveCache(w, r, data, err, mimeM3U)
	}
}

// AggregateGuide serves GET /epg.xml (all providers, namespaced ids).
func AggregateGuide(cc *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := cc.ReadAggregateXMLTV()
		serveCache(w, r, data, err, mimeXML)
	}
}

func serveCache[T ~[]byte](w http.ResponseWriter, r *http.Request, data T, err error, contentType string) {
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	body := []byte(data)
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("ETag", etag)
	if noneMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(body)
}

// noneMatch reports whether If-None-Match matches etag (strong comparison).
func noneMatch(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" {
			return true
		}
		if strings.HasPrefix(part, "W/") {
			part = strings.TrimSpace(part[2:])
		}
		if part == etag {
			return true
		}
	}
	return false
}
