package refresh

import (
	"fmt"
	"strings"

	"github.com/j27-aurum/gofast/internal/model"
)

// EmissionPolicy selects direct upstream versus stable FASTProxy playback URLs.
type EmissionPolicy struct {
	ProxyBaseURL     string
	ProxyAll         bool
	ExcludeUnhealthy bool
}

type emissionStats struct {
	NeedsProxyDropped int
	UnhealthyDropped  int
}

func applyEmissionPolicy(channels []model.Channel, policy EmissionPolicy) ([]model.Channel, emissionStats) {
	out := make([]model.Channel, len(channels))
	copy(out, channels)
	stats := emissionStats{}
	for index := range out {
		channel := &out[index]
		channel.Classification = channel.Classification.Canonical()
		channel.EmittedURL = ""
		if channel.Excluded {
			continue
		}
		if channel.Classification == model.ClassDRM {
			channel.Excluded = true
			if channel.FilterReason == "" {
				channel.FilterReason = model.FilterReasonDRM
			}
			continue
		}
		if policy.ExcludeUnhealthy && channel.Health.StatusOrUntested() == model.HealthDown {
			channel.Excluded = true
			if channel.FilterReason == "" {
				channel.FilterReason = model.FilterReasonUnhealthy
			}
			stats.UnhealthyDropped++
			continue
		}
		requiresProxy := policy.ProxyAll || channel.Classification.RequiresAmagiProxy()
		if requiresProxy && policy.ProxyBaseURL == "" {
			channel.Excluded = true
			if channel.FilterReason == "" {
				channel.FilterReason = model.FilterReasonNeedsFASTProxy
			}
			stats.NeedsProxyDropped++
			continue
		}
		if requiresProxy {
			channel.EmittedURL = proxyStreamURL(policy.ProxyBaseURL, channel.Provider, channel.NormalizedID)
			continue
		}
		channel.EmittedURL = channel.StreamURL
	}
	return out, stats
}

func proxyStreamURL(baseURL string, provider model.ProviderID, normalizedID string) string {
	return fmt.Sprintf("%s/stream/%s/%s.m3u8",
		strings.TrimRight(baseURL, "/"),
		provider,
		normalizedID,
	)
}
