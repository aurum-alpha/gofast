package cache_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

func TestPurgeNonCurrentKeepsServing(t *testing.T) {
	cc := cache.New(t.TempDir())
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("A")}, model.M3UFile("A"), model.XMLTVFile("<a/>"), provider.Meta{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("B")}, model.M3UFile("B"), model.XMLTVFile("<b/>"), provider.Meta{}); err != nil {
		t.Fatal(err)
	}
	providerDir := filepath.Join(ccRoot(t, cc), "lg", "generations")
	extra := filepath.Join(providerDir, "extra-old")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, "playlist.m3u"), []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(providerDir, ".staging-leftover")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	m3uBefore, err := cc.ReadM3U("lg")
	if err != nil || string(m3uBefore) != "B" {
		t.Fatalf("before purge: %q %v", m3uBefore, err)
	}

	stats, err := cc.PurgeNonCurrent("lg")
	if err != nil {
		t.Fatal(err)
	}
	if stats.DeletedFiles < 1 {
		t.Fatalf("expected deletions, got %+v", stats)
	}
	m3uAfter, err := cc.ReadM3U("lg")
	if err != nil || string(m3uAfter) != "B" {
		t.Fatalf("after purge serving broken: %q %v", m3uAfter, err)
	}
	entries, err := os.ReadDir(providerDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "extra-old" || e.Name() == ".staging-leftover" {
			t.Fatalf("leftover %q still present", e.Name())
		}
	}
}

func ccRoot(t *testing.T, cc *cache.Cache) string {
	t.Helper()
	// LogoPath of a known file reveals root: {root}/lg/logos/x
	path, err := cc.LogoPath("lg", "probe.png")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(path)))
}

func TestInventoryAndLogoDelete(t *testing.T) {
	dir := t.TempDir()
	cc := cache.New(dir)
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("RAW")}, model.M3UFile("#"), model.XMLTVFile("<tv/>"), provider.Meta{}); err != nil {
		t.Fatal(err)
	}
	if err := cc.WriteLogo("lg", "ch1.png", []byte("png")); err != nil {
		t.Fatal(err)
	}
	if err := cc.WriteLogoMeta("lg", "ch1.png", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	inv, err := cc.Inventory([]model.ProviderID{"lg"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.GenerationCount < 1 || inv.LogoBytes < 1 {
		t.Fatalf("inventory: %+v", inv)
	}
	found := false
	for _, p := range inv.Providers {
		if p.ID == "lg" && p.Known && p.Logos.Files >= 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("lg not in inventory: %+v", inv.Providers)
	}

	stats, err := cc.DeleteChannelLogos("lg", "ch1")
	if err != nil {
		t.Fatal(err)
	}
	if stats.DeletedFiles < 2 {
		t.Fatalf("delete channel logos: %+v", stats)
	}
	if _, err := cc.StatLogo("lg", "ch1.png"); err == nil {
		t.Fatal("logo still present")
	}
}

func TestSweepOrphans(t *testing.T) {
	dir := t.TempDir()
	cc := cache.New(dir)
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("RAW")}, model.M3UFile("#"), model.XMLTVFile("<tv/>"), provider.Meta{}); err != nil {
		t.Fatal(err)
	}
	if err := cc.WriteLogo("lg", "keep.png", []byte("k")); err != nil {
		t.Fatal(err)
	}
	if err := cc.WriteLogo("lg", "gone.png", []byte("g")); err != nil {
		t.Fatal(err)
	}
	if err := cc.WriteLogo("lg", "keep.jpg", []byte("stale")); err != nil {
		t.Fatal(err)
	}
	// Unconfigured provider dir
	junk := filepath.Join(dir, "notaprovider", "generations", "x")
	if err := os.MkdirAll(junk, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(junk, "f"), []byte("f"), 0o644); err != nil {
		t.Fatal(err)
	}
	gens := filepath.Join(dir, "lg", "generations")
	staging := filepath.Join(gens, ".staging-crash")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	lineup := map[model.ProviderID]map[string]struct{}{
		"lg": {"keep": {}},
	}
	keepFiles := map[model.ProviderID]map[string]string{
		"lg": {"keep": "keep.png"},
	}
	stats, err := cc.SweepOrphans([]model.ProviderID{"lg"}, lineup, keepFiles)
	if err != nil {
		t.Fatal(err)
	}
	if stats.DeletedFiles < 1 {
		t.Fatalf("expected sweep deletions: %+v", stats)
	}
	if _, err := os.Stat(filepath.Join(dir, "notaprovider")); !os.IsNotExist(err) {
		t.Fatalf("unknown provider dir still present: %v", err)
	}
	if _, err := cc.StatLogo("lg", "keep.png"); err != nil {
		t.Fatal("keep logo removed")
	}
	if _, err := cc.StatLogo("lg", "gone.png"); err == nil {
		t.Fatal("orphan logo still present")
	}
	if _, err := cc.StatLogo("lg", "keep.jpg"); err == nil {
		t.Fatal("stale ext still present")
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("staging leftover still present")
	}
}
