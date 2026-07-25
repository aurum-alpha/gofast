package proxyactivity

import (
	"path/filepath"
	"testing"
	"time"
)

func TestIngestAndStatus(t *testing.T) {
	t.Parallel()
	s, err := Open(filepath.Join(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	err = s.IngestBatch("proxy-1", []Event{{
		Kind: "stream_open", At: time.Now().UTC(), Provider: "lg", ChannelID: "news",
	}, {
		Kind: "playlist_fail", At: time.Now().UTC(), Provider: "lg", ChannelID: "news",
		Reason: "upstream_4xx", Message: "status 404", Status: 404,
	}}, &Snapshot{
		At:             time.Now().UTC(),
		ActiveSessions: 1,
		StreamOpens:    3,
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.StatusView()
	if err != nil {
		t.Fatal(err)
	}
	if st.Snapshot == nil || st.Snapshot.ActiveSessions != 1 {
		t.Fatalf("snapshot=%+v", st.Snapshot)
	}
	if st.Snapshot.ProxyID != "proxy-1" {
		t.Fatalf("proxy_id=%s", st.Snapshot.ProxyID)
	}
	if len(st.Recent) < 2 || len(st.RecentFail) < 1 {
		t.Fatalf("recent=%d fails=%d", len(st.Recent), len(st.RecentFail))
	}
}
