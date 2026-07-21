// Package snapshot holds last-good emitted playlists and channel rows per provider.
package snapshot

import (
	"sort"
	"sync"

	"github.com/j27-aurum/gofast/internal/model"
)

// Snapshot is one provider's published M3U/XMLTV bytes plus channel/programme
// rows for the API/UI.
type Snapshot struct {
	ProviderID     string
	Label          string
	M3U            []byte
	XML            []byte
	Channels       []model.Channel
	Programmes     []model.Programme
	ChannelCount   int
	ProgrammeCount int
}

// Store is an in-memory map of provider id → last-good snapshot.
type Store struct {
	mu   sync.RWMutex
	byID map[string]Snapshot
}

// NewStore returns an empty snapshot store.
func NewStore() *Store {
	return &Store{byID: make(map[string]Snapshot)}
}

// Get returns a copy of the snapshot for id.
func (s *Store) Get(id string) (Snapshot, bool) {
	if s == nil {
		return Snapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.byID[id]
	return snap, ok
}

// ListChannels returns channels from all snapshots, sorted by provider then number then id.
func (s *Store) ListChannels() ChannelList {
	if s == nil {
		return ChannelList{Channels: []model.Channel{}}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := 0
	for _, snap := range s.byID {
		n += len(snap.Channels)
	}
	out := make([]model.Channel, 0, n)
	for _, snap := range s.byID {
		out = append(out, snap.Channels...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return lineupLess(out[i].Provider, out[j].Provider, out[i].OffsetNumber, out[j].OffsetNumber, out[i].NormalizedID, out[j].NormalizedID)
	})
	return ChannelList{Channels: out}
}

// Snapshots returns a copy of every provider's snapshot, sorted by provider id.
// Used to build combined/aggregated outputs (e.g. the guide XMLTV).
func (s *Store) Snapshots() []Snapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Snapshot, 0, len(s.byID))
	for _, snap := range s.byID {
		out = append(out, snap)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ProviderID < out[j].ProviderID
	})
	return out
}

// lineupLess orders lineup rows by provider, then export channel number
// (unnumbered last), then normalized id.
func lineupLess(provI, provJ string, numI, numJ int, idI, idJ string) bool {
	if provI != provJ {
		return provI < provJ
	}
	// Unnumbered channels sort after numbered ones.
	if numI == 0 {
		numI = 1 << 30
	}
	if numJ == 0 {
		numJ = 1 << 30
	}
	if numI != numJ {
		return numI < numJ
	}
	return idI < idJ
}

// Put replaces the snapshot for a provider id.
func (s *Store) Put(snap Snapshot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[snap.ProviderID] = snap
}

// ChannelList is the GET /api/channels JSON envelope.
type ChannelList struct {
	Channels []model.Channel `json:"channels"`
}
