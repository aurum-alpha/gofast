package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/clientaccess"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/server"
)

func TestPlaylistAccessRecords200And304(t *testing.T) {
	cc := cache.New(t.TempDir())
	if err := cc.CommitAggregate(model.M3UFile("#EXTM3U\n"), model.XMLTVFile("<tv/>")); err != nil {
		t.Fatal(err)
	}
	access, err := clientaccess.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	req := httptest.NewRequest(http.MethodGet, "/playlist.m3u", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	server.AggregatePlaylist(cc, access).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	etag := rec.Header().Get("ETag")
	req2 := httptest.NewRequest(http.MethodGet, "/playlist.m3u", nil)
	req2.Header.Set("If-None-Match", etag)
	req2.Header.Set("X-Forwarded-For", "203.0.113.11")
	rec2 := httptest.NewRecorder()
	server.AggregatePlaylist(cc, access).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("want 304, got %d", rec2.Code)
	}

	empty := cache.New(t.TempDir())
	rec3 := httptest.NewRecorder()
	server.AggregateGuide(empty, access).ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/epg.xml", nil))
	if rec3.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec3.Code)
	}

	sum, err := access.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if len(sum) != 1 || sum[0].File != "playlist.m3u" || sum[0].Hits30d != 2 {
		t.Fatalf("summary=%+v", sum)
	}
	if sum[0].LastIP != "203.0.113.11" || sum[0].LastStatus != http.StatusNotModified {
		t.Fatalf("last=%+v", sum[0])
	}
}

func TestClientAccessAPI(t *testing.T) {
	access, err := clientaccess.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	if err := access.Record("lg.m3u", "10.1.1.1", "TestAgent/1.0", 200, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	h := server.ClientAccessHandler(access)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/client-access", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body struct {
		Summary []struct {
			File    string `json:"file"`
			Hits30d int    `json:"hits_30d"`
		} `json:"summary"`
		Recent []struct {
			File string `json:"file"`
			IP   string `json:"ip"`
		} `json:"recent"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Summary) != 1 || body.Summary[0].File != "lg.m3u" {
		t.Fatalf("body=%+v", body)
	}
	if len(body.Recent) != 1 || body.Recent[0].IP != "10.1.1.1" {
		t.Fatalf("recent=%+v", body.Recent)
	}
}
