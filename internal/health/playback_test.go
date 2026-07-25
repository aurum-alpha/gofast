package health

import (
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestHealthCheckFromProxyEvent(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

	t.Run("playlist_ok", func(t *testing.T) {
		c, ok := HealthCheckFromProxyEvent("playlist_ok", "", "", 200, 12, at)
		if !ok || c.Result != model.HealthCheckSuccess || c.Source != SourcePlayback {
			t.Fatalf("got %+v ok=%v", c, ok)
		}
	})
	t.Run("playlist_fail", func(t *testing.T) {
		c, ok := HealthCheckFromProxyEvent("playlist_fail", "upstream_4xx", "status 404", 404, 5, at)
		if !ok || c.Result != model.HealthCheckFailure || c.FailureClass != "upstream_4xx" || c.HTTPStatus != 404 {
			t.Fatalf("got %+v ok=%v", c, ok)
		}
	})
	t.Run("origin_miss", func(t *testing.T) {
		c, ok := HealthCheckFromProxyEvent("origin_miss", "origin_miss", "not found", 0, 1, at)
		if !ok || c.Result != model.HealthCheckFailure {
			t.Fatalf("got %+v ok=%v", c, ok)
		}
	})
	t.Run("seg_fail ignored client_cancel", func(t *testing.T) {
		_, ok := HealthCheckFromProxyEvent("seg_fail", "client_cancel", "canceled", 0, 1, at)
		if ok {
			t.Fatal("expected ignore")
		}
	})
	t.Run("seg_fail counted", func(t *testing.T) {
		c, ok := HealthCheckFromProxyEvent("seg_fail", "upstream_5xx", "502", 502, 9, at)
		if !ok || c.Result != model.HealthCheckFailure {
			t.Fatalf("got %+v ok=%v", c, ok)
		}
	})
	t.Run("stream_open ignored", func(t *testing.T) {
		_, ok := HealthCheckFromProxyEvent("stream_open", "", "", 0, 0, at)
		if ok {
			t.Fatal("expected ignore")
		}
	})
}
