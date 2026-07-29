package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

func clearDeployEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PORT", "")
	t.Setenv("FASTGEN_BASE_URL", "")
	t.Setenv("FASTGEN_DATA_DIR", "")
	t.Setenv("FASTGEN_PROXY_BASE_URL", "")
	t.Setenv("FASTGEN_PROXY_INTERNAL_URL", "")
	t.Setenv("FASTGEN_PROXY_ALL", "")
	t.Setenv("FASTGEN_CACHE_LOGOS", "")
	t.Setenv("FASTGEN_HEALTH_CONSECUTIVE_FAILURES", "")
	t.Setenv("FASTGEN_HEALTH_EXCLUDE_UNHEALTHY", "")
	t.Setenv("FASTGEN_HEALTH_L1_INTERVAL", "")
	t.Setenv("FASTGEN_HEALTH_L1_WORKERS", "")
	t.Setenv("FASTGEN_HEALTH_L2_INTERVAL", "")
	t.Setenv("FASTGEN_HEALTH_L2_ENABLED", "")
	t.Setenv("FASTGEN_HEALTH_L2_WORKERS", "")
	t.Setenv("FASTGEN_HEALTH_L3_ENABLED", "")
	t.Setenv("FASTGEN_HEALTH_L3_INTERVAL", "")
	t.Setenv("FASTGEN_HEALTH_L3_HEALTHY_SAMPLE", "")
	t.Setenv("FASTGEN_HEALTH_FFPROBE_PATH", "")
	t.Setenv("FASTGEN_SMTP_USERNAME", "")
	t.Setenv("FASTGEN_SMTP_PASSWORD", "")
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaults(t *testing.T) {
	cfg := defaults()
	if cfg.Listen != DefaultListen {
		t.Fatalf("Listen: got %q want %q", cfg.Listen, DefaultListen)
	}
	if cfg.DataDir != DefaultDataDir {
		t.Fatalf("DataDir: got %q want %q", cfg.DataDir, DefaultDataDir)
	}
	if cfg.Timeouts.HTTPClient != 60*time.Second {
		t.Fatalf("HTTPClient: got %v", cfg.Timeouts.HTTPClient)
	}
	if cfg.Logging.Level != "info" {
		t.Fatalf("Level: got %q", cfg.Logging.Level)
	}
	if len(cfg.Providers) != 0 {
		t.Fatalf("defaults must not bake in providers, got %d", len(cfg.Providers))
	}
	if cfg.ProxyBaseURL != "" || cfg.ProxyAllEnabled() {
		t.Fatalf("unexpected proxy defaults: %+v", cfg)
	}
	if cfg.CacheLogosEnabled() {
		t.Fatal("cache_logos should default false")
	}
	if cfg.HealthConsecutiveFailures() != 3 {
		t.Fatalf("health consecutive_failures default: got %d", cfg.HealthConsecutiveFailures())
	}
}

func TestNewFromYAML(t *testing.T) {
	clearDeployEnv(t)
	path := writeConfig(t, `
listen: ":9090"
base_url: "http://example.local:9090"
data_dir: "/var/gofast"
proxy_base_url: "https://proxy.example/"
proxy_all: true
timeouts:
  http_client: 30s
logging:
  level: debug
`)
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9090" {
		t.Errorf("Listen: got %q", cfg.Listen)
	}
	if cfg.BaseURL != "http://example.local:9090" {
		t.Errorf("BaseURL: got %q", cfg.BaseURL)
	}
	if cfg.DataDir != "/var/gofast" {
		t.Errorf("DataDir: got %q", cfg.DataDir)
	}
	if cfg.ProxyBaseURL != "https://proxy.example" || !cfg.ProxyAllEnabled() {
		t.Errorf("proxy config: base=%q all=%v", cfg.ProxyBaseURL, cfg.ProxyAllEnabled())
	}
	if cfg.Timeouts.HTTPClient != 30*time.Second {
		t.Errorf("HTTPClient: got %v", cfg.Timeouts.HTTPClient)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Level: got %q", cfg.Logging.Level)
	}
}

