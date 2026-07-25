// Session mint (Google DAI) — FASTProxy path for Classification SESSION.
//
// Narrative (J27-65):
//
// Catalog DistroTV / DAI URLs look like ordinary HLS masters:
//
//	https://dai.google.com/linear/hls/event/{EVENT_ID}/master.m3u8
//
// Those published masters are often already HTTP 404 by the time a community
// M3U is scraped. Playability requires mint-on-tune-in: POST Google’s DAI Full
// Service stream-create endpoint, read JSON, and hand the player a live
// stream_manifest URL.
//
// v1 strategy (deliberately not Amagi rewrite):
//
//  1. Parse EVENT_ID from the catalog origin URL.
//  2. POST https://dai.google.com/linear/v1/hls/event/{EVENT_ID}/stream
//     (application/x-www-form-urlencoded; never HEAD).
//  3. Prefer JSON field stream_manifest; fall back to hls_master_playlist.
//  4. HTTP 302 Location = that URL so Jellyfin talks to DAI for variants/segments.
//  5. Cache the minted URL briefly per (provider, id) so playlist re-polls do
//     not stampede DAI.
//
// v1 does NOT: rewrite playlists through /seg, poll media-verification / ID3,
// or send DAI HMAC/API keys (defer auth until we see failures in the wild).
//
// Glossary: docs/TERMINOLOGY.md (SESSION, mint-on-tune-in, Google DAI, stream_manifest).
package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

const (
	mintHTTPTimeout = 20 * time.Second
	mintCacheTTL    = 3 * time.Minute
)

// daiMintResponse is the subset of Google DAI stream-create JSON we need.
type daiMintResponse struct {
	StreamManifest    string `json:"stream_manifest"`
	HLSMasterPlaylist string `json:"hls_master_playlist"`
	StreamID          string `json:"stream_id"`
}

func mintCacheKey(provider model.ProviderID, channelID string) string {
	return string(provider) + "/" + channelID
}

// daiEventID extracts the linear HLS event id from a catalog master URL.
// Accepts …/linear/hls/event/{id}/… on dai.google.com (and subdomains).
func daiEventID(catalogURL string) (string, error) {
	u, err := url.Parse(catalogURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid catalog URL")
	}
	host := strings.ToLower(u.Hostname())
	if host != "dai.google.com" && !strings.HasSuffix(host, ".dai.google.com") {
		return "", fmt.Errorf("not a dai.google.com host")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// expect … linear hls event EVENT_ID …
	for i := 0; i+3 < len(parts); i++ {
		if strings.EqualFold(parts[i], "linear") &&
			strings.EqualFold(parts[i+1], "hls") &&
			strings.EqualFold(parts[i+2], "event") &&
			parts[i+3] != "" {
			return parts[i+3], nil
		}
	}
	return "", fmt.Errorf("no /linear/hls/event/{id} in path")
}

// mintClient performs DAI stream-create POSTs (GET-only elsewhere still holds for probes).
type mintClient struct {
	http *http.Client
	// base overrides https://dai.google.com (tests only).
	base string
}

func newMintClient() *mintClient {
	return &mintClient{
		http: &http.Client{
			Timeout: mintHTTPTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if req.Method == http.MethodHead {
					return fmt.Errorf("HEAD redirect refused")
				}
				return nil
			},
		},
	}
}

func (c *mintClient) mintEndpoint(eventID string) string {
	base := strings.TrimRight(c.base, "/")
	if base == "" {
		base = "https://dai.google.com"
	}
	return base + "/linear/v1/hls/event/" + url.PathEscape(eventID) + "/stream"
}

func (c *mintClient) mint(ctx context.Context, eventID string, headers map[string]string) (manifestURL string, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.mintEndpoint(eventID), strings.NewReader(""))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		if k != "" && v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.StatusCode, fmt.Errorf("dai mint HTTP %d", resp.StatusCode)
	}
	var parsed daiMintResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", resp.StatusCode, fmt.Errorf("dai mint JSON: %w", err)
	}
	manifest := strings.TrimSpace(parsed.StreamManifest)
	if manifest == "" {
		manifest = strings.TrimSpace(parsed.HLSMasterPlaylist)
	}
	if manifest == "" {
		return "", resp.StatusCode, fmt.Errorf("dai mint JSON missing stream_manifest")
	}
	return manifest, resp.StatusCode, nil
}
