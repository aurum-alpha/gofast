package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/server"
)

func TestMetricsPrometheusText(t *testing.T) {
	fetchedAt := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
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
				ProgrammeCount: 1,
				FetchedAt:      fetchedAt,
				Programmes: []model.Programme{
					{
						ChannelID: "a",
						Title:     "Show",
						Start:     fetchedAt,
						Stop:      fetchedAt.Add(30 * time.Minute),
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
		LastAttemptAt: fetchedAt.Add(time.Hour),
		LastError:     "upstream unavailable",
		LastErrorAt:   fetchedAt.Add(time.Hour),
	})
	feed.RecordRefresh(false, 1500*time.Millisecond)
	feed.RecordRefresh(true, 2*time.Second)

	rec := httptest.NewRecorder()
	server.MetricsHandler(reg).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") || !strings.Contains(ct, "version=0.0.4") {
		t.Fatalf("Content-Type = %q", ct)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		`gofast_provider_stale{provider="lg"} 1`,
		`gofast_provider_exported_channels{provider="lg"} 1`,
		`gofast_provider_exported_programmes{provider="lg"} 1`,
		`gofast_provider_refresh_total{provider="lg",result="success"} 1`,
		`gofast_provider_refresh_total{provider="lg",result="failure"} 1`,
		`gofast_provider_refresh_duration_seconds{provider="lg"} 2`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}
