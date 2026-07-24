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
				{Provider: "lg", ID: "a", NormalizedID: "a", Name: "Alpha", Number: 1, OffsetNumber: 1001, StreamURL: "https://upstream/a", EmittedURL: "https://proxy/stream/lg/a.m3u8", Classification: model.ClassAmagiSSAI},
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
	if list.Channels[0].StreamURL != "https://upstream/a" || list.Channels[0].EmittedURL != "https://proxy/stream/lg/a.m3u8" {
		t.Fatalf("upstream/emitted URLs lost: %+v", list.Channels[0])
	}
}

func TestChannelAPI(t *testing.T) {
	reg := regWith(
		map[model.ProviderID]model.ProviderSettings{"lg": {ID: "lg", Label: "LG"}},
		map[model.ProviderID]provider.Lineup{
			"lg": {Channels: []model.Channel{
				{Provider: "lg", ID: "Best of British TV", NormalizedID: "Best_of_British_TV", Name: "British", StreamURL: "https://up/b", Classification: model.ClassNative},
			}},
		},
	)
	h := server.ChannelHandler(reg)
	request := func(method, provider, normalizedID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/channels/"+provider+"/"+normalizedID, nil)
		req.SetPathValue("provider", provider)
		req.SetPathValue("normalizedId", normalizedID)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := request(http.MethodGet, "lg", "Best_of_British_TV")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var ch model.Channel
	if err := json.Unmarshal(rec.Body.Bytes(), &ch); err != nil {
		t.Fatal(err)
	}
	if ch.Name != "British" || ch.ID != "Best of British TV" || ch.NormalizedID != "Best_of_British_TV" {
		t.Fatalf("%+v", ch)
	}
	if rec := request(http.MethodGet, "lg", "missing"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing want 404, got %d", rec.Code)
	}
	if rec := request(http.MethodGet, "unknown", "Best_of_British_TV"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown provider want 404, got %d", rec.Code)
	}
	if rec := request(http.MethodPost, "lg", "Best_of_British_TV"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST want 405, got %d", rec.Code)
	}
}
