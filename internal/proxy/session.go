package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

const (
	sessionTTL = 5 * time.Minute
	segTTL     = 2 * time.Minute
)

// Session holds per-tune-in upstream context so Amagi query/session tokens stay coherent.
type Session struct {
	ID             string
	Provider       model.ProviderID
	ChannelID      string
	MasterURL      string
	VariantURLs    []string
	RequestHeaders map[string]string
	ExpiresAt      time.Time
}

// SegToken maps an opaque /seg/{token} name to an absolute upstream URL.
type SegToken struct {
	Token          string
	UpstreamURL    string
	RequestHeaders map[string]string
	Provider       model.ProviderID
	ChannelID      string
	ExpiresAt      time.Time
}

// Store is the in-memory session and segment-token map with sliding TTLs.
type Store struct {
	mu       sync.Mutex
	sessions map[string]*Session
	segs     map[string]*SegToken
}

// NewStore returns an empty session/seg store.
func NewStore() *Store {
	return &Store{
		sessions: make(map[string]*Session),
		segs:     make(map[string]*SegToken),
	}
}

// NewSession mints a session for a channel rewrite.
func (s *Store) NewSession(provider model.ProviderID, channelID, masterURL string, headers map[string]string, variants []string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(time.Now())
	id := randomID(16)
	sess := &Session{
		ID:             id,
		Provider:       provider,
		ChannelID:      channelID,
		MasterURL:      masterURL,
		VariantURLs:    append([]string(nil), variants...),
		RequestHeaders: copyHeaders(headers),
		ExpiresAt:      time.Now().Add(sessionTTL),
	}
	s.sessions[id] = sess
	logEvent(slog.LevelInfo, EventSessionStart,
		"sid", id, "provider", provider, "channel", channelID,
		"variants", len(variants), "upstream", urlHostPath(masterURL))
	return sess
}

// GetSession returns a session and slides its TTL.
func (s *Store) GetSession(id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(time.Now())
	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	sess.ExpiresAt = time.Now().Add(sessionTTL)
	return sess, true
}

// MintSeg creates a segment token with a media-like suffix for ffmpeg allowlists.
func (s *Store) MintSeg(upstreamURL, ext string, headers map[string]string, provider model.ProviderID, channelID string) string {
	if ext == "" {
		ext = ".ts"
	}
	if ext[0] != '.' {
		ext = "." + ext
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(time.Now())
	token := randomID(16) + ext
	s.segs[token] = &SegToken{
		Token:          token,
		UpstreamURL:    upstreamURL,
		RequestHeaders: copyHeaders(headers),
		Provider:       provider,
		ChannelID:      channelID,
		ExpiresAt:      time.Now().Add(segTTL),
	}
	return token
}

// GetSeg returns a segment token and slides its TTL.
func (s *Store) GetSeg(token string) (*SegToken, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(time.Now())
	seg, ok := s.segs[token]
	if !ok {
		return nil, false
	}
	seg.ExpiresAt = time.Now().Add(segTTL)
	return seg, true
}

// Stats returns live counts for snapshots.
func (s *Store) Stats() (sessions, segs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(time.Now())
	return len(s.sessions), len(s.segs)
}

func (s *Store) expireLocked(now time.Time) {
	for id, sess := range s.sessions {
		if now.After(sess.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
	for tok, seg := range s.segs {
		if now.After(seg.ExpiresAt) {
			delete(s.segs, tok)
		}
	}
}

func randomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func copyHeaders(h map[string]string) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = v
	}
	return out
}
