package logocache

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
)

func TestEnsureConditionalRevalidateOnRefresh(t *testing.T) {
	var hits int
	var sawConditional bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("If-None-Match") == `"v1"` {
			sawConditional = true
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte("png-bytes"))
	}))
	t.Cleanup(srv.Close)

	store := cache.New(t.TempDir())
	c := New(store, srv.Client(), "http://fastgen.lan:8180", time.Hour)
	ch := model.Channel{
		Provider:     "lg",
		ID:           "ch1",
		NormalizedID: "ch1",
		LogoURL:      srv.URL + "/logo.png",
	}

	got, logoErr := c.Ensure(context.Background(), ch)
	want := "http://fastgen.lan:8180/logos/lg/ch1.png"
	if got != want || logoErr != "" {
		t.Fatalf("first Ensure: got %q err %q want %q", got, logoErr, want)
	}
	if hits != 1 {
		t.Fatalf("hits=%d want 1", hits)
	}

	got, logoErr = c.Ensure(context.Background(), ch)
	if got != want || logoErr != "" || hits != 2 || !sawConditional {
		t.Fatalf("revalidate: got=%q err=%q hits=%d conditional=%v", got, logoErr, hits, sawConditional)
	}
}

func TestEnsureNoValidatorsSkipsWithinMaxAge(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-bytes"))
	}))
	t.Cleanup(srv.Close)

	store := cache.New(t.TempDir())
	c := New(store, srv.Client(), "http://base", time.Hour)
	ch := model.Channel{Provider: "lg", NormalizedID: "ch1", LogoURL: srv.URL + "/a.png"}

	if got, err := c.Ensure(context.Background(), ch); got != "http://base/logos/lg/ch1.png" || err != "" {
		t.Fatalf("download: %q %q", got, err)
	}
	if hits != 1 {
		t.Fatalf("hits=%d", hits)
	}
	if got, err := c.Ensure(context.Background(), ch); got != "http://base/logos/lg/ch1.png" || err != "" || hits != 1 {
		t.Fatalf("max-age skip: got=%q err=%q hits=%d", got, err, hits)
	}
}

func TestEnsureURLChangeForcesRedownload(t *testing.T) {
	var hits int
	var lastPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		lastPath = r.URL.Path
		if r.Header.Get("If-None-Match") != "" {
			t.Errorf("URL change must not send conditional headers")
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("ETag", `"v`+r.URL.Path+`"`)
		_, _ = w.Write([]byte("body-" + r.URL.Path))
	}))
	t.Cleanup(srv.Close)

	store := cache.New(t.TempDir())
	c := New(store, srv.Client(), "http://base", time.Hour)
	ch := model.Channel{Provider: "lg", NormalizedID: "ch1", LogoURL: srv.URL + "/old.png"}
	if got, err := c.Ensure(context.Background(), ch); got != "http://base/logos/lg/ch1.png" || err != "" {
		t.Fatalf("first: %q %q", got, err)
	}

	ch.LogoURL = srv.URL + "/new.png"
	if got, err := c.Ensure(context.Background(), ch); got != "http://base/logos/lg/ch1.png" || err != "" {
		t.Fatalf("after url change: %q %q", got, err)
	}
	if hits != 2 || lastPath != "/new.png" {
		t.Fatalf("hits=%d path=%q", hits, lastPath)
	}
	data, err := store.ReadLogo("lg", "ch1.png")
	if err != nil || string(data) != "body-/new.png" {
		t.Fatalf("bytes: %q %v", data, err)
	}
	meta := c.readMeta("lg", "ch1.png")
	if meta.SourceURL != ch.LogoURL {
		t.Fatalf("source_url=%q", meta.SourceURL)
	}
}