func TestNewEnvOverridesYAML(t *testing.T) {
	path := writeConfig(t, `
listen: ":9090"
base_url: "http://from-file"
data_dir: "/from-file"
proxy_base_url: "https://proxy.from-file"
proxy_all: true
`)
	t.Setenv("PORT", "8180")
	t.Setenv("FASTGEN_BASE_URL", "http://from-env")
	t.Setenv("FASTGEN_DATA_DIR", "/from-env")
	t.Setenv("FASTGEN_PROXY_BASE_URL", "https://proxy.from-env///")
	t.Setenv("FASTGEN_PROXY_ALL", "false")

	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8180" {
		t.Errorf("Listen: PORT should win, got %q", cfg.Listen)
	}
	if cfg.BaseURL != "http://from-env" {
		t.Errorf("BaseURL: env should win, got %q", cfg.BaseURL)
	}
	if cfg.DataDir != "/from-env" {
		t.Errorf("DataDir: env should win, got %q", cfg.DataDir)
	}
	if cfg.ProxyBaseURL != "https://proxy.from-env" || cfg.ProxyAllEnabled() {
		t.Errorf("proxy env should win: base=%q all=%v", cfg.ProxyBaseURL, cfg.ProxyAllEnabled())
	}
}

func TestNewProxyInternalURL(t *testing.T) {
	clearDeployEnv(t)
	path := writeConfig(t, `
proxy_base_url: "http://localhost:8181"
proxy_internal_url: "http://fastproxy:8181/"
`)
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProxyBaseURL != "http://localhost:8181" || cfg.ProxyInternalURL != "http://fastproxy:8181" {
		t.Fatalf("proxy bases: public=%q internal=%q", cfg.ProxyBaseURL, cfg.ProxyInternalURL)
	}

	t.Setenv("FASTGEN_PROXY_INTERNAL_URL", "http://proxy:8181///")
	cfg, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProxyInternalURL != "http://proxy:8181" {
		t.Fatalf("env should win: %q", cfg.ProxyInternalURL)
	}

	clearDeployEnv(t)
	_, err = New(writeConfig(t, `proxy_internal_url: "http://fastproxy:8181"`))
	if err == nil || !strings.Contains(err.Error(), "proxy_internal_url requires proxy_base_url") {
		t.Fatalf("expected require public base, got %v", err)
	}
}

func TestNewEmptyPathDefaultsAndEnv(t *testing.T) {
	t.Setenv("PORT", ":7777")
	t.Setenv("FASTGEN_BASE_URL", "http://env-only")
	t.Setenv("FASTGEN_DATA_DIR", "/tmp/data")

	cfg, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":7777" || cfg.BaseURL != "http://env-only" || cfg.DataDir != "/tmp/data" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if cfg.Timeouts.HTTPClient != 60*time.Second {
		t.Fatalf("expected default timeout, got %v", cfg.Timeouts.HTTPClient)
	}
	if len(cfg.Providers) != 0 {
		t.Fatalf("empty path must not invent providers, got %d", len(cfg.Providers))
	}
}

func TestNewMissingFile(t *testing.T) {
	_, err := New(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want os.ErrNotExist, got %v", err)
	}
}

func TestNewRejectsInvalidProxyConfig(t *testing.T) {
	clearDeployEnv(t)
	tests := []string{
		"proxy_all: true\n",
		"proxy_base_url: proxy.internal:8181\n",
		"proxy_base_url: https://proxy.test?token=nope\n",
	}
	for _, body := range tests {
		if _, err := New(writeConfig(t, body)); err == nil {
			t.Fatalf("expected error for %q", body)
		}
	}
}

