package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/j27-aurum/gofast/internal/refresh"
	"github.com/j27-aurum/gofast/internal/server"
)

func TestStatusAPI(t *testing.T) {
	st := &refresh.Status{}
	st.SetLogos(true, 3, 10, "lg")
	h := server.StatusHandler(st)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var view refresh.View
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Ready || !view.Logos.Running || view.Logos.Done != 3 || view.Logos.Total != 10 || view.Logos.Provider != "lg" {
		t.Fatalf("%+v", view)
	}
	st.SetLogos(false, 10, 10, "")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Ready || view.Logos.Running {
		t.Fatalf("%+v", view)
	}
}
