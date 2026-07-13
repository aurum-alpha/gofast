package ui_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/ui"
)

func TestSPAIndex(t *testing.T) {
	s := httptest.NewServer(ui.Handler())
	defer s.Close()

	resp, err := http.Get(s.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "GoFAST") {
		t.Fatalf("index missing GoFAST brand, body=%q", body[:min(200, len(body))])
	}
	if !strings.Contains(body, "root") {
		t.Fatalf("index missing root mount")
	}
}

func TestSPAFallback(t *testing.T) {
	s := httptest.NewServer(ui.Handler())
	defer s.Close()

	resp, err := http.Get(s.URL + "/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "root") {
		t.Fatal("SPA fallback should serve index.html")
	}
}
