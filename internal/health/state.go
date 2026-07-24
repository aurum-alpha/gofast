package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// scheduleFile is the on-disk shape under StatePath (restart-safe L1/L2 anchors).
type scheduleFile struct {
	Version  int       `json:"version"`
	LastL1At time.Time `json:"last_l1_at,omitempty"`
	NextL1At time.Time `json:"next_l1_at,omitempty"`
	LastL2At time.Time `json:"last_l2_at,omitempty"`
	NextL2At time.Time `json:"next_l2_at,omitempty"`
	// LastL3At and NextL3At are read-only compatibility fields for v1 files.
	LastL3At time.Time `json:"last_l3_at,omitempty"`
	NextL3At time.Time `json:"next_l3_at,omitempty"`
}

func (s *Scheduler) loadState() {
	if s == nil || s.StatePath == "" {
		return
	}
	data, err := os.ReadFile(s.StatePath)
	if err != nil {
		return
	}
	var f scheduleFile
	if err := json.Unmarshal(data, &f); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.Version < 2 {
		s.lastL2At = f.LastL2At
		s.nextL2At = f.NextL2At
		s.lastL3At = f.LastL3At
		s.nextL3At = f.NextL3At
		return
	}
	s.lastL2At = f.LastL1At
	s.nextL2At = f.NextL1At
	s.lastL3At = f.LastL2At
	s.nextL3At = f.NextL2At
}

func (s *Scheduler) saveState() {
	if s == nil || s.StatePath == "" {
		return
	}
	s.mu.Lock()
	f := scheduleFile{
		Version:  2,
		LastL1At: s.lastL2At,
		NextL1At: s.nextL2At,
		LastL2At: s.lastL3At,
		NextL2At: s.nextL3At,
	}
	s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.StatePath), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	tmp := s.StatePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, s.StatePath)
}
