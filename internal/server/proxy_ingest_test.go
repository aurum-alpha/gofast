package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/health"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
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
	server.ProxyEventsHandler(store, nil, nil).ServeHTTP(rec, req)
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
	if st.HeartbeatCount != 1 {
		t.Fatalf("heartbeat_count=%d", st.HeartbeatCount)
	}
}

func TestProxyEventsQueryFilters(t *testing.T) {
	store, err := proxyactivity.Open(filepath.Join(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	_ = store.IngestBatch("t", []proxyactivity.Event{
		{Kind: "playlist_ok", At: time.Now().UTC(), Provider: "lg", ChannelID: "a"},
		{Kind: "playlist_fail", At: time.Now().UTC(), Provider: "lg", ChannelID: "a", Reason: "x"},
		{Kind: "seg_ok", At: time.Now().UTC(), Provider: "pluto", ChannelID: "b"},
	}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/events?failures=1&provider=lg&limit=50", nil)
	server.ProxyEventsQueryHandler(store).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Events []proxyactivity.Event `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Events) != 1 || body.Events[0].Kind != "playlist_fail" {
		t.Fatalf("events=%+v", body.Events)
	}
}

func TestProxyEventsFeedHealthFSM(t *testing.T) {
	act, err := proxyactivity.Open(filepath.Join(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = act.Close() })

	attrs, err := channelattr.Open(filepath.Join(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = attrs.Close() })

	bus := channelattr.NewBus(16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go channelattr.Receive(ctx, bus, attrs)

	em := &health.Emitter{Bus: bus, Store: attrs, ConsecutiveFailures: 3}
	reg := regWith(
		map[model.ProviderID]model.ProviderSettings{
			model.ProviderLG: {ID: model.ProviderLG},
		},
		map[model.ProviderID]provider.Lineup{
			model.ProviderLG: {Channels: []model.Channel{{
				Provider: model.ProviderLG, NormalizedID: "news", Name: "News",
			}}},
		},
	)
	feed, ok := reg.Feed(model.ProviderLG)
	if !ok {
		t.Fatal("no feed")
	}

	post := func(kind, reason string) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"schema_version": 1,
			"proxy_id":       "fsm",
			"events": []map[string]any{{
				"kind": kind, "at": time.Now().UTC(),
				"provider": "lg", "channel_id": "news", "reason": reason, "status": 502,
			}},
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/proxy/events", bytes.NewReader(body))
		server.ProxyEventsHandler(act, em, reg).ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("ingest %s status=%d", kind, rec.Code)
		}
	}

	waitHealth := func(want model.Health) model.ChannelHealth {
		t.Helper()
		deadline := time.Now().Add(asyncWait)
		for time.Now().Before(deadline) {
			raw, ok := attrs.Current(model.ProviderLG, "news", channelattr.KindHealth)
			if !ok {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			var h model.ChannelHealth
			if err := json.Unmarshal(raw, &h); err != nil {
				t.Fatal(err)
			}
			if h.Status == want {
				return h
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", want)
		return model.ChannelHealth{}
	}

	post("playlist_fail", "upstream_5xx")
	waitHealth(model.HealthDegraded)
	post("playlist_fail", "upstream_5xx")
	post("playlist_fail", "upstream_5xx")
	waitHealth(model.HealthDown)

	post("playlist_ok", "")
	h := waitHealth(model.HealthHealthy)
	if h.LastCheck != model.HealthCheckSuccess {
		t.Fatalf("after ok: %+v", h)
	}

	// client_cancel must not move FSM away from healthy.
	post("seg_fail", "client_cancel")
	time.Sleep(50 * time.Millisecond)
	raw, ok := attrs.Current(model.ProviderLG, "news", channelattr.KindHealth)
	if !ok {
		t.Fatal("missing health")
	}
	var afterCancel model.ChannelHealth
	if err := json.Unmarshal(raw, &afterCancel); err != nil {
		t.Fatal(err)
	}
	if afterCancel.Status != model.HealthHealthy {
		t.Fatalf("client_cancel changed health: %+v", afterCancel)
	}

	live := feed.Channels()
	if len(live) != 1 || live[0].Health.Status != model.HealthHealthy {
		t.Fatalf("live feed health=%+v", live)
	}
}
