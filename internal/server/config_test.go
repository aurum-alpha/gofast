package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

// newConfigStore writes body as config.yaml in a temp dir and loads a Store.
func newConfigStore(t *testing.T, body string) *config.Store {
	t.Helper()
	for _, env := range []string{
		"PORT", "FASTGEN_BASE_URL", "FASTGEN_DATA_DIR", "FASTGEN_PROXY_BASE_URL", "FASTGEN_PROXY_INTERNAL_URL",
		"FASTGEN_PROXY_ALL", "FASTGEN_CACHE_LOGOS",
	} {
		t.Setenv(env, "")
	}
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

const configTestYAML = `listen: ":8180"
base_url: "http://localhost:8180"
proxy_base_url: "http://proxy:8181"
cache_logos: true
timeouts:
  http_client: 45s
logging:
  level: debug
health:
  consecutive_failures: 4
  l1_interval: 12h
  l2_enabled: true
  l2_interval: 30m
artwork_tls:
  cdn.example:
    insecure_skip_verify: true
providers:
  lg:
    label: "LG Channels"
    min_channels: 50
`

func TestConfigHandler(t *testing.T) {
	store := newConfigStore(t, configTestYAML)
	t.Setenv("FASTGEN_PROXY_ALL", "false")
	reg := provider.NewRegistry(nil, map[model.ProviderID]model.ProviderSettings{
		model.ProviderLG: {Label: "LG Channels", MinChannels: 50},
	})

	h := ConfigHandler(store, reg, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var got ConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Revision == "" || got.Revision != store.Revision() {
		t.Fatalf("revision = %q", got.Revision)
	}
	if !got.Source.FromFile || got.Source.Path != store.Path() || !got.Source.Writable {
		t.Fatalf("source = %+v", got.Source)
	}

	listen := got.Fields["listen"]
	if listen.Value != ":8180" || !listen.RestartRequired || listen.Editable {
		t.Fatalf("listen field = %+v", listen)
	}
	base := got.Fields["base_url"]
	if base.Value != "http://localhost:8180" || base.Source != "file" || !base.Editable {
		t.Fatalf("base_url field = %+v", base)
	}
	proxyAll := got.Fields["proxy_all"]
	if proxyAll.Source != "env" || proxyAll.Env != "FASTGEN_PROXY_ALL" || proxyAll.Editable {
		t.Fatalf("proxy_all field = %+v", proxyAll)
	}
	if v := got.Fields["cache_logos"].Value; v != true {
		t.Fatalf("cache_logos = %v", v)
	}
	if v := got.Fields["timeouts.http_client"].Value; v != "45s" {
		t.Fatalf("http_client = %v", v)
	}
	if v := got.Fields["logging.level"].Value; v != "debug" {
		t.Fatalf("log level = %v", v)
	}
	if v := got.Fields["health.consecutive_failures"].Value; v != float64(4) {
		t.Fatalf("consecutive_failures = %v", v)
	}
	if v := got.Fields["health.l1_interval"]; v.Value != "12h0m0s" || v.Source != "file" {
		t.Fatalf("l1_interval = %+v", v)
	}
	if v := got.Fields["health.max_per_host"]; v.Value != float64(2) || v.Source != "default" {
		t.Fatalf("max_per_host = %+v", v)
	}

	if len(got.ArtworkTLS) != 1 || got.ArtworkTLS[0].Host != "cdn.example" || !got.ArtworkTLS[0].InsecureSkipVerify {
		t.Fatalf("artwork_tls = %+v", got.ArtworkTLS)
	}
	if len(got.Providers) != 7 {
		t.Fatalf("providers = %d", len(got.Providers))
	}
	var lg *ConfigProvider
	for i := range got.Providers {
		if got.Providers[i].Settings.ID == model.ProviderLG {
			lg = &got.Providers[i]
		}
		if !got.Providers[i].Configured && got.Providers[i].Settings.IsEnabled() {
			t.Fatalf("unconfigured provider enabled: %+v", got.Providers[i])
		}
	}
	if lg == nil || !lg.Configured || !lg.Settings.IsEnabled() || lg.Settings.Label != "LG Channels" {
		t.Fatalf("lg = %+v", lg)
	}
	if len(lg.FieldSupport) == 0 {
		t.Fatalf("lg field_support empty")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/config", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d", rec.Code)
	}
}

func putConfig(t *testing.T, h http.HandlerFunc, body string, origin string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	h.ServeHTTP(rec, req)
	return rec
}

func TestConfigSaveHandler(t *testing.T) {
	store := newConfigStore(t, configTestYAML)
	h := ConfigSaveHandler(store, nil, nil)
	rev := store.Revision()

	rec := putConfig(t, h, `{"revision":"`+rev+`","ops":[{"path":"cache_logos","value":false},{"path":"health.l1_interval","value":"6h"}]}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var resp configSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Revision == rev || resp.Revision != store.Revision() {
		t.Fatalf("revision = %q", resp.Revision)
	}
	cfg := store.Current()
	if cfg.CacheLogosEnabled() {
		t.Fatal("cache_logos still enabled after save")
	}
	if cfg.HealthL1Interval().String() != "6h0m0s" {
		t.Fatalf("l1_interval = %s", cfg.HealthL1Interval())
	}

	// Stale revision → 409.
	if rec := putConfig(t, h, `{"revision":"`+rev+`","ops":[{"path":"cache_logos","value":true}]}`, ""); rec.Code != http.StatusConflict {
		t.Fatalf("stale revision status = %d", rec.Code)
	}
	// Restart-only field → 422.
	if rec := putConfig(t, h, `{"ops":[{"path":"listen","value":":9999"}]}`, ""); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("listen status = %d", rec.Code)
	}
	// Env-shadowed field → 422.
	t.Setenv("FASTGEN_CACHE_LOGOS", "true")
	if rec := putConfig(t, h, `{"ops":[{"path":"cache_logos","value":true}]}`, ""); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("env-shadowed status = %d", rec.Code)
	}
	t.Setenv("FASTGEN_CACHE_LOGOS", "")
	// Invalid candidate (proxy_all without proxy_base_url) → 422.
	if rec := putConfig(t, h, `{"ops":[{"path":"proxy_all","value":true},{"path":"proxy_base_url","remove":true}]}`, ""); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid candidate status = %d body %s", rec.Code, rec.Body.String())
	}
	// Malformed body / empty ops → 400.
	if rec := putConfig(t, h, `{"ops":`, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed status = %d", rec.Code)
	}
	if rec := putConfig(t, h, `{"ops":[]}`, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty ops status = %d", rec.Code)
	}
	// Cross-origin → 403.
	if rec := putConfig(t, h, `{"ops":[{"path":"cache_logos","value":true}]}`, "http://evil.example"); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", rec.Code)
	}
	// Same-origin Origin header is accepted (example.com is httptest's default host).
	if rec := putConfig(t, h, `{"ops":[{"path":"cache_logos","value":true}]}`, "http://example.com"); rec.Code != http.StatusOK {
		t.Fatalf("same-origin status = %d body %s", rec.Code, rec.Body.String())
	}
}
