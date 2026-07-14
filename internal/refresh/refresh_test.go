package refresh_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/provider/lg"
	"github.com/j27-aurum/gofast/internal/refresh"
	"github.com/j27-aurum/gofast/internal/snapshot"
)

func TestOncePublishesLGFromFixture(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "provider", "lg", "testdata", "schedulelist.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	t.Setenv("PORT", "")
	t.Setenv("FASTGEN_BASE_URL", "")
	t.Setenv("FASTGEN_DATA_DIR", "")

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "providers:\n  lg:\n    label: LG\n    min_channels: 1\n    channels_url: " + srv.URL + "\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.New(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	client := httpx.NewClient(5*time.Second, 0)
	settings := map[string]model.ProviderSettings{
		"lg": lg.DefaultSettings().Merge(cfg.Providers["lg"]),
	}
	readers := map[string]provider.Reader{"lg": lg.New(settings["lg"], client)}
	reg := provider.NewRegistry(readers, settings)
	store := snapshot.NewStore()
	refresh.Once(context.Background(), reg, store, nil)

	snap, ok := store.Get("lg")
	if !ok || len(snap.M3U) == 0 || len(snap.XML) == 0 {
		t.Fatalf("snapshot missing: %+v ok=%v", snap, ok)
	}
	if snap.ChannelCount < 1 || snap.ProgrammeCount < 1 {
		t.Fatalf("counts: %+v", snap)
	}
}
