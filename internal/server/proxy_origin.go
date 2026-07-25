package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

// proxyOriginResponse is the lean channel lookup FASTProxy uses to resolve
// /stream/{provider}/{id} to an upstream URL. request_headers are included
// here (unlike the public channel JSON) so Amagi fetches can reuse provider UAs.
type proxyOriginResponse struct {
	StreamURL      string               `json:"stream_url"`
	Classification model.Classification `json:"classification"`
	RequestHeaders map[string]string    `json:"request_headers,omitempty"`
}

// ProxyOriginHandler serves GET /api/proxy/origin/{provider}/{normalizedId}.
// Intended for server-to-server use on the Docker network (FASTProxy → gen).
func ProxyOriginHandler(reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		providerID := model.ProviderID(r.PathValue("provider"))
		normalizedID := r.PathValue("normalizedId")
		feed, ok := reg.Feed(providerID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		for _, ch := range feed.Channels() {
			if ch.NormalizedID != normalizedID {
				continue
			}
			headers := ch.RequestHeaders
			if len(headers) == 0 {
				headers = nil
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(proxyOriginResponse{
				StreamURL:      ch.StreamURL,
				Classification: ch.Classification.Canonical(),
				RequestHeaders: headers,
			})
			return
		}
		http.NotFound(w, r)
	}
}
