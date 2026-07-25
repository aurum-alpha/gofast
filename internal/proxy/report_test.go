package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestReporterDoesNotBlockEmitWhenFull(t *testing.T) {
	t.Parallel()
	r := NewReporter("", NewStore())
	// Fill buffer without a consumer Run loop draining for a moment.
	for i := 0; i < reportBufferSize+50; i++ {
		r.Emit(Event{Kind: EventSegOK, Bytes: 1})
	}
	if r.drop.Load() == 0 {
		t.Fatal("expected drops when buffer full")
	}
}

func TestReporterPostsWithoutBlockingHandlerPath(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		var env ingestEnvelope
		_ = json.NewDecoder(r.Body).Decode(&env)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	store := NewStore()
	r := NewReporter(srv.URL, store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	done := make(chan struct{})
	go func() {
		r.Emit(Event{Kind: EventStreamOpen, Provider: "lg", ChannelID: "news"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Emit blocked")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && hits.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
}
