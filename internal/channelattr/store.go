package channelattr

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
	_ "modernc.org/sqlite"
)

const (
	dirName  = "channelattr"
	fileName = "attr.db"

	historyDefaultLimit = 50
	historyMaxLimit     = 200

	eventsSinceDefaultLimit = 500
	eventsSinceMaxLimit     = 2000

	eventRetention = 90 * 24 * time.Hour
	pruneEvery     = time.Minute
)

// Store is SQLite-backed current + history for channel attributes.
type Store struct {
	db        *sql.DB
	path      string // absolute path to attr.db
	dir       string // channelattr directory
	mu        sync.RWMutex
	byKey     map[string]entry // provider\x00channel\x00kind
	lastPrune time.Time
}

type entry struct {
	value  json.RawMessage
	at     time.Time
	source string
}

// HistoryEvent is one append-only attr event (newest-first from History).
type HistoryEvent struct {
	At     time.Time       `json:"at"`
	Source string          `json:"source,omitempty"`
	Value  json.RawMessage `json:"value"`
}

// TimelineEvent is one cross-channel history row (oldest-first from EventsSince).
type TimelineEvent struct {
	Provider  model.ProviderID `json:"provider"`
	ChannelID string           `json:"channel_id"`
	Kind      Kind             `json:"kind"`
	At        time.Time        `json:"at"`
	Source    string           `json:"source,omitempty"`
	Value     json.RawMessage  `json:"value"`
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
	s := &Store{db: db, path: path, dir: dir, byKey: make(map[string]entry)}
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
CREATE INDEX IF NOT EXISTS channel_attr_events_kind_at
  ON channel_attr_events (kind, at);
`)
	if err != nil {
		return fmt.Errorf("channelattr: migrate: %w", err)
	}
	return nil
}

// Annotate copies current health and classification onto channel values.
// Classification is applied only when the channel field is empty so a fresh
// classify on the refresh path is not overwritten by a slightly stale Current.
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
			} else {
				out[i].Health = h
			}
		}
		if out[i].Classification != "" {
			continue
		}
		if e, ok := s.byKey[key(provider, out[i].NormalizedID, KindClassification)]; ok {
			var c model.Classification
			if err := json.Unmarshal(e.value, &c); err != nil {
				slog.Warn("channelattr: bad classification json", "provider", provider, "channel", out[i].NormalizedID, "err", err)
				continue
			}
			out[i].Classification = c.Canonical()
		}
	}
	return out
}

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
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

// CurrentPresence returns KindPresence current values for provider, keyed by
// channel id. Includes both present and absent rows.
func (s *Store) CurrentPresence(provider model.ProviderID) map[string]Presence {
	out := make(map[string]Presence)
	if s == nil {
		return out
	}
	prefix := string(provider) + "\x00"
	suffix := "\x00" + string(KindPresence)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k, e := range s.byKey {
		if !strings.HasPrefix(k, prefix) || !strings.HasSuffix(k, suffix) {
			continue
		}
		channelID := strings.TrimSuffix(strings.TrimPrefix(k, prefix), suffix)
		if channelID == "" {
			continue
		}
		var p Presence
		if err := json.Unmarshal(e.value, &p); err != nil {
			slog.Warn("channelattr: bad presence json", "provider", provider, "channel", channelID, "err", err)
			continue
		}
		out[channelID] = p
	}
	return out
}

// AbsentEntries returns every Current presence row with state=absent.
func (s *Store) AbsentEntries() []AbsentEntry {
	if s == nil {
		return nil
	}
	suffix := "\x00" + string(KindPresence)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []AbsentEntry
	for k, e := range s.byKey {
		if !strings.HasSuffix(k, suffix) {
			continue
		}
		rest := strings.TrimSuffix(k, suffix)
		provider, channelID, ok := strings.Cut(rest, "\x00")
		if !ok || provider == "" || channelID == "" {
			continue
		}
		var p Presence
		if err := json.Unmarshal(e.value, &p); err != nil || p.State != PresenceAbsent {
			continue
		}
		out = append(out, AbsentEntry{
			Provider:  model.ProviderID(provider),
			ChannelID: channelID,
			Presence:  p,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].ChannelID < out[j].ChannelID
	})
	return out
}

// PresenceSummary counts current absents and present/absent events since `since`.
type PresenceSummary struct {
	AbsentNow int `json:"absent_now"`
	Dropped7d int `json:"dropped_7d"`
	Added7d   int `json:"added_7d"`
}

// SummarizePresence builds a PresenceSummary for Status (7-day window from now).
func (s *Store) SummarizePresence(ctx context.Context, now time.Time) (PresenceSummary, error) {
	var out PresenceSummary
	if s == nil {
		return out, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out.AbsentNow = len(s.AbsentEntries())
	since := now.UTC().Add(-7 * 24 * time.Hour)
	events, err := s.EventsSince(ctx, since, []Kind{KindPresence}, "", 0)
	if err != nil {
		return out, err
	}
	for _, ev := range events {
		var p Presence
		if err := json.Unmarshal(ev.Value, &p); err != nil {
			continue
		}
		switch p.State {
		case PresenceAbsent:
			out.Dropped7d++
		case PresencePresent:
			out.Added7d++
		}
	}
	return out, nil
}

// EventCount returns how many history rows exist (tests / ops).
func (s *Store) EventCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channel_attr_events`).Scan(&n)
	return n, err
}

