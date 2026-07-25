// Package proxyactivity persists FASTProxy telemetry that gen shows on Status.
// Proxy itself stays headless; this SQLite DB under {data_dir}/cache is the
// control-plane glass for rewrite/segment activity (not the health FSM).
package proxyactivity

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	fileName   = "proxy_activity.db"
	retention  = 48 * time.Hour
	pruneEvery = time.Minute
	recentMax  = 100
)

// Event is one ingested proxy telemetry row.
type Event struct {
	Kind       string          `json:"kind"`
	At         time.Time       `json:"at"`
	Provider   string          `json:"provider,omitempty"`
	ChannelID  string          `json:"channel_id,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	Message    string          `json:"message,omitempty"`
	Status     int             `json:"status,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
	Bytes      int64           `json:"bytes,omitempty"`
	Attrs      json.RawMessage `json:"attrs,omitempty"`
}

// Snapshot is the latest live view reported by a proxy process.
type Snapshot struct {
	At              time.Time `json:"at"`
	ProxyID         string    `json:"proxy_id"`
	ActiveSessions  int       `json:"active_sessions"`
	ActiveSegTokens int       `json:"active_seg_tokens"`
	StreamOpens     uint64    `json:"stream_opens"`
	Stream302s      uint64    `json:"stream_302s"`
	PlaylistOK      uint64    `json:"playlist_ok"`
	PlaylistFail    uint64    `json:"playlist_fail"`
	SegOK           uint64    `json:"seg_ok"`
	SegFail         uint64    `json:"seg_fail"`
	SegBytes        uint64    `json:"seg_bytes"`
	EventsDropped   uint64    `json:"events_dropped"`
}

// Status is the API view for the Status Proxy glass.
type Status struct {
	Snapshot   *Snapshot `json:"snapshot,omitempty"`
	Heartbeat  time.Time `json:"heartbeat,omitempty"`
	Stale      bool      `json:"stale"`
	Recent     []Event   `json:"recent"`
	RecentFail []Event   `json:"recent_failures"`
}

// Store is SQLite-backed proxy activity under cacheDir/proxy_activity.db.
type Store struct {
	db *sql.DB

	mu        sync.Mutex
	lastPrune time.Time
}

