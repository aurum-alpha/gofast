package groups

import (
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
)

func boolPtr(b bool) *bool { return &b }

func TestLookupOffIsLegacy(t *testing.T) {
	p := Compile(Doc{Enabled: false, Merges: []Merge{{Name: "News", Members: []string{"NEWS"}}}})
	if p.Enabled() {
		t.Fatal("policy should be disabled")
	}
	// Apply is a no-op when off: EmittedGroup stays empty.
	out := Apply([]model.Channel{{Group: "NEWS"}}, model.ProviderLG, p)
	if out[0].EmittedGroup != "" || out[0].Excluded {
		t.Fatalf("off policy mutated channel: %+v", out[0])
	}
}

func TestLookupAssignedAndUnassigned(t *testing.T) {
	p := Compile(Doc{
		Enabled: true,
		Merges:  []Merge{{Name: "News", Members: []string{"NEWS", "News & Info"}}},
	})
	// Assigned member resolves to the canonical name.
	if title, mapped, disabled := p.Lookup(model.ProviderLG, "  news  "); title != "News" || !mapped || disabled {
		t.Fatalf("assigned lookup = (%q,%v,%v)", title, mapped, disabled)
	}
	// Different upstream string that is also a member.
	if title, mapped, _ := p.Lookup(model.ProviderPluto, "News & Info"); title != "News" || !mapped {
		t.Fatalf("member lookup = (%q,%v)", title, mapped)
	}
	// Unassigned emits the bare (trimmed) upstream string, no prefix.
	if title, mapped, _ := p.Lookup(model.ProviderPluto, "  Movies "); title != "Movies" || mapped {
		t.Fatalf("unassigned lookup = (%q,%v)", title, mapped)
	}
}

func TestApplyAutoMergeAcrossProviders(t *testing.T) {
	p := Compile(Doc{Enabled: true})
	// No merges: identical bare strings collapse because EmittedGroup == upstream.
	a := Apply([]model.Channel{{Group: "News"}}, model.ProviderPluto, p)
	b := Apply([]model.Channel{{Group: "News"}}, model.ProviderSamsung, p)
	if a[0].EmittedGroup != "News" || b[0].EmittedGroup != "News" {
		t.Fatalf("auto-merge failed: %q vs %q", a[0].EmittedGroup, b[0].EmittedGroup)
	}
}

func TestDisableViaMergeEnabledFalse(t *testing.T) {
	p := Compile(Doc{
		Enabled: true,
		Merges:  []Merge{{Name: "Shopping", Members: []string{"Shopping", "QVC"}, Enabled: boolPtr(false)}},
	})
	out := Apply([]model.Channel{{Group: "QVC", Name: "QVC"}}, model.ProviderSamsung, p)
	if !out[0].Excluded {
		t.Fatal("disabled merge should exclude channel")
	}
	if out[0].FilterReason != model.DisabledGroupReason("Shopping") {
		t.Fatalf("reason = %q", out[0].FilterReason)
	}
	if out[0].EmittedGroup != "" {
		t.Fatalf("disabled channel should have no EmittedGroup, got %q", out[0].EmittedGroup)
	}
}

func TestDisableGlobalAndPerProvider(t *testing.T) {
	p := Compile(Doc{
		Enabled:  true,
		Disabled: []string{"Weather", "samsung/Local"},
	})
	// Global: any provider's Weather (case-insensitive).
	if _, _, d := p.Lookup(model.ProviderLG, "weather"); !d {
		t.Fatal("global disable should match any provider")
	}
	// Per-provider: only samsung's Local.
	if _, _, d := p.Lookup(model.ProviderSamsung, "Local"); !d {
		t.Fatal("samsung/Local should disable samsung")
	}
	if _, _, d := p.Lookup(model.ProviderPluto, "Local"); d {
		t.Fatal("samsung/Local must not disable other providers")
	}
}

func TestDisableWinsOverMerge(t *testing.T) {
	// A member is assigned to an enabled merge but also globally disabled.
	p := Compile(Doc{
		Enabled:  true,
		Merges:   []Merge{{Name: "News", Members: []string{"NEWS"}}},
		Disabled: []string{"NEWS"},
	})
	out := Apply([]model.Channel{{Group: "NEWS"}}, model.ProviderLG, p)
	if !out[0].Excluded {
		t.Fatal("disable must win over an enabled merge")
	}
	if out[0].FilterReason != model.DisabledGroupReason("News") {
		t.Fatalf("reason should use canonical name: %q", out[0].FilterReason)
	}
}

func TestApplyPreservesPriorExclusion(t *testing.T) {
	p := Compile(Doc{Enabled: true, Merges: []Merge{{Name: "News", Members: []string{"NEWS"}}}})
	in := []model.Channel{{Group: "NEWS", Excluded: true, FilterReason: model.FilterReasonDRM}}
	out := Apply(in, model.ProviderLG, p)
	if out[0].FilterReason != model.FilterReasonDRM {
		t.Fatalf("prior exclusion reason overwritten: %q", out[0].FilterReason)
	}
	if out[0].EmittedGroup != "" {
		t.Fatal("already-excluded channel should not get EmittedGroup")
	}
}
