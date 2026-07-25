package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/proxyactivity"
	"github.com/j27-aurum/gofast/internal/server"
)

func TestProxyEventsAndStatus(t *testing.T) {
	store, err := proxyactivity.Open(filepath.Join(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	body, _ := json.Marshal(map[string]any{
		"schema_version": 1,
		"proxy_id":       "t1",
		"sent_at":        time.Now().UTC(),
		"events": []map[string]any{{
			"kind": "playlist_fail", "at": time.Now().UTC(),
			"provider": "lg", "channel_id": "news", "reason": "upstream_4xx", "status": 404,
		}},
		"snapshot": map[string]any{
			"at": time.Now().UTC(), "active_sessions": 2, "stream_opens": 5,
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/proxy/events", bytes.NewReader(body))
	server.ProxyEventsHandler(store).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ingest status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/proxy/status", nil)
	server.ProxyStatusHandler(store).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status=%d", rec2.Code)
	}
	var st proxyactivity.Status
	if err := json.Unmarshal(rec2.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Snapshot == nil || st.Snapshot.ActiveSessions != 2 || len(st.RecentFail) < 1 {
		t.Fatalf("status view=%+v", st)
	}
}
