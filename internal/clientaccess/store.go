// Package clientaccess persists non-UI M3U/XMLTV emit hits (Jellyfin pulls)
// with a rolling 30-day history for the Status dashboard.
package clientaccess

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	fileName      = "client_access.db"
	retention     = 30 * 24 * time.Hour
	pruneEvery    = time.Minute
	recentDefault = 50
	recentMaxCap  = 2000
	userAgentMax  = 512
)

// Query filters Recent results. Zero values mean unrestricted.
type Query struct {
	File   string
	IP     string // case-insensitive substring
	Status int    // 0 = any
	Limit  int
}

// Event is one recorded emit pull.
type Event struct {
	File      string    `json:"file"`
	At        time.Time `json:"at"`
	IP        string    `json:"ip"`
	Status    int       `json:"status"`
	UserAgent string    `json:"user_agent,omitempty"`
}

// FileSummary is the per-file rollup for the dashboard.
type FileSummary struct {
	File       string    `json:"file"`
	Hits30d    int       `json:"hits_30d"`
	LastAt     time.Time `json:"last_at"`
	LastIP     string    `json:"last_ip"`
	LastStatus int       `json:"last_status"`
}

// Store is a SQLite-backed access event log under cacheDir/client_access.db.
type Store struct {
	db *sql.DB

	mu        sync.Mutex
	lastPrune time.Time
}

// Open creates/opens client_access.db under cacheDir (typically {data_dir}/cache).
func Open(cacheDir string) (*Store, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("clientaccess: mkdir: %w", err)
	}
	path := filepath.Join(cacheDir, fileName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("clientaccess: open: %w", err)
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

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS access_event (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  file       TEXT NOT NULL,
  at         TEXT NOT NULL,
  ip         TEXT NOT NULL,
  status     INTEGER NOT NULL,
  user_agent TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS access_event_file_at ON access_event(file, at DESC);
CREATE INDEX IF NOT EXISTS access_event_at ON access_event(at);
`)
	if err != nil {
		return fmt.Errorf("clientaccess: migrate: %w", err)
	}
	// Older DBs created before user_agent existed.
	_, _ = s.db.Exec(`ALTER TABLE access_event ADD COLUMN user_agent TEXT NOT NULL DEFAULT ''`)
	return nil
}

// Close releases the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Record inserts one emit access event and periodically prunes rows older than 30 days.
// Nil store is a no-op.
func (s *Store) Record(file, ip, userAgent string, status int, at time.Time) error {
	if s == nil {
		return nil
	}
	file = strings.TrimSpace(file)
	if file == "" {
		return nil
	}
	if ip == "" {
		ip = "unknown"
	}
	userAgent = truncateUA(userAgent)
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	_, err := s.db.Exec(
		`INSERT INTO access_event (file, at, ip, status, user_agent) VALUES (?, ?, ?, ?, ?)`,
		file, at.Format(time.RFC3339Nano), ip, status, userAgent,
	)
	if err != nil {
		return fmt.Errorf("clientaccess: insert: %w", err)
	}
	s.mu.Lock()
	due := s.lastPrune.IsZero() || time.Since(s.lastPrune) >= pruneEvery
	s.mu.Unlock()
	if due {
		_ = s.prune(at)
	}
	return nil
}

func truncateUA(ua string) string {
	ua = strings.TrimSpace(ua)
	if len(ua) <= userAgentMax {
		return ua
	}
	return ua[:userAgentMax]
}

func (s *Store) prune(now time.Time) error {
	cutoff := now.UTC().Add(-retention).Format(time.RFC3339Nano)
	_, err := s.db.Exec(`DELETE FROM access_event WHERE at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("clientaccess: prune: %w", err)
	}
	s.mu.Lock()
	s.lastPrune = time.Now()
	s.mu.Unlock()
	return nil
}

// Summary returns per-file rollups for events in the retention window, sorted by file.
func (s *Store) Summary() ([]FileSummary, error) {
	if s == nil {
		return nil, nil
	}
	cutoff := time.Now().UTC().Add(-retention).Format(time.RFC3339Nano)
	// Single query: avoid nested QueryRow while MaxOpenConns(1).
	rows, err := s.db.Query(`
SELECT e.file, counts.hits, e.at, e.ip, e.status
FROM access_event e
INNER JOIN (
  SELECT file, COUNT(*) AS hits, MAX(id) AS max_id
  FROM access_event
  WHERE at >= ?
  GROUP BY file
) counts ON e.id = counts.max_id
ORDER BY e.file
`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("clientaccess: summary: %w", err)
	}
	defer rows.Close()

	var out []FileSummary
	for rows.Next() {
		var (
			sum   FileSummary
			atRaw string
		)
		if err := rows.Scan(&sum.File, &sum.Hits30d, &atRaw, &sum.LastIP, &sum.LastStatus); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, atRaw); err == nil {
			sum.LastAt = t
		} else if t, err := time.Parse(time.RFC3339, atRaw); err == nil {
			sum.LastAt = t
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

// Recent returns the newest matching events (newest first).
func (s *Store) Recent(q Query) ([]Event, error) {
	if s == nil {
		return nil, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = recentDefault
	}
	if limit > recentMaxCap {
		limit = recentMaxCap
	}
	cutoff := time.Now().UTC().Add(-retention).Format(time.RFC3339Nano)
	args := []any{cutoff}
	var b strings.Builder
	b.WriteString(`SELECT file, at, ip, status, user_agent FROM access_event WHERE at >= ?`)
	if file := strings.TrimSpace(q.File); file != "" {
		b.WriteString(` AND file = ?`)
		args = append(args, file)
	}
	if ip := strings.TrimSpace(q.IP); ip != "" {
		b.WriteString(` AND instr(lower(ip), lower(?)) > 0`)
		args = append(args, ip)
	}
	if q.Status != 0 {
		b.WriteString(` AND status = ?`)
		args = append(args, q.Status)
	}
	b.WriteString(` ORDER BY at DESC, id DESC LIMIT ?`)
	args = append(args, limit)

	rows, err := s.db.Query(b.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("clientaccess: recent: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var (
			e     Event
			atRaw string
		)
		if err := rows.Scan(&e.File, &atRaw, &e.IP, &e.Status, &e.UserAgent); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, atRaw); err == nil {
			e.At = t
		} else if t, err := time.Parse(time.RFC3339, atRaw); err == nil {
			e.At = t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ClientIP returns the best-effort caller IP from proxy headers or RemoteAddr.
func ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			xff = xff[:i]
		}
		if ip := strings.TrimSpace(xff); ip != "" {
			return ip
		}
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
		return xr
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}
