package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/stirr"
)

func TestServeStirrResolve302(t *testing.T) {
	playSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"media": []string{"https://cdn.example/live/master.m3u8?AV_NONCE=[vx_nonce]"}},
			},
		})
	}))
	t.Cleanup(playSrv.Close)

	origin := NewStaticOrigin()
	origin.Set(model.ProviderSTIRR, "5407", ChannelOrigin{
		StreamURL:      stirr.OpaqueStreamURL("5407"),
		Classification: model.ClassStirrResolve,
		RequestHeaders: map[string]string{"User-Agent": stirr.BrowserUA},
	})
	h := NewHandler(origin, NewStore(), nil)
	h.Stirr = stirr.NewResolver(httpx.NewClient(5*time.Second, 1), playSrv.URL+"/%s", stirr.BrowserUA)

	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream/stirr/5407.m3u8", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://cdn.example/live/master.m3u8") {
		t.Fatalf("Location=%s", loc)
	}
	if strings.Contains(loc, "[vx_nonce]") {
		t.Fatalf("nonce not filled: %s", loc)
	}
}

func TestServeStirrResolveDeadSSAI(t *testing.T) {
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

	origin := NewStaticOrigin()
	origin.Set(model.ProviderSTIRR, "7291", ChannelOrigin{
		StreamURL:      stirr.OpaqueStreamURL("7291"),
		Classification: model.ClassStirrResolve,
	})
	h := NewHandler(origin, NewStore(), nil)
	h.Stirr = stirr.NewResolver(httpx.NewClient(5*time.Second, 1), playSrv.URL+"/%s", stirr.BrowserUA)

	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream/stirr/7291.m3u8?browser=1", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
