package logocache

import (
	"errors"
	"net/http"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

// ServeHTTP serves GET /logos/{provider}/{file}: fresh disk hit, else block on
// the fetch pool (miss = full GET, soft-stale = conditional GET).
func (c *Cache) ServeHTTP(w http.ResponseWriter, r *http.Request, provider model.ProviderID, file string, resolve SourceFunc) {
	if c == nil || c.store == nil {
		http.NotFound(w, r)
		return
	}
	path, err := c.store.LogoPath(provider, file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	channelID, ok := logoChannelID(file)
	if !ok {
		http.NotFound(w, r)
		return
	}

	meta := c.readMeta(provider, file)
	sourceURL := meta.SourceURL
	var headers map[string]string
	if resolve != nil {
		if src, hdr, ok := resolve(provider, channelID); ok && src != "" {
			headers = hdr
			if sourceURL == "" {
				sourceURL = src
			} else if sourceURL != src {
				sourceURL = src
				_, _ = c.store.DeleteChannelLogos(provider, channelID)
				meta = fileMeta{}
				path, _ = c.store.LogoPath(provider, file)
			}
		}
	}
	if sourceURL == "" {
		if _, err := c.store.StatLogo(provider, file); err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, path)
		return
	}

	_, statErr := c.store.StatLogo(provider, file)
	hasFile := statErr == nil
	fresh := hasFile && c.freshAge(provider, file, meta) && (meta.SourceURL == "" || meta.SourceURL == sourceURL)
	if fresh {
		http.ServeFile(w, r, path)
		return
	}

	forceFull := !hasFile || (meta.SourceURL != "" && meta.SourceURL != sourceURL)
	err = c.waitFetch(r.Context(), provider, file, sourceURL, headers, forceFull)
	if err != nil {
		var status httpStatusError
		if errors.As(err, &status) && (status.code == http.StatusForbidden || status.code == http.StatusNotFound) {
			http.NotFound(w, r)
			return
		}
		if hasFile {
			http.ServeFile(w, r, path)
			return
		}
		http.Error(w, "logo fetch failed", http.StatusBadGateway)
		return
	}

	if _, err := c.store.StatLogo(provider, file); err != nil {
		if alt := c.findLogoFile(provider, channelID); alt != "" {
			file = alt
			path, err = c.store.LogoPath(provider, file)
			if err != nil {
				http.NotFound(w, r)
				return
			}
		} else {
			http.NotFound(w, r)
			return
		}
	}
	http.ServeFile(w, r, path)
}

func (c *Cache) freshAge(provider model.ProviderID, file string, meta fileMeta) bool {
	if !meta.FetchedAt.IsZero() {
		return time.Since(meta.FetchedAt) <= c.maxAge
	}
	return c.withinMaxAge(provider, file)
}
