package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// ErrStaleRevision is returned by Save when the caller's revision does not
// match the current file revision (someone else saved first).
var ErrStaleRevision = errors.New("config: stale revision")

// ErrInvalid wraps validation failures of a save candidate: the ops produced a
// config that the boot load path (New) rejects. The file and runtime are
// untouched.
var ErrInvalid = errors.New("config: invalid")

// Reloader is implemented by subsystems that reconcile themselves to a new
// config snapshot after a save. Each implementation decides how: full re-arm,
// internal diff against last-applied state, or no-op.
type Reloader interface {
	Reload(ctx context.Context, cfg *Config) error
}

// ReloaderFunc adapts a function to the Reloader interface.
type ReloaderFunc func(ctx context.Context, cfg *Config) error

// Reload calls f.
func (f ReloaderFunc) Reload(ctx context.Context, cfg *Config) error { return f(ctx, cfg) }

// ReloadResult is one subsystem's outcome from a post-save kick.
type ReloadResult struct {
	Name  string `json:"name"`
	Error string `json:"error,omitempty"`
}

type namedReloader struct {
	name string
	r    Reloader
}

// Store owns the config lifecycle: the single load path (New), an atomic
// immutable snapshot for lock-free reads, comment-preserving persistence, and
// the post-save fan-out to registered Reloaders. Snapshots must never be
// mutated after Load returns.
type Store struct {
	path      string
	mu        sync.Mutex // serializes Save (and the Load inside it)
	current   atomic.Pointer[Config]
	revision  atomic.Pointer[string]
	fromFile  atomic.Bool
	reloaders []namedReloader
}

// NewStore returns a Store for the config file at path. An empty path means
// defaults + env only (saves are rejected with ErrReadOnly). Call Load before
// Current.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Current returns the immutable config snapshot (lock-free).
func (s *Store) Current() *Config { return s.current.Load() }

// FromFile reports whether the current snapshot included the YAML file.
func (s *Store) FromFile() bool { return s.fromFile.Load() }

// Path returns the config file path ("" when running without a file).
func (s *Store) Path() string { return s.path }

// Revision returns the SHA-256 of the current file bytes (optimistic
// concurrency token for Save).
func (s *Store) Revision() string {
	if r := s.revision.Load(); r != nil {
		return *r
	}
	return ""
}

// Load runs the one true load path (New) and swaps the snapshot. A missing
// file falls back to defaults + env and writes the baked-in defaults so the
// operator has an editable config.yaml (matching first-boot behavior).
func (s *Store) Load() error {
	cfg, err := New(s.path)
	fromFile := s.path != ""
	if s.path != "" && errors.Is(err, os.ErrNotExist) {
		if werr := WriteDefault(s.path); werr != nil {
			slog.Warn("config file missing and defaults not written; using defaults + env",
				"path", s.path, "err", werr)
			cfg, err = New("")
			fromFile = false
		} else {
			slog.Info("config file missing; wrote defaults", "path", s.path)
			cfg, err = New(s.path)
		}
	}
	if err != nil {
		return err
	}
	rev := s.computeRevision()
	s.current.Store(cfg)
	s.revision.Store(&rev)
	s.fromFile.Store(fromFile)
	return nil
}

// Register appends a named Reloader to the ordered post-save kick list.
// Registration must complete before the first Save (not concurrency-safe).
func (s *Store) Register(name string, r Reloader) {
	s.reloaders = append(s.reloaders, namedReloader{name: name, r: r})
}

// Save applies ops to a candidate copy of config.yaml, validates the candidate
// through the boot load path (New, env overlay included), atomically replaces
// the file (keeping .bak), reloads the snapshot, and kicks every registered
// Reloader in order. A failure at any step before the rename leaves the file
// and runtime untouched. A non-empty revision must match the current file
// revision or Save fails with ErrStaleRevision.
func (s *Store) Save(ctx context.Context, revision string, ops []PathOp) ([]ReloadResult, error) {
	if s.path == "" {
		return nil, ErrReadOnly
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if revision != "" && revision != s.Revision() {
		return nil, ErrStaleRevision
	}
	prior, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("config: read %s: %w", s.path, classify(err))
	}
	candidate, err := ApplyPathOps(prior, ops)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := s.validateCandidate(candidate); err != nil {
		return nil, err
	}
	if err := atomicWriteWithBackup(s.path, candidate, prior); err != nil {
		return nil, err
	}
	if err := s.Load(); err != nil {
		// The candidate just validated; a failure here is an I/O race.
		return nil, fmt.Errorf("config: reload after save: %w", err)
	}
	return s.kick(ctx), nil
}

// kick runs every registered Reloader against the current snapshot, logging
// failures without blocking later entries.
func (s *Store) kick(ctx context.Context) []ReloadResult {
	cfg := s.Current()
	results := make([]ReloadResult, 0, len(s.reloaders))
	for _, nr := range s.reloaders {
		result := ReloadResult{Name: nr.name}
		if err := nr.r.Reload(ctx, cfg); err != nil {
			slog.Error("config reload failed", "subsystem", nr.name, "err", err)
			result.Error = err.Error()
		}
		results = append(results, result)
	}
	return results
}

// computeRevision hashes the current file bytes (missing file hashes empty).
func (s *Store) computeRevision() string {
	var data []byte
	if s.path != "" {
		data, _ = os.ReadFile(s.path)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// validateCandidate writes candidate to a temp file next to the target and
// runs New on it so the exact boot path (env overlay and all) accepts it.
func (s *Store) validateCandidate(candidate []byte) error {
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".config-candidate-*.yaml")
	if err != nil {
		return fmt.Errorf("config: temp: %w", classify(err))
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(candidate); err != nil {
		tmp.Close()
		return fmt.Errorf("config: write candidate: %w", classify(err))
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close candidate: %w", classify(err))
	}
	if _, err := New(name); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}
