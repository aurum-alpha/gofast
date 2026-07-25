package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

func emitTestRegistry(t *testing.T, ch model.Channel) *provider.Registry {
	t.Helper()
	settings := model.ProviderSettings{ID: model.ProviderLG, Label: "LG", MinChannels: 1}
	reg := provider.NewRegistry(nil, map[model.ProviderID]model.ProviderSettings{
		model.ProviderLG: settings,
	})
	feed := reg.Upsert(model.ProviderLG, nil, settings)
	feed.Set(provider.Lineup{Channels: []model.Channel{ch}})
	return reg
}

func TestChannelEmitGETAndPUT(t *testing.T) {
	body := `providers:
  lg:
    enabled: true
    min_channels: 1
    label: LG
`
	store := newConfigStore(t, body)
	reg := emitTestRegistry(t, model.Channel{
		Provider:     model.ProviderLG,
		ID:           "ch1",
		NormalizedID: "ch1",
		Name:         "Upstream",
		OffsetNumber: 1001,
		Group:        "News",
		StreamURL:    "https://example/a.m3u8",
	})

	get := ChannelEmitHandler(store, reg)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/channels/lg/ch1/emit", nil)
	req.SetPathValue("provider", "lg")
	req.SetPathValue("normalizedId", "ch1")
	get.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got channelEmitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Revision == "" || !got.Writable || got.Channel.NormalizedID != "ch1" {
		t.Fatalf("GET response: %+v", got)
	}

	num := 55
	putBody, _ := json.Marshal(channelEmitRequest{
		Revision: got.Revision,
		Emit: &model.ChannelEmit{
			Export: model.ExportEnabled,
			Name:   "Custom Name",
			Number: &num,
			Group:  "My Group",
		},
	})
	put := ChannelEmitSaveHandler(store, reg)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/channels/lg/ch1/emit", bytes.NewReader(putBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	req.SetPathValue("provider", "lg")
	req.SetPathValue("normalizedId", "ch1")
	put.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	var saved channelEmitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Revision == got.Revision {
		t.Fatal("revision should change")
	}

	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("channel_emit")) || !bytes.Contains(raw, []byte("Custom Name")) {
		t.Fatalf("yaml missing emit:\n%s", raw)
	}

	resetBody, _ := json.Marshal(channelEmitRequest{Revision: saved.Revision, Emit: nil})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/channels/lg/ch1/emit", bytes.NewReader(resetBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	req.SetPathValue("provider", "lg")
	req.SetPathValue("normalizedId", "ch1")
	put.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status=%d body=%s", rec.Code, rec.Body.String())
	}
	raw, _ = os.ReadFile(store.Path())
	if bytes.Contains(raw, []byte("channel_emit")) {
		t.Fatalf("channel_emit should be removed:\n%s", raw)
	}
}

func TestChannelEmitPUTStaleRevision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("providers:\n  lg:\n    enabled: true\n    min_channels: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	reg := emitTestRegistry(t, model.Channel{
		Provider: model.ProviderLG, NormalizedID: "ch1", Name: "A", StreamURL: "https://x",
	})

	body, _ := json.Marshal(channelEmitRequest{
		Revision: "deadbeef",
		Emit:     &model.ChannelEmit{Name: "X"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/channels/lg/ch1/emit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	req.SetPathValue("provider", "lg")
	req.SetPathValue("normalizedId", "ch1")
	ChannelEmitSaveHandler(store, reg).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d", rec.Code)
	}
}
