package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/server"
	"github.com/j27-aurum/gofast/internal/snapshot"
)

func TestGuideAPI(t *testing.T) {
	store := snapshot.NewStore()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.Put(snapshot.Snapshot{
		ProviderID: "lg",
		Channels: []model.Channel{
			{Provider: "lg", ID: "news", NormalizedID: "news", Name: "News", Number: 5, OffsetNumber: 1005},
		},
		Programmes: []model.Programme{
			{ChannelID: "news", Title: "Evening", Start: start.Add(time.Hour), Stop: start.Add(2 * time.Hour)},
			{ChannelID: "news", Title: "Morning", Start: start, Stop: start.Add(time.Hour)},
		},
	})

	h := server.GuideHandler(store)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/guide", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	var list snapshot.GuideList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Channels) != 1 {
		t.Fatalf("channels: %d", len(list.Channels))
	}
	ch := list.Channels[0]
	if ch.NormalizedID != "news" || ch.OffsetNumber != 1005 || len(ch.Programmes) != 2 {
		t.Fatalf("guide channel: %+v", ch)
	}
	// Programmes are sorted by start.
	if ch.Programmes[0].Title != "Morning" || ch.Programmes[1].Title != "Evening" {
		t.Fatalf("programme order: %+v", ch.Programmes)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/guide", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}
