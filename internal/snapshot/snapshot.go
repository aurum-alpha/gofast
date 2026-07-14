// Package snapshot holds last-good emitted playlists per provider.
package snapshot

import "sync"

// Snapshot is one provider's published M3U/XMLTV bytes.
type Snapshot struct {
	ProviderID   string
	M3U          []byte
	XML          []byte
	ChannelCount int
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

// Put replaces the snapshot for a provider id.
func (s *Store) Put(snap Snapshot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[snap.ProviderID] = snap
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
