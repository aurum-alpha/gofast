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
	softRetryDelay   = time.Second
)

// SegmentProber is Health L1: GET of the first media segment (NATIVE only on schedule).
// Prefers a ranged GET; on HTTP 416 retries without Range. Soft-retries
// timeout/5xx once when SoftRetries > 0. Uses ProbeURL (Emitted when set).
type SegmentProber struct {
	HTTP *httpx.Client
	// SoftRetries is extra attempts after a soft-fail (timeout/5xx/reset).
	// Default 1 when unset via zero — callers should set explicitly from config.
	SoftRetries int
	// ProxyPublicBase / ProxyInternalBase rewrite EmittedURL for gen-side probes.
	ProxyPublicBase   string
	ProxyInternalBase string
}

// Check implements Source for a segment probe against ProbeURL(ch).
func (p *SegmentProber) Check(ctx context.Context, ch model.Channel) model.HealthCheck {
	start := time.Now()
	at := start.UTC()
	check := model.HealthCheck{At: at, Source: "health_l1"}
	if p == nil || p.HTTP == nil {
		return finishCheck(failCheck(check, "no_client", "segment prober has no HTTP client"), start)
	}
	streamURL := RewriteProxyProbeURL(ProbeURL(ch), p.ProxyPublicBase, p.ProxyInternalBase)
	if streamURL == "" {
		return finishCheck(failCheck(check, "no_url", "channel has no stream_url or emitted_url"), start)
	}
	check.FinalURL = streamURL

	retries := p.SoftRetries
	if retries < 0 {
		retries = 0
	}
	var last model.HealthCheck
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				out := failCheck(check, "timeout", "soft retry aborted: "+ctx.Err().Error())
				out.FinalURL = check.FinalURL
				return finishCheck(out, start)
			case <-time.After(softRetryDelay):
			}
		}
		last = p.checkOnce(ctx, check, streamURL, ch.RequestHeaders)
		if last.Result == model.HealthCheckSuccess {
			if attempt > 0 {
				// Keep success; duration covers all attempts via finishCheck.
			}
			return finishCheck(last, start)
		}
		if !isSoftFailure(last) || attempt == retries {
			if attempt > 0 && last.Detail != "" {
				last.Detail = fmt.Sprintf("retried after %s\n\n%s", softFailLabel(last), last.Detail)
			}
			return finishCheck(last, start)
		}
	}
	return finishCheck(last, start)
}

func finishCheck(check model.HealthCheck, start time.Time) model.HealthCheck {
	check.DurationMs = time.Since(start).Milliseconds()
	if check.DurationMs <= 0 {
		check.DurationMs = 1
	}
	return check
}

