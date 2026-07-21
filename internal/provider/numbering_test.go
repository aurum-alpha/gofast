package provider

import (
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestChannelNumberAssignmentsRemainStable(t *testing.T) {
	channels := []model.Channel{
		{NormalizedID: "alpha"},
		{NormalizedID: "native", Number: 7, OffsetNumber: 1007},
		{NormalizedID: "beta"},
	}
	assignments, err := (ChannelNumberAssignments{}).Apply(channels, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if channels[0].OffsetNumber != 5000 || channels[1].OffsetNumber != 1007 || channels[2].OffsetNumber != 5001 {
		t.Fatalf("initial numbers: %+v", channels)
	}

	reordered := []model.Channel{{NormalizedID: "gamma"}, {NormalizedID: "alpha"}}
	assignments, err = assignments.Apply(reordered, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if reordered[0].OffsetNumber != 5002 || reordered[1].OffsetNumber != 5000 {
		t.Fatalf("stable numbers after reorder/removal: %+v", reordered)
	}

	reappeared := []model.Channel{{NormalizedID: "beta"}}
	if _, err := assignments.Apply(reappeared, 7000); err != nil {
		t.Fatal(err)
	}
	if reappeared[0].OffsetNumber != 5001 {
		t.Fatalf("reappeared channel renumbered: %+v", reappeared[0])
	}
}

func TestChannelNumberAssignmentsDoNotRecycleOrMutateSource(t *testing.T) {
	original := ChannelNumberAssignments{"gone": 6000}
	channels := []model.Channel{{NormalizedID: "new"}}
	assignments, err := original.Apply(channels, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if channels[0].OffsetNumber != 6001 {
		t.Fatalf("new number = %d, want 6001", channels[0].OffsetNumber)
	}
	if len(original) != 1 {
		t.Fatalf("source map mutated: %+v", original)
	}
	if assignments["gone"] != 6000 || assignments["new"] != 6001 {
		t.Fatalf("assignments: %+v", assignments)
	}
}

func TestChannelNumberAssignmentsRejectInvalidHistory(t *testing.T) {
	tests := []ChannelNumberAssignments{
		{"": 5000},
		{"alpha": 0},
		{"alpha": 5000, "beta": 5000},
	}
	for _, assignments := range tests {
		if _, err := assignments.Apply(nil, 5000); err == nil {
			t.Fatalf("expected error for %+v", assignments)
		}
	}
}
