package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/server"
)

type proxyOriginStubReader struct{}

func (proxyOriginStubReader) Fetch(context.Context) (provider.Raw, error) {
	return provider.Raw{"x": []byte("x")}, nil
}

func (proxyOriginStubReader) Parse(provider.Raw) ([]model.Channel, []model.Programme, error) {
	return nil, nil, nil
}

func TestProxyOriginHandler(t *testing.T) {
	enabled := true
	settings := model.ProviderSettings{ID: model.ProviderLG, Enabled: &enabled, Label: "LG"}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: proxyOriginStubReader{}},
		map[model.ProviderID]model.ProviderSettings{model.ProviderLG: settings},
	)
	feed, _ := reg.Feed(model.ProviderLG)
	feed.Set(provider.Lineup{
		Channels: []model.Channel{{
			Provider:       model.ProviderLG,
			NormalizedID:   "news",
			StreamURL:      "https://cdn.example/beacon/master.m3u8?s=1",
			Classification: model.ClassAmagiSSAI,
			RequestHeaders: map[string]string{"User-Agent": "GoFAST-test/1"},
		}},
	})

	h := server.ProxyOriginHandler(reg)

	t.Run("ok", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/proxy/origin/lg/news", nil)
		req.SetPathValue("provider", "lg")
		req.SetPathValue("normalizedId", "news")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
		}
		var got struct {
			StreamURL      string               `json:"stream_url"`
			Classification model.Classification `json:"classification"`
			RequestHeaders map[string]string    `json:"request_headers"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.StreamURL == "" || got.Classification != model.ClassAmagiSSAI {
			t.Fatalf("got %+v", got)
		}
		if got.RequestHeaders["User-Agent"] != "GoFAST-test/1" {
			t.Fatalf("headers = %#v", got.RequestHeaders)
		}
	})

	t.Run("missing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/proxy/origin/lg/missing", nil)
		req.SetPathValue("provider", "lg")
		req.SetPathValue("normalizedId", "missing")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}