func (p *SegmentProber) checkOnce(ctx context.Context, base model.HealthCheck, streamURL string, headers map[string]string) model.HealthCheck {
	check := base
	segURL, status, encrypted, meta, err := firstSegmentURL(ctx, p.HTTP, streamURL, headers)
	applyGETMeta(&check, meta)
	if err != nil {
		return failHTTP(check, status, err)
	}
	body, status, contentType, meta, err := getBody(ctx, p.HTTP, segURL, headers, segmentRangeEnd)
	applyGETMeta(&check, meta)
	if err != nil {
		return failHTTP(check, status, fmt.Errorf("segment GET %s: %w", detailURL(segURL), err))
	}
	check.HTTPStatus = status
	check.BytesRead = len(body)
	if check.FinalURL == "" {
		check.FinalURL = meta.FinalURL
	}
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

func applyGETMeta(check *model.HealthCheck, meta getMeta) {
	if meta.FinalURL != "" {
		check.FinalURL = meta.FinalURL
	}
	if meta.RangeUsed {
		check.RangeUsed = true
	}
	if meta.RangeRetried {
		check.RangeRetried = true
	}
	if meta.BytesRead > check.BytesRead {
		check.BytesRead = meta.BytesRead
	}
}

func isSoftFailure(check model.HealthCheck) bool {
	switch check.FailureClass {
	case "timeout", "http_5xx", "conn_reset", "eof":
		return true
	}
	if strings.HasPrefix(check.FailureClass, "http_5") {
		return true
	}
	if check.HTTPStatus >= 500 && check.HTTPStatus <= 599 {
		return true
	}
	return false
}

func softFailLabel(check model.HealthCheck) string {
	if check.FailureClass != "" {
		return check.FailureClass
	}
	if check.HTTPStatus > 0 {
		return fmt.Sprintf("http_%d", check.HTTPStatus)
	}
	return "transient_error"
}

func failHTTP(check model.HealthCheck, status int, err error) model.HealthCheck {
	out := failFromErr(check, err)
	if out.HTTPStatus == 0 && status > 0 {
		out.HTTPStatus = status
	}
	return out
}

// probeHTTPError carries an HTTP status for Health L1 failures (playlist/segment).
type probeHTTPError struct {
	Status int
	Msg    string
}

func (e *probeHTTPError) Error() string { return e.Msg }

type getMeta struct {
	FinalURL     string
	BytesRead    int
	RangeUsed    bool
	RangeRetried bool
}

func firstSegmentURL(ctx context.Context, client *httpx.Client, streamURL string, headers map[string]string) (segURL string, lastStatus int, encrypted bool, meta getMeta, err error) {
	body, finalURL, status, m, err := fetchPlaylist(ctx, client, streamURL, headers)
	meta = m
	if err != nil {
		return "", status, false, meta, err
	}
	lastStatus = status
	variants, segments, encrypted := parsePlaylist(body)
	mediaURL := finalURL
	if len(variants) > 0 {
		resolved, err := resolveRef(finalURL, variants[0])
		if err != nil {
			return "", lastStatus, false, meta, fmt.Errorf("resolve variant %q from %s: %w", variants[0], detailURL(finalURL), err)
		}
		body, mediaURL, status, m, err = fetchPlaylist(ctx, client, resolved, headers)
		meta.RangeUsed = meta.RangeUsed || m.RangeUsed
		meta.RangeRetried = meta.RangeRetried || m.RangeRetried
		if m.FinalURL != "" {
			meta.FinalURL = m.FinalURL
		}
		meta.BytesRead += m.BytesRead
		if err != nil {
			return "", status, false, meta, err
		}
		lastStatus = status
		_, segments, encrypted = parsePlaylist(body)
	}
	if len(segments) == 0 {
		return "", lastStatus, encrypted, meta, &probeHTTPError{
			Status: lastStatus,
			Msg: fmt.Sprintf("no segments in playlist\nURL: %s\nHTTP %d\nBody-Length: %d\n\n%s",
				detailURL(mediaURL), lastStatus, len(body), bodyForDetail(body)),
		}
	}
	abs, err := resolveRef(mediaURL, segments[0])
	if err != nil {
		return segments[0], lastStatus, encrypted, meta, nil
	}
	return abs, lastStatus, encrypted, meta, nil
}

func fetchPlaylist(ctx context.Context, client *httpx.Client, rawURL string, headers map[string]string) ([]byte, string, int, getMeta, error) {
	res := doGET(ctx, client, rawURL, headers, playlistRangeEnd, true)
	meta := getMeta{FinalURL: res.FinalURL, BytesRead: len(res.Body), RangeUsed: true}
	if res.Status == http.StatusRequestedRangeNotSatisfiable {
		res = doGET(ctx, client, rawURL, headers, playlistRangeEnd, false)
		meta.RangeRetried = true
		meta.RangeUsed = false
		meta.FinalURL = res.FinalURL
		meta.BytesRead = len(res.Body)
	}
	if res.Err != nil {
		return nil, rawURL, res.Status, meta, res.Err
	}
	if res.Status != 200 && res.Status != 206 {
		return nil, rawURL, res.Status, meta, &probeHTTPError{
			Status: res.Status,
			Msg: formatHTTPFailure("playlist", rawURL, fmt.Sprintf("%d", res.Status),
				res.ContentType, res.Body, false),
		}
	}
	return res.Body, res.FinalURL, res.Status, meta, nil
}

func getBody(ctx context.Context, client *httpx.Client, rawURL string, headers map[string]string, end int64) ([]byte, int, string, getMeta, error) {
	res := doGET(ctx, client, rawURL, headers, end, true)
	meta := getMeta{FinalURL: res.FinalURL, BytesRead: len(res.Body), RangeUsed: true}
	if res.Status == http.StatusRequestedRangeNotSatisfiable {
		res = doGET(ctx, client, rawURL, headers, end, false)
		meta.RangeRetried = true
		meta.RangeUsed = false
		meta.FinalURL = res.FinalURL
		meta.BytesRead = len(res.Body)
	}
	return res.Body, res.Status, res.ContentType, meta, res.Err
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
	return contentTypeSuggestsMedia(contentType) && !isMostlyText(body)
}

func looksLikeMedia(body []byte) bool {
	if len(body) < minSegmentBytes {
		return false
	}
	if body[0] == 0x47 {
		return true
	}
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
