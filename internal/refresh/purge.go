package refresh

import (
	"context"
	"errors"
	"log/slog"
	"path"
	"strings"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/providerset"
)

// PurgeResult is the JSON body for soft-purge endpoints.
type PurgeResult struct {
	cache.ClearStats
	Refresh string            `json:"refresh"` // accepted | skipped | partial
	Notes   map[string]string `json:"notes,omitempty"`
}

// PurgeAndRefresh soft-purges one provider's non-current generations, optionally
// clears logos, then kicks TriggerAsync. Returns ErrUnknownProvider /
// ErrRefreshInFlight from TriggerAsync when refresh cannot start.
func (s *Service) PurgeAndRefresh(ctx context.Context, id model.ProviderID, clearLogos bool) (PurgeResult, error) {
	var out PurgeResult
	if s == nil || s.cache == nil {
		return out, ErrUnknownProvider
	}
	stats, err := s.cache.PurgeNonCurrent(id)
	if err != nil {
		return out, err
	}
	out.ClearStats = stats
	slog.Info("cache soft purge", "provider", id, "deleted_files", stats.DeletedFiles, "deleted_bytes", stats.DeletedBytes)

	if clearLogos {
		ls, err := s.cache.DeleteProviderLogos(id)
		if err != nil {
			return out, err
		}
		out.Add(ls)
		slog.Info("cache logos cleared", "provider", id, "deleted_files", ls.DeletedFiles, "deleted_bytes", ls.DeletedBytes)
	}

	if err := s.TriggerAsync(ctx, id); err != nil {
		return out, err
	}
	out.Refresh = "accepted"
	return out, nil
}

// PurgeAllAndRefresh soft-purges every enabled provider + aggregate, sweeps
// orphans, and kicks refreshes. Per-provider in-flight refreshes are noted
// without failing the whole call.
func (s *Service) PurgeAllAndRefresh(ctx context.Context, clearLogos bool) (PurgeResult, error) {
	var out PurgeResult
	out.Notes = map[string]string{}
	if s == nil || s.cache == nil {
		return out, ErrUnknownProvider
	}

	enabled := s.enabledIDs()
	for _, id := range enabled {
		stats, err := s.cache.PurgeNonCurrent(id)
		if err != nil {
			return out, err
		}
		out.Add(stats)
		slog.Info("cache soft purge", "provider", id, "deleted_files", stats.DeletedFiles, "deleted_bytes", stats.DeletedBytes)
		if clearLogos {
			ls, err := s.cache.DeleteProviderLogos(id)
			if err != nil {
				return out, err
			}
			out.Add(ls)
		}
	}

	agg, err := s.cache.PurgeNonCurrentAggregate()
	if err != nil {
		return out, err
	}
	out.Add(agg)
	slog.Info("cache soft purge", "provider", "aggregate", "deleted_files", agg.DeletedFiles, "deleted_bytes", agg.DeletedBytes)

	sweep, err := s.cache.SweepOrphans(providerset.Known(), s.lineupChannelIDs(), s.lineupLogoFiles())
	if err != nil {
		return out, err
	}
	out.Add(sweep)
	if sweep.DeletedFiles > 0 {
		slog.Info("cache orphan sweep", "deleted_files", sweep.DeletedFiles, "deleted_bytes", sweep.DeletedBytes)
	}

	accepted, skipped := 0, 0
	for _, id := range enabled {
		err := s.TriggerAsync(ctx, id)
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrRefreshInFlight):
			skipped++
			out.Notes[string(id)] = "refresh already in progress"
		case errors.Is(err, ErrUnknownProvider):
			skipped++
			out.Notes[string(id)] = "provider not running"
		default:
			return out, err
		}
	}
	switch {
	case accepted > 0 && skipped == 0:
		out.Refresh = "accepted"
	case accepted > 0:
		out.Refresh = "partial"
	default:
		out.Refresh = "skipped"
	}

	if s.notify != nil {
		s.notify()
	}
	return out, nil
}

