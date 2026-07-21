package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
	var list model.ProviderList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Providers) != 1 || list.Providers[0].ID != "lg" {
		t.Fatalf("%+v", list)
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
