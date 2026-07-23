package channelattr

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
	_ "modernc.org/sqlite"
)

const (
	dirName  = "channelattr"
	fileName = "attr.db"
)

// Store is SQLite-backed current + history for channel attributes.
type Store struct {
	db    *sql.DB
	mu    sync.RWMutex
	byKey map[string]entry // provider\x00channel\x00kind
}

type entry struct {
	value  json.RawMessage
	at     time.Time
	source string
}

// Open creates/opens attr.db under dataDir/channelattr/.
func Open(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("channelattr: mkdir: %w", err)
	}
	path := filepath.Join(dir, fileName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("channelattr: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, byKey: make(map[string]entry)}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS channel_attr_current (
  provider   TEXT NOT NULL,
  channel_id TEXT NOT NULL,
  kind       TEXT NOT NULL,
  value      TEXT NOT NULL,
  at         TEXT NOT NULL,
  source     TEXT,
  PRIMARY KEY (provider, channel_id, kind)
);
CREATE TABLE IF NOT EXISTS channel_attr_events (
  id         INTEGER PRIMARY KEY,
  provider   TEXT NOT NULL,
  channel_id TEXT NOT NULL,
  kind       TEXT NOT NULL,
  value      TEXT NOT NULL,
  at         TEXT NOT NULL,
  source     TEXT
);
CREATE INDEX IF NOT EXISTS channel_attr_events_lookup
  ON channel_attr_events (provider, channel_id, kind, at);
`)
	if err != nil {
		return fmt.Errorf("channelattr: migrate: %w", err)
	}
	return nil
}

// LoadCurrent loads channel_attr_current into memory (O(attrs), not history).
func (s *Store) LoadCurrent() error {
	rows, err := s.db.Query(`SELECT provider, channel_id, kind, value, at, source FROM channel_attr_current`)
	if err != nil {
		return fmt.Errorf("channelattr: load current: %w", err)
	}
	defer rows.Close()

	next := make(map[string]entry)
	for rows.Next() {
		var provider, channelID, kind, value, atStr, source string
		if err := rows.Scan(&provider, &channelID, &kind, &value, &atStr, &source); err != nil {
			return fmt.Errorf("channelattr: scan current: %w", err)
		}
		at, err := time.Parse(time.RFC3339Nano, atStr)
		if err != nil {
			at, err = time.Parse(time.RFC3339, atStr)
			if err != nil {
				slog.Warn("channelattr: bad current at", "provider", provider, "channel", channelID, "err", err)
				continue
			}
		}
		next[key(model.ProviderID(provider), channelID, Kind(kind))] = entry{
			value:  json.RawMessage(value),
			at:     at,
			source: source,
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.byKey = next
	s.mu.Unlock()
	return nil
}

// Handle appends history and upserts current for one event.
func (s *Store) Handle(ctx context.Context, ev Event) error {
	if ev.Provider == "" || ev.ChannelID == "" || ev.Kind == "" {
		return fmt.Errorf("channelattr: event missing provider, channel_id, or kind")
	}
	if len(ev.Value) == 0 {
		return fmt.Errorf("channelattr: empty value")
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	atStr := ev.At.UTC().Format(time.RFC3339Nano)
	value := string(ev.Value)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
INSERT INTO channel_attr_events (provider, channel_id, kind, value, at, source)
VALUES (?, ?, ?, ?, ?, ?)`,
		string(ev.Provider), ev.ChannelID, string(ev.Kind), value, atStr, ev.Source); err != nil {
		return fmt.Errorf("channelattr: insert event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO channel_attr_current (provider, channel_id, kind, value, at, source)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(provider, channel_id, kind) DO UPDATE SET
  value=excluded.value, at=excluded.at, source=excluded.source`,
		string(ev.Provider), ev.ChannelID, string(ev.Kind), value, atStr, ev.Source); err != nil {
		return fmt.Errorf("channelattr: upsert current: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	s.mu.Lock()
	s.byKey[key(ev.Provider, ev.ChannelID, ev.Kind)] = entry{
		value:  append(json.RawMessage(nil), ev.Value...),
		at:     ev.At.UTC(),
		source: ev.Source,
	}
	s.mu.Unlock()
	return nil
}

// Current returns the latest JSON value for a channel attribute.
func (s *Store) Current(provider model.ProviderID, channelID string, kind Kind) (json.RawMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.byKey[key(provider, channelID, kind)]
	if !ok {
		return nil, false
	}
	return append(json.RawMessage(nil), e.value...), true
}

// Annotate copies current health (and later other kinds) onto channel values.
func (s *Store) Annotate(provider model.ProviderID, chs []model.Channel) []model.Channel {
	if s == nil || len(chs) == 0 {
		return chs
	}
	out := make([]model.Channel, len(chs))
	copy(out, chs)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range out {
		if e, ok := s.byKey[key(provider, out[i].NormalizedID, KindHealth)]; ok {
			var h model.ChannelHealth
			if err := json.Unmarshal(e.value, &h); err != nil {
				slog.Warn("channelattr: bad health json", "provider", provider, "channel", out[i].NormalizedID, "err", err)
				continue
			}
			out[i].Health = h
		}
	}
	return out
}

// EventCount returns how many history rows exist (tests / ops).
func (s *Store) EventCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channel_attr_events`).Scan(&n)
	return n, err
}

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func key(provider model.ProviderID, channelID string, kind Kind) string {
	return string(provider) + "\x00" + channelID + "\x00" + string(kind)
}
