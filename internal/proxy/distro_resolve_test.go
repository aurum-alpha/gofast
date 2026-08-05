package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/distrotv"
)

func TestServeDistroResolve302(t *testing.T) {
	feed := `{"shows":{"1":{"type":"live","title":"Daystar","seasons":[{"episodes":[{"id":"48","content":{"url":"https://live-mcl.cdn01.net/smarttv/x/playlist.m3u8"}}]}]}}}`
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	t.Cleanup(feedSrv.Close)

	origin := NewStaticOrigin()
	origin.Set(model.ProviderDistroTV, "QQ_48", ChannelOrigin{
		StreamURL:      distrotv.OpaqueStreamURL("QQ_48"),
		Classification: model.ClassDistroResolve,
		RequestHeaders: map[string]string{"User-Agent": distrotv.BrowserUA},
	})
	h := NewHandler(origin, NewStore(), nil)
	h.Distro = distrotv.NewResolver(httpx.NewClient(5*time.Second, 1), feedSrv.URL+"?type=live", distrotv.AndroidUA)

	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream/distrotv/QQ_48.m3u8", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://live-mcl.cdn01.net/") {
		t.Fatalf("Location=%s", loc)
	}
}
