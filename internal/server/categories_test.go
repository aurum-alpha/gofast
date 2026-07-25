package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/categories"
	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/server"
)

func categoriesTestReg(t *testing.T) *provider.Registry {
	t.Helper()
	settings := map[model.ProviderID]model.ProviderSettings{
		model.ProviderPluto: {ID: model.ProviderPluto, Enabled: boolPtr(true), Label: "Pluto"},
		model.ProviderLG:    {ID: model.ProviderLG, Enabled: boolPtr(true), Label: "LG"},
	}
	return regWith(settings, map[model.ProviderID]provider.Lineup{
		model.ProviderPluto: {
			Channels: []model.Channel{{Provider: model.ProviderPluto, NormalizedID: "a", Name: "A", StreamURL: "https://x"}},
			Programmes: []model.Programme{
				{ChannelID: "a", Title: "One", Categories: []string{"Movies", "Comedy"}, Start: time.Now(), Stop: time.Now().Add(time.Hour)},
				{ChannelID: "a", Title: "Two", Categories: []string{"Film"}, Start: time.Now(), Stop: time.Now().Add(time.Hour)},
			},
			FetchedAt: time.Now(),
		},
		model.ProviderLG: {
			Channels: []model.Channel{{Provider: model.ProviderLG, NormalizedID: "b", Name: "B", StreamURL: "https://x"}},
			Programmes: []model.Programme{
				{ChannelID: "b", Title: "Three", Categories: []string{"Movies"}, Start: time.Now(), Stop: time.Now().Add(time.Hour)},
			},
			FetchedAt: time.Now(),
		},
	})
}

func liveCategoriesPolicy(store *config.Store) func() *categories.Policy {
	return func() *categories.Policy { return categories.Compile(store.Current().Categories) }
}

func TestCategoriesHandlerGET(t *testing.T) {
	reg := categoriesTestReg(t)
	store := groupsTestStore(t, "categories:\n  enabled: true\n")
	h := server.CategoriesHandler(store, liveCategoriesPolicy(store), reg)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/categories", nil))
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
		t.Fatal("enabled")
	}
	var movies bool
	for _, d := range body.Discovered {
		if d.Name == "Movies" {
			movies = true
			if d.Total != 2 || !d.AutoMerged {
				t.Fatalf("Movies=%+v", d)
			}
		}
	}
	if !movies {
		t.Fatalf("discovered=%+v", body.Discovered)
	}
}

func TestCategoriesHandlerPUTPersists(t *testing.T) {
	reg := categoriesTestReg(t)
	store := groupsTestStore(t, "listen: \":8180\"\n")
	h := server.CategoriesSaveHandler(store, liveCategoriesPolicy(store), reg)
	payload := `{"enabled":true,"merges":[{"name":"Movie","members":["Movie","Movies","Film"]}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/categories", strings.NewReader(payload)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	doc := store.Current().Categories
	if !doc.Enabled || len(doc.Merges) != 1 || doc.Merges[0].Name != "Movie" {
		t.Fatalf("doc=%+v", doc)
	}
}
