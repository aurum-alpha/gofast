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
	if st.HeartbeatCount != 1 {
		t.Fatalf("heartbeat_count=%d", st.HeartbeatCount)
	}
	if len(st.Recent) < 2 || len(st.RecentFail) < 1 {
		t.Fatalf("recent=%d fails=%d", len(st.Recent), len(st.RecentFail))
	}

	err = s.IngestBatch("proxy-1", nil, &Snapshot{At: time.Now().UTC(), ActiveSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.StatusView()
	if err != nil {
		t.Fatal(err)
	}
	if st.HeartbeatCount != 2 {
		t.Fatalf("heartbeat_count after second snap=%d", st.HeartbeatCount)
	}
}

func TestQueryEventsFilters(t *testing.T) {
	t.Parallel()
	s, err := Open(filepath.Join(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	now := time.Now().UTC()
	err = s.IngestBatch("p", []Event{
		{Kind: "playlist_ok", At: now, Provider: "lg", ChannelID: "a"},
		{Kind: "playlist_fail", At: now.Add(time.Second), Provider: "lg", ChannelID: "a"},
		{Kind: "seg_fail", At: now.Add(2 * time.Second), Provider: "pluto", ChannelID: "b"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	fails, err := s.QueryEvents(Query{FailuresOnly: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(fails) != 2 {
		t.Fatalf("failures=%d", len(fails))
	}

	lg, err := s.QueryEvents(Query{Provider: "lg", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(lg) != 2 {
		t.Fatalf("lg=%d", len(lg))
	}

	ok, err := s.QueryEvents(Query{Kind: "playlist_ok", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(ok) != 1 || ok[0].Kind != "playlist_ok" {
		t.Fatalf("kind filter=%+v", ok)
	}
}
