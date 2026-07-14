// Package classifier probes HLS playlists and buckets channels as NATIVE, BEACON, or DRM.
package classifier

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
)

const (
	DefaultWorkers   = 8
	playlistRangeEnd = 256*1024 - 1 // first 256 KiB via ranged GET
	segmentSampleN   = 5
)

// Client classifies stream URLs using ranged GET (never HEAD).
type Client struct {
	http    *httpx.Client
	workers int
}

// New returns a classifier. Nil http falls back to httpx defaults; workers <= 0 → DefaultWorkers.
func New(httpClient *httpx.Client, workers int) *Client {
	if httpClient == nil {
		httpClient = httpx.NewClient(0, 0)
	}
	if workers <= 0 {
		workers = DefaultWorkers
	}
	return &Client{http: httpClient, workers: workers}
}

// Classify probes streamURL: master → first variant → first ~5 segments.
// Fetch errors classify as NATIVE (never drop on transient failure).
// Channels already marked DRM are left as DRM without probing.
func (c *Client) Classify(ctx context.Context, streamURL string) model.Classification {
	if c == nil {
		return model.ClassNative
	}
	class, err := c.probe(ctx, streamURL)
	if err != nil {
		return model.ClassNative
	}
	return class
}

// ClassifyChannels sets Classification on each channel using a bounded worker pool.
// DRM (already set) is left alone. BEACON channels are not excluded here — callers
// decide export policy (e.g. drop when proxy_base_url is unset).
func (c *Client) ClassifyChannels(ctx context.Context, channels []model.Channel) []model.Channel {
	if c == nil || len(channels) == 0 {
		return channels
	}
	out := make([]model.Channel, len(channels))
	copy(out, channels)

	sem := make(chan struct{}, c.workers)
	var wg sync.WaitGroup
	for i := range out {
		if out[i].Classification == model.ClassDRM {
			continue
		}
		if out[i].StreamURL == "" {
			out[i].Classification = model.ClassNative
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i].Classification = c.Classify(ctx, out[i].StreamURL)
		}(i)
	}
	wg.Wait()
	return out
}

func (c *Client) probe(ctx context.Context, streamURL string) (model.Classification, error) {
	body, finalURL, err := c.fetchPlaylist(ctx, streamURL)
	if err != nil {
		return model.ClassNative, err
	}
	variants, segments := parsePlaylist(body)
	mediaURL := finalURL
	if len(variants) > 0 {
		resolved, err := resolveRef(finalURL, variants[0])
		if err != nil {
			return model.ClassNative, err
		}
		body, mediaURL, err = c.fetchPlaylist(ctx, resolved)
		if err != nil {
			return model.ClassNative, err
		}
		_, segments = parsePlaylist(body)
	}
	if len(segments) == 0 {
		return model.ClassNative, nil
	}
	n := segmentSampleN
	if len(segments) < n {
		n = len(segments)
	}
	for _, seg := range segments[:n] {
		abs, err := resolveRef(mediaURL, seg)
		if err != nil {
			// Unresolvable relative → treat path as given
			abs = seg
		}
		if isBeaconURI(abs) {
			return model.ClassBeacon, nil
		}
	}
	return model.ClassNative, nil
}

func (c *Client) fetchPlaylist(ctx context.Context, rawURL string) (body []byte, finalURL string, err error) {
	resp, err := c.http.RangedGet(ctx, rawURL, 0, playlistRangeEnd)
	if err != nil {
		return nil, rawURL, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		return nil, rawURL, fmt.Errorf("playlist HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, playlistRangeEnd+1))
	if err != nil {
		return nil, rawURL, err
	}
	finalURL = rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return data, finalURL, nil
}

func resolveRef(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return b.ResolveReference(r).String(), nil
}

// parsePlaylist returns variant URIs (master) and segment URIs (media).
func parsePlaylist(body []byte) (variants, segments []string) {
	var pendingVariant, pendingSegment bool
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			pendingVariant = true
			pendingSegment = false
			continue
		}
		if strings.HasPrefix(line, "#EXTINF") {
			pendingSegment = true
			pendingVariant = false
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case pendingVariant:
			variants = append(variants, line)
			pendingVariant = false
		case pendingSegment:
			segments = append(segments, line)
			pendingSegment = false
		default:
			// Bare URI after tags we don't track — treat as segment.
			segments = append(segments, line)
		}
	}
	return variants, segments
}

// isBeaconURI reports Amagi-style SSAI: /beacon/ in the path, or no media
// extension (.ts/.aac/.mp4/.m4s) before the query string.
func isBeaconURI(raw string) bool {
	pathPart := raw
	if u, err := url.Parse(raw); err == nil && u.Path != "" {
		pathPart = u.Path
	} else if i := strings.IndexByte(raw, '?'); i >= 0 {
		pathPart = raw[:i]
	}
	lower := strings.ToLower(pathPart)
	if strings.Contains(lower, "/beacon/") {
		return true
	}
	ext := strings.ToLower(path.Ext(pathPart))
	switch ext {
	case ".ts", ".aac", ".mp4", ".m4s":
		return false
	default:
		return true
	}
}
