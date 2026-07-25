package proxy

import (
	"net/http"
	"strings"
)

// publicBaseFromRequest derives the absolute proxy origin clients should use
// in rewritten playlist URIs. Prefer X-Forwarded-* behind a reverse proxy so
// Jellyfin keeps talking to the same host gen embedded in proxy_base_url.
func publicBaseFromRequest(r *http.Request) string {
	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	// X-Forwarded-Host may be a comma list; take the first.
	if i := strings.IndexByte(host, ','); i >= 0 {
		host = strings.TrimSpace(host[:i])
	}
	return strings.TrimRight(proto+"://"+host, "/")
}
