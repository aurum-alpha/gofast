package server

import (
	"net/http"
	"strings"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/xmltv"
)

// GuideXML serves GET /api/guide.xml — all providers aggregated into one XMLTV
// document with provider-namespaced ids. ?includeAll=true also emits excluded/DRM
// channels (diagnostic view); default is export-only (what Jellyfin gets).
func GuideXML(reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeGuide(w, sourcesFrom(reg.Feeds()), includeAll(r), true)
	}
}

// GuideProviderXML serves GET /api/guide/{provider}.xml — one provider's XMLTV
// with bare ids. A non-.xml suffix or unknown provider is a 404 (non-existent
// resource). ?includeAll= is honored as in GuideXML.
func GuideProviderXML(reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, ok := strings.CutSuffix(r.PathValue("file"), ".xml")
		if !ok || id == "" {
			http.NotFound(w, r)
			return
		}
		f, ok := reg.Feed(model.ProviderID(id))
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeGuide(w, sourcesFrom([]*provider.Feed{f}), includeAll(r), false)
	}
}

func includeAll(r *http.Request) bool {
	return r.URL.Query().Get("includeAll") == "true"
}

func sourcesFrom(feeds []*provider.Feed) []xmltv.Source {
	sources := make([]xmltv.Source, 0, len(feeds))
	for _, f := range feeds {
		lin := f.Lineup()
		sources = append(sources, xmltv.Source{
			Provider:   f.ID(),
			Label:      f.Label(),
			Channels:   lin.Channels,
			Programmes: lin.Programmes,
		})
	}
	return sources
}

func writeGuide(w http.ResponseWriter, sources []xmltv.Source, all, namespaceIDs bool) {
	data, err := xmltv.MarshalAll(sources, xmltv.Options{
		IncludeExcluded: all,
		NamespaceIDs:    namespaceIDs,
	})
	if err != nil {
		http.Error(w, "guide not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(data)
}
