package model

// ProviderID identifies a provider: the providers.<id> config key, the on-disk
// cache directory name, and the tvg-id namespace for combined documents.
//
// Each implemented provider has a constant below; add one per new adapter.
// YAML ids without a matching implementation are ignored (warned) at startup.
type ProviderID string

const (
	// ProviderDistroTV is the DistroTV published-pair provider.
	ProviderDistroTV ProviderID = "distrotv"
	// ProviderLG is the LG Channels US provider.
	ProviderLG ProviderID = "lg"
	// ProviderLocalNow is the LocalNow published-pair provider.
	ProviderLocalNow ProviderID = "localnow"
	// ProviderPlex is the Plex Free TV US provider (i.mjh.nz).
	ProviderPlex ProviderID = "plex"
	// ProviderPluto is the Pluto TV US provider.
	ProviderPluto ProviderID = "pluto"
	// ProviderRoku is the Roku Channel provider.
	ProviderRoku ProviderID = "roku"
	// ProviderSamsung is the Samsung TV Plus US provider.
	ProviderSamsung ProviderID = "samsung"
	// ProviderXumo is the Xumo Play published-pair provider.
	ProviderXumo ProviderID = "xumo"
)