// Stats summarizes on-disk size and row counts for the channelattr store.
type Stats struct {
	DBPath        string         `json:"db_path"`
	DBBytes       int64          `json:"db_bytes"`
	CurrentRows   int            `json:"current_rows"`
	EventRows     int            `json:"event_rows"`
	Kinds         map[string]int `json:"kinds,omitempty"`
	OldestEventAt *time.Time     `json:"oldest_event_at,omitempty"`
	NewestEventAt *time.Time     `json:"newest_event_at,omitempty"`
	SiblingFiles  []SiblingFile  `json:"sibling_files,omitempty"`
}

// SiblingFile is a non-db file under channelattr/ (e.g. health_schedule.json).
type SiblingFile struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

// Stats returns inventory fields for GET /api/cache.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	out := Stats{Kinds: map[string]int{}}
	if s == nil || s.db == nil {
		return out, fmt.Errorf("channelattr: nil store")
	}
	out.DBPath = filepath.ToSlash(filepath.Join(dirName, fileName))
	if info, err := os.Stat(s.path); err == nil {
		out.DBBytes = info.Size()
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channel_attr_current`).Scan(&out.CurrentRows); err != nil {
		return out, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channel_attr_events`).Scan(&out.EventRows); err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT kind, COUNT(*) FROM channel_attr_current GROUP BY kind`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return out, err
		}
		out.Kinds[kind] = n
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	var oldest, newest sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(at), MAX(at) FROM channel_attr_events`).Scan(&oldest, &newest); err != nil {
		return out, err
	}
	if oldest.Valid {
		if t, ok := parseAttrTime(oldest.String); ok {
			out.OldestEventAt = &t
		}
	}
	if newest.Valid {
		if t, ok := parseAttrTime(newest.String); ok {
			out.NewestEventAt = &t
		}
	}
	if s.dir != "" {
		entries, err := os.ReadDir(s.dir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() || entry.Name() == fileName {
					continue
				}
				info, err := entry.Info()
				if err != nil {
					continue
				}
				out.SiblingFiles = append(out.SiblingFiles, SiblingFile{Name: entry.Name(), Bytes: info.Size()})
			}
			sort.Slice(out.SiblingFiles, func(i, j int) bool {
				return out.SiblingFiles[i].Name < out.SiblingFiles[j].Name
			})
		}
	}
	return out, nil
}

func parseAttrTime(atStr string) (time.Time, bool) {
	at, err := time.Parse(time.RFC3339Nano, atStr)
	if err != nil {
		at, err = time.Parse(time.RFC3339, atStr)
		if err != nil {
			return time.Time{}, false
		}
	}
	return at.UTC(), true
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
	due := s.lastPrune.IsZero() || time.Since(s.lastPrune) >= pruneEvery
	s.mu.Unlock()

	if due {
		_ = s.prune(ev.At.UTC())
	}
	return nil
}

