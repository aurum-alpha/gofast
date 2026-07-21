package main

import (
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

func TestKnownProviderWiringRequiresProviderBlock(t *testing.T) {
	settings := knownProviderSettings(nil)
	readers := knownProviderReaders(settings, nil)
	if len(settings) != 7 {
		t.Fatalf("known settings: %+v", settings)
	}
	if _, ok := readers[model.ProviderLG]; !ok {
		t.Fatal("LG should preserve existing default enablement")
	}
	for _, id := range []model.ProviderID{
		model.ProviderDistroTV,
		model.ProviderLocalNow,
		model.ProviderPluto,
		model.ProviderRoku,
		model.ProviderSamsung,
		model.ProviderXumo,
	} {
		if settings[id].IsEnabled() {
			t.Errorf("%s should be disabled without a YAML block", id)
		}
		if _, ok := readers[id]; ok {
			t.Errorf("%s reader should not be wired", id)
		}
	}
	registry := provider.NewRegistry(readers, settings)
	got := registry.Providers().Providers
	want := []model.ProviderID{
		model.ProviderDistroTV,
		model.ProviderLG,
		model.ProviderLocalNow,
		model.ProviderPluto,
		model.ProviderRoku,
		model.ProviderSamsung,
		model.ProviderXumo,
	}
	if len(got) != len(want) {
		t.Fatalf("providers: %+v", got)
	}
	for index, id := range want {
		if got[index].ID != id {
			t.Errorf("provider[%d] = %q want %q", index, got[index].ID, id)
		}
	}
}

func TestKnownProviderWiringHonorsPresenceAndExplicitFalse(t *testing.T) {
	disabled := false
	overlays := map[model.ProviderID]model.ProviderSettings{
		model.ProviderDistroTV: {},
		model.ProviderLocalNow: {},
		model.ProviderPluto:    {},
		model.ProviderRoku:     {},
		model.ProviderSamsung:  {Enabled: &disabled},
		model.ProviderXumo:     {Enabled: &disabled},
	}
	settings := knownProviderSettings(overlays)
	readers := knownProviderReaders(settings, nil)
	for _, id := range []model.ProviderID{
		model.ProviderDistroTV,
		model.ProviderLocalNow,
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
	if settings[model.ProviderSamsung].IsEnabled() {
		t.Fatal("explicit false should disable Samsung")
	}
	if _, ok := readers[model.ProviderSamsung]; ok {
		t.Fatal("disabled Samsung reader should not be wired")
	}
	if settings[model.ProviderXumo].IsEnabled() {
		t.Fatal("explicit false should disable Xumo")
	}
	if _, ok := readers[model.ProviderXumo]; ok {
		t.Fatal("disabled Xumo reader should not be wired")
	}
}
