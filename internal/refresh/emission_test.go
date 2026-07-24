package refresh

import (
	"testing"

	"github.com/j27-aurum/gofast/internal/groups"
	"github.com/j27-aurum/gofast/internal/model"
)

// TestGroupPolicyThenEmission verifies the prepare() ordering: group taxonomy
// marks disabled channels excluded (which emission preserves) and sets
// EmittedGroup on the survivors before emission runs.
func TestGroupPolicyThenEmission(t *testing.T) {
	policy := groups.Compile(groups.Doc{
		Enabled:  true,
		Merges:   []groups.Merge{{Name: "News", Members: []string{"NEWS"}}},
		Disabled: []string{"Shopping"},
	})
	in := []model.Channel{
		{Provider: model.ProviderLG, NormalizedID: "a", StreamURL: "https://up.test/a.m3u8", Classification: model.ClassNative, Group: "NEWS"},
		{Provider: model.ProviderLG, NormalizedID: "b", StreamURL: "https://up.test/b.m3u8", Classification: model.ClassNative, Group: "Shopping"},
	}
	afterGroups := groups.Apply(in, model.ProviderLG, policy)
	got, _ := applyEmissionPolicy(afterGroups, EmissionPolicy{})

	if got[0].EmittedGroup != "News" || got[0].Excluded {
		t.Fatalf("news channel = %+v", got[0])
	}
	if got[0].EmittedURL != "https://up.test/a.m3u8" {
		t.Fatalf("news channel should still emit a URL: %q", got[0].EmittedURL)
	}
	if !got[1].Excluded || got[1].FilterReason != model.DisabledGroupReason("Shopping") {
		t.Fatalf("shopping channel = %+v", got[1])
	}
	if got[1].EmittedURL != "" {
		t.Fatalf("disabled channel must not emit a URL: %q", got[1].EmittedURL)
	}
}

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
			name:           "amagi through selective proxy",
			classification: model.ClassAmagiSSAI,
			policy:         EmissionPolicy{ProxyBaseURL: "https://proxy.test"},
			wantURL:        "https://proxy.test/stream/lg/news.m3u8",
		},
		{
			name:           "legacy BEACON through selective proxy",
			classification: "BEACON",
			policy:         EmissionPolicy{ProxyBaseURL: "https://proxy.test"},
			wantURL:        "https://proxy.test/stream/lg/news.m3u8",
		},
		{
			name:           "amagi without proxy",
			classification: model.ClassAmagiSSAI,
			wantExcluded:   true,
			wantReason:     model.FilterReasonNeedsFASTProxy,
			wantDropped:    1,
		},
		{
			name:           "session direct (not Amagi proxy)",
			classification: model.ClassSession,
			wantURL:        "https://upstream.test/live.m3u8",
		},
		{
			name:           "session does not need Amagi proxy",
			classification: model.ClassSession,
			policy:         EmissionPolicy{},
			wantURL:        "https://upstream.test/live.m3u8",
		},
		{
			name:           "xumo ssai direct",
			classification: model.ClassXumoSSAI,
			wantURL:        "https://upstream.test/live.m3u8",
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
		Classification: model.ClassAmagiSSAI,
		Excluded:       true,
		FilterReason:   "configured exclusion",
	}}, EmissionPolicy{})
	if !got[0].Excluded || got[0].FilterReason != "configured exclusion" || stats.NeedsProxyDropped != 0 {
		t.Fatalf("channel=%+v stats=%+v", got[0], stats)
	}
}

func TestApplyEmissionPolicyExcludeUnhealthy(t *testing.T) {
	got, stats := applyEmissionPolicy([]model.Channel{{
		Provider:       model.ProviderLG,
		NormalizedID:   "news",
		StreamURL:      "https://upstream.test/live.m3u8",
		Classification: model.ClassNative,
		Health:         model.ChannelHealth{Status: model.HealthDown},
	}}, EmissionPolicy{ExcludeUnhealthy: true})
	if !got[0].Excluded || got[0].FilterReason != model.FilterReasonUnhealthy || stats.UnhealthyDropped != 1 {
		t.Fatalf("channel=%+v stats=%+v", got[0], stats)
	}
	got2, stats2 := applyEmissionPolicy([]model.Channel{{
		Classification: model.ClassNative,
		StreamURL:      "https://upstream.test/live.m3u8",
		Health:         model.ChannelHealth{Status: model.HealthDown},
	}}, EmissionPolicy{})
	if got2[0].Excluded || stats2.UnhealthyDropped != 0 {
		t.Fatalf("default must keep DOWN exported: %+v stats=%+v", got2[0], stats2)
	}
}
