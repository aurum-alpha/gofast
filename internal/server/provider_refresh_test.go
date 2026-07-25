package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/refresh"
	"github.com/j27-aurum/gofast/internal/server"
)

type blockingReader struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingReader) Fetch(context.Context) (provider.Raw, error) {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return provider.Raw{"fixture": []byte("RAW")}, nil
}

func (r *blockingReader) Parse(provider.Raw) ([]model.Channel, []model.Programme, error) {
	start := time.Now().UTC()
	return []model.Channel{{
			ID:        "news",
			Name:      "News",
			StreamURL: "https://example.test/news.m3u8",
		}}, []model.Programme{{
			ChannelID: "news",
			Title:     "News",
			Start:     start,
			Stop:      start.Add(time.Hour),
		}}, nil
}

func TestProviderRefreshAPI(t *testing.T) {
	reader := &blockingReader{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	// A fresh FetchedAt + long interval keeps the schedule loop idle so only the
	// API endpoint drives the reader.
	settings := map[model.ProviderID]model.ProviderSettings{
		"lg": {ID: "lg", Label: "LG", MinChannels: 1, RefreshInterval: time.Hour},
	}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{"lg": reader},
		settings,
	)
	lgFeed, _ := reg.Feed("lg")
	lgFeed.Set(provider.Lineup{FetchedAt: time.Now()})
	cc := cache.New(t.TempDir())
	svc := refresh.New(nil, reg, nil, cc, nil, nil, nil, nil, nil)
	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc.Run(runCtx)

	h := server.ProviderRefreshHandler(svc, runCtx)
	request := func(method, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/providers/"+id+"/refresh", nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := request(http.MethodGet, "lg"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET want 405, got %d", rec.Code)
	}
	if rec := request(http.MethodPost, "unknown"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown want 404, got %d", rec.Code)
	}

	rec := request(http.MethodPost, "lg")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first POST want 202, got %d body %s", rec.Code, rec.Body.String())
	}
	select {
	case <-reader.started:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not start")
	}

	if rec := request(http.MethodPost, "lg"); rec.Code != http.StatusConflict {
		t.Fatalf("concurrent POST want 409, got %d", rec.Code)
	}

	before := lgFeed.FetchedAt()
	close(reader.release)
	feed, ok := reg.Feed("lg")
	if !ok {
		t.Fatal("missing feed")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if feed.FetchedAt().After(before) {
			// Let CommitProvider / status writes finish before TempDir cleanup.
			time.Sleep(50 * time.Millisecond)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("refresh did not complete")
}
