package server

import (
	"net/http"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
)

// LogoFile serves GET /logos/{provider}/{file} from the shared disk cache
// ({provider}/logos/ under the cache root).
func LogoFile(cc *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		provider := model.ProviderID(r.PathValue("provider"))
		file := r.PathValue("file")
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
