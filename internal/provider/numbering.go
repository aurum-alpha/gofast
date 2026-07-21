package provider

import (
	"fmt"

	"github.com/j27-aurum/gofast/internal/model"
)

// ChannelNumberAssignments retains first-seen synthetic numbers forever,
// including IDs absent from the current lineup.
type ChannelNumberAssignments map[string]int

// Apply clones the historical map, assigns stable numbers to currently
// numberless channels, and writes those numbers to Channel.OffsetNumber.
func (a ChannelNumberAssignments) Apply(channels []model.Channel, base int) (ChannelNumberAssignments, error) {
	assignments := a.Clone()
	used := make(map[int]string, len(assignments))
	highest := 0
	for id, number := range assignments {
		if id == "" || number <= 0 {
			return nil, fmt.Errorf("invalid synthetic channel number assignment %q=%d", id, number)
		}
		if previous, duplicate := used[number]; duplicate {
			return nil, fmt.Errorf("duplicate synthetic channel number %d for %q and %q", number, previous, id)
		}
		used[number] = id
		if number > highest {
			highest = number
		}
	}
	if base <= 0 {
		return assignments, nil
	}
	next := base
	if highest >= next {
		next = highest + 1
	}
	for index := range channels {
		channel := &channels[index]
		if channel.Number > 0 || channel.NormalizedID == "" {
			continue
		}
		if number, ok := assignments[channel.NormalizedID]; ok {
			channel.OffsetNumber = number
			continue
		}
		if next <= 0 {
			return nil, fmt.Errorf("synthetic channel number overflow")
		}
		assignments[channel.NormalizedID] = next
		channel.OffsetNumber = next
		next++
	}
	return assignments, nil
}

// Clone returns an independent copy.
func (a ChannelNumberAssignments) Clone() ChannelNumberAssignments {
	if len(a) == 0 {
		return make(ChannelNumberAssignments)
	}
	cloned := make(ChannelNumberAssignments, len(a))
	for id, number := range a {
		cloned[id] = number
	}
	return cloned
}
