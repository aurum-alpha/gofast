package mjh_test

import (
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/provider/pluto"
	"github.com/j27-aurum/gofast/internal/provider/roku"
	"github.com/j27-aurum/gofast/internal/provider/samsung"
)

func TestProviderDefaultsAndConstructors(t *testing.T) {
	tests := []struct {
		id     model.ProviderID
		offset int
		region string
		slug   string
		reader func() provider.Reader
	}{
		{model.ProviderPluto, 2000, "us", "plu-{id}.m3u8", func() provider.Reader {
			settings := pluto.DefaultSettings()
			return pluto.New(settings, nil)
		}},
		{model.ProviderSamsung, 3000, "us", "", func() provider.Reader {
			settings := samsung.DefaultSettings()
			return samsung.New(settings, nil)
		}},
		{model.ProviderRoku, 4000, "", "rok-{id}.m3u8", func() provider.Reader {
			settings := roku.DefaultSettings()
			return roku.New(settings, nil)
		}},
	}
	defaults := []model.ProviderSettings{
		pluto.DefaultSettings(),
		samsung.DefaultSettings(),
		roku.DefaultSettings(),
	}
	for index, test := range tests {
		settings := defaults[index]
		if settings.ID != test.id || settings.ChannelNumberOffset != test.offset || settings.Region != test.region || settings.SlugTemplate != test.slug {
			t.Errorf("%s defaults: %+v", test.id, settings)
		}
		if test.reader() == nil {
			t.Errorf("%s constructor returned nil", test.id)
		}
	}
}
