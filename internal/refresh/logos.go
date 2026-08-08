package refresh

import (
	"github.com/j27-aurum/gofast/internal/logocache"
	"github.com/j27-aurum/gofast/internal/model"
)

// applyLogoEmitRewrite rewrites LogoURL to local /logos/ paths when cache_logos
// is on. No upstream HTTP — bytes are filled lazily on GET /logos/.
func applyLogoEmitRewrite(logos *logocache.Cache, chs []model.Channel) {
	if logos == nil {
		return
	}
	logos.RewriteURLs(chs)
}
