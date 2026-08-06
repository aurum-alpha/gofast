package stirr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
)

func TestExtractMediaURL(t *testing.T) {
	body := []byte(`{"data":[{"media":["https://ssai.aniview.com/stream.m3u8?AV_NONCE=[vx_nonce]"]}]}`)
	got, err := ExtractMediaURL(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "aniview.com") {
		t.Fatalf("got %q", got)
	}
}

func TestFillMacros(t *testing.T) {
	in := "https://x.example/m.m3u8?AV_NONCE=[vx_nonce]&other=[foo]"
	out := FillMacros(in)
	if strings.Contains(out, "[vx_nonce]") || strings.Contains(out, "[foo]") {
		t.Fatalf("macros remain: %q", out)
	}
	if !strings.Contains(out, "AV_NONCE=") {
		t.Fatalf("missing nonce param: %q", out)
	}
}

func TestResolveRejectsAniviewCON(t *testing.T) {
	aniview := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"CON","description":"can not get a content"}`))
	}))
	t.Cleanup(aniview.Close)

	playSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"media": []string{aniview.URL + "/stream.m3u8?AV_NONCE=[vx_nonce]"}},
			},
		})
	}))
	t.Cleanup(playSrv.Close)

	r := NewResolver(httpx.NewClient(5*time.Second, 1), playSrv.URL+"/%s", BrowserUA)
	_, err := r.Resolve(context.Background(), "7291")
	if !errors.Is(err, ErrDeadSSAI) {
		t.Fatalf("err=%v want ErrDeadSSAI", err)
	}
}

func TestResolveOKSkipsProbeForNonAniview(t *testing.T) {
	playSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"media": []string{"https://cdn.example/live/master.m3u8"}},
			},
		})
	}))
	t.Cleanup(playSrv.Close)

	r := NewResolver(httpx.NewClient(5*time.Second, 1), playSrv.URL+"/%s", BrowserUA)
	got, err := r.Resolve(context.Background(), OpaqueStreamURL("5407"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://cdn.example/live/master.m3u8" {
		t.Fatalf("got %q", got)
	}
}
