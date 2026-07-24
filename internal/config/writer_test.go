package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/groups"
)

func TestWriteMapKeyPreservesCommentsAndUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := `# top comment
listen: ":8180"   # inline comment
future_unknown_key: keep-me
providers:
  lg:
    label: LG
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	doc := groups.Doc{Enabled: true, Merges: []groups.Merge{{Name: "News", Members: []string{"NEWS", "News & Info"}}}}
	if err := WriteMapKey(path, "groups", doc); err != nil {
		t.Fatalf("WriteMapKey: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{"# top comment", "# inline comment", "future_unknown_key: keep-me", "groups:", "News & Info"} {
		if !strings.Contains(got, want) {
			t.Errorf("written config missing %q:\n%s", want, got)
		}
	}

	// .bak holds the prior bytes.
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("read .bak: %v", err)
	}
	if string(bak) != original {
		t.Errorf(".bak does not match prior bytes:\n%s", bak)
	}

	// Round-trips back through the loader.
	cfg, err := New(path)
	if err != nil {
		t.Fatalf("New after write: %v", err)
	}
	if !cfg.Groups.Enabled || len(cfg.Groups.Merges) != 1 || cfg.Groups.Merges[0].Name != "News" {
		t.Fatalf("reloaded groups = %+v", cfg.Groups)
	}
}

func TestWriteMapKeyMissingFileCreates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := WriteMapKey(path, "groups", groups.Doc{Enabled: true}); err != nil {
		t.Fatalf("WriteMapKey on missing file: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
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

func TestWriteMapKeyReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("listen: \":8180\"\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	err := WriteMapKey(path, "groups", groups.Doc{Enabled: true})
	if err == nil {
		t.Skip("filesystem allowed write to read-only dir (running as root?)")
	}
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected ErrReadOnly, got %v", err)
	}
}
