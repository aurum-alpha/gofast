package refresh

import (
	"sync"

	"github.com/j27-aurum/gofast/internal/version"
)

// Status is process-wide boot / logo-cache progress for GET /api/status.
type Status struct {
	mu            sync.RWMutex
	logosRunning  bool
	logosDone     int
	logosTotal    int
	logosProvider string
}

// LogosView is the JSON shape under status.logos.
type LogosView struct {
	Running  bool   `json:"running"`
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	Provider string `json:"provider,omitempty"`
}

// View is the GET /api/status JSON envelope.
type View struct {
	Ready   bool         `json:"ready"`
	Version version.Info `json:"version"`
	Logos   LogosView    `json:"logos"`
}

// Snapshot returns a consistent copy for HTTP handlers.
func (s *Status) Snapshot() View {
	if s == nil {
		return View{Ready: true, Version: version.Current()}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return View{
		Ready:   !s.logosRunning,
		Version: version.Current(),
		Logos: LogosView{
			Running:  s.logosRunning,
			Done:     s.logosDone,
			Total:    s.logosTotal,
			Provider: s.logosProvider,
		},
	}
}

// SetLogos updates logo-cache progress (running=false clears the provider label).
func (s *Status) SetLogos(running bool, done, total int, provider string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logosRunning = running
	s.logosDone = done
	s.logosTotal = total
	if !running {
		s.logosProvider = ""
		return
	}
	s.logosProvider = provider
}