// prune deletes event rows older than eventRetention. Current rows are kept.
func (s *Store) prune(now time.Time) error {
	if s == nil || s.db == nil {
		return nil
	}
	cutoff := now.UTC().Add(-eventRetention).Format(time.RFC3339Nano)
	_, err := s.db.Exec(`DELETE FROM channel_attr_events WHERE at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("channelattr: prune: %w", err)
	}
	s.mu.Lock()
	s.lastPrune = time.Now()
	s.mu.Unlock()
	return nil
}

// EventsSince returns presence/classification (or caller-specified) events at or
// after since, oldest-first, for report assembly. Empty provider means all
// providers. Empty kinds defaults to presence + classification.
func (s *Store) EventsSince(ctx context.Context, since time.Time, kinds []Kind, provider model.ProviderID, limit int) ([]TimelineEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("channelattr: nil store")
	}
	if limit <= 0 {
		limit = eventsSinceDefaultLimit
	}
	if limit > eventsSinceMaxLimit {
		limit = eventsSinceMaxLimit
	}
	if len(kinds) == 0 {
		kinds = []Kind{KindPresence, KindClassification}
	}
	placeholders := make([]string, len(kinds))
	args := make([]any, 0, 2+len(kinds))
	for i, k := range kinds {
		placeholders[i] = "?"
		args = append(args, string(k))
	}
	sinceStr := since.UTC().Format(time.RFC3339Nano)
	args = append(args, sinceStr)
	q := `SELECT provider, channel_id, kind, at, source, value FROM channel_attr_events
WHERE kind IN (` + strings.Join(placeholders, ",") + `) AND at >= ?`
	if provider != "" {
		q += ` AND provider = ?`
		args = append(args, string(provider))
	}
	q += ` ORDER BY at ASC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("channelattr: events since: %w", err)
	}
	defer rows.Close()

	var out []TimelineEvent
	for rows.Next() {
		var providerStr, channelID, kindStr, atStr, source, value string
		if err := rows.Scan(&providerStr, &channelID, &kindStr, &atStr, &source, &value); err != nil {
			return nil, fmt.Errorf("channelattr: events since scan: %w", err)
		}
		at, ok := parseAttrTime(atStr)
		if !ok {
			slog.Warn("channelattr: bad events-since at", "provider", providerStr, "channel", channelID, "at", atStr)
			continue
		}
		out = append(out, TimelineEvent{
			Provider:  model.ProviderID(providerStr),
			ChannelID: channelID,
			Kind:      Kind(kindStr),
			At:        at,
			Source:    source,
			Value:     json.RawMessage(value),
		})
	}
	return out, rows.Err()
}

// History returns newest-first events for one channel attribute.
func (s *Store) History(ctx context.Context, provider model.ProviderID, channelID string, kind Kind, limit int) ([]HistoryEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("channelattr: nil store")
	}
	if limit <= 0 {
		limit = historyDefaultLimit
	}
	if limit > historyMaxLimit {
		limit = historyMaxLimit
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT at, source, value FROM channel_attr_events
WHERE provider = ? AND channel_id = ? AND kind = ?
ORDER BY at DESC
LIMIT ?`, string(provider), channelID, string(kind), limit)
	if err != nil {
		return nil, fmt.Errorf("channelattr: history: %w", err)
	}
	defer rows.Close()

	var out []HistoryEvent
	for rows.Next() {
		var atStr, source, value string
		if err := rows.Scan(&atStr, &source, &value); err != nil {
			return nil, fmt.Errorf("channelattr: history scan: %w", err)
		}
		at, err := time.Parse(time.RFC3339Nano, atStr)
		if err != nil {
			at, err = time.Parse(time.RFC3339, atStr)
			if err != nil {
				slog.Warn("channelattr: bad history at", "provider", provider, "channel", channelID, "err", err)
				continue
			}
		}
		out = append(out, HistoryEvent{
			At:     at.UTC(),
			Source: source,
			Value:  json.RawMessage(value),
		})
	}
	return out, rows.Err()
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

// SuccessRate returns the fraction of health history rows whose LastCheck is
// success among events at or after since. ok is false when no qualifying rows.
func SuccessRate(events []HistoryEvent, since time.Time) (rate float64, ok bool) {
	var total, success int
	for _, ev := range events {
		if !since.IsZero() && ev.At.Before(since) {
			continue
		}
		var h model.ChannelHealth
		if err := json.Unmarshal(ev.Value, &h); err != nil {
			continue
		}
		switch h.LastCheck {
		case model.HealthCheckSuccess:
			total++
			success++
		case model.HealthCheckFailure:
			total++
		}
	}
	if total == 0 {
		return 0, false
	}
	return float64(success) / float64(total), true
}

func key(provider model.ProviderID, channelID string, kind Kind) string {
	return string(provider) + "\x00" + channelID + "\x00" + string(kind)
}