func TestEnsureConditional304(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte("png-bytes"))
	}))
	t.Cleanup(srv.Close)

	store := cache.New(t.TempDir())
	c := New(store, srv.Client(), "http://base", time.Nanosecond)
	ch := model.Channel{Provider: "lg", NormalizedID: "ch1", LogoURL: srv.URL + "/a.png"}

	if got, err := c.Ensure(context.Background(), ch); got != "http://base/logos/lg/ch1.png" || err != "" {
		t.Fatalf("download: %q %q", got, err)
	}
	path, err := store.LogoPath("lg", "ch1.png")
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}

	if got, err := c.Ensure(context.Background(), ch); got != "http://base/logos/lg/ch1.png" || err != "" {
		t.Fatalf("304: %q %q", got, err)
	}
	if hits != 2 {
		t.Fatalf("hits=%d want 2", hits)
	}
}

func TestEnsureFetchFailureKeepsUpstream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	c := New(cache.New(t.TempDir()), srv.Client(), "http://base", 0)
	upstream := srv.URL + "/missing.png"
	ch := model.Channel{Provider: "lg", NormalizedID: "x", LogoURL: upstream}
	if got, logoErr := c.Ensure(context.Background(), ch); got != upstream || logoErr != "" {
		t.Fatalf("got %q err %q want upstream kept", got, logoErr)
	}
}

func TestEnsureForbiddenClearsLogo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	c := New(cache.New(t.TempDir()), srv.Client(), "http://base", 0)
	chs := []model.Channel{{
		Provider:     "distrotv",
		NormalizedID: "dtv_EPGElectricNOW",
		LogoURL:      srv.URL + "/logo.png",
	}}
	c.Rewrite(context.Background(), chs)
	if chs[0].LogoURL != "" || chs[0].LogoError != "HTTP 403" {
		t.Fatalf("got logo=%q error=%q", chs[0].LogoURL, chs[0].LogoError)
	}
}

func TestEnsureNotFoundClearsLogo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	got, logoErr := New(cache.New(t.TempDir()), srv.Client(), "http://base", 0).Ensure(
		context.Background(),
		model.Channel{Provider: "lg", NormalizedID: "gone", LogoURL: srv.URL + "/x.png"},
	)
	if got != "" || logoErr != "HTTP 404" {
		t.Fatalf("got %q err %q", got, logoErr)
	}
}

func TestRewrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg"))
	}))
	t.Cleanup(srv.Close)

	c := New(cache.New(t.TempDir()), srv.Client(), "http://base", 0)
	chs := []model.Channel{
		{Provider: "pluto", NormalizedID: "a", LogoURL: srv.URL + "/a.jpg"},
		{Provider: "pluto", NormalizedID: "b", LogoURL: ""},
	}
	c.Rewrite(context.Background(), chs)
	if chs[0].LogoURL != "http://base/logos/pluto/a.jpg" {
		t.Fatalf("chs[0]=%q", chs[0].LogoURL)
	}
	if chs[1].LogoURL != "" {
		t.Fatalf("empty logo should stay empty")
	}
}

func TestArtworkClientInsecureHostOnly(t *testing.T) {
	caPEM, serverCert, serverKey := mustTestCert(t, "logos.example")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{serverCert.Raw},
			PrivateKey:  serverKey,
		}},
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(tlsLn)
	t.Cleanup(func() { srv.Close() })

	addr := ln.Addr().String()
	_, port, _ := net.SplitHostPort(addr)

	plain := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "https://127.0.0.1:"+port+"/", nil)
	if _, err := plain.Do(req); err == nil {
		t.Fatal("expected system client TLS failure")
	}

	client, err := NewArtworkClient(2*time.Second, map[string]HostPolicy{
		"127.0.0.1": {InsecureSkipVerify: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req.Clone(context.Background()))
	if err != nil {
		t.Fatalf("insecure host client: %v", err)
	}
	resp.Body.Close()

	strict, err := NewArtworkClient(2*time.Second, map[string]HostPolicy{
		"logos.example": {CAPem: string(caPEM)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strict.Do(req.Clone(context.Background())); err == nil {
		t.Fatal("expected TLS failure when insecure not set for this host")
	}
}

func mustTestCert(t *testing.T, cn string) (caPEM []byte, cert *x509.Certificate, key *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{cn},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return caPEM, cert, key
}
