package provider

import (
	"context"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/model"
)

// fakeProvider is a test double with fixed channels/programmes.
type fakeProvider struct {
	id         string
	Channels   []model.Channel
	Programmes []model.Programme
	Err        error
}

func (f *fakeProvider) ID() string { return f.id }

func (f *fakeProvider) Fetch(ctx context.Context) ([]model.Channel, []model.Programme, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if f.Err != nil {
		return nil, nil, f.Err
	}
	chs := append([]model.Channel(nil), f.Channels...)
	progs := append([]model.Programme(nil), f.Programmes...)
	return chs, progs, nil
}

func fakeFactory(f *fakeProvider) Factory {
	return func(id string, _ config.Provider) (Provider, error) {
		cp := *f
		cp.id = id
		return &cp, nil
	}
}
