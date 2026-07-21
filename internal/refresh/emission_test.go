package refresh

import (
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestApplyEmissionPolicy(t *testing.T) {
	tests := []struct {
		name           string
		classification model.Classification
		policy         EmissionPolicy
		wantURL        string
		wantExcluded   bool
		wantReason     string
		wantDropped    int
	}{
		{
			name:           "native direct",
			classification: model.ClassNative,
			wantURL:        "https://upstream.test/live.m3u8",
		},
		{
			name:           "native through proxy all",
			classification: model.ClassNative,
			policy:         EmissionPolicy{ProxyBaseURL: "https://proxy.test", ProxyAll: true},
			wantURL:        "https://proxy.test/stream/lg/news.m3u8",
		},
		{
			name:           "beacon through selective proxy",
			classification: model.ClassBeacon,
			policy:         EmissionPolicy{ProxyBaseURL: "https://proxy.test"},
			wantURL:        "https://proxy.test/stream/lg/news.m3u8",
		},
		{
			name:           "beacon without proxy",
			classification: model.ClassBeacon,
			wantExcluded:   true,
			wantReason:     model.FilterReasonNeedsFASTProxy,
			wantDropped:    1,
		},
		{
			name:           "drm always excluded",
			classification: model.ClassDRM,
			policy:         EmissionPolicy{ProxyBaseURL: "https://proxy.test", ProxyAll: true},
			wantExcluded:   true,
			wantReason:     model.FilterReasonDRM,
		},
		{
			name:    "unclassified remains direct",
			wantURL: "https://upstream.test/live.m3u8",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := model.Channel{
				Provider:       model.ProviderLG,
				NormalizedID:   "news",
				StreamURL:      "https://upstream.test/live.m3u8",
				Classification: test.classification,
			}
			got, stats := applyEmissionPolicy([]model.Channel{input}, test.policy)
			if got[0].EmittedURL != test.wantURL || got[0].Excluded != test.wantExcluded ||
				got[0].FilterReason != test.wantReason || stats.NeedsProxyDropped != test.wantDropped {
				t.Fatalf("channel=%+v stats=%+v", got[0], stats)
			}
			if got[0].StreamURL != input.StreamURL {
				t.Fatalf("upstream URL mutated: %q", got[0].StreamURL)
			}
		})
	}
}

func TestApplyEmissionPolicyPreservesEarlierExclusion(t *testing.T) {
	got, stats := applyEmissionPolicy([]model.Channel{{
		Classification: model.ClassBeacon,
		Excluded:       true,
		FilterReason:   "configured exclusion",
	}}, EmissionPolicy{})
	if !got[0].Excluded || got[0].FilterReason != "configured exclusion" || stats.NeedsProxyDropped != 0 {
		t.Fatalf("channel=%+v stats=%+v", got[0], stats)
	}
}
