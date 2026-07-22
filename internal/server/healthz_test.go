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

func TestHealthzOK(t *testing.T) {
	s := httptest.NewServer((&server.Server{}).Handler())
	defer s.Close()

	resp, err := http.Get(s.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.OK {
		t.Fatalf("body = %#v", body)
	}
}

func TestHealthzProviderStale(t *testing.T) {
	fetchedAt := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	attemptAt := fetchedAt.Add(time.Hour)
	reg := regWith(
		map[model.ProviderID]model.ProviderSettings{
			"lg": {ID: "lg", Label: "LG Channels"},
		},
		map[model.ProviderID]provider.Lineup{
			"lg": {
				Channels: []model.Channel{
					{Provider: "lg", ID: "a", NormalizedID: "a", Name: "A", StreamURL: "https://a"},
				},
				ChannelCount:   1,
				ProgrammeCount: 2,
				FetchedAt:      fetchedAt,
				Programmes: []model.Programme{
					{
						ChannelID: "a",
						Title:     "Show",
						Start:     fetchedAt,
						Stop:      fetchedAt.Add(30 * time.Minute),
					},
					{
						ChannelID: "a",
						Title:     "Encore",
						Start:     fetchedAt.Add(time.Hour),
						Stop:      fetchedAt.Add(90 * time.Minute),
					},
				},
			},
		},
	)
	feed, ok := reg.Feed("lg")
	if !ok {
		t.Fatal("missing lg feed")
	}
	feed.SetStatus(provider.Status{
		LastAttemptAt: attemptAt,
		LastError:     "upstream unavailable",
		LastErrorAt:   attemptAt,
	})

	srv := &server.Server{Healthz: server.HealthzHandler(reg)}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var body struct {
		OK        bool `json:"ok"`
		Providers []struct {
			ID                 string    `json:"id"`
			Label              string    `json:"label"`
			Stale              bool      `json:"stale"`
			FetchedAt          time.Time `json:"fetched_at"`
			LastAttemptAt      time.Time `json:"last_attempt_at"`
			LastError          string    `json:"last_error"`
			LastErrorAt        time.Time `json:"last_error_at"`
			ExportedChannels   int       `json:"exported_channels"`
			ExportedProgrammes int       `json:"exported_programmes"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.OK {
		t.Fatalf("ok = false")
	}
	if len(body.Providers) != 1 {
		t.Fatalf("providers = %#v", body.Providers)
	}
	p := body.Providers[0]
	if p.ID != "lg" || p.Label != "LG Channels" {
		t.Fatalf("identity = %#v", p)
	}
	if !p.Stale || p.LastError != "upstream unavailable" {
		t.Fatalf("stale fields = %#v", p)
	}
	if !p.FetchedAt.Equal(fetchedAt) || !p.LastAttemptAt.Equal(attemptAt) || !p.LastErrorAt.Equal(attemptAt) {
		t.Fatalf("times = %#v", p)
	}
	if p.ExportedChannels != 1 || p.ExportedProgrammes != 2 {
		t.Fatalf("counts = %#v", p)
	}
}
