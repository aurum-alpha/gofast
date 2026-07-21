package provider

import (
	"context"

	"github.com/j27-aurum/gofast/internal/model"
)

// fakeReader is a minimal Reader used to create enabled feeds in tests.
type fakeReader struct{}

func (fakeReader) Fetch(context.Context) ([]byte, error) { return nil, nil }

func (fakeReader) Parse([]byte) ([]model.Channel, []model.Programme, error) {
	return nil, nil, nil
}
