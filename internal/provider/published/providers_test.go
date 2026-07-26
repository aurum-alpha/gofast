package published_test

import (
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/provider/distrotv"
	"github.com/j27-aurum/gofast/internal/provider/localnow"
	"github.com/j27-aurum/gofast/internal/provider/tubi"
	"github.com/j27-aurum/gofast/internal/provider/xumo"
)

func TestProviderDefaultsAndConstructors(t *testing.T) {
	tests := []struct {
		settings model.ProviderSettings
		id       model.ProviderID
		base     int
		minimum  int
		reader   func(model.ProviderSettings) provider.Reader
	}{
		{distrotv.DefaultSettings(), model.ProviderDistroTV, 6000, 50, func(settings model.ProviderSettings) provider.Reader {
			return distrotv.New(settings, nil)
		}},
		{localnow.DefaultSettings(), model.ProviderLocalNow, 7000, 10, func(settings model.ProviderSettings) provider.Reader {
			return localnow.New(settings, nil)
		}},
		{tubi.DefaultSettings(), model.ProviderTubi, 9000, 100, func(settings model.ProviderSettings) provider.Reader {
			return tubi.New(settings, nil)
		}},
		{xumo.DefaultSettings(), model.ProviderXumo, 5000, 100, func(settings model.ProviderSettings) provider.Reader {
			return xumo.New(settings, nil)
		}},
	}
	for _, test := range tests {
		if test.settings.ID != test.id || test.settings.SynthesizeChannelNumbers != test.base || test.settings.MinChannels != test.minimum {
			t.Errorf("%s defaults: %+v", test.id, test.settings)
		}
		if test.id == model.ProviderLocalNow && test.settings.UserAgent == "" {
			t.Error("LocalNow browser user-agent is required")
		}
		if test.reader(test.settings) == nil {
			t.Errorf("%s constructor returned nil", test.id)
		}
	}
}
