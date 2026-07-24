package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

func TestConfigHandler(t *testing.T) {
	proxyAll := false
	cacheLogos := true
	exclude := false
	l2 := true
	cfg := &config.Config{
		Listen:       ":8180",
		BaseURL:      "http://localhost:8180",
		DataDir:      "/data",
		ProxyBaseURL: "http://proxy:8181",
		ProxyAll:     &proxyAll,
		CacheLogos:   &cacheLogos,
		Timeouts:     config.Timeouts{HTTPClient: 45 * time.Second},
		Logging:      config.Logging{Level: "debug"},
		Health: config.Health{
			ConsecutiveFailures: 4,
			ExcludeUnhealthy:    &exclude,
			L1Interval:          12 * time.Hour,
			L2Enabled:           &l2,
			L2Interval:          30 * time.Minute,
			L2Workers:           3,
			L2Timeout:           20 * time.Second,
			FFProbePath:         "/usr/bin/ffprobe",
		},
		ArtworkTLS: map[string]config.ArtworkTLS{
			"cdn.example": {CAPem: "-----BEGIN CERTIFICATE-----\nMII\n-----END CERTIFICATE-----", InsecureSkipVerify: false},
		},
	}
	reg := provider.NewRegistry(nil, map[model.ProviderID]model.ProviderSettings{
		model.ProviderLG: {Label: "LG Channels", MinChannels: 50},
	})

	h := ConfigHandler(cfg, "/data/config.yaml", true, reg, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var got ConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Source.FromFile || got.Source.Path != "/data/config.yaml" {
		t.Fatalf("source = %+v", got.Source)
	}
	if got.Listen != ":8180" || got.BaseURL != "http://localhost:8180" {
		t.Fatalf("deploy = listen %q base %q", got.Listen, got.BaseURL)
	}
	if !got.CacheLogos || got.ProxyAll {
		t.Fatalf("flags cache=%v proxy_all=%v", got.CacheLogos, got.ProxyAll)
	}
	if got.HTTPTimeout != "45s" || got.LogLevel != "debug" {
		t.Fatalf("timeout/log = %q %q", got.HTTPTimeout, got.LogLevel)
	}
	if got.Health.ConsecutiveFailures != 4 || !got.Health.L2Enabled || got.Health.L1Interval != "12h0m0s" {
		t.Fatalf("health = %+v", got.Health)
	}
	if len(got.ArtworkTLS) != 1 || got.ArtworkTLS[0].Host != "cdn.example" || !got.ArtworkTLS[0].CAPemSet {
		t.Fatalf("artwork_tls = %+v", got.ArtworkTLS)
	}
	if strings.Contains(rec.Body.String(), "BEGIN CERTIFICATE") {
		t.Fatal("CA PEM leaked in response")
	}
	if len(got.Providers) != 1 || got.Providers[0].ID != model.ProviderLG {
		t.Fatalf("providers = %+v", got.Providers)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/config", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d", rec.Code)
	}
}
