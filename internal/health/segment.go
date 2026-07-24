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

// SegmentProber is L2: GET of the first media segment (NATIVE only on schedule).
// Prefers a ranged GET; on HTTP 416 retries without Range (some SSAI/CDNs
// reject byte ranges while a plain GET works — same path ffprobe uses).
type SegmentProber struct {
	HTTP *httpx.Client
}

// Check implements Source for a segment probe against ch.StreamURL (or EmittedURL).
func (p *SegmentProber) Check(ctx context.Context, ch model.Channel) model.HealthCheck {
	at := time.Now().UTC()
	check := model.HealthCheck{At: at, Source: "probe_l2"}
	if p == nil || p.HTTP == nil {
		return failCheck(check, "no_client", "segment prober has no HTTP client")
	}
	streamURL := ch.StreamURL
	if streamURL == "" {
		return failCheck(check, "no_url", "channel has no stream_url")
	}
	segURL, status, encrypted, err := firstSegmentURL(ctx, p.HTTP, streamURL, ch.RequestHeaders)
	if err != nil {
		return failHTTP(check, status, err)
	}
	body, status, contentType, err := getBody(ctx, p.HTTP, segURL, ch.RequestHeaders, segmentRangeEnd)
	if err != nil {
		return failHTTP(check, status, fmt.Errorf("segment GET %s: %w", detailURL(segURL), err))
	}
	check.HTTPStatus = status
	if status != 200 && status != 206 {
		return failCheck(check, fmt.Sprintf("http_%d", status),
			formatHTTPFailure("segment", segURL, fmt.Sprintf("%d", status), contentType, body, false))
	}
	if !segmentOK(body, contentType, encrypted) {
		return failCheck(check, "empty_segment",
			formatHTTPFailure("segment", segURL, fmt.Sprintf("%d (not media)", status), contentType, body, false))
	}
	check.Result = model.HealthCheckSuccess
	return check
}

func failHTTP(check model.HealthCheck, status int, err error) model.HealthCheck {
	out := failFromErr(check, err)
	if out.HTTPStatus == 0 && status > 0 {
		out.HTTPStatus = status
	}
	return out
}

// probeHTTPError carries an HTTP status for L2 failures (playlist/segment).
type probeHTTPError struct {
	Status int
	Msg    string
}

func (e *probeHTTPError) Error() string { return e.Msg }

func firstSegmentURL(ctx context.Context, client *httpx.Client, streamURL string, headers map[string]string) (segURL string, lastStatus int, encrypted bool, err error) {
	body, finalURL, status, err := fetchPlaylist(ctx, client, streamURL, headers)
	if err != nil {
		return "", status, false, err
	}
	lastStatus = status
	variants, segments, encrypted := parsePlaylist(body)
	mediaURL := finalURL
	if len(variants) > 0 {
		resolved, err := resolveRef(finalURL, variants[0])
		if err != nil {
			return "", lastStatus, false, fmt.Errorf("resolve variant %q from %s: %w", variants[0], detailURL(finalURL), err)
		}
		body, mediaURL, status, err = fetchPlaylist(ctx, client, resolved, headers)
		if err != nil {
			return "", status, false, err
		}
		lastStatus = status
		_, segments, encrypted = parsePlaylist(body)
	}
	if len(segments) == 0 {
		return "", lastStatus, encrypted, &probeHTTPError{
			Status: lastStatus,
			Msg: fmt.Sprintf("no segments in playlist\nURL: %s\nHTTP %d\nBody-Length: %d\n\n%s",
				detailURL(mediaURL), lastStatus, len(body), bodyForDetail(body)),
		}
	}
	abs, err := resolveRef(mediaURL, segments[0])
	if err != nil {
		return segments[0], lastStatus, encrypted, nil
	}
	return abs, lastStatus, encrypted, nil
}

