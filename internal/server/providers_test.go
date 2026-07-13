package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/j27-aurum/gofast/internal/config"
)

func TestProvidersAPI(t *testing.T) {
	cfg := config.Config{
		Listen: ":8180",
		Providers: map[string]config.Provider{
			"lg": {Label: "LG", MinChannels: 50},
		},
	}
	h := ProvidersHandler("/data/config.yaml", true, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var view config.ProvidersView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.FromFile || len(view.Providers) != 1 || view.Providers[0].ID != "lg" {
		t.Fatalf("%+v", view)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/providers", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}
