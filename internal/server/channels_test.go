package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/server"
	"github.com/j27-aurum/gofast/internal/snapshot"
)

func TestChannelsAPI(t *testing.T) {
	store := snapshot.NewStore()
	store.Put(snapshot.Snapshot{
		ProviderID: "lg",
		Channels: []model.Channel{
			{Provider: "lg", ID: "a", NormalizedID: "a", Name: "Alpha", Number: 1, OffsetNumber: 1001, StreamURL: "https://a"},
			{Provider: "lg", ID: "b", NormalizedID: "b", Name: "Bad", Excluded: true, FilterReason: "exclusion", StreamURL: "https://b"},
		},
	})

	h := server.ChannelsHandler(store)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/channels", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var list snapshot.ChannelList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Channels) != 2 || list.Channels[0].Name != "Alpha" {
		t.Fatalf("%+v", list.Channels)
	}
}
