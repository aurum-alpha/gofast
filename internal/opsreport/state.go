package opsreport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

const (
	stateFileName = "state.json"
	dirName       = "ops_reports"
)

// Kind classifies a full report send (not Test SMTP).
type Kind string

const (
	KindOfficial Kind = "official"
	KindPreview  Kind = "preview"
	KindTest     Kind = "test"
)

// ProviderTally is durable refresh success/fail counts since last official send.
type ProviderTally struct {
	Successes uint64 `json:"successes"`
	Failures  uint64 `json:"failures"`
}

// State is persisted under {data_dir}/ops_reports/state.json.
type State struct {
	LastSuccessAt    time.Time                          `json:"last_success_at,omitempty"`
	LastSuccessLocal string                             `json:"last_success_local,omitempty"`
	LastError        string                             `json:"last_error,omitempty"`
	LastErrorAt      time.Time                          `json:"last_error_at,omitempty"`
	NextAt           time.Time                          `json:"next_at,omitempty"`
	RefreshTallies   map[model.ProviderID]ProviderTally `json:"refresh_tallies,omitempty"`
}

// stateStore guards disk state + in-memory tallies.
type stateStore struct {
	mu   sync.Mutex
	path string
	cur  State
}

func newStateStore(dataDir string) *stateStore {
	dir := filepath.Join(dataDir, dirName)
	return &stateStore{
		path: filepath.Join(dir, stateFileName),
		cur: State{
			RefreshTallies: map[model.ProviderID]ProviderTally{},
		},
	}
}

func (s *stateStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return fmt.Errorf("opsreport: parse state: %w", err)
	}
	if st.RefreshTallies == nil {
		st.RefreshTallies = map[model.ProviderID]ProviderTally{}
	}
	s.cur = st
	return nil
}

func (s *stateStore) snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneState(s.cur)
}

func (s *stateStore) lastSuccess() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur.LastSuccessAt
}

func (s *stateStore) setNext(next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur.NextAt = next.UTC()
	return s.saveLocked()
}

func (s *stateStore) recordOfficialSuccess(at time.Time, local string, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur.LastSuccessAt = at.UTC()
	s.cur.LastSuccessLocal = local
	s.cur.LastError = ""
	s.cur.LastErrorAt = time.Time{}
	s.cur.NextAt = next.UTC()
	s.cur.RefreshTallies = map[model.ProviderID]ProviderTally{}
	return s.saveLocked()
}

func (s *stateStore) recordOfficialError(at time.Time, msg string, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur.LastError = msg
	s.cur.LastErrorAt = at.UTC()
	s.cur.NextAt = next.UTC()
	return s.saveLocked()
}

func (s *stateStore) Inc(provider model.ProviderID, ok bool) {
	if s == nil || provider == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur.RefreshTallies == nil {
		s.cur.RefreshTallies = map[model.ProviderID]ProviderTally{}
	}
	t := s.cur.RefreshTallies[provider]
	if ok {
		t.Successes++
	} else {
		t.Failures++
	}
	s.cur.RefreshTallies[provider] = t
	_ = s.saveLocked()
}

func (s *stateStore) tallies() map[model.ProviderID]ProviderTally {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[model.ProviderID]ProviderTally, len(s.cur.RefreshTallies))
	for k, v := range s.cur.RefreshTallies {
		out[k] = v
	}
	return out
}

func (s *stateStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.cur, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func cloneState(in State) State {
	out := in
	out.RefreshTallies = make(map[model.ProviderID]ProviderTally, len(in.RefreshTallies))
	for k, v := range in.RefreshTallies {
		out.RefreshTallies[k] = v
	}
	return out
}

// Dir returns {dataDir}/ops_reports.
func Dir(dataDir string) string {
	return filepath.Join(dataDir, dirName)
}