func TestCacheLogosRequiresBaseURL(t *testing.T) {
	clearDeployEnv(t)
	if _, err := New(writeConfig(t, "cache_logos: true\n")); err == nil {
		t.Fatal("expected cache_logos without base_url to fail")
	}
	cfg, err := New(writeConfig(t, `
cache_logos: true
base_url: "http://fastgen.lan:8180"
`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CacheLogosEnabled() || cfg.BaseURL != "http://fastgen.lan:8180" {
		t.Fatalf("unexpected: enabled=%v base=%q", cfg.CacheLogosEnabled(), cfg.BaseURL)
	}
}

func TestCacheLogosDisabledSkipsBaseURLRequirement(t *testing.T) {
	clearDeployEnv(t)
	cfg, err := New(writeConfig(t, "cache_logos: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CacheLogosEnabled() {
		t.Fatal("expected disabled")
	}
}

func TestCacheLogosEnv(t *testing.T) {
	clearDeployEnv(t)
	t.Setenv("FASTGEN_CACHE_LOGOS", "true")
	t.Setenv("FASTGEN_BASE_URL", "http://from-env:8180")
	cfg, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CacheLogosEnabled() || cfg.BaseURL != "http://from-env:8180" {
		t.Fatalf("got enabled=%v base=%q", cfg.CacheLogosEnabled(), cfg.BaseURL)
	}
}

func TestHealthConsecutiveFailuresYAML(t *testing.T) {
	clearDeployEnv(t)
	cfg, err := New(writeConfig(t, `
health:
  consecutive_failures: 5
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HealthConsecutiveFailures() != 5 {
		t.Fatalf("got %d", cfg.HealthConsecutiveFailures())
	}
}

func TestHealthTierYAMLAliases(t *testing.T) {
	clearDeployEnv(t)
	cfg, err := New(writeConfig(t, `
health:
  l2_interval: 12h
  l2_workers: 7
  l3_enabled: true
  l3_interval: 15m
  l3_workers: 3
  l3_timeout: 20s
  l3_healthy_sample: 0.4
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HealthL1Interval() != 12*time.Hour || cfg.HealthL1Workers() != 7 ||
		!cfg.HealthL2Enabled() || cfg.HealthL2Interval() != 15*time.Minute ||
		cfg.HealthL2Workers() != 3 || cfg.HealthL2Timeout() != 20*time.Second ||
		cfg.HealthL2HealthySample() != 0.4 {
		t.Fatalf("legacy aliases not applied: %+v", cfg.Health)
	}
}

func TestHealthTierNewYAMLKeys(t *testing.T) {
	clearDeployEnv(t)
	cfg, err := New(writeConfig(t, `
health:
  l1_interval: 12h
  l1_workers: 7
  l2_enabled: true
  l2_interval: 15m
  l2_workers: 3
  l2_timeout: 20s
  l2_healthy_sample: 0.4
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HealthL1Interval() != 12*time.Hour || cfg.HealthL1Workers() != 7 ||
		!cfg.HealthL2Enabled() || cfg.HealthL2Interval() != 15*time.Minute ||
		cfg.HealthL2Workers() != 3 || cfg.HealthL2Timeout() != 20*time.Second ||
		cfg.HealthL2HealthySample() != 0.4 {
		t.Fatalf("new keys not applied: %+v", cfg.Health)
	}
}

func TestHealthConsecutiveFailuresEnv(t *testing.T) {
	clearDeployEnv(t)
	t.Setenv("FASTGEN_HEALTH_CONSECUTIVE_FAILURES", "2")
	cfg, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HealthConsecutiveFailures() != 2 {
		t.Fatalf("got %d", cfg.HealthConsecutiveFailures())
	}
}

func TestHealthConsecutiveFailuresEnvInvalid(t *testing.T) {
	clearDeployEnv(t)
	t.Setenv("FASTGEN_HEALTH_CONSECUTIVE_FAILURES", "0")
	if _, err := New(""); err == nil {
		t.Fatal("expected error for N < 1")
	}
}

func TestArtworkTLSParse(t *testing.T) {
	clearDeployEnv(t)

	if _, err := New(writeConfig(t, `
artwork_tls:
  example.com:
    ca_pem: |
      -----BEGIN CERTIFICATE-----
      not-a-real-certificate
      -----END CERTIFICATE-----
`)); err == nil {
		t.Fatal("expected invalid ca_pem to fail")
	}

	cfg, err := New(writeConfig(t, `
artwork_tls:
  TVPNLOGOPUS.SamsungCloud.tv:
    insecure_skip_verify: true
`))
	if err != nil {
		t.Fatal(err)
	}
	policy, ok := cfg.ArtworkTLS["tvpnlogopus.samsungcloud.tv"]
	if !ok || !policy.InsecureSkipVerify {
		t.Fatalf("artwork_tls: %+v", cfg.ArtworkTLS)
	}
}

func TestArtworkTLSValidCAPem(t *testing.T) {
	clearDeployEnv(t)
	pemBytes := mustSelfSignedCAPem(t)
	body := "artwork_tls:\n  logos.example:\n    ca_pem: |\n"
	for _, line := range strings.Split(strings.TrimSpace(string(pemBytes)), "\n") {
		body += "      " + line + "\n"
	}
	cfg, err := New(writeConfig(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ArtworkTLS["logos.example"].CAPem == "" {
		t.Fatal("expected ca_pem to be retained")
	}
}

func mustSelfSignedCAPem(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestNewRejectsInvalidProxyAllEnv(t *testing.T) {
	clearDeployEnv(t)
	t.Setenv("FASTGEN_PROXY_ALL", "sometimes")
	if _, err := New(""); err == nil {
		t.Fatal("expected invalid FASTGEN_PROXY_ALL error")
	}
}

func TestNewPartialYAMLKeepsDefaults(t *testing.T) {
	clearDeployEnv(t)
	path := writeConfig(t, "base_url: http://only-base\n")
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != DefaultListen {
		t.Errorf("Listen default lost: %q", cfg.Listen)
	}
	if cfg.DataDir != DefaultDataDir {
		t.Errorf("DataDir default lost: %q", cfg.DataDir)
	}
	if cfg.BaseURL != "http://only-base" {
		t.Errorf("BaseURL: got %q", cfg.BaseURL)
	}
	if cfg.Timeouts.HTTPClient != 60*time.Second {
		t.Errorf("HTTPClient default lost: %v", cfg.Timeouts.HTTPClient)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Level default lost: %q", cfg.Logging.Level)
	}
}

func TestNewDurationYAML(t *testing.T) {
	clearDeployEnv(t)
	path := writeConfig(t, "timeouts:\n  http_client: 45s\n")
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeouts.HTTPClient != 45*time.Second {
		t.Fatalf("got %v want 45s", cfg.Timeouts.HTTPClient)
	}
}

func TestMergeNilOverlay(t *testing.T) {
	cfg := defaults()
	cfg.merge(nil)
	if cfg.Listen != DefaultListen {
		t.Fatalf("nil overlay changed Listen: %q", cfg.Listen)
	}
}

func TestMergeOverlaysSetFieldsOnly(t *testing.T) {
	cfg := defaults()
	cfg.merge(&Config{BaseURL: "http://from-overlay"})
	if cfg.BaseURL != "http://from-overlay" {
		t.Fatalf("BaseURL: %q", cfg.BaseURL)
	}
	if cfg.Listen != DefaultListen || cfg.DataDir != DefaultDataDir {
		t.Fatalf("unset fields changed: listen=%q data_dir=%q", cfg.Listen, cfg.DataDir)
	}
	if cfg.Timeouts.HTTPClient != 60*time.Second || cfg.Logging.Level != "info" {
		t.Fatalf("defaults lost: %+v", cfg)
	}
}

func TestMergeProviders(t *testing.T) {
	cfg := defaults()
	cfg.Providers = map[model.ProviderID]model.ProviderSettings{"keep": {Label: "Keep"}}

	cfg.merge(&Config{}) // Providers nil → keep base
	if _, ok := cfg.Providers["keep"]; !ok {
		t.Fatal("nil Providers overlay must leave base providers")
	}

	cfg.merge(&Config{
		Providers: map[model.ProviderID]model.ProviderSettings{"lg": {Label: "LG"}},
	})
	if len(cfg.Providers) != 1 || cfg.Providers["lg"].Label != "LG" {
		t.Fatalf("non-nil Providers must replace: %+v", cfg.Providers)
	}
	if _, ok := cfg.Providers["keep"]; ok {
		t.Fatal("replaced map must not keep old keys")
	}

	cfg.merge(&Config{Providers: map[model.ProviderID]model.ProviderSettings{}})
	if cfg.Providers == nil || len(cfg.Providers) != 0 {
		t.Fatalf("empty Providers map must replace with empty: %+v", cfg.Providers)
	}
}

func TestNormalizeListen(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"8180", ":8180"},
		{":8180", ":8180"},
		{"0.0.0.0:8180", "0.0.0.0:8180"},
		{" 9191 ", ":9191"},
		{"", ""},
		{"not-a-port", "not-a-port"},
	}
	for _, tt := range tests {
		if got := NormalizeListen(tt.in); got != tt.want {
			t.Errorf("NormalizeListen(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func TestListenFromEnv(t *testing.T) {
	t.Run("port set", func(t *testing.T) {
		t.Setenv("PORT", "9191")
		if got := ListenFromEnv(":8181"); got != ":9191" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("fallback", func(t *testing.T) {
		t.Setenv("PORT", "")
		if got := ListenFromEnv("8181"); got != ":8181" {
			t.Fatalf("fallback got %q", got)
		}
	})
}

func TestEnvOverlay(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("FASTGEN_BASE_URL", "http://env")
	t.Setenv("FASTGEN_DATA_DIR", "/env-data")
	o, err := envOverlay()
	if err != nil {
		t.Fatal(err)
	}
	if o.Listen != ":8080" || o.BaseURL != "http://env" || o.DataDir != "/env-data" {
		t.Fatalf("%+v", o)
	}
	if o.Timeouts.HTTPClient != 0 || o.Logging.Level != "" || o.Providers != nil {
		t.Fatalf("overlay must only set env fields: %+v", o)
	}
}

func TestOpsReportDefaultsAndEnvPassword(t *testing.T) {
	clearDeployEnv(t)
	cfg, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpsReport.IsEnabled() {
		t.Fatal("ops report should be off by default")
	}
	if cfg.OpsReport.TimezoneOrDefault() != "America/Los_Angeles" || cfg.OpsReport.SendAtOrDefault() != "00:00" {
		t.Fatalf("schedule defaults: %+v", cfg.OpsReport)
	}
	if !cfg.OpsReport.SMTP.STARTTLSOrDefault() || cfg.OpsReport.SMTP.PortOrDefault() != 587 {
		t.Fatalf("smtp defaults: %+v", cfg.OpsReport.SMTP)
	}

	path := writeConfig(t, `
ops_report:
  enabled: true
  timezone: America/New_York
  send_at: "07:30"
  from: gofast@example.com
  to: [ops@example.com]
  smtp:
    host: smtp.example.com
    password: yaml-secret
`)
	cfg, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OpsReport.IsEnabled() || cfg.OpsReport.SMTP.Password != "yaml-secret" {
		t.Fatalf("yaml password: %+v", cfg.OpsReport)
	}
	t.Setenv("FASTGEN_SMTP_PASSWORD", "env-secret")
	t.Setenv("FASTGEN_SMTP_USERNAME", "env-user")
	cfg, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpsReport.SMTP.Password != "env-secret" || cfg.OpsReport.SMTP.Username != "env-user" {
		t.Fatalf("env should win: %+v", cfg.OpsReport.SMTP)
	}
	shadow := EnvShadow()
	if shadow["ops_report.smtp.password"] != "FASTGEN_SMTP_PASSWORD" {
		t.Fatalf("password shadow = %q", shadow["ops_report.smtp.password"])
	}
}

func TestOpsReportValidation(t *testing.T) {
	clearDeployEnv(t)
	_, err := New(writeConfig(t, `
ops_report:
  enabled: true
  from: a@example.com
  to: [b@example.com]
`))
	if err == nil || !strings.Contains(err.Error(), "smtp.host") {
		t.Fatalf("expected host required, got %v", err)
	}
	_, err = New(writeConfig(t, `
ops_report:
  enabled: true
  timezone: Not/AZone
  from: a@example.com
  to: [b@example.com]
  smtp:
    host: smtp.example.com
`))
	if err == nil || !strings.Contains(err.Error(), "timezone") {
		t.Fatalf("expected timezone error, got %v", err)
	}
}

func TestParseSendAt(t *testing.T) {
	h, m, err := ParseSendAt("07:30")
	if err != nil || h != 7 || m != 30 {
		t.Fatalf("got %d:%d %v", h, m, err)
	}
	if _, _, err := ParseSendAt("25:00"); err == nil {
		t.Fatal("expected bad hour")
	}
}
