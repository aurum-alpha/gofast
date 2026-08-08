package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
)

func TestLogoFileServesAndRejectsTraversal(t *testing.T) {
	cc := cache.New(t.TempDir())
	if err := cc.WriteLogo(model.ProviderLG, "ch1.png", []byte("png")); err != nil {
		t.Fatal(err)
	}

	h := LogoFile(nil, cc)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logos/lg/ch1.png", nil)
	req.SetPathValue("provider", "lg")
	req.SetPathValue("file", "ch1.png")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "png" {
		t.Fatalf("got status=%d body=%q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/logos/lg/../secret", nil)
	req.SetPathValue("provider", "lg")
	req.SetPathValue("file", "../secret")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("traversal: status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/logos/lg/missing.png", nil)
	req.SetPathValue("provider", "lg")
	req.SetPathValue("file", "missing.png")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing: status=%d", rec.Code)
	}
}
