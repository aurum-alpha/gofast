package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/server"
)

func programmesTestReg(t *testing.T, ch model.Channel, progs []model.Programme) *provider.Registry {
	t.Helper()
	settings := map[model.ProviderID]model.ProviderSettings{
		model.ProviderLG: {ID: model.ProviderLG, Enabled: boolPtr(true), Label: "LG"},
	}
	return regWith(settings, map[model.ProviderID]provider.Lineup{
		model.ProviderLG: {
			Channels:   []model.Channel{ch},
			Programmes: progs,
			FetchedAt:  time.Now(),
		},
	})
}

func getProgrammes(t *testing.T, reg *provider.Registry, path string) *httptest.ResponseRecorder {
	t.Helper()
	h := server.ChannelProgrammesHandler(reg)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("provider", "lg")
	req.SetPathValue("normalizedId", "news")
	h.ServeHTTP(rec, req)
	return rec
}

func TestChannelProgrammesNotFound(t *testing.T) {
	reg := programmesTestReg(t, model.Channel{
		Provider: model.ProviderLG, NormalizedID: "news", Name: "News", StreamURL: "https://x",
	}, nil)
	h := server.ChannelProgrammesHandler(reg)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/channels/lg/missing/programmes", nil)
	req.SetPathValue("provider", "lg")
	req.SetPathValue("normalizedId", "missing")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestChannelProgrammesEmptyJSONArray(t *testing.T) {
	reg := programmesTestReg(t, model.Channel{
		Provider: model.ProviderLG, NormalizedID: "news", Name: "News", StreamURL: "https://x",
	}, nil)
	rec := getProgrammes(t, reg, "/api/channels/lg/news/programmes")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"programmes":[]`) {
		t.Fatalf("want empty programmes array, got %s", rec.Body.String())
	}
}

func TestChannelProgrammesFiltersAndWindow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	from := now.Add(-30 * time.Minute)
	to := now.Add(2 * time.Hour)
	progs := []model.Programme{
		{ChannelID: "news", Title: "Now Show", Start: now.Add(-10 * time.Minute), Stop: now.Add(50 * time.Minute)},
		{ChannelID: "news", Title: "Next Show", Start: now.Add(50 * time.Minute), Stop: now.Add(110 * time.Minute)},
		{ChannelID: "other", Title: "Other Channel", Start: now, Stop: now.Add(time.Hour)},
		{ChannelID: "news", Title: " ", Start: now, Stop: now.Add(time.Hour)}, // invalid title
		{ChannelID: "news", Title: "Far Past", Start: now.Add(-48 * time.Hour), Stop: now.Add(-47 * time.Hour)},
		{ChannelID: "news", Title: "Far Future", Start: now.Add(48 * time.Hour), Stop: now.Add(49 * time.Hour)},
	}
	reg := programmesTestReg(t, model.Channel{
		Provider: model.ProviderLG, NormalizedID: "news", Name: "News", StreamURL: "https://x",
	}, progs)

	path := "/api/channels/lg/news/programmes?from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339)
	rec := getProgrammes(t, reg, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Programmes []model.Programme `json:"programmes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Programmes) != 2 {
		t.Fatalf("programmes=%+v (want 2)", body.Programmes)
	}
	if body.Programmes[0].Title != "Now Show" || body.Programmes[1].Title != "Next Show" {
		t.Fatalf("order/titles=%+v", body.Programmes)
	}
}

func TestChannelProgrammesDefaultWindow(t *testing.T) {
	now := time.Now().UTC()
	progs := []model.Programme{
		{ChannelID: "news", Title: "In Window", Start: now.Add(time.Hour), Stop: now.Add(2 * time.Hour)},
		{ChannelID: "news", Title: "Too Far", Start: now.Add(24 * time.Hour), Stop: now.Add(25 * time.Hour)},
	}
	reg := programmesTestReg(t, model.Channel{
		Provider: model.ProviderLG, NormalizedID: "news", Name: "News", StreamURL: "https://x",
	}, progs)
	rec := getProgrammes(t, reg, "/api/channels/lg/news/programmes")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Programmes []model.Programme `json:"programmes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Programmes) != 1 || body.Programmes[0].Title != "In Window" {
		t.Fatalf("programmes=%+v", body.Programmes)
	}
}

func TestChannelProgrammesExcludedChannel(t *testing.T) {
	now := time.Now().UTC()
	reg := programmesTestReg(t, model.Channel{
		Provider:     model.ProviderLG,
		NormalizedID: "news",
		Name:         "News",
		StreamURL:    "https://x",
		Excluded:     true,
		FilterReason: "DRM",
	}, []model.Programme{
		{ChannelID: "news", Title: "Still Here", Start: now, Stop: now.Add(time.Hour)},
	})
	rec := getProgrammes(t, reg, "/api/channels/lg/news/programmes")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body struct {
		Programmes []model.Programme `json:"programmes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Programmes) != 1 {
		t.Fatalf("want programmes for excluded channel, got %+v", body.Programmes)
	}
}

func TestChannelProgrammesBadQuery(t *testing.T) {
	reg := programmesTestReg(t, model.Channel{
		Provider: model.ProviderLG, NormalizedID: "news", Name: "News", StreamURL: "https://x",
	}, nil)
	cases := []string{
		"/api/channels/lg/news/programmes?from=not-a-time",
		"/api/channels/lg/news/programmes?to=also-bad",
		"/api/channels/lg/news/programmes?from=2026-07-25T12:00:00Z&to=2026-07-25T11:00:00Z",
	}
	for _, path := range cases {
		rec := getProgrammes(t, reg, path)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d body=%s (want 400)", path, rec.Code, rec.Body.String())
		}
	}
}
