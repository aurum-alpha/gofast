package distrotv

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
)

const feedCacheTTL = 30 * time.Minute

// Resolver refreshes Distro jsrdn feed URLs for tune-in (shared by FASTProxy).
type Resolver struct {
	client   *httpx.Client
	feedBase string
	ua       string

	mu    sync.Mutex
	cache map[string]feedCacheEntry // geo → entry
}

type feedCacheEntry struct {
	at   time.Time
	urls map[string]string // raw episode id → upstream URL
}

// NewResolver builds a tune-in resolver. feedBase empty → DefaultFeedURL.
func NewResolver(client *httpx.Client, feedBase, userAgent string) *Resolver {
	if client == nil {
		client = httpx.NewClient(0, 0)
	}
	if strings.TrimSpace(feedBase) == "" {
		feedBase = DefaultFeedURL
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = AndroidUA
	}
	return &Resolver{
		client:   client,
		feedBase: feedBase,
		ua:       userAgent,
		cache:    map[string]feedCacheEntry{},
	}
}

// Resolve returns a sanitized playable HLS URL for an opaque Distro StreamURL
// or catalog channel id (QQ_123).
func (r *Resolver) Resolve(ctx context.Context, opaqueOrID string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("distrotv: nil resolver")
	}
	id := strings.TrimSpace(opaqueOrID)
	if parsed, ok := ParseOpaque(id); ok {
		id = parsed
	}
	geo, rawID := SplitChannelID(id, DefaultGeo)
	if rawID == "" {
		return "", fmt.Errorf("distrotv: empty channel id")
	}
	upstream, err := r.lookup(ctx, geo, rawID, false)
	if err != nil {
		return "", err
	}
	if upstream == "" {
		upstream, err = r.lookup(ctx, geo, rawID, true)
		if err != nil {
			return "", err
		}
	}
	if upstream == "" {
		return "", fmt.Errorf("distrotv: channel %s not in feed geo=%s", id, geo)
	}
	return SanitizeURL(upstream), nil
}

func (r *Resolver) lookup(ctx context.Context, geo, rawID string, force bool) (string, error) {
	r.mu.Lock()
	ent, ok := r.cache[geo]
	fresh := ok && time.Since(ent.at) < feedCacheTTL
	if !force && fresh {
		u := ent.urls[rawID]
		r.mu.Unlock()
		return u, nil
	}
	r.mu.Unlock()

	urls, err := r.fetchURLMap(ctx, geo)
	if err != nil {
		r.mu.Lock()
		if stale, ok := r.cache[geo]; ok {
			u := stale.urls[rawID]
			r.mu.Unlock()
			if u != "" {
				return u, nil
			}
		} else {
			r.mu.Unlock()
		}
		return "", err
	}
	r.mu.Lock()
	r.cache[geo] = feedCacheEntry{at: time.Now(), urls: urls}
	u := urls[rawID]
	r.mu.Unlock()
	return u, nil
}

func (r *Resolver) fetchURLMap(ctx context.Context, geo string) (map[string]string, error) {
	rawURL := FeedURL(r.feedBase, geo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", r.ua)
	req.Header.Set("Accept", "application/json,*/*")
	resp, err := r.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("distrotv feed HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRawSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxRawSize {
		return nil, fmt.Errorf("distrotv feed too large")
	}
	return URLMapFromFeed(body)
}

// NeedsPlaylistProxy reports whether the host should be fetched and rewritten
// through FASTProxy (Origin/Referer or relative-segment quirks) instead of 302.
func NeedsPlaylistProxy(playURL string) bool {
	host, err := urlParseHost(playURL)
	if err != nil || host == "" {
		return false
	}
	switch host {
	case "d3s7x6kmqcnb6b.cloudfront.net",
		"d35j504z0x2vu2.cloudfront.net",
		"global.cgtn.cicc.media.caton.cloud":
		return true
	}
	// Distro still carries Amagi playout masters; rewrite so beacon segments work.
	if strings.HasSuffix(host, ".amagi.tv") || host == "amagi.tv" {
		return true
	}
	return false
}

func urlParseHost(raw string) (string, error) {
	if i := strings.Index(raw, "://"); i >= 0 {
		rest := raw[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			return strings.ToLower(rest[:j]), nil
		}
		return strings.ToLower(rest), nil
	}
	return "", fmt.Errorf("no host")
}
