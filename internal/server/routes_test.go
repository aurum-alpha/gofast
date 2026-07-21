package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/server"
)

// Ensures production route registration does not panic on ServeMux conflicts
// (method-specific wildcards vs unscoped /healthz).
func TestHandlerRouteRegistration(t *testing.T) {
	cc := cache.New(t.TempDir())
	s := &server.Server{
		Routes: func(mux *http.ServeMux) {
			mux.HandleFunc("GET /api/providers", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			mux.HandleFunc("GET /{file}", server.PlaylistFile(cc, nil))
			mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
		},
	}
	h := s.Handler() // must not panic

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz: %d", rec.Code)
	}
}
