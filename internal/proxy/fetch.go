package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
)

const defaultUA = "Mozilla/5.0 (compatible; GoFAST-proxy/1.0)"

// playlistClient fetches upstream HLS text. Uses httpx so HEAD is impossible.
type playlistClient struct {
	http *httpx.Client
}

func newPlaylistClient(timeout time.Duration) *playlistClient {
	return &playlistClient{http: httpx.NewClient(timeout, 2)}
}

func (c *playlistClient) get(ctx context.Context, rawURL string, headers map[string]string) (body string, finalURL string, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", 0, err
	}
	applyHeaders(req, headers)
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", "", resp.StatusCode, err
	}
	final := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return string(b), final, resp.StatusCode, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	return string(b), final, resp.StatusCode, nil
}

// segmentClient shuttles media bytes with redirect following and no tiny timeout.
type segmentClient struct {
	http *http.Client
}

func newSegmentClient() *segmentClient {
	return &segmentClient{
		http: &http.Client{
			// No Timeout: long-lived io.Copy; cancel via request context.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if req.Method == http.MethodHead {
					return fmt.Errorf("HEAD forbidden")
				}
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

func (c *segmentClient) open(ctx context.Context, rawURL string, headers map[string]string) (*http.Response, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	applyHeaders(req, headers)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	redirects := 0
	if resp.Request != nil {
		// Count hops roughly from the request chain; Go doesn't expose via length here.
		_ = redirects
	}
	return resp, redirects, nil
}

func applyHeaders(req *http.Request, headers map[string]string) {
	hasUA := false
	for k, v := range headers {
		if strings.EqualFold(k, "User-Agent") {
			hasUA = true
		}
		req.Header.Set(k, v)
	}
	if !hasUA {
		req.Header.Set("User-Agent", defaultUA)
	}
}

func classifyUpstreamErr(err error, status int) string {
	if status >= 400 && status < 500 {
		return ReasonUpstream4xx
	}
	if status >= 500 {
		return ReasonUpstream5xx
	}
	if err == nil {
		return ReasonUpstreamError
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") {
		return ReasonUpstreamTimeout
	}
	return ReasonUpstreamError
}
