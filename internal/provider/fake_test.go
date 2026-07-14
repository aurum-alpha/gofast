package provider

import (
	"context"

	"github.com/j27-aurum/gofast/internal/model"
)

// fakeReader is a test double with fixed channels/programmes.
type fakeReader struct {
	id         string
	Channels   []model.Channel
	Programmes []model.Programme
	Err        error
}

func (f *fakeReader) ID() string { return f.id }

func (f *fakeReader) Fetch(ctx context.Context) ([]model.Channel, []model.Programme, error) {
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
