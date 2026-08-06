// Package classifier probes HLS playlists and buckets channels by stream dialect.
package classifier

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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

// Client classifies stream URLs using URL heuristics and ranged GET (never HEAD).
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

// FromURL reports SESSION or XUMO_SSAI when the stream URL shape matches without
// probing. ok is false when no URL heuristic applies (caller should probe or keep prior).
func FromURL(streamURL string) (model.Classification, bool) {
	return classifyByURL(streamURL)
}

// Classify classifies streamURL: URL heuristics first, then master → first variant → first ~5 segments.
// Fetch errors classify as NATIVE (never drop on transient failure).
// Channels already marked DRM are left as DRM without probing.
func (c *Client) Classify(ctx context.Context, streamURL string) model.Classification {
	return c.classify(ctx, streamURL, nil)
}

// classify resolves the dialect. SESSION URLs are never probed (fetching a
// Google DAI mint URL creates a fake tune-in or 404s). An ads.* match is only
// a hint: Amagi playout also carries ads.* params, and its beacon-shaped media
// playlists break direct-play ffmpeg clients, so the playlist probe still runs
// and beacon detection wins. Plain media segments or a probe failure fall back
// to the XUMO_SSAI hint so transient fetch errors never flip the label.
func (c *Client) classify(ctx context.Context, streamURL string, headers map[string]string) model.Classification {
	hint, hinted := classifyByURL(streamURL)
	if hinted && (hint == model.ClassSession || hint == model.ClassDistroResolve || hint == model.ClassStirrResolve) {
		return hint
	}
	if c == nil {
		if hinted {
			return hint
		}
		return model.ClassNative
	}
	class, err := c.probe(ctx, streamURL, headers)
	if err == nil && class == model.ClassAmagiSSAI {
		return class
	}
	if hinted {
		return hint
	}
	if err != nil {
		return model.ClassNative
	}
	return class
}

// ClassifyChannels sets Classification on each channel using a bounded worker pool.
// DRM (already set) is left alone. Amagi SSAI channels are not excluded here — callers
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
		if out[i].Classification == model.ClassDRM ||
			out[i].Classification == model.ClassDistroResolve ||
			out[i].Classification == model.ClassStirrResolve {
			logClassified(&out[i], "pre-marked")
			continue
		}
		if out[i].StreamURL == "" {
			out[i].Classification = model.ClassNative
			logClassified(&out[i], "no stream")
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i].Classification = c.classify(ctx, out[i].StreamURL, out[i].RequestHeaders)
			logClassified(&out[i], "probed")
		}(i)
	}
	wg.Wait()
	return out
}

// logClassified emits one line per channel showing the probed stream and result.
func logClassified(ch *model.Channel, via string) {
	slog.Info("classified",
		"provider", ch.Provider,
		"id", ch.NormalizedID,
		"name", ch.Name,
		"class", ch.Classification,
		"via", via,
		"stream_url", ch.StreamURL,
	)
}

// classifyByURL applies host/query heuristics that do not need a playlist fetch.
// SESSION: Google DAI mint-on-tune-in URLs (often 404 without a fresh session).
// XUMO_SSAI: CloudFront/Xumo SSAI that needs ads.* query params. The XUMO_SSAI
// answer is a hint, not a verdict — Amagi playout URLs carry ads.* too, and only
// the playlist probe can tell them apart (see classify).
func classifyByURL(streamURL string) (model.Classification, bool) {
	if strings.HasPrefix(strings.TrimSpace(streamURL), "distro://channel/") {
		return model.ClassDistroResolve, true
	}
	if strings.HasPrefix(strings.TrimSpace(streamURL), "stirr://channel/") {
		return model.ClassStirrResolve, true
	}
	u, err := url.Parse(streamURL)
	if err != nil || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if (host == "dai.google.com" || strings.HasSuffix(host, ".dai.google.com")) &&
		strings.Contains(u.Path, "/linear/hls/") {
		return model.ClassSession, true
	}
	for key := range u.Query() {
		if strings.HasPrefix(strings.ToLower(key), "ads.") {
			return model.ClassXumoSSAI, true
		}
	}
	return "", false
}

func (c *Client) probe(ctx context.Context, streamURL string, headers map[string]string) (model.Classification, error) {
	body, finalURL, err := c.fetchPlaylist(ctx, streamURL, headers)
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
		body, mediaURL, err = c.fetchPlaylist(ctx, resolved, headers)
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
		if isAmagiSSAIURI(abs) {
			return model.ClassAmagiSSAI, nil
		}
	}
	return model.ClassNative, nil
}

func (c *Client) fetchPlaylist(ctx context.Context, rawURL string, headers map[string]string) (body []byte, finalURL string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, rawURL, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", playlistRangeEnd))
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := c.http.Do(ctx, req)
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

// isAmagiSSAIURI reports Amagi-style SSAI: /beacon/ in the path, or no media
// extension (.ts/.aac/.mp4/.m4s) before the query string.
func isAmagiSSAIURI(raw string) bool {
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
