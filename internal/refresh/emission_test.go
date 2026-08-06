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
	// Excluded for group still gets a playback URL when a path exists.
	if got[1].EmittedURL != "https://up.test/b.m3u8" {
		t.Fatalf("disabled channel should still mint EmittedURL: %q", got[1].EmittedURL)
	}
}

func TestApplyEmissionPolicy(t *testing.T) {
	tests := []struct {
		name           string
		classification model.Classification
		streamURL      string // empty → default upstream without query
		policy         EmissionPolicy
		wantURL        string
		wantExcluded   bool
		wantReason     model.FilterReason
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
			name:           "session through selective proxy",
			classification: model.ClassSession,
			policy:         EmissionPolicy{ProxyBaseURL: "https://proxy.test"},
			wantURL:        "https://proxy.test/stream/lg/news.m3u8",
		},
		{
			name:           "session without proxy",
			classification: model.ClassSession,
			wantExcluded:   true,
			wantReason:     model.FilterReasonNeedsFASTProxy,
			wantDropped:    1,
		},
		{
			name:           "session through proxy all",
			classification: model.ClassSession,
			policy:         EmissionPolicy{ProxyBaseURL: "https://proxy.test", ProxyAll: true},
			wantURL:        "https://proxy.test/stream/lg/news.m3u8",
		},
		{
			name:           "xumo ssai direct",
			classification: model.ClassXumoSSAI,
			streamURL:      "https://cdn.example/hls/master.m3u8?ads.xumo_channelId=99992260&ads.channelId=99992260",
			wantURL:        "https://cdn.example/hls/master.m3u8?ads.xumo_channelId=99992260&ads.channelId=99992260",
		},
		{
			name:           "xumo ssai through proxy all",
			classification: model.ClassXumoSSAI,
			streamURL:      "https://cdn.example/hls/master.m3u8?ads.xumo_channelId=99992260&ads.channelId=99992260",
			policy:         EmissionPolicy{ProxyBaseURL: "https://proxy.test", ProxyAll: true},
			wantURL:        "https://proxy.test/stream/lg/news.m3u8",
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
			streamURL := test.streamURL
			if streamURL == "" {
				streamURL = "https://upstream.test/live.m3u8"
			}
			input := model.Channel{
				Provider:       model.ProviderLG,
				NormalizedID:   "news",
				StreamURL:      streamURL,
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

func TestApplyEmissionPolicyDeadSSAINoEmittedURL(t *testing.T) {
	got, _ := applyEmissionPolicy([]model.Channel{{
		Provider:       model.ProviderSTIRR,
		NormalizedID:   "7291",
		StreamURL:      "stirr://channel/7291",
		Classification: model.ClassStirrResolve,
		Excluded:       true,
		FilterReason:   model.FilterReasonDeadSSAI,
		FilterReasons:  []model.FilterReason{model.FilterReasonDeadSSAI},
	}}, EmissionPolicy{ProxyBaseURL: "https://proxy.test"})
	if !got[0].Excluded || got[0].FilterReason != model.FilterReasonDeadSSAI {
		t.Fatalf("expected dead SSAI exclusion: %+v", got[0])
	}
	if got[0].EmittedURL != "" {
		t.Fatalf("dead SSAI should not mint EmittedURL, got %q", got[0].EmittedURL)
	}
}

func TestApplyEmissionPolicyPreservesEarlierExclusion(t *testing.T) {
	got, stats := applyEmissionPolicy([]model.Channel{{
		Provider:       model.ProviderLG,
		NormalizedID:   "news",
		StreamURL:      "https://upstream.test/live.m3u8",
		Classification: model.ClassAmagiSSAI,
		Excluded:       true,
		FilterReason:   model.ExclusionMatched(nil),
		FilterReasons:  []model.FilterReason{model.FilterReason("exclusion matched")},
	}}, EmissionPolicy{})
	if !got[0].Excluded {
		t.Fatal("expected excluded")
	}
	if !model.HasFilterReasonKind(got[0].FilterReasons, model.FilterKindExclusion) {
		t.Fatalf("expected exclusion kept: %+v", got[0].FilterReasons)
	}
	if !model.HasFilterReason(got[0].FilterReasons, model.FilterReasonNeedsFASTProxy) {
		t.Fatalf("expected needs-proxy accumulated: %+v", got[0].FilterReasons)
	}
	if stats.NeedsProxyDropped != 1 {
		t.Fatalf("stats=%+v", stats)
	}
	if got[0].EmittedURL != "" {
		t.Fatalf("no proxy base → no EmittedURL: %q", got[0].EmittedURL)
	}
}

func TestApplyEmissionPolicyMintsURLDespiteSoftExclusion(t *testing.T) {
	got, _ := applyEmissionPolicy([]model.Channel{{
		Provider:       model.ProviderLG,
		NormalizedID:   "news",
		StreamURL:      "https://upstream.test/live.m3u8",
		Classification: model.ClassNative,
		Excluded:       true,
		FilterReason:   model.FilterReasonDuplicate,
		FilterReasons:  []model.FilterReason{model.FilterReasonDuplicate},
	}}, EmissionPolicy{ProxyBaseURL: "https://proxy.test"})
	if !got[0].Excluded || got[0].FilterReason != model.FilterReasonDuplicate {
		t.Fatalf("channel=%+v", got[0])
	}
	if got[0].EmittedURL != "https://upstream.test/live.m3u8" {
		t.Fatalf("should mint direct URL despite duplicate: %q", got[0].EmittedURL)
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
		Provider:       model.ProviderLG,
		NormalizedID:   "news",
		Classification: model.ClassNative,
		StreamURL:      "https://upstream.test/live.m3u8",
		Health:         model.ChannelHealth{Status: model.HealthDown},
	}}, EmissionPolicy{})
	if got2[0].Excluded || stats2.UnhealthyDropped != 0 {
		t.Fatalf("default must keep DOWN exported: %+v stats=%+v", got2[0], stats2)
	}
	got3, stats3 := applyEmissionPolicy([]model.Channel{{
		Provider:       model.ProviderLG,
		NormalizedID:   "news",
		StreamURL:      "https://upstream.test/live.m3u8",
		Classification: model.ClassNative,
		Health:         model.ChannelHealth{Status: model.HealthDown},
		ForceInclude:   true,
	}}, EmissionPolicy{ExcludeUnhealthy: true})
	if got3[0].Excluded || stats3.UnhealthyDropped != 0 || got3[0].EmittedURL == "" {
		t.Fatalf("force-include should emit unhealthy: %+v stats=%+v", got3[0], stats3)
	}
}

// Under proxy_all the playlist URL is stable: a NATIVE→Amagi (or legacy BEACON)
// classification flip must not rewrite the emitted stream URL (J27-29).
func TestApplyEmissionPolicyProxyAllNativeToAmagiFlipStableURL(t *testing.T) {
	policy := EmissionPolicy{ProxyBaseURL: "https://proxy.test/", ProxyAll: true}
	base := model.Channel{
		Provider:     model.ProviderLG,
		NormalizedID: "news",
		StreamURL:    "https://upstream.test/live.m3u8",
	}
	want := "https://proxy.test/stream/lg/news.m3u8"

	for _, class := range []model.Classification{
		model.ClassNative,
		model.ClassAmagiSSAI,
		model.ClassSession,
		"BEACON",
	} {
		ch := base
		ch.Classification = class
		got, stats := applyEmissionPolicy([]model.Channel{ch}, policy)
		if got[0].Excluded || stats.NeedsProxyDropped != 0 {
			t.Fatalf("class=%q excluded: %+v stats=%+v", class, got[0], stats)
		}
		if got[0].EmittedURL != want {
			t.Fatalf("class=%q EmittedURL=%q want %q", class, got[0].EmittedURL, want)
		}
		if got[0].StreamURL != base.StreamURL {
			t.Fatalf("class=%q upstream mutated: %q", class, got[0].StreamURL)
		}
	}
}
