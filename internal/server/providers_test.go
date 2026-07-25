package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/server"
)

func TestProvidersAPI(t *testing.T) {
	reg := regWith(
		map[model.ProviderID]model.ProviderSettings{"lg": {ID: "lg", Label: "LG", MinChannels: 50}},
		map[model.ProviderID]provider.Lineup{},
	)
	h := server.ProvidersHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Providers []struct {
			ID    model.ProviderID `json:"id"`
			Stats provider.Stats   `json:"stats"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Providers) != 1 || list.Providers[0].ID != "lg" {
		t.Fatalf("%+v", list)
	}
	if list.Providers[0].Stats.TotalChannels != 0 {
		t.Fatalf("empty lineup stats: %+v", list.Providers[0].Stats)
	}
	// Server listen/path must not leak into the providers payload.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["listen"]; ok {
		t.Fatal("providers API must not include listen")
	}
	if _, ok := raw["path"]; ok {
		t.Fatal("providers API must not include path")
	}

	req = httptest.NewRequest(http.MethodPost, "/api/providers", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

func TestProvidersAPIIncludesStats(t *testing.T) {
	fetchedAt := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	reg := regWith(
		map[model.ProviderID]model.ProviderSettings{"lg": {ID: "lg", Label: "LG"}},
		map[model.ProviderID]provider.Lineup{"lg": {
			Channels: []model.Channel{
				{NormalizedID: "a", StreamURL: "https://a"},
				{NormalizedID: "b", Excluded: true, FilterReason: "DRM", StreamURL: "https://b"},
			},
			ChannelCount: 2,
			FetchedAt:    fetchedAt,
		}},
	)
	rec := httptest.NewRecorder()
	server.ProvidersHandler(reg).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/providers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var list struct {
		Providers []struct {
			ID    model.ProviderID `json:"id"`
			Stats provider.Stats   `json:"stats"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Providers) != 1 {
		t.Fatalf("%+v", list)
	}
	st := list.Providers[0].Stats
	if st.TotalChannels != 2 || st.ExcludedChannels != 1 || !st.FetchedAt.Equal(fetchedAt) {
		t.Fatalf("body=%s stats: %+v", rec.Body.String(), st)
	}
	// ExportedChannels comes from lineup.ChannelCount (export gate count), not live non-excluded.
	if st.ExportedChannels != 2 {
		t.Fatalf("exported_channels want ChannelCount=2, got %d", st.ExportedChannels)
	}
}

func TestProviderDetailAPI(t *testing.T) {
	fetchedAt := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	reg := regWith(
		map[model.ProviderID]model.ProviderSettings{"lg": {
			ID: "lg", Label: "LG", MinChannels: 1, RefreshInterval: 10 * time.Hour,
		}},
		map[model.ProviderID]provider.Lineup{"lg": {
			Channels:     []model.Channel{{NormalizedID: "news", StreamURL: "https://news"}},
			ChannelCount: 1,
			FetchedAt:    fetchedAt,
		}},
	)
	feed, ok := reg.Feed("lg")
	if !ok {
		t.Fatal("missing lg feed")
	}
	feed.SetRefreshSchedule(10*time.Hour, 6*time.Hour, true)

	h := server.ProviderDetailHandler(reg)
	request := func(method, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/providers/"+id, nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := request(http.MethodGet, "lg")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Settings model.ProviderSettings `json:"settings"`
		Stats    provider.Stats         `json:"stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Settings.ID != "lg" || detail.Stats.TotalChannels != 1 || !detail.Stats.FetchedAt.Equal(fetchedAt) {
		t.Fatalf("detail: %+v", detail)
	}
	if !detail.Stats.RefreshIntervalClamped ||
		detail.Stats.RefreshIntervalConfigured != "10h0m0s" ||
		detail.Stats.RefreshIntervalEffective != "6h0m0s" {
		t.Fatalf("clamp stats: %+v", detail.Stats)
	}
	if rec := request(http.MethodGet, "unknown"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown want 404, got %d", rec.Code)
	}
	if rec := request(http.MethodPost, "lg"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST want 405, got %d", rec.Code)
	}
}

func TestProviderDetailDisabledProvider(t *testing.T) {
	disabled := false
	reg := provider.NewRegistry(nil, map[model.ProviderID]model.ProviderSettings{
		"lg": {ID: "lg", Label: "LG", Enabled: &disabled},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/providers/lg", nil)
	req.SetPathValue("id", "lg")
	rec := httptest.NewRecorder()
	server.ProviderDetailHandler(reg).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Settings model.ProviderSettings `json:"settings"`
		Stats    provider.Stats         `json:"stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Settings.Enabled == nil || *detail.Settings.Enabled || detail.Stats.TotalChannels != 0 {
		t.Fatalf("disabled detail: %+v", detail)
	}
	if detail.Stats.ByClassification == nil || detail.Stats.ByGroup == nil || detail.Stats.FilterReasons == nil {
		t.Fatalf("disabled detail rollups must be empty maps, got %+v", detail.Stats)
	}
}
