package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestStreamCORSHeadersAndPreflight(t *testing.T) {
	origin := NewStaticOrigin()
	origin.Set(model.ProviderLG, "sports", ChannelOrigin{
		StreamURL:      "https://cdn.example/native.m3u8",
		Classification: model.ClassNative,
	})
	h := NewHandler(origin, NewStore(), nil)
	mux := http.NewServeMux()
	h.Register(mux)

	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/stream/lg/sports.m3u8", nil))
	if getRec.Code != http.StatusFound {
		t.Fatalf("GET status=%d", getRec.Code)
	}
	if got := getRec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("GET ACAO=%q", got)
	}

	optRec := httptest.NewRecorder()
	mux.ServeHTTP(optRec, httptest.NewRequest(http.MethodOptions, "/stream/lg/sports.m3u8", nil))
	if optRec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status=%d", optRec.Code)
	}
	if got := optRec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("OPTIONS ACAO=%q", got)
	}
	if got := optRec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("OPTIONS missing Allow-Methods")
	}
}
