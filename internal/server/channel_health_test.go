package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/server"
)

type healthStubReader struct{}

func (healthStubReader) Fetch(context.Context) (provider.Raw, error) {
	return provider.Raw{"x": []byte("x")}, nil
}

func (healthStubReader) Parse(provider.Raw) ([]model.Channel, []model.Programme, error) {
	return nil, nil, nil
}

func TestChannelHealthHistory(t *testing.T) {
	settings := model.ProviderSettings{ID: model.ProviderLG, Enabled: boolPtr(true), Label: "LG"}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: healthStubReader{}},
		map[model.ProviderID]model.ProviderSettings{model.ProviderLG: settings},
	)
	feed, _ := reg.Feed(model.ProviderLG)
	feed.Set(provider.Lineup{
		Channels: []model.Channel{{
			Provider:     model.ProviderLG,
			NormalizedID: "news",
			Name:         "News",
			StreamURL:    "https://example/live.m3u8",
		}},
		FetchedAt: time.Now(),
	})

	store, err := channelattr.Open(filepath.Join(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	v, _ := json.Marshal(model.ChannelHealth{Status: model.HealthHealthy, LastCheck: model.HealthCheckSuccess})
	if err := store.Handle(context.Background(), channelattr.Event{
		Provider:  model.ProviderLG,
		ChannelID: "news",
		Kind:      channelattr.KindHealth,
		Value:     v,
		At:        time.Now().UTC(),
		Source:    "health_l1",
	}); err != nil {
		t.Fatal(err)
	}

	h := server.ChannelHealthHistoryHandler(reg, store)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/channels/lg/news/health/history", nil)
	req.SetPathValue("provider", "lg")
	req.SetPathValue("normalizedId", "news")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Events []channelattr.HistoryEvent `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Events) != 1 {
		t.Fatalf("events=%+v", body.Events)
	}
}

func TestChannelHealthProbeUnavailable(t *testing.T) {
	settings := model.ProviderSettings{ID: model.ProviderLG, Enabled: boolPtr(true), Label: "LG"}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: healthStubReader{}},
		map[model.ProviderID]model.ProviderSettings{model.ProviderLG: settings},
	)
	feed, _ := reg.Feed(model.ProviderLG)
	feed.Set(provider.Lineup{
		Channels: []model.Channel{{NormalizedID: "news", StreamURL: "https://example/x"}},
	})
	h := server.ChannelHealthProbeHandler(reg, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/channels/lg/news/health/probe", nil)
	req.SetPathValue("provider", "lg")
	req.SetPathValue("normalizedId", "news")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}

	h2 := server.ChannelHealthProbeL1Handler(reg, nil)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/channels/lg/news/health/probe/l1", nil)
	req.SetPathValue("provider", "lg")
	req.SetPathValue("normalizedId", "news")
	h2.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("l1 status %d", rec.Code)
	}
}

func boolPtr(v bool) *bool { return &v }
