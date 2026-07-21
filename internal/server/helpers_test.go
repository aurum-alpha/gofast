package server_test

import (
	"context"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

// stubReader is a no-op Reader used to create enabled feeds in handler tests.
type stubReader struct{}

func (stubReader) Fetch(context.Context) (provider.Raw, error) { return nil, nil }

func (stubReader) Parse(provider.Raw) ([]model.Channel, []model.Programme, error) {
	return nil, nil, nil
}

// regWith builds a registry with the given providers enabled and each feed
// pre-populated with the matching lineup.
func regWith(settings map[model.ProviderID]model.ProviderSettings, lineups map[model.ProviderID]provider.Lineup) *provider.Registry {
	readers := map[model.ProviderID]provider.Reader{}
	for id := range settings {
		readers[id] = stubReader{}
	}
	reg := provider.NewRegistry(readers, settings)
	for id, lin := range lineups {
		if f, ok := reg.Feed(id); ok {
			f.Set(lin)
		}
	}
	return reg
}
