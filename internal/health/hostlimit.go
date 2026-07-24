package health

import (
	"context"
	"net/url"
	"sync"
)

// HostLimiter caps concurrent probes per URL hostname.
type HostLimiter struct {
	max int
	mu  sync.Mutex
	// per-host semaphore channels of capacity max
	hosts map[string]chan struct{}
}

// NewHostLimiter returns a limiter with max in-flight probes per host (min 1).
func NewHostLimiter(maxPerHost int) *HostLimiter {
	if maxPerHost < 1 {
		maxPerHost = 2
	}
	return &HostLimiter{
		max:   maxPerHost,
		hosts: make(map[string]chan struct{}),
	}
}

// Acquire blocks until a slot is available for the host of rawURL, or ctx is done.
// Release must be called once when the probe finishes.
func (h *HostLimiter) Acquire(ctx context.Context, rawURL string) (release func(), err error) {
	if h == nil {
		return func() {}, nil
	}
	host := probeHost(rawURL)
	sem := h.sem(host)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	}
}

func (h *HostLimiter) sem(host string) chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hosts == nil {
		h.hosts = make(map[string]chan struct{})
	}
	if host == "" {
		host = "_"
	}
	ch, ok := h.hosts[host]
	if !ok {
		ch = make(chan struct{}, h.max)
		h.hosts[host] = ch
	}
	return ch
}

func probeHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Hostname()
}
