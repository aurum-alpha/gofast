package httpx

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const (
	DefaultTimeout  = 60 * time.Second
	DefaultRetries  = 3
	initialRetryGap = 500 * time.Millisecond
)

// Client performs outbound HTTP with timeouts and retry-with-backoff.
// Stream probes must use GET (with Range when needed), never HEAD.
type Client struct {
	httpClient *http.Client
	retries    int
}

// NewClient returns a Client with the given timeout and retry count.
// Zero timeout or retries fall back to defaults.
func NewClient(timeout time.Duration, retries int) *Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if retries <= 0 {
		retries = DefaultRetries
	}
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		retries:    retries,
	}
}

// Do executes req with retry and backoff. The request context is honored.
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodHead {
		return nil, fmt.Errorf("httpx: HEAD is forbidden for stream probes; use GET with Range")
	}
	req = req.WithContext(ctx)

	var lastErr error
	gap := initialRetryGap
	for attempt := 1; attempt <= c.retries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		resp, err := c.httpClient.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt == c.retries {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(gap):
		}
		gap *= 2
	}
	return nil, fmt.Errorf("httpx: after %d attempts: %w", c.retries, lastErr)
}

// RangedGet issues GET url with a Range header. HEAD is never used.
func (c *Client) RangedGet(ctx context.Context, url string, start, end int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	return c.Do(ctx, req)
}
