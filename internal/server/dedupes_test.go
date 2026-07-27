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

func dedupeBool(v bool) *bool { return &v }

func dedupeTestStore(t *testing.T, body string) *config.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	return store
}

func dedupeTestRegistry(t *testing.T) *provider.Registry {
	t.Helper()
	settings := map[model.ProviderID]model.ProviderSettings{
		model.ProviderSamsung: {ID: model.ProviderSamsung, Enabled: dedupeBool(true), Label: "Samsung"},
		model.ProviderPluto:   {ID: model.ProviderPluto, Enabled: dedupeBool(true), Label: "Pluto"},
	}
	reg := provider.NewRegistry(nil, settings)
	samsung := reg.Upsert(model.ProviderSamsung, nil, settings[model.ProviderSamsung])
	samsung.Set(provider.Lineup{Channels: []model.Channel{
		{Provider: model.ProviderSamsung, ID: "s1", NormalizedID: "s1", Name: "Cheaters · Samsung", OffsetNumber: 100},
	}})
	pluto := reg.Upsert(model.ProviderPluto, nil, settings[model.ProviderPluto])
	pluto.Set(provider.Lineup{Channels: []model.Channel{
		{Provider: model.ProviderPluto, ID: "p1", NormalizedID: "p1", Name: "Cheaters · Pluto", OffsetNumber: 200},
	}})
	return reg
}

func TestDedupesHandlerGET(t *testing.T) {
	store := dedupeTestStore(t, "dedupe:\n  preferred_providers: [samsung, pluto]\n")
	reg := dedupeTestRegistry(t)
	h := DedupesHandler(store, reg)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dedupes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	var body dedupesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Summary.Clusters != 1 || body.Summary.Unresolved != 1 {
		t.Fatalf("summary %+v", body.Summary)
	}
	if len(body.Clusters) != 1 || body.Clusters[0].Title != "Cheaters" || len(body.Clusters[0].Members) != 2 {
		t.Fatalf("clusters %+v", body.Clusters)
	}
	if len(body.PreferredProviders) != 2 || body.PreferredProviders[0] != model.ProviderSamsung {
		t.Fatalf("preferred %+v", body.PreferredProviders)
	}
}

func TestDedupesApplyKeepOne(t *testing.T) {
	store := dedupeTestStore(t, `providers:
  samsung:
    enabled: true
    label: Samsung
  pluto:
    enabled: true
    label: Pluto
`)
	reg := dedupeTestRegistry(t)
	h := DedupesApplyHandler(store, reg)
	payload := map[string]any{
		"revision":            store.Revision(),
		"preferred_providers": []string{"samsung", "pluto"},
		"keep_all_keys":       []string{},
		"actions": []map[string]string{
			{"provider": "samsung", "id": "s1", "export": "enabled"},
			{"provider": "pluto", "id": "p1", "export": "disabled"},
		},
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/dedupes/apply", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}

	cfg := store.Current()
	if cfg.Providers[model.ProviderPluto].ChannelEmit["p1"].ExportMode() != model.ExportDisabled {
		t.Fatalf("pluto emit %+v", cfg.Providers[model.ProviderPluto].ChannelEmit)
	}
	if cfg.Providers[model.ProviderSamsung].ChannelEmit["s1"].ExportMode() != model.ExportEnabled {
		t.Fatalf("samsung emit %+v", cfg.Providers[model.ProviderSamsung].ChannelEmit)
	}
	if len(cfg.Dedupe.PreferredProviders) != 2 {
		t.Fatalf("dedupe doc %+v", cfg.Dedupe)
	}

	var body dedupesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Summary.Unresolved != 0 || body.Clusters[0].Status != "resolved" {
		t.Fatalf("after apply summary/cluster %+v %+v", body.Summary, body.Clusters[0])
	}
}

func TestDedupesApplyPreservesOtherEmitFields(t *testing.T) {
	store := dedupeTestStore(t, `providers:
  samsung:
    enabled: true
    label: Samsung
    channel_emit:
      s1:
        name: Custom Cheaters
  pluto:
    enabled: true
    label: Pluto
`)
	reg := dedupeTestRegistry(t)
	h := DedupesApplyHandler(store, reg)
	payload := map[string]any{
		"revision":            store.Revision(),
		"preferred_providers": []string{},
		"keep_all_keys":       []string{},
		"actions": []map[string]string{
			{"provider": "samsung", "id": "s1", "export": "disabled"},
		},
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/dedupes/apply", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	row := store.Current().Providers[model.ProviderSamsung].ChannelEmit["s1"]
	if row.Name != "Custom Cheaters" || row.ExportMode() != model.ExportDisabled {
		t.Fatalf("row %+v", row)
	}
}
