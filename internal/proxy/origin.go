package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

const originCacheTTL = 30 * time.Second

// ChannelOrigin is the upstream identity FASTProxy needs for one emitted stream URL.
type ChannelOrigin struct {
	StreamURL      string
	Classification model.Classification
	RequestHeaders map[string]string
}

// Origin resolves (provider, normalizedId) to an upstream channel. Production
// uses GenClient against FASTGen; tests use StaticOrigin.
type Origin interface {
	Lookup(ctx context.Context, provider model.ProviderID, normalizedID string) (ChannelOrigin, error)
}

// StaticOrigin is an in-memory Origin for tests.
type StaticOrigin struct {
	mu   sync.RWMutex
	byID map[string]ChannelOrigin
}

// NewStaticOrigin returns an empty StaticOrigin.
func NewStaticOrigin() *StaticOrigin {
	return &StaticOrigin{byID: make(map[string]ChannelOrigin)}
}

// Set registers an origin for provider/id.
func (s *StaticOrigin) Set(provider model.ProviderID, normalizedID string, o ChannelOrigin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[string(provider)+"/"+normalizedID] = o
}

// Lookup implements Origin.
func (s *StaticOrigin) Lookup(_ context.Context, provider model.ProviderID, normalizedID string) (ChannelOrigin, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.byID[string(provider)+"/"+normalizedID]
	if !ok {
		return ChannelOrigin{}, fmt.Errorf("origin: not found")
	}
	return o, nil
}

// GenClient pulls origin from FASTGen's /api/proxy/origin endpoint.
type GenClient struct {
	BaseURL    string
	HTTPClient *http.Client

	mu    sync.Mutex
	cache map[string]originCacheEntry
}

type originCacheEntry struct {
	origin    ChannelOrigin
	err       error
	expiresAt time.Time
}

// NewGenClient builds a GenClient for genBase (no trailing slash).
func NewGenClient(genBase string, hc *http.Client) *GenClient {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &GenClient{
		BaseURL:    strings.TrimRight(strings.TrimSpace(genBase), "/"),
		HTTPClient: hc,
		cache:      make(map[string]originCacheEntry),
	}
}

type genOriginJSON struct {
	StreamURL      string               `json:"stream_url"`
	Classification model.Classification `json:"classification"`
	RequestHeaders map[string]string    `json:"request_headers"`
}

// Lookup implements Origin with a short positive/negative cache.
func (c *GenClient) Lookup(ctx context.Context, provider model.ProviderID, normalizedID string) (ChannelOrigin, error) {
	key := string(provider) + "/" + normalizedID
	now := time.Now()
	c.mu.Lock()
	if e, ok := c.cache[key]; ok && now.Before(e.expiresAt) {
		c.mu.Unlock()
		logEvent(slog.LevelInfo, EventOriginLookup,
			"provider", provider, "channel", normalizedID, "cache", true,
			"classification", e.origin.Classification, "err", errString(e.err))
		return e.origin, e.err
	}
	c.mu.Unlock()

	start := time.Now()
	url := fmt.Sprintf("%s/api/proxy/origin/%s/%s", c.BaseURL, provider, normalizedID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ChannelOrigin{}, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.storeCache(key, ChannelOrigin{}, err, now)
		logEvent(slog.LevelInfo, EventOriginMiss,
			"provider", provider, "channel", normalizedID, "cache", false,
			"duration_ms", time.Since(start).Milliseconds(), "err", err.Error())
		return ChannelOrigin{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		err = fmt.Errorf("origin: not found")
		c.storeCache(key, ChannelOrigin{}, err, now)
		logEvent(slog.LevelInfo, EventOriginMiss,
			"provider", provider, "channel", normalizedID, "cache", false,
			"duration_ms", time.Since(start).Milliseconds(), "status", resp.StatusCode)
		return ChannelOrigin{}, err
	}
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("origin: gen status %d", resp.StatusCode)
		c.storeCache(key, ChannelOrigin{}, err, now)
		logEvent(slog.LevelInfo, EventOriginMiss,
			"provider", provider, "channel", normalizedID, "cache", false,
			"duration_ms", time.Since(start).Milliseconds(), "status", resp.StatusCode)
		return ChannelOrigin{}, err
	}
	var body genOriginJSON
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		c.storeCache(key, ChannelOrigin{}, err, now)
		return ChannelOrigin{}, err
	}
	o := ChannelOrigin{
		StreamURL:      body.StreamURL,
		Classification: body.Classification.Canonical(),
		RequestHeaders: body.RequestHeaders,
	}
	c.storeCache(key, o, nil, now)
	logEvent(slog.LevelInfo, EventOriginLookup,
		"provider", provider, "channel", normalizedID, "cache", false,
		"classification", o.Classification, "upstream", urlHostPath(o.StreamURL),
		"duration_ms", time.Since(start).Milliseconds())
	return o, nil
}

func (c *GenClient) storeCache(key string, o ChannelOrigin, err error, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = originCacheEntry{origin: o, err: err, expiresAt: now.Add(originCacheTTL)}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