func fetchPlaylist(ctx context.Context, client *httpx.Client, rawURL string, headers map[string]string) ([]byte, string, int, error) {
	res := doGET(ctx, client, rawURL, headers, playlistRangeEnd, true)
	if res.Status == http.StatusRequestedRangeNotSatisfiable {
		res = doGET(ctx, client, rawURL, headers, playlistRangeEnd, false)
	}
	if res.Err != nil {
		return nil, rawURL, res.Status, res.Err
	}
	if res.Status != 200 && res.Status != 206 {
		return nil, rawURL, res.Status, &probeHTTPError{
			Status: res.Status,
			Msg: formatHTTPFailure("playlist", rawURL, fmt.Sprintf("%d", res.Status),
				res.ContentType, res.Body, false),
		}
	}
	return res.Body, res.FinalURL, res.Status, nil
}

func getBody(ctx context.Context, client *httpx.Client, rawURL string, headers map[string]string, end int64) ([]byte, int, string, error) {
	res := doGET(ctx, client, rawURL, headers, end, true)
	if res.Status == http.StatusRequestedRangeNotSatisfiable {
		res = doGET(ctx, client, rawURL, headers, end, false)
	}
	return res.Body, res.Status, res.ContentType, res.Err
}

type getResult struct {
	Body        []byte
	FinalURL    string
	Status      int
	ContentType string
	Err         error
}

func doGET(ctx context.Context, client *httpx.Client, rawURL string, headers map[string]string, end int64, withRange bool) getResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return getResult{FinalURL: rawURL, Err: fmt.Errorf("request %s: %w", detailURL(rawURL), err)}
	}
	if withRange && end >= 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", end))
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(ctx, req)
	if err != nil {
		return getResult{FinalURL: rawURL, Err: fmt.Errorf("GET %s: %w", detailURL(rawURL), err)}
	}
	defer resp.Body.Close()
	limit := end + 1
	if !withRange || end < 0 {
		limit = playlistRangeEnd + 1
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return getResult{
			FinalURL:    rawURL,
			Status:      resp.StatusCode,
			ContentType: resp.Header.Get("Content-Type"),
			Err:         fmt.Errorf("read %s: %w", detailURL(rawURL), err),
		}
	}
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return getResult{
		Body:        data,
		FinalURL:    finalURL,
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
	}
}

// segmentOK reports whether a successful segment GET looks like real media.
// Cleartext MPEG-TS / fMP4 are sniffed; AES-encrypted HLS segments are
// ciphertext (no 0x47 sync) so we accept size + optional media Content-Type.
func segmentOK(body []byte, contentType string, encrypted bool) bool {
	if len(body) < minSegmentBytes {
		return false
	}
	if looksLikeMedia(body) {
		return true
	}
	if encrypted {
		return true
	}
	// Fallback: CDN labeled it as media (common for AES-128 TS served as video/MP2T).
	return contentTypeSuggestsMedia(contentType) && !isMostlyText(body)
}

func looksLikeMedia(body []byte) bool {
	if len(body) < minSegmentBytes {
		return false
	}
	if body[0] == 0x47 {
		return true
	}
	// Sync may be slightly offset; require two consecutive 188-byte packets.
	limit := len(body) - 188
	if limit > 188 {
		limit = 188
	}
	for off := 0; off <= limit; off++ {
		if body[off] == 0x47 && off+188 < len(body) && body[off+188] == 0x47 {
			return true
		}
	}
	if len(body) >= 8 {
		box := string(body[4:8])
		switch box {
		case "ftyp", "styp", "moof", "mdat":
			return true
		}
	}
	return false
}

func contentTypeSuggestsMedia(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "video/mp2t", "video/mp2ts", "video/mpegts", "video/mpeg", "video/mp4",
		"audio/mp4", "audio/aac", "audio/mpeg",
		"application/mp2t", "application/octet-stream":
		return true
	default:
		return strings.HasPrefix(ct, "video/") || strings.HasPrefix(ct, "audio/")
	}
}

func parsePlaylist(body []byte) (variants, segments []string, encrypted bool) {
	var pendingVariant, pendingSegment bool
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			upper := strings.ToUpper(line)
			if strings.Contains(upper, "METHOD=NONE") {
				encrypted = false
			} else if strings.Contains(upper, "METHOD=") {
				// AES-128, SAMPLE-AES, SAMPLE-AES-CTR, …
				encrypted = true
			}
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
	return variants, segments, encrypted
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
