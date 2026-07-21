package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/server"
)

func guideReg() *provider.Registry {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return regWith(
		map[model.ProviderID]model.ProviderSettings{"lg": {ID: "lg", Label: "LG"}},
		map[model.ProviderID]provider.Lineup{
			"lg": {
				Channels: []model.Channel{
					{Provider: "lg", ID: "news", NormalizedID: "news", Name: "News", OffsetNumber: 1005, StreamURL: "https://s"},
					{Provider: "lg", ID: "drop", NormalizedID: "drop", Name: "Bad", StreamURL: "https://x", Excluded: true},
				},
				Programmes: []model.Programme{
					{ChannelID: "news", Title: "Morning", Start: start, Stop: start.Add(time.Hour)},
					{ChannelID: "drop", Title: "Hidden", Start: start, Stop: start.Add(time.Hour)},
				},
			},
		},
	)
}

func TestGuideXMLAggregate(t *testing.T) {
	h := server.GuideXML(guideReg())

	// Default: export-only, provider-namespaced ids.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/guide.xml", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="lg.news"`) {
		t.Fatalf("expected namespaced channel id, got %s", body)
	}
	if !strings.Contains(body, `channel="lg.news"`) {
		t.Fatalf("expected namespaced programme ref, got %s", body)
	}
	if strings.Contains(body, "lg.drop") {
		t.Fatalf("excluded channel leaked into default guide: %s", body)
	}

	// includeAll: excluded channel appears too.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/guide.xml?includeAll=true", nil))
	if !strings.Contains(rec.Body.String(), `id="lg.drop"`) {
		t.Fatalf("includeAll should include excluded channel: %s", rec.Body.String())
	}

	// Method guard.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/guide.xml", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

func TestGuideProviderXML(t *testing.T) {
	h := server.GuideProviderXML(guideReg())

	serve := func(file string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/guide/"+file, nil)
		req.SetPathValue("file", file)
		h.ServeHTTP(rec, req)
		return rec
	}

	// Known provider: bare ids (no provider prefix).
	rec := serve("lg.xml")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="news"`) || strings.Contains(body, "lg.news") {
		t.Fatalf("expected bare ids, got %s", body)
	}

	// Unknown provider and non-.xml suffix are both 404.
	if rec := serve("nope.xml"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown provider: want 404, got %d", rec.Code)
	}
	if rec := serve("lg"); rec.Code != http.StatusNotFound {
		t.Fatalf("non-.xml: want 404, got %d", rec.Code)
	}
}
