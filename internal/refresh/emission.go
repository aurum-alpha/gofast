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

		if channel.NormalizedID == "" {
			channel.AddFilterReason(model.FilterReasonMissingIdentity)
		}
		if strings.TrimSpace(channel.StreamURL) == "" {
			channel.AddFilterReason(model.FilterReasonMissingStream)
		}

		class := channel.Classification
		if class == model.ClassDRM {
			channel.AddFilterReason(model.FilterReasonDRM)
		} else if class != "" && !class.Known() {
			channel.AddFilterReason(model.FilterReasonUnsupportedClassification)
		}

		if policy.ExcludeUnhealthy && !channel.ForceInclude &&
			channel.Health.StatusOrUntested() == model.HealthDown {
			channel.AddFilterReason(model.FilterReasonUnhealthy)
			stats.UnhealthyDropped++
		}

		requiresProxy := policy.ProxyAll || class.RequiresProxy()
		proxyBase := strings.TrimSpace(policy.ProxyBaseURL)
		canMintProxy := requiresProxy && proxyBase != ""
		needsProxyMissing := requiresProxy && proxyBase == ""

		if needsProxyMissing {
			// Only when proxy is not configured — never when ProxyBaseURL is set.
			channel.AddFilterReason(model.FilterReasonNeedsFASTProxy)
			stats.NeedsProxyDropped++
		}

		// Mint EmittedURL whenever a playback path exists, even if other reasons
		// exclude the channel from the playlist (duplicate, regex, …).
		deadSSAI := model.HasFilterReason(channel.FilterReasons, model.FilterReasonDeadSSAI)
		noPlayback := class == model.ClassDRM ||
			deadSSAI ||
			(class != "" && !class.Known()) ||
			channel.NormalizedID == "" ||
			strings.TrimSpace(channel.StreamURL) == "" ||
			needsProxyMissing
		if noPlayback {
			continue
		}
		// Class B (demux-stable) wins over /stream/ when proxy is configured.
		// #56 thin fixture: Pluto until #57 general Class B detection.
		if proxyBase != "" && classBPreferStable(*channel) {
			channel.EmittedURL = proxyStableURL(proxyBase, channel.Provider, channel.NormalizedID)
			continue
		}
		if canMintProxy {
			channel.EmittedURL = proxyStreamURL(proxyBase, channel.Provider, channel.NormalizedID)
			continue
		}
		channel.EmittedURL = channel.StreamURL
	}
	return out, stats
}

func classBPreferStable(ch model.Channel) bool {
	return ch.Provider == model.ProviderPluto
}

func proxyStreamURL(baseURL string, provider model.ProviderID, normalizedID string) string {
	return fmt.Sprintf("%s/stream/%s/%s.m3u8",
		strings.TrimRight(baseURL, "/"),
		provider,
		normalizedID,
	)
}

func proxyStableURL(baseURL string, provider model.ProviderID, normalizedID string) string {
	return fmt.Sprintf("%s/stable/%s/%s.ts",
		strings.TrimRight(baseURL, "/"),
		provider,
		normalizedID,
	)
}
