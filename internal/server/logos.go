package server

import (
	"net/http"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/refresh"
)

// LogoFile serves GET /logos/{provider}/{file}. When cache_logos is on, miss and
// soft-stale paths block on the lazy fetcher; otherwise disk-only.
func LogoFile(svc *refresh.Service, cc *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		provider := model.ProviderID(r.PathValue("provider"))
		file := r.PathValue("file")
		if svc != nil {
			if logos := svc.LogoCache(); logos != nil {
				logos.ServeHTTP(w, r, provider, file, svc.ResolveLogoSource)
				return
			}
		}
		if cc == nil {
			http.NotFound(w, r)
			return
		}
		path, err := cc.LogoPath(provider, file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if _, err := cc.StatLogo(provider, file); err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, path)
	}
}
