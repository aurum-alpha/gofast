package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/groups"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/server"
)

func groupsTestRegistry(t *testing.T) *provider.Registry {
	t.Helper()
	settings := map[model.ProviderID]model.ProviderSettings{
		model.ProviderLG:    {ID: model.ProviderLG, Enabled: boolPtr(true), Label: "LG"},
		model.ProviderPluto: {ID: model.ProviderPluto, Enabled: boolPtr(true), Label: "Pluto"},
	}
	reg := provider.NewRegistry(map[model.ProviderID]provider.Reader{
		model.ProviderLG:    healthStubReader{},
		model.ProviderPluto: healthStubReader{},
	}, settings)
	lg, _ := reg.Feed(model.ProviderLG)
	lg.Set(provider.Lineup{Channels: []model.Channel{
		{Provider: model.ProviderLG, NormalizedID: "a", Name: "A", Group: "News", StreamURL: "https://x/a.m3u8"},
	}, FetchedAt: time.Now()})
	pl, _ := reg.Feed(model.ProviderPluto)
	pl.Set(provider.Lineup{Channels: []model.Channel{
		{Provider: model.ProviderPluto, NormalizedID: "b", Name: "B", Group: "News", StreamURL: "https://x/b.m3u8"},
		{Provider: model.ProviderPluto, NormalizedID: "c", Name: "C", Group: "Movies", StreamURL: "https://x/c.m3u8"},
	}, FetchedAt: time.Now()})
	return reg
}

func TestGroupsHandlerGET(t *testing.T) {
	reg := groupsTestRegistry(t)
	holder := groups.NewHolder(groups.Doc{Enabled: true})
	h := server.GroupsHandler(holder, reg, filepath.Join(t.TempDir(), "config.yaml"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/groups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Enabled    bool `json:"enabled"`
		Discovered []struct {
			Name       string `json:"name"`
			Total      int    `json:"total"`
			AutoMerged bool   `json:"auto_merged"`
		} `json:"discovered"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Enabled {
		t.Fatal("enabled should be true")
	}
	var news, movies bool
	for _, d := range body.Discovered {
		if d.Name == "News" {
			news = true
			if d.Total != 2 || !d.AutoMerged {
				t.Fatalf("News bucket = %+v (want total 2, auto-merged)", d)
			}
		}
		if d.Name == "Movies" {
			movies = true
			if d.AutoMerged {
				t.Fatal("single-provider Movies must not be auto-merged")
			}
		}
	}
	if !news || !movies {
		t.Fatalf("discovered missing buckets: %+v", body.Discovered)
	}
}

func TestGroupsHandlerPUTPersistsAndApplies(t *testing.T) {
	reg := groupsTestRegistry(t)
	holder := groups.NewHolder(groups.Doc{})
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("listen: \":8180\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	applied := 0
	h := server.GroupsSaveHandler(holder, reg, path, func() { applied++ })

	payload := `{"enabled":true,"merges":[{"name":"News","members":["News"],"enabled":true}],"disabled":["Movies"]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/groups", strings.NewReader(payload))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	if applied != 1 {
		t.Fatalf("apply called %d times, want 1", applied)
	}
	if doc := holder.Doc(); !doc.Enabled || len(doc.Merges) != 1 || len(doc.Disabled) != 1 {
		t.Fatalf("holder not updated: %+v", doc)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "groups:") || !strings.Contains(string(out), "News") {
		t.Fatalf("config not written with groups: %s", out)
	}
	if !strings.Contains(string(out), "listen:") {
		t.Fatalf("config writer dropped existing keys: %s", out)
	}
}

func TestGroupsHandlerPUTRejectsDuplicateNames(t *testing.T) {
	reg := groupsTestRegistry(t)
	holder := groups.NewHolder(groups.Doc{})
	path := filepath.Join(t.TempDir(), "config.yaml")
	h := server.GroupsSaveHandler(holder, reg, path, func() {})

	payload := `{"enabled":true,"merges":[{"name":"News","members":["A"]},{"name":"news","members":["B"]}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/groups", strings.NewReader(payload)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body=%s (want 400)", rec.Code, rec.Body.String())
	}
}
