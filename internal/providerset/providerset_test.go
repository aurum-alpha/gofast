package providerset

import (
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"gopkg.in/yaml.v3"
)

var allIDs = []model.ProviderID{
	model.ProviderDistroTV,
	model.ProviderLG,
	model.ProviderLocalNow,
	model.ProviderPlex,
	model.ProviderPluto,
	model.ProviderRoku,
	model.ProviderSamsung,
	model.ProviderTubi,
	model.ProviderXumo,
}

func TestSettingsRequireProviderBlock(t *testing.T) {
	settings := Settings(nil)
	readers := Readers(settings, nil)
	if len(settings) != len(allIDs) {
		t.Fatalf("known settings: %+v", settings)
	}
	for _, id := range allIDs {
		if settings[id].IsEnabled() {
			t.Errorf("%s should be disabled without a YAML block", id)
		}
		if _, ok := readers[id]; ok {
			t.Errorf("%s reader should not be wired", id)
		}
	}
	registry := provider.NewRegistry(readers, settings)
	got := registry.Providers().Providers
	if len(got) != len(allIDs) {
		t.Fatalf("providers: %+v", got)
	}
	for index, id := range allIDs {
		if got[index].ID != id {
			t.Errorf("provider[%d] = %q want %q", index, got[index].ID, id)
		}
	}
}

func TestSettingsHonorPresenceAndExplicitFalse(t *testing.T) {
	disabled := false
	overlays := map[model.ProviderID]model.ProviderSettings{
		model.ProviderDistroTV: {},
		model.ProviderLG:       {},
		model.ProviderLocalNow: {},
		model.ProviderPlex:     {},
		model.ProviderPluto:    {},
		model.ProviderRoku:     {},
		model.ProviderSamsung:  {Enabled: &disabled},
		model.ProviderXumo:     {Enabled: &disabled},
	}
	settings := Settings(overlays)
	readers := Readers(settings, nil)
	for _, id := range []model.ProviderID{
		model.ProviderDistroTV,
		model.ProviderLG,
		model.ProviderLocalNow,
		model.ProviderPlex,
		model.ProviderPluto,
		model.ProviderRoku,
	} {
		if !settings[id].IsEnabled() {
			t.Errorf("%s should default enabled when configured", id)
		}
		if _, ok := readers[id]; !ok {
			t.Errorf("%s reader not wired", id)
		}
	}
	for _, id := range []model.ProviderID{model.ProviderSamsung, model.ProviderXumo} {
		if settings[id].IsEnabled() {
			t.Errorf("explicit false should disable %s", id)
		}
		if _, ok := readers[id]; ok {
			t.Errorf("disabled %s reader should not be wired", id)
		}
	}
}

func TestSettingsHonorExplicitZeroChannelNumberOffset(t *testing.T) {
	var overlay model.ProviderSettings
	if err := yaml.Unmarshal([]byte("channel_number_offset: 0\n"), &overlay); err != nil {
		t.Fatal(err)
	}
	settings := Settings(map[model.ProviderID]model.ProviderSettings{
		model.ProviderLG: overlay,
	})
	if got := settings[model.ProviderLG].ChannelNumberOffset; got != 0 {
		t.Fatalf("effective LG offset: got %d want 0 (default is 1000)", got)
	}
}

func TestKnownAndFieldSupport(t *testing.T) {
	known := Known()
	if len(known) != len(allIDs) {
		t.Fatalf("Known() = %v", known)
	}
	for i, id := range allIDs {
		if known[i] != id {
			t.Fatalf("Known()[%d] = %q want %q", i, known[i], id)
		}
	}
	for _, id := range allIDs {
		if len(FieldSupport(id)) == 0 {
			t.Errorf("%s has no field support listed", id)
		}
	}
	if FieldSupport("nope") != nil {
		t.Fatal("unknown id should have nil field support")
	}
	if _, ok := Reader("nope", model.ProviderSettings{}, nil); ok {
		t.Fatal("unknown id should have no reader")
	}
}
