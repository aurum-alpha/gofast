package model

// ProviderID identifies a provider: the providers.<id> config key, the on-disk
// cache directory name, and the tvg-id namespace for combined documents.
//
// Each implemented provider has a constant below; add one per new adapter.
// YAML ids without a matching implementation are ignored (warned) at startup.
type ProviderID string

const (
	// ProviderLG is the LG Channels US provider.
	ProviderLG ProviderID = "lg"
)
