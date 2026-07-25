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
