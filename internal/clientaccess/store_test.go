package clientaccess_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/clientaccess"
)

func TestRecordSummaryRecentAndPrune(t *testing.T) {
	dir := t.TempDir()
	store, err := clientaccess.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-31 * 24 * time.Hour)
	if err := store.Record("playlist.m3u", "10.0.0.1", "ua-old", 200, old); err != nil {
		t.Fatal(err)
	}
	if err := store.Record("playlist.m3u", "10.0.0.2", "ua-mid", 200, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.Record("playlist.m3u", "10.0.0.3", "Jellyfin-Server/10.9.0", 304, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Record("epg.xml", "10.0.0.9", "curl/8.0", 200, now); err != nil {
		t.Fatal(err)
	}

	// Force prune of the 31-day-old row by resetting throttle via another Record
	// after waiting — use a fresh store so Open boot-prunes.
	_ = store.Close()
	store, err = clientaccess.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sum, err := store.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if len(sum) != 2 {
		t.Fatalf("summary=%+v", sum)
	}
	var playlist *clientaccess.FileSummary
	for i := range sum {
		if sum[i].File == "playlist.m3u" {
			playlist = &sum[i]
		}
	}
	if playlist == nil || playlist.Hits30d != 2 {
		t.Fatalf("playlist summary=%+v (want hits_30d=2 after prune)", playlist)
	}
	if playlist.LastIP != "10.0.0.3" || playlist.LastStatus != 304 {
		t.Fatalf("last=%+v", playlist)
	}

	recent, err := store.Recent(clientaccess.Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 3 {
		t.Fatalf("recent=%+v", recent)
	}
	if recent[0].File != "epg.xml" && recent[0].File != "playlist.m3u" {
		t.Fatalf("recent[0]=%+v", recent[0])
	}
	if recent[0].At.Before(recent[len(recent)-1].At) {
		t.Fatalf("recent not newest-first: %+v", recent)
	}
	var foundUA bool
	for _, e := range recent {
		if e.File == "playlist.m3u" && e.UserAgent == "Jellyfin-Server/10.9.0" {
			foundUA = true
		}
	}
	if !foundUA {
		t.Fatalf("want user_agent on playlist event, got %+v", recent)
	}
	filtered, err := store.Recent(clientaccess.Query{File: "epg.xml", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].File != "epg.xml" || filtered[0].UserAgent != "curl/8.0" {
		t.Fatalf("file filter=%+v", filtered)
	}
}

func TestClientIP(t *testing.T) {
	req := httptestReq("192.168.1.50:1234")
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := clientaccess.ClientIP(req); got != "203.0.113.9" {
		t.Fatalf("xff got %q", got)
	}
	req2 := httptestReq("192.168.1.50:1234")
	req2.Header.Set("X-Real-IP", "198.51.100.2")
	if got := clientaccess.ClientIP(req2); got != "198.51.100.2" {
		t.Fatalf("xri got %q", got)
	}
	req3 := httptestReq("192.168.1.50:1234")
	if got := clientaccess.ClientIP(req3); got != "192.168.1.50" {
		t.Fatalf("remote got %q", got)
	}
}

func httptestReq(remote string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "/playlist.m3u", nil)
	req.RemoteAddr = remote
	return req
}

func TestOpenCreatesDB(t *testing.T) {
	dir := t.TempDir()
	s, err := clientaccess.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(filepath.Join(dir, "client_access.db")); err != nil {
		t.Fatal(err)
	}
}
