package proxy

import (
	"net/http"
	"strings"
)

// publicBaseFromRequest derives the absolute proxy origin clients should use
// in rewritten playlist URIs. Prefer X-Forwarded-* behind a reverse proxy so
// Jellyfin keeps talking to the same host gen embedded in proxy_base_url.
//
// When TLS terminates at nginx and X-Forwarded-Proto is missing, this falls
// back to http — set Handler.PublicBase (FASTPROXY_PUBLIC_BASE_URL) instead.
func publicBaseFromRequest(r *http.Request) string {
	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	// X-Forwarded-Proto may be a comma list; take the first.
	if i := strings.IndexByte(proto, ','); i >= 0 {
		proto = strings.TrimSpace(proto[:i])
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

// resolvePublicBase returns the absolute origin for rewritten playlist URIs.
// Configured PublicBase (deploy-time HTTPS origin) wins over request headers.
func (h *Handler) resolvePublicBase(r *http.Request) string {
	if h != nil {
		if base := strings.TrimRight(strings.TrimSpace(h.PublicBase), "/"); base != "" {
			return base
		}
	}
	return publicBaseFromRequest(r)
}
