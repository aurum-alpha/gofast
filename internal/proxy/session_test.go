package proxy

import (
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestStoreTTL(t *testing.T) {
	t.Parallel()
	s := NewStore()
	sess := s.NewSession(model.ProviderLG, "news", "https://x/m.m3u8", nil, []string{"https://x/v.m3u8"})
	tok := s.MintSeg("https://x/beacon", ".ts", nil, model.ProviderLG, "news")

	if _, ok := s.GetSession(sess.ID); !ok {
		t.Fatal("session missing")
	}
	if _, ok := s.GetSeg(tok); !ok {
		t.Fatal("seg missing")
	}

	s.mu.Lock()
	s.sessions[sess.ID].ExpiresAt = time.Now().Add(-time.Second)
	s.segs[tok].ExpiresAt = time.Now().Add(-time.Second)
	s.expireLocked(time.Now())
	s.mu.Unlock()

	if _, ok := s.GetSession(sess.ID); ok {
		t.Fatal("session should expire")
	}
	if _, ok := s.GetSeg(tok); ok {
		t.Fatal("seg should expire")
	}
}

func TestMintSegReusesUpstreamURL(t *testing.T) {
	t.Parallel()
	s := NewStore()
	a := s.MintSeg("https://cdn.example/seg1.ts", ".ts", nil, model.ProviderPluto, "ch")
	b := s.MintSeg("https://cdn.example/seg1.ts", ".ts", map[string]string{"X": "1"}, model.ProviderPluto, "ch")
	if a != b {
		t.Fatalf("expected stable token, got %q vs %q", a, b)
	}
	c := s.MintSeg("https://cdn.example/seg2.ts", ".ts", nil, model.ProviderPluto, "ch")
	if c == a {
		t.Fatal("different upstream URLs must not share a token")
	}
}