// Open creates/opens the activity DB.
func Open(cacheDir string) (*Store, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("proxyactivity: mkdir: %w", err)
	}
	path := filepath.Join(cacheDir, fileName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("proxyactivity: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	_ = s.prune(time.Now().UTC())
	return s, nil
}

// Close closes the DB.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS proxy_event (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  at TEXT NOT NULL,
  provider TEXT NOT NULL DEFAULT '',
  channel_id TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  status INTEGER NOT NULL DEFAULT 0,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  bytes INTEGER NOT NULL DEFAULT 0,
  attrs TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS proxy_event_at ON proxy_event(at DESC);
CREATE INDEX IF NOT EXISTS proxy_event_kind_at ON proxy_event(kind, at DESC);

CREATE TABLE IF NOT EXISTS proxy_snapshot (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  proxy_id TEXT NOT NULL,
  at TEXT NOT NULL,
  json TEXT NOT NULL
);
`)
	if err != nil {
		return fmt.Errorf("proxyactivity: migrate: %w", err)
	}
	return nil
}

// IngestBatch appends events and optionally replaces the current snapshot.
func (s *Store) IngestBatch(proxyID string, events []Event, snap *Snapshot) error {
	if s == nil {
		return fmt.Errorf("proxyactivity: nil store")
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, ev := range events {
		if ev.At.IsZero() {
			ev.At = now
		}
		attrs := ""
		if len(ev.Attrs) > 0 {
			attrs = string(ev.Attrs)
		}
		_, err := tx.Exec(`
INSERT INTO proxy_event(kind, at, provider, channel_id, reason, message, status, duration_ms, bytes, attrs)
VALUES(?,?,?,?,?,?,?,?,?,?)`,
			ev.Kind, ev.At.UTC().Format(time.RFC3339Nano),
			ev.Provider, ev.ChannelID, ev.Reason, ev.Message,
			ev.Status, ev.DurationMS, ev.Bytes, attrs)
		if err != nil {
			return fmt.Errorf("proxyactivity: insert event: %w", err)
		}
	}
	if snap != nil {
		if snap.At.IsZero() {
			snap.At = now
		}
		if proxyID != "" {
			snap.ProxyID = proxyID
		}
		raw, err := json.Marshal(snap)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`
INSERT INTO proxy_snapshot(id, proxy_id, at, json) VALUES(1,?,?,?)
ON CONFLICT(id) DO UPDATE SET proxy_id=excluded.proxy_id, at=excluded.at, json=excluded.json`,
			snap.ProxyID, snap.At.UTC().Format(time.RFC3339Nano), string(raw))
		if err != nil {
			return fmt.Errorf("proxyactivity: upsert snapshot: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.prune(now)
	return nil
}

// StatusView returns snapshot + recent events for the UI.
func (s *Store) StatusView() (Status, error) {
	var out Status
	if s == nil {
		return out, fmt.Errorf("proxyactivity: nil store")
	}
	row := s.db.QueryRow(`SELECT proxy_id, at, json FROM proxy_snapshot WHERE id = 1`)
	var proxyID, atStr, raw string
	err := row.Scan(&proxyID, &atStr, &raw)
	if err == nil {
		var snap Snapshot
		if json.Unmarshal([]byte(raw), &snap) == nil {
			if snap.ProxyID == "" {
				snap.ProxyID = proxyID
			}
			out.Snapshot = &snap
		}
		if t, e := time.Parse(time.RFC3339Nano, atStr); e == nil {
			out.Heartbeat = t
			out.Stale = time.Since(t) > 2*time.Minute
		}
	} else if err != sql.ErrNoRows {
		return out, err
	} else {
		out.Stale = true
	}

	recent, err := s.recent("", recentMax)
	if err != nil {
		return out, err
	}
	out.Recent = recent
	fails, err := s.recentFailures(40)
	if err != nil {
		return out, err
	}
	out.RecentFail = fails
	return out, nil
}

func (s *Store) recent(kind string, limit int) ([]Event, error) {
	if limit <= 0 || limit > recentMax {
		limit = recentMax
	}
	var rows *sql.Rows
	var err error
	if kind == "" {
		rows, err = s.db.Query(`
SELECT kind, at, provider, channel_id, reason, message, status, duration_ms, bytes, attrs
FROM proxy_event ORDER BY at DESC LIMIT ?`, limit)
	} else {
		rows, err = s.db.Query(`
SELECT kind, at, provider, channel_id, reason, message, status, duration_ms, bytes, attrs
FROM proxy_event WHERE kind = ? ORDER BY at DESC LIMIT ?`, kind, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) recentFailures(limit int) ([]Event, error) {
	rows, err := s.db.Query(`
SELECT kind, at, provider, channel_id, reason, message, status, duration_ms, bytes, attrs
FROM proxy_event
WHERE kind IN ('playlist_fail','seg_fail','origin_miss')
ORDER BY at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	var out []Event
	for rows.Next() {
		var ev Event
		var atStr, attrs string
		if err := rows.Scan(&ev.Kind, &atStr, &ev.Provider, &ev.ChannelID, &ev.Reason, &ev.Message,
			&ev.Status, &ev.DurationMS, &ev.Bytes, &attrs); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, atStr); err == nil {
			ev.At = t
		}
		if attrs != "" {
			ev.Attrs = json.RawMessage(attrs)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) prune(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lastPrune.IsZero() && now.Sub(s.lastPrune) < pruneEvery {
		return nil
	}
	s.lastPrune = now
	cutoff := now.Add(-retention).Format(time.RFC3339Nano)
	_, err := s.db.Exec(`DELETE FROM proxy_event WHERE at < ?`, cutoff)
	return err
}
