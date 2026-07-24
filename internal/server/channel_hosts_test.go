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

func TestChannelHostsAPI(t *testing.T) {
	type node struct {
		Label    string `json:"label"`
		Count    int    `json:"count"`
		Children []node `json:"children"`
	}
	reg := regWith(
		map[model.ProviderID]model.ProviderSettings{
			"lg":    {ID: "lg"},
			"pluto": {ID: "pluto"},
		},
		map[model.ProviderID]provider.Lineup{
			"lg": {Channels: []model.Channel{
				{Provider: "lg", NormalizedID: "a", StreamURL: "https://dai.google.com/linear/a.m3u8"},
				{Provider: "lg", NormalizedID: "b", StreamURL: "https://DAI.google.com/linear/b.m3u8"},
				{Provider: "lg", NormalizedID: "c", StreamURL: "https://cdn.example.com:8443/x.m3u8"},
				{Provider: "lg", NormalizedID: "bad", StreamURL: "not-a-url"},
				{Provider: "lg", NormalizedID: "empty", StreamURL: ""},
			}},
			"pluto": {Channels: []model.Channel{
				{Provider: "pluto", NormalizedID: "p", StreamURL: "https://service-stitcher.clusters.pluto.tv/v1/stitch.m3u8"},
				{Provider: "pluto", NormalizedID: "ip", StreamURL: "http://192.168.1.10/stream.m3u8"},
			}},
		},
	)

	h := server.ChannelHostsHandler(reg)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/channels/hosts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	var body struct {
		URLField string `json:"url_field"`
		Unparsed int    `json:"unparsed"`
		Tree     []node `json:"tree"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.URLField != "stream_url" {
		t.Fatalf("url_field=%q", body.URLField)
	}
	if body.Unparsed != 2 {
		t.Fatalf("unparsed=%d want 2", body.Unparsed)
	}

	byLabel := map[string]node{}
	for _, n := range body.Tree {
		byLabel[n.Label] = n
	}

	// Root is TLD-first: com (3) first by count.
	if body.Tree[0].Label != "com" || body.Tree[0].Count != 3 {
		t.Fatalf("first root want com/3, got %+v", body.Tree[0])
	}
	comKids := map[string]node{}
	for _, c := range byLabel["com"].Children {
		comKids[c.Label] = c
	}
	google := comKids["google"]
	if google.Count != 2 || len(google.Children) != 1 || google.Children[0].Label != "dai" || google.Children[0].Count != 2 {
		t.Fatalf("com→google→dai: %+v", google)
	}
	example := comKids["example"]
	if example.Count != 1 || len(example.Children) != 1 || example.Children[0].Label != "cdn" {
		t.Fatalf("com→example→cdn: %+v", example)
	}

	tv := byLabel["tv"]
	if tv.Count != 1 || len(tv.Children) == 0 || tv.Children[0].Label != "pluto" {
		t.Fatalf("tv→pluto…: %+v", tv)
	}

	ip := byLabel["192.168.1.10"]
	if ip.Count != 1 || len(ip.Children) != 0 {
		t.Fatalf("IP leaf: %+v", ip)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/channels/hosts", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST want 405, got %d", rec.Code)
	}
}
