package opsreport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	archiveRetention = 90 * 24 * time.Hour
	archivePrefix    = "report-"
)

// Archive is one stored full report (official or preview).
type Archive struct {
	Version     int       `json:"version"`
	ID          string    `json:"id"`
	Kind        Kind      `json:"kind"`
	GeneratedAt time.Time `json:"generated_at"`
	Subject     string    `json:"subject"`
	Text        string    `json:"text"`
	HTML        string    `json:"html"`
	Report      Report    `json:"report"`
}

// ArchiveMeta is a list row without bodies.
type ArchiveMeta struct {
	ID          string    `json:"id"`
	Kind        Kind      `json:"kind"`
	GeneratedAt time.Time `json:"generated_at"`
	Subject     string    `json:"subject"`
	Filename    string    `json:"filename"`
}

func writeArchive(dir string, kind Kind, generatedAt time.Time, subject, text, html string, report Report) (Archive, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Archive{}, err
	}
	id := archiveID(generatedAt)
	a := Archive{
		Version:     1,
		ID:          id,
		Kind:        kind,
		GeneratedAt: generatedAt.UTC(),
		Subject:     subject,
		Text:        text,
		HTML:        html,
		Report:      report,
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return Archive{}, err
	}
	name := archiveFilename(id)
	path := filepath.Join(dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return Archive{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return Archive{}, err
	}
	_ = pruneArchives(dir, generatedAt.Add(-archiveRetention))
	return a, nil
}

func archiveID(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

func archiveFilename(id string) string {
	return archivePrefix + id + ".json"
}

func loadArchive(dir, id string) (Archive, error) {
	id = sanitizeArchiveID(id)
	if id == "" {
		return Archive{}, fmt.Errorf("opsreport: empty archive id")
	}
	path := filepath.Join(dir, archiveFilename(id))
	data, err := os.ReadFile(path)
	if err != nil {
		return Archive{}, err
	}
	var a Archive
	if err := json.Unmarshal(data, &a); err != nil {
		return Archive{}, fmt.Errorf("opsreport: parse archive: %w", err)
	}
	if a.ID == "" {
		a.ID = id
	}
	return a, nil
}

func listArchives(dir string, limit int) ([]ArchiveMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, archivePrefix) && strings.HasSuffix(name, ".json") {
			names = append(names, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	out := make([]ArchiveMeta, 0, limit)
	for _, name := range names {
		if len(out) >= limit {
			break
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var a Archive
		if json.Unmarshal(data, &a) != nil {
			continue
		}
		id := a.ID
		if id == "" {
			id = strings.TrimSuffix(strings.TrimPrefix(name, archivePrefix), ".json")
		}
		out = append(out, ArchiveMeta{
			ID:          id,
			Kind:        a.Kind,
			GeneratedAt: a.GeneratedAt,
			Subject:     a.Subject,
			Filename:    name,
		})
	}
	return out, nil
}

func pruneArchives(dir string, olderThan time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cutoff := olderThan.UTC()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, archivePrefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().UTC().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	return nil
}

func sanitizeArchiveID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(id, archivePrefix)
	id = strings.TrimSuffix(id, ".json")
	for _, r := range id {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == 'T' || r == 'Z' {
			continue
		}
		return ""
	}
	return id
}
