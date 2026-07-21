package server

import (
	"net/http"
	"strings"

	"github.com/j27-aurum/gofast/internal/snapshot"
	"github.com/j27-aurum/gofast/internal/xmltv"
)

// GuideXML serves GET /api/guide.xml — all providers aggregated into one XMLTV
// document with provider-namespaced ids. ?includeAll=true also emits excluded/DRM
// channels (diagnostic view); default is export-only (what Jellyfin gets).
func GuideXML(store *snapshot.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sources := sourcesFrom(store.Snapshots())
		writeGuide(w, sources, includeAll(r), true)
	}
}

// GuideProviderXML serves GET /api/guide/{provider}.xml — one provider's XMLTV
// with bare ids. Non-.xml suffix or an unknown provider is a 404 (non-existent
// resource). ?includeAll= is honored as in GuideXML.
func GuideProviderXML(store *snapshot.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		file := r.PathValue("file")
		id, ok := strings.CutSuffix(file, ".xml")
		if !ok || id == "" {
			http.NotFound(w, r)
			return
		}
		snap, ok := store.Get(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeGuide(w, sourcesFrom([]snapshot.Snapshot{snap}), includeAll(r), false)
	}
}

func includeAll(r *http.Request) bool {
	return r.URL.Query().Get("includeAll") == "true"
}

func sourcesFrom(snaps []snapshot.Snapshot) []xmltv.Source {
	sources := make([]xmltv.Source, 0, len(snaps))
	for _, snap := range snaps {
		sources = append(sources, xmltv.Source{
			Provider:   snap.ProviderID,
			Label:      snap.Label,
			Channels:   snap.Channels,
			Programmes: snap.Programmes,
		})
	}
	return sources
}

func writeGuide(w http.ResponseWriter, sources []xmltv.Source, all, namespaceIDs bool) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	if err := xmltv.WriteAll(w, sources, all, namespaceIDs); err != nil {
		http.Error(w, "guide not ready", http.StatusServiceUnavailable)
	}
}
