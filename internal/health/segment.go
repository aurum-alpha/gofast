package health

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
)

const (
	playlistRangeEnd = 256*1024 - 1
	segmentRangeEnd  = 64*1024 - 1
	minSegmentBytes  = 188 // one MPEG-TS packet
)

// SegmentProber is L2: ranged GET of the first media segment (NATIVE only on schedule).
type SegmentProber struct {
	HTTP *httpx.Client
}

// Check implements Source for a segment probe against ch.StreamURL (or EmittedURL).
func (p *SegmentProber) Check(ctx context.Context, ch model.Channel) model.HealthCheck {
	at := time.Now().UTC()
	check := model.HealthCheck{At: at, Source: "probe_l2"}
	if p == nil || p.HTTP == nil {
		check.Result = model.HealthCheckFailure
		check.FailureClass = "no_client"
		return check
	}
	streamURL := ch.StreamURL
	if streamURL == "" {
		check.Result = model.HealthCheckFailure
		check.FailureClass = "no_url"
		return check
	}
	segURL, err := firstSegmentURL(ctx, p.HTTP, streamURL, ch.RequestHeaders)
	if err != nil {
		check.Result = model.HealthCheckFailure
		check.FailureClass = failureClass(err)
		return check
	}
	body, status, err := rangedBody(ctx, p.HTTP, segURL, ch.RequestHeaders, segmentRangeEnd)
	if err != nil {
		check.Result = model.HealthCheckFailure
		check.FailureClass = failureClass(err)
		return check
	}
	if status != 200 && status != 206 {
		check.Result = model.HealthCheckFailure
		check.FailureClass = fmt.Sprintf("http_%d", status)
		return check
	}
	if !looksLikeMedia(body) {
		check.Result = model.HealthCheckFailure
		check.FailureClass = "empty_segment"
		return check
	}
	check.Result = model.HealthCheckSuccess
	return check
}

func firstSegmentURL(ctx context.Context, client *httpx.Client, streamURL string, headers map[string]string) (string, error) {
	body, finalURL, err := fetchPlaylist(ctx, client, streamURL, headers)
	if err != nil {
		return "", err
	}
	variants, segments := parsePlaylist(body)
	mediaURL := finalURL
	if len(variants) > 0 {
		resolved, err := resolveRef(finalURL, variants[0])
		if err != nil {
			return "", err
		}
		body, mediaURL, err = fetchPlaylist(ctx, client, resolved, headers)
		if err != nil {
			return "", err
		}
		_, segments = parsePlaylist(body)
	}
	if len(segments) == 0 {
		return "", fmt.Errorf("no segments")
	}
	abs, err := resolveRef(mediaURL, segments[0])
	if err != nil {
		return segments[0], nil
	}
	return abs, nil
}

func fetchPlaylist(ctx context.Context, client *httpx.Client, rawURL string, headers map[string]string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, rawURL, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", playlistRangeEnd))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(ctx, req)
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
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return data, finalURL, nil
}

func rangedBody(ctx context.Context, client *httpx.Client, rawURL string, headers map[string]string, end int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", end))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, end+1))
	return data, resp.StatusCode, err
}

func looksLikeMedia(body []byte) bool {
	if len(body) < minSegmentBytes {
		return false
	}
	// MPEG-TS sync byte
	if body[0] == 0x47 {
		return true
	}
	// fMP4 / ISO BMFF: size + 'ftyp' or 'styp' or 'moof'
	if len(body) >= 8 {
		box := string(body[4:8])
		switch box {
		case "ftyp", "styp", "moof", "mdat":
			return true
		}
	}
	return false
}

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
			segments = append(segments, line)
		}
	}
	return variants, segments
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

func failureClass(err error) string {
	if err == nil {
		return "unknown"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "403"):
		return "http_403"
	case strings.Contains(msg, "404"):
		return "http_404"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "timeout"
	case strings.Contains(msg, "no segments"):
		return "no_segments"
	default:
		return "fetch_error"
	}
}