// ClearAllLogos deletes every cached logo. Next GET /logos/ refills lazily.
func (s *Service) ClearAllLogos(ctx context.Context) (cache.ClearStats, error) {
	if s == nil || s.cache == nil {
		return cache.ClearStats{}, nil
	}
	stats, err := s.cache.DeleteAllLogos()
	if err != nil {
		return stats, err
	}
	slog.Info("cache logos cleared", "scope", "all", "deleted_files", stats.DeletedFiles, "deleted_bytes", stats.DeletedBytes)
	return stats, nil
}

// ClearProviderLogos deletes one provider's logos. Next GET refills lazily.
func (s *Service) ClearProviderLogos(ctx context.Context, id model.ProviderID) (cache.ClearStats, error) {
	if s == nil || s.cache == nil {
		return cache.ClearStats{}, ErrUnknownProvider
	}
	if err := s.ensureKnownProvider(id); err != nil {
		return cache.ClearStats{}, err
	}
	stats, err := s.cache.DeleteProviderLogos(id)
	if err != nil {
		return stats, err
	}
	slog.Info("cache logos cleared", "provider", id, "deleted_files", stats.DeletedFiles, "deleted_bytes", stats.DeletedBytes)
	return stats, nil
}

// ClearChannelLogo deletes one channel's logo files. Next GET refills lazily.
func (s *Service) ClearChannelLogo(ctx context.Context, id model.ProviderID, channelID string) (cache.ClearStats, error) {
	if s == nil || s.cache == nil {
		return cache.ClearStats{}, ErrUnknownProvider
	}
	if err := s.ensureKnownProvider(id); err != nil {
		return cache.ClearStats{}, err
	}
	stats, err := s.cache.DeleteChannelLogos(id, channelID)
	if err != nil {
		return stats, err
	}
	slog.Info("cache logos cleared", "provider", id, "channel", channelID, "deleted_files", stats.DeletedFiles, "deleted_bytes", stats.DeletedBytes)
	return stats, nil
}

func (s *Service) enabledIDs() []model.ProviderID {
	if s == nil || s.reg == nil {
		return nil
	}
	feeds := s.reg.Feeds()
	ids := make([]model.ProviderID, 0, len(feeds))
	for _, f := range feeds {
		ids = append(ids, f.ID())
	}
	return ids
}

func (s *Service) lineupChannelIDs() map[model.ProviderID]map[string]struct{} {
	out := map[model.ProviderID]map[string]struct{}{}
	if s == nil || s.reg == nil {
		return out
	}
	for _, f := range s.reg.Feeds() {
		set := map[string]struct{}{}
		for _, ch := range f.Channels() {
			id := ch.NormalizedID
			if id == "" {
				id = model.NormalizeID(ch.ID)
			}
			if id != "" {
				set[id] = struct{}{}
			}
		}
		out[f.ID()] = set
	}
	return out
}

func (s *Service) lineupLogoFiles() map[model.ProviderID]map[string]string {
	out := map[model.ProviderID]map[string]string{}
	if s == nil || s.reg == nil {
		return out
	}
	for _, f := range s.reg.Feeds() {
		files := map[string]string{}
		for _, ch := range f.Channels() {
			id := ch.NormalizedID
			if id == "" {
				id = model.NormalizeID(ch.ID)
			}
			if id == "" {
				continue
			}
			u := ch.LogoURL
			if u == "" {
				continue
			}
			if i := strings.Index(u, "/logos/"); i >= 0 {
				rest := u[i+len("/logos/"):]
				parts := strings.SplitN(rest, "/", 2)
				if len(parts) == 2 && parts[0] == string(f.ID()) && parts[1] != "" {
					files[id] = path.Base(parts[1])
				}
			}
		}
		if len(files) > 0 {
			out[f.ID()] = files
		}
	}
	return out
}

func (s *Service) ensureKnownProvider(id model.ProviderID) error {
	if s.reg != nil {
		if _, ok := s.reg.Feed(id); ok {
			return nil
		}
	}
	for _, known := range providerset.Known() {
		if known == id {
			return nil
		}
	}
	return ErrUnknownProvider
}
