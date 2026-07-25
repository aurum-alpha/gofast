package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/groups"
)

func TestApplyPathOpsPreservesCommentsAndUnknownKeys(t *testing.T) {
	original := `# top comment
listen: ":8180"   # inline comment
future_unknown_key: keep-me
providers:
  lg:
    label: LG
`
	doc := groups.Doc{Enabled: true, Merges: []groups.Merge{{Name: "News", Members: []string{"NEWS", "News & Info"}}}}
	out, err := ApplyPathOps([]byte(original), []PathOp{{Path: "groups", Value: doc}})
	if err != nil {
		t.Fatalf("ApplyPathOps: %v", err)
	}
	got := string(out)
	for _, want := range []string{"# top comment", "# inline comment", "future_unknown_key: keep-me", "groups:", "News & Info"} {
		if !strings.Contains(got, want) {
			t.Errorf("written config missing %q:\n%s", want, got)
		}
	}

	// Round-trips back through the loader.
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := New(path)
	if err != nil {
		t.Fatalf("New after write: %v", err)
	}
	if !cfg.Groups.Enabled || len(cfg.Groups.Merges) != 1 || cfg.Groups.Merges[0].Name != "News" {
		t.Fatalf("reloaded groups = %+v", cfg.Groups)
	}
}

func TestApplyPathOpsNestedSetAndRemove(t *testing.T) {
	original := `# keep this comment
health:
  l1_interval: 12h   # probe often
  l1_workers: 8
providers:
  lg:
    label: LG West
`
	out, err := ApplyPathOps([]byte(original), []PathOp{
		{Path: "health.l2_enabled", Value: true},
		{Path: "providers.lg.channel_number_offset", Value: float64(1000)},
		{Path: "providers.pluto.enabled", Value: true},
		{Path: "health.l1_workers", Remove: true},
	})
	if err != nil {
		t.Fatalf("ApplyPathOps: %v", err)
	}
	got := string(out)
	for _, want := range []string{"# keep this comment", "# probe often", "l2_enabled: true", "channel_number_offset: 1000", "label: LG West"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "l1_workers") {
		t.Errorf("removed leaf still present:\n%s", got)
	}
	if strings.Contains(got, "1000.0") || strings.Contains(got, "!!float") {
		t.Errorf("integral float not normalized to int:\n%s", got)
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := New(path)
	if err != nil {
		t.Fatalf("New after write: %v", err)
	}
	if !cfg.HealthL2Enabled() {
		t.Fatal("l2_enabled not set")
	}
	if got := cfg.Providers["lg"].ChannelNumberOffset; got != 1000 {
		t.Fatalf("lg offset = %d", got)
	}
	if !cfg.Providers["pluto"].IsEnabled() {
		t.Fatal("pluto not enabled")
	}
}

func TestApplyPathOpsRemovePrunesEmptyParents(t *testing.T) {
	original := `listen: ":8180"
providers:
  pluto:
    enabled: true
`
	out, err := ApplyPathOps([]byte(original), []PathOp{
		{Path: "providers.pluto.enabled", Remove: true},
	})
	if err != nil {
		t.Fatalf("ApplyPathOps: %v", err)
	}
	got := string(out)
	if strings.Contains(got, "providers") || strings.Contains(got, "pluto") {
		t.Errorf("empty parents not pruned:\n%s", got)
	}
	if !strings.Contains(got, "listen") {
		t.Errorf("unrelated key lost:\n%s", got)
	}
}

func TestApplyPathOpsEmptyDocument(t *testing.T) {
	out, err := ApplyPathOps(nil, []PathOp{{Path: "cache_logos", Value: true}})
	if err != nil {
		t.Fatalf("ApplyPathOps: %v", err)
	}
	if !strings.Contains(string(out), "cache_logos: true") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestApplyPathOpsRejectsScalarIntermediate(t *testing.T) {
	if _, err := ApplyPathOps([]byte("listen: \":8180\"\n"), []PathOp{{Path: "listen.port", Value: 1}}); err == nil {
		t.Fatal("expected error for scalar intermediate segment")
	}
	if _, err := ApplyPathOps(nil, []PathOp{{Path: "health..x", Value: 1}}); err == nil {
		t.Fatal("expected error for empty path segment")
	}
}

func TestFileKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `base_url: http://example:8180
health:
  l1_interval: 12h
providers:
  lg:
    label: LG
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	keys, err := FileKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"base_url", "health.l1_interval", "providers.lg.label"} {
		if !keys[want] {
			t.Errorf("missing key %q in %v", want, keys)
		}
	}
	if keys["cache_logos"] {
		t.Error("cache_logos should not be present")
	}

	missing, err := FileKeys(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing file: keys=%v err=%v", missing, err)
	}
}

func TestWriteDefaultGeneratesThenPreserves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "listen:") {
		t.Fatalf("generated default missing listen:\n%s", first)
	}
	// The generated file must round-trip back through the loader (durations etc.).
	if _, err := New(path); err != nil {
		t.Fatalf("generated default does not reload: %v", err)
	}
	// Second call must not overwrite an existing file.
	if err := os.WriteFile(path, []byte("listen: \":9999\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault second: %v", err)
	}
	again, _ := os.ReadFile(path)
	if !strings.Contains(string(again), "9999") {
		t.Fatalf("WriteDefault overwrote existing file:\n%s", again)
	}
}

func TestStoreSaveReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("listen: \":8180\"\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	_, err := store.Save(context.Background(), "", []PathOp{{Path: "cache_logos", Value: false}})
	if err == nil {
		t.Skip("filesystem allowed write to read-only dir (running as root?)")
	}
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected ErrReadOnly, got %v", err)
	}
}
