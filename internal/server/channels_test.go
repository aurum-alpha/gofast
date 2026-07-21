package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/server"
)

func TestChannelsAPI(t *testing.T) {
	reg := regWith(
		map[model.ProviderID]model.ProviderSettings{"lg": {ID: "lg", Label: "LG"}},
		map[model.ProviderID]provider.Lineup{
			"lg": {Channels: []model.Channel{
				{Provider: "lg", ID: "a", NormalizedID: "a", Name: "Alpha", Number: 1, OffsetNumber: 1001, StreamURL: "https://a"},
				{Provider: "lg", ID: "b", NormalizedID: "b", Name: "Bad", Excluded: true, FilterReason: "exclusion", StreamURL: "https://b"},
			}},
		},
	)

	h := server.ChannelsHandler(reg)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/channels", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var list struct {
		Channels []model.Channel `json:"channels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Channels) != 2 || list.Channels[0].Name != "Alpha" {
		t.Fatalf("%+v", list.Channels)
	}
}
