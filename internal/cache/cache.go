// Package cache is the sole owner of the on-disk generated artifacts. It maps a
// provider id to its files and exposes typed reads/writes; no other package
// knows the layout or touches disk, so persistence is controlled centrally.
//
// Layout under root:
//
//	<id>/current                         selected generation name
//	<id>/status.json                     refresh attempt/error state
//	<id>/generations/<generation>/raw/*  provider-native responses
//	<id>/generations/<generation>/*.m3u  per-provider playlist
//	<id>/generations/<generation>/*.xml  per-provider guide
//	<id>/generations/<generation>/meta.json
//	<id>/logos/<channel_id>.<ext>        durable channel logos (outside generations)
//	aggregate/current                    selected aggregate generation
//	aggregate/generations/<generation>/playlist.m3u
//	aggregate/generations/<generation>/epg.xml
//	Legacy root playlist.m3u / epg.xml remain readable until the next rebuild.
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

const (
	fileM3U        = "playlist.m3u"
	fileXML        = "guide.xml"
	fileMeta       = "meta.json"
	dirRaw         = "raw"
	fileStatus     = "status.json"
	fileCurrent    = "current"
	dirGenerations = "generations"
	dirLogos       = "logos"

	dirAggregate = "aggregate"
	aggM3U       = "playlist.m3u"
	aggXML       = "epg.xml"
)

// M3U is a rendered playlist document.
type M3U []byte

// XMLTV is a rendered guide document.
type XMLTV []byte

// Cache reads and writes the generated artifacts rooted at a directory.
type Cache struct {
	root string
}

// New returns a Cache rooted at dir.
func New(root string) *Cache { return &Cache{root: root} }

// CommitProvider publishes raw, playlist, guide, and meta as one immutable
// generation. Replacing current is the sole commit point.
func (c *Cache) CommitProvider(id model.ProviderID, raw provider.Raw, m3u M3U, xml XMLTV, meta provider.Meta) error {
	if len(raw) == 0 {
		return errors.New("cache: raw snapshot is empty")
	}
	for name := range raw {
		if !validRawName(name) {
			return fmt.Errorf("cache: invalid raw filename %q", name)
		}
	}
	providerDir, err := c.providerDir(id)
	if err != nil {
		return err
	}
	generationsDir := filepath.Join(providerDir, dirGenerations)
	if err := os.MkdirAll(generationsDir, 0o755); err != nil {
		return fmt.Errorf("cache: mkdir generations: %w", err)
	}
	stage, err := os.MkdirTemp(generationsDir, ".staging-")
	if err != nil {
		return fmt.Errorf("cache: staging generation: %w", err)
	}
	defer os.RemoveAll(stage)

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("cache: marshal meta: %w", err)
	}
	rawDir := filepath.Join(stage, dirRaw)
	if err := os.Mkdir(rawDir, 0o755); err != nil {
		return fmt.Errorf("cache: mkdir raw: %w", err)
	}
	rawNames := make([]string, 0, len(raw))
	for name := range raw {
		rawNames = append(rawNames, name)
	}
	sort.Strings(rawNames)
	for _, name := range rawNames {
		if err := writeSynced(filepath.Join(rawDir, name), raw[name]); err != nil {
			return err
		}
	}
	if err := syncDir(rawDir); err != nil {
		return err
	}

	files := []struct {
		name string
		data []byte
	}{
		{fileM3U, m3u},
		{fileXML, xml},
		{fileMeta, metaBytes},
	}
	for _, file := range files {
		if err := writeSynced(filepath.Join(stage, file.name), file.data); err != nil {
			return err
		}
	}
	if err := syncDir(stage); err != nil {
		return err
	}
	name := strings.TrimPrefix(filepath.Base(stage), ".staging-")
	generationDir := filepath.Join(generationsDir, name)
	if err := os.Rename(stage, generationDir); err != nil {
		return fmt.Errorf("cache: publish generation: %w", err)
	}
	if err := syncDir(generationsDir); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(providerDir, fileCurrent), []byte(name+"\n")); err != nil {
		return err
	}
	c.cleanupGenerations(generationsDir, name)
	return nil
}

// ReadRaw returns a provider's exact upstream responses from one generation.
func (c *Cache) ReadRaw(id model.ProviderID) (provider.Raw, error) {
	dir, _, err := c.selectedDir(id)
	if err != nil {
		return nil, err
	}
	return readRaw(dir)
}

// ReadM3U returns a provider's playlist (fs.ErrNotExist if not yet generated).
func (c *Cache) ReadM3U(id model.ProviderID) (M3U, error) {
	dir, _, err := c.selectedDir(id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, fileM3U))
	return M3U(b), err
}

// ReadXMLTV returns a provider's guide (fs.ErrNotExist if not yet generated).
func (c *Cache) ReadXMLTV(id model.ProviderID) (XMLTV, error) {
	dir, _, err := c.selectedDir(id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, fileXML))
	return XMLTV(b), err
}

// CommitAggregate publishes playlist and guide as one immutable generation.
// Replacing aggregate/current is the sole commit point for the pair.
func (c *Cache) CommitAggregate(m3u M3U, xml XMLTV) error {
	aggregateDir := filepath.Join(c.root, dirAggregate)
	generationsDir := filepath.Join(aggregateDir, dirGenerations)
	if err := os.MkdirAll(generationsDir, 0o755); err != nil {
		return fmt.Errorf("cache: mkdir aggregate generations: %w", err)
	}
	stage, err := os.MkdirTemp(generationsDir, ".staging-")
	if err != nil {
		return fmt.Errorf("cache: staging aggregate generation: %w", err)
	}
	defer os.RemoveAll(stage)

	files := []struct {
		name string
		data []byte
	}{
		{aggM3U, m3u},
		{aggXML, xml},
	}
	for _, file := range files {
		if err := writeSynced(filepath.Join(stage, file.name), file.data); err != nil {
			return err
		}
	}
	if err := syncDir(stage); err != nil {
		return err
	}
	name := strings.TrimPrefix(filepath.Base(stage), ".staging-")
	generationDir := filepath.Join(generationsDir, name)
	if err := os.Rename(stage, generationDir); err != nil {
		return fmt.Errorf("cache: publish aggregate generation: %w", err)
	}
	if err := syncDir(generationsDir); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(aggregateDir, fileCurrent), []byte(name+"\n")); err != nil {
		return err
	}
	c.cleanupGenerations(generationsDir, name)
	return nil
}

// ReadAggregateM3U returns the combined playlist (fs.ErrNotExist if not generated).
func (c *Cache) ReadAggregateM3U() (M3U, error) {
	dir, err := c.selectedAggregateDir()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, aggM3U))
	return M3U(b), err
}

// ReadAggregateXMLTV returns the combined guide (fs.ErrNotExist if not generated).
func (c *Cache) ReadAggregateXMLTV() (XMLTV, error) {
	dir, err := c.selectedAggregateDir()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, aggXML))
	return XMLTV(b), err
}

// LoadMeta reads one provider's meta.json (fetch time + classifications).
func (c *Cache) LoadMeta(id model.ProviderID) (provider.Meta, bool) {
	dir, _, err := c.selectedDir(id)
	if err != nil {
		return provider.Meta{}, false
	}
	b, err := os.ReadFile(filepath.Join(dir, fileMeta))
	if err != nil {
		return provider.Meta{}, false
	}
	var m provider.Meta
	if err := json.Unmarshal(b, &m); err != nil {
		return provider.Meta{}, false
	}
	return m, true
}

// LoadProvider returns matching raw and metadata from one selected generation.
// legacy reports the pre-generation flat layout.
func (c *Cache) LoadProvider(id model.ProviderID) (provider.Raw, provider.Meta, bool, error) {
	dir, legacy, err := c.selectedDir(id)
	if err != nil {
		return nil, provider.Meta{}, false, err
	}
	raw, err := readRaw(dir)
	if err != nil {
		return nil, provider.Meta{}, legacy, err
	}
	b, err := os.ReadFile(filepath.Join(dir, fileMeta))
	if err != nil {
		return nil, provider.Meta{}, legacy, err
	}
	var meta provider.Meta
	if err := json.Unmarshal(b, &meta); err != nil {
		return nil, provider.Meta{}, legacy, fmt.Errorf("cache: unmarshal meta: %w", err)
	}
	return raw, meta, legacy, nil
}

// LoadStatus reads provider refresh-attempt status.
func (c *Cache) LoadStatus(id model.ProviderID) (provider.Status, bool) {
	dir, err := c.providerDir(id)
	if err != nil {
		return provider.Status{}, false
	}
	b, err := os.ReadFile(filepath.Join(dir, fileStatus))
	if err != nil {
		return provider.Status{}, false
	}
	var status provider.Status
	if err := json.Unmarshal(b, &status); err != nil {
		return provider.Status{}, false
	}
	return status, true
}

// WriteStatus persists provider refresh-attempt status independently of the
// last-known-good content generation.
func (c *Cache) WriteStatus(id model.ProviderID, status provider.Status) error {
	dir, err := c.providerDir(id)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("cache: marshal status: %w", err)
	}
	return atomicWrite(filepath.Join(dir, fileStatus), b)
}

// providerDir returns {root}/{id}, rejecting ids that could escape the root.
func (c *Cache) providerDir(id model.ProviderID) (string, error) {
	name := string(id)
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fs.ErrNotExist
	}
	return filepath.Join(c.root, name), nil
}

// selectedDir resolves current exactly once. If no pointer exists, it falls
// back to the legacy flat provider directory.
func (c *Cache) selectedDir(id model.ProviderID) (string, bool, error) {
	dir, err := c.providerDir(id)
	if err != nil {
		return "", false, err
	}
	b, err := os.ReadFile(filepath.Join(dir, fileCurrent))
	if errors.Is(err, fs.ErrNotExist) {
		return dir, true, nil
	}
	if err != nil {
		return "", false, err
	}
	name := strings.TrimSpace(string(b))
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", false, fs.ErrNotExist
	}
	selected := filepath.Join(dir, dirGenerations, name)
	if info, err := os.Stat(selected); err != nil || !info.IsDir() {
		return "", false, fs.ErrNotExist
	}
	return selected, false, nil
}

// selectedAggregateDir resolves aggregate/current, falling back to the legacy
// root-level playlist.m3u / epg.xml pair when no pointer exists.
func (c *Cache) selectedAggregateDir() (string, error) {
	aggregateDir := filepath.Join(c.root, dirAggregate)
	b, err := os.ReadFile(filepath.Join(aggregateDir, fileCurrent))
	if errors.Is(err, fs.ErrNotExist) {
		if _, err := os.Stat(filepath.Join(c.root, aggM3U)); err != nil {
			return "", err
		}
		if _, err := os.Stat(filepath.Join(c.root, aggXML)); err != nil {
			return "", err
		}
		return c.root, nil
	}
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(string(b))
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fs.ErrNotExist
	}
	selected := filepath.Join(aggregateDir, dirGenerations, name)
	if info, err := os.Stat(selected); err != nil || !info.IsDir() {
		return "", fs.ErrNotExist
	}
	return selected, nil
}

func (c *Cache) cleanupGenerations(dir, current string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type generation struct {
		name string
		mod  int64
	}
	var generations []generation
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".staging-") {
			_ = os.RemoveAll(filepath.Join(dir, name))
			continue
		}
		info, err := entry.Info()
		if err == nil {
			generations = append(generations, generation{name, info.ModTime().UnixNano()})
		}
	}
	sort.Slice(generations, func(i, j int) bool { return generations[i].mod > generations[j].mod })
	keptPrevious := false
	for _, generation := range generations {
		if generation.name == current {
			continue
		}
		if !keptPrevious {
			keptPrevious = true
			continue
		}
		_ = os.RemoveAll(filepath.Join(dir, generation.name))
	}
}

func readRaw(dir string) (provider.Raw, error) {
	rawDir := filepath.Join(dir, dirRaw)
	info, err := os.Stat(rawDir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(rawDir)
		if err != nil {
			return nil, err
		}
		return provider.Raw{provider.LegacyRaw: data}, nil
	}
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		return nil, err
	}
	raw := make(provider.Raw, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !validRawName(entry.Name()) {
			return nil, fs.ErrNotExist
		}
		data, err := os.ReadFile(filepath.Join(rawDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		raw[entry.Name()] = data
	}
	return raw, nil
}

// LogoPath returns the absolute path for a logo file under {id}/logos/{file}.
func (c *Cache) LogoPath(id model.ProviderID, file string) (string, error) {
	providerDir, err := c.providerDir(id)
	if err != nil {
		return "", err
	}
	if !validRawName(file) {
		return "", fs.ErrNotExist
	}
	return filepath.Join(providerDir, dirLogos, file), nil
}

// WriteLogo atomically writes logo bytes under {id}/logos/{file}.
func (c *Cache) WriteLogo(id model.ProviderID, file string, data []byte) error {
	path, err := c.LogoPath(id, file)
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

// ReadLogo reads a logo file from {id}/logos/{file}.
func (c *Cache) ReadLogo(id model.ProviderID, file string) ([]byte, error) {
	path, err := c.LogoPath(id, file)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// StatLogo returns file info for a logo under {id}/logos/{file}.
func (c *Cache) StatLogo(id model.ProviderID, file string) (fs.FileInfo, error) {
	path, err := c.LogoPath(id, file)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fs.ErrNotExist
	}
	return info, nil
}

// WriteLogoMeta atomically writes the ETag/Last-Modified sidecar for a logo file.
func (c *Cache) WriteLogoMeta(id model.ProviderID, file string, data []byte) error {
	path, err := c.LogoPath(id, file)
	if err != nil {
		return err
	}
	return atomicWrite(path+".meta", data)
}

// ReadLogoMeta reads the ETag/Last-Modified sidecar for a logo file.
func (c *Cache) ReadLogoMeta(id model.ProviderID, file string) ([]byte, error) {
	path, err := c.LogoPath(id, file)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path + ".meta")
}

// ClearStats counts files and bytes removed by a destructive cache operation.
type ClearStats struct {
	DeletedFiles int   `json:"deleted_files"`
	DeletedBytes int64 `json:"deleted_bytes"`
}

// Add merges other into s.
func (s *ClearStats) Add(other ClearStats) {
	s.DeletedFiles += other.DeletedFiles
	s.DeletedBytes += other.DeletedBytes
}

// DirStats is a file count and total byte size.
type DirStats struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

// GenerationInfo describes one generation or leftover staging directory.
type GenerationInfo struct {
	Name      string `json:"name"`
	Bytes     int64  `json:"bytes"`
	Files     int    `json:"files"`
	IsCurrent bool   `json:"is_current"`
	IsStaging bool   `json:"is_staging"`
}

// ProviderInventory is on-disk usage for one provider (or aggregate).
type ProviderInventory struct {
	ID            string           `json:"id"`
	Current       string           `json:"current,omitempty"`
	Generations   []GenerationInfo `json:"generations"`
	Logos         DirStats         `json:"logos"`
	BytesTotal    int64            `json:"bytes_total"`
	OrphanStaging int              `json:"orphan_staging"`
	Known         bool             `json:"known"`
}

// Inventory is a full walk of the cache root.
type Inventory struct {
	Providers       []ProviderInventory `json:"providers"`
	Aggregate       *ProviderInventory  `json:"aggregate,omitempty"`
	BytesTotal      int64               `json:"bytes_total"`
	LogoBytes       int64               `json:"logo_bytes"`
	LogoFiles       int                 `json:"logo_files"`
	GenerationCount int                 `json:"generation_count"`
	UnknownDirs     []string            `json:"unknown_dirs,omitempty"`
}

// Inventory walks {root} and reports sizes for known providers plus any
// unexpected top-level dirs (except aggregate and reserved DB files).
func (c *Cache) Inventory(known []model.ProviderID) (Inventory, error) {
	knownSet := make(map[string]struct{}, len(known))
	for _, id := range known {
		knownSet[string(id)] = struct{}{}
	}
	var out Inventory
	entries, err := os.ReadDir(c.root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, nil
		}
		return out, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			continue
		}
		switch name {
		case dirAggregate:
			inv, err := c.inventoryProviderDir(name, filepath.Join(c.root, name), true)
			if err != nil {
				return out, err
			}
			inv.Known = true
			out.Aggregate = &inv
			out.BytesTotal += inv.BytesTotal
			out.LogoBytes += inv.Logos.Bytes
			out.LogoFiles += inv.Logos.Files
			for _, g := range inv.Generations {
				if !g.IsStaging {
					out.GenerationCount++
				}
			}
		default:
			if name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
				continue
			}
			_, isKnown := knownSet[name]
			inv, err := c.inventoryProviderDir(name, filepath.Join(c.root, name), isKnown)
			if err != nil {
				return out, err
			}
			inv.Known = isKnown
			out.Providers = append(out.Providers, inv)
			out.BytesTotal += inv.BytesTotal
			out.LogoBytes += inv.Logos.Bytes
			out.LogoFiles += inv.Logos.Files
			for _, g := range inv.Generations {
				if !g.IsStaging {
					out.GenerationCount++
				}
			}
			if !isKnown {
				out.UnknownDirs = append(out.UnknownDirs, name)
			}
		}
	}
	sort.Slice(out.Providers, func(i, j int) bool { return out.Providers[i].ID < out.Providers[j].ID })
	sort.Strings(out.UnknownDirs)
	return out, nil
}

func (c *Cache) inventoryProviderDir(id, providerDir string, includeLogos bool) (ProviderInventory, error) {
	inv := ProviderInventory{ID: id, Generations: []GenerationInfo{}}
	current := readCurrentName(filepath.Join(providerDir, fileCurrent))
	inv.Current = current
	gensDir := filepath.Join(providerDir, dirGenerations)
	entries, err := os.ReadDir(gensDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return inv, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		staging := strings.HasPrefix(name, ".staging-")
		bytes, files, err := dirUsage(filepath.Join(gensDir, name))
		if err != nil {
			return inv, err
		}
		inv.Generations = append(inv.Generations, GenerationInfo{
			Name:      name,
			Bytes:     bytes,
			Files:     files,
			IsCurrent: name == current && !staging,
			IsStaging: staging,
		})
		inv.BytesTotal += bytes
		if staging {
			inv.OrphanStaging++
		}
	}
	sort.Slice(inv.Generations, func(i, j int) bool {
		a, b := inv.Generations[i], inv.Generations[j]
		if a.IsCurrent != b.IsCurrent {
			return a.IsCurrent
		}
		if a.IsStaging != b.IsStaging {
			return !a.IsStaging
		}
		return a.Name < b.Name
	})
	if includeLogos {
		logosDir := filepath.Join(providerDir, dirLogos)
		bytes, files, err := dirUsage(logosDir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return inv, err
		}
		inv.Logos = DirStats{Files: files, Bytes: bytes}
		inv.BytesTotal += bytes
	}
	return inv, nil
}

// PurgeNonCurrent deletes leftover staging dirs and every generation except
// the one named by current. Serving current is preserved (soft purge).
func (c *Cache) PurgeNonCurrent(id model.ProviderID) (ClearStats, error) {
	providerDir, err := c.providerDir(id)
	if err != nil {
		return ClearStats{}, err
	}
	return c.purgeNonCurrentDir(providerDir)
}

// PurgeNonCurrentAggregate soft-purges aggregate generations the same way.
func (c *Cache) PurgeNonCurrentAggregate() (ClearStats, error) {
	return c.purgeNonCurrentDir(filepath.Join(c.root, dirAggregate))
}

func (c *Cache) purgeNonCurrentDir(providerDir string) (ClearStats, error) {
	var stats ClearStats
	gensDir := filepath.Join(providerDir, dirGenerations)
	entries, err := os.ReadDir(gensDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return stats, nil
		}
		return stats, err
	}
	current := readCurrentName(filepath.Join(providerDir, fileCurrent))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == current && !strings.HasPrefix(name, ".staging-") {
			continue
		}
		path := filepath.Join(gensDir, name)
		s, err := removeAllCounting(path)
		if err != nil {
			return stats, err
		}
		stats.Add(s)
	}
	return stats, nil
}

// DeleteLogo removes one logo file and its .meta sidecar.
func (c *Cache) DeleteLogo(id model.ProviderID, file string) (ClearStats, error) {
	path, err := c.LogoPath(id, file)
	if err != nil {
		return ClearStats{}, err
	}
	var stats ClearStats
	for _, p := range []string{path, path + ".meta"} {
		s, err := removeFileCounting(p)
		if err != nil {
			return stats, err
		}
		stats.Add(s)
	}
	return stats, nil
}

// DeleteProviderLogos removes every file under {id}/logos/.
func (c *Cache) DeleteProviderLogos(id model.ProviderID) (ClearStats, error) {
	providerDir, err := c.providerDir(id)
	if err != nil {
		return ClearStats{}, err
	}
	return removeAllCounting(filepath.Join(providerDir, dirLogos))
}

// DeleteAllLogos removes logos/ under every provider directory.
func (c *Cache) DeleteAllLogos() (ClearStats, error) {
	var stats ClearStats
	entries, err := os.ReadDir(c.root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return stats, nil
		}
		return stats, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == dirAggregate {
			continue
		}
		s, err := removeAllCounting(filepath.Join(c.root, entry.Name(), dirLogos))
		if err != nil {
			return stats, err
		}
		stats.Add(s)
	}
	return stats, nil
}

// DeleteChannelLogos removes logo files for one channel id (any extension + .meta).
func (c *Cache) DeleteChannelLogos(id model.ProviderID, channelID string) (ClearStats, error) {
	if channelID == "" || channelID != filepath.Base(channelID) || strings.ContainsAny(channelID, `/\`) {
		return ClearStats{}, fs.ErrNotExist
	}
	providerDir, err := c.providerDir(id)
	if err != nil {
		return ClearStats{}, err
	}
	logosDir := filepath.Join(providerDir, dirLogos)
	entries, err := os.ReadDir(logosDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ClearStats{}, nil
		}
		return ClearStats{}, err
	}
	prefix := channelID + "."
	var stats ClearStats
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		base := strings.TrimSuffix(name, ".meta")
		if !strings.HasPrefix(base, prefix) {
			continue
		}
		if idFromFile, ok := logoChannelID(base); !ok || idFromFile != channelID {
			continue
		}
		s, err := removeFileCounting(filepath.Join(logosDir, name))
		if err != nil {
			return stats, err
		}
		stats.Add(s)
	}
	return stats, nil
}

// SweepOrphans removes leftover staging dirs, generations beyond current+1,
// logo files whose channel id is not in lineup (and stale extensions when
// keepFiles is set), and whole provider dirs not in known.
//
// lineup maps provider → set of channel NormalizedIDs still in the feed.
// keepFiles maps provider → channelID → expected logo filename (optional);
// when set, other logo files for that channel id are removed.
func (c *Cache) SweepOrphans(known []model.ProviderID, lineup map[model.ProviderID]map[string]struct{}, keepFiles map[model.ProviderID]map[string]string) (ClearStats, error) {
	knownSet := make(map[string]struct{}, len(known)+1)
	for _, id := range known {
		knownSet[string(id)] = struct{}{}
	}
	knownSet[dirAggregate] = struct{}{}

	var stats ClearStats
	entries, err := os.ReadDir(c.root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return stats, nil
		}
		return stats, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, ok := knownSet[name]; !ok {
			s, err := removeAllCounting(filepath.Join(c.root, name))
			if err != nil {
				return stats, err
			}
			stats.Add(s)
			continue
		}
		providerDir := filepath.Join(c.root, name)
		s, err := c.sweepProviderOrphans(providerDir, name == dirAggregate)
		if err != nil {
			return stats, err
		}
		stats.Add(s)
		if name == dirAggregate {
			continue
		}
		id := model.ProviderID(name)
		s, err = c.sweepProviderLogos(providerDir, lineup[id], keepFiles[id])
		if err != nil {
			return stats, err
		}
		stats.Add(s)
	}
	return stats, nil
}

func (c *Cache) sweepProviderOrphans(providerDir string, isAggregate bool) (ClearStats, error) {
	_ = isAggregate
	var stats ClearStats
	gensDir := filepath.Join(providerDir, dirGenerations)
	entries, err := os.ReadDir(gensDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return stats, nil
		}
		return stats, err
	}
	current := readCurrentName(filepath.Join(providerDir, fileCurrent))
	type generation struct {
		name string
		mod  int64
	}
	var generations []generation
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".staging-") {
			s, err := removeAllCounting(filepath.Join(gensDir, name))
			if err != nil {
				return stats, err
			}
			stats.Add(s)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		generations = append(generations, generation{name, info.ModTime().UnixNano()})
	}
	sort.Slice(generations, func(i, j int) bool { return generations[i].mod > generations[j].mod })
	keptPrevious := false
	for _, g := range generations {
		if g.name == current {
			continue
		}
		if !keptPrevious {
			keptPrevious = true
			continue
		}
		s, err := removeAllCounting(filepath.Join(gensDir, g.name))
		if err != nil {
			return stats, err
		}
		stats.Add(s)
	}
	return stats, nil
}

func (c *Cache) sweepProviderLogos(providerDir string, keepIDs map[string]struct{}, keepFiles map[string]string) (ClearStats, error) {
	var stats ClearStats
	logosDir := filepath.Join(providerDir, dirLogos)
	entries, err := os.ReadDir(logosDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return stats, nil
		}
		return stats, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		isMeta := strings.HasSuffix(name, ".meta")
		base := strings.TrimSuffix(name, ".meta")
		channelID, ok := logoChannelID(base)
		if !ok {
			continue
		}
		if keepIDs != nil {
			if _, ok := keepIDs[channelID]; !ok {
				s, err := removeFileCounting(filepath.Join(logosDir, name))
				if err != nil {
					return stats, err
				}
				stats.Add(s)
				continue
			}
		}
		if keepFiles != nil {
			if want, ok := keepFiles[channelID]; ok && want != "" && base != want {
				s, err := removeFileCounting(filepath.Join(logosDir, name))
				if err != nil {
					return stats, err
				}
				stats.Add(s)
				_ = isMeta
				continue
			}
		}
	}
	return stats, nil
}

func logoChannelID(filename string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		return strings.TrimSuffix(filename, filepath.Ext(filename)), true
	default:
		return "", false
	}
}

func readCurrentName(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(b))
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return ""
	}
	return name
}

func dirUsage(root string) (bytes int64, files int, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files++
		bytes += info.Size()
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return 0, 0, nil
	}
	return bytes, files, err
}

func removeAllCounting(path string) (ClearStats, error) {
	var stats ClearStats
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		stats.DeletedFiles++
		stats.DeletedBytes += info.Size()
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ClearStats{}, err
	}
	if err := os.RemoveAll(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return stats, err
	}
	return stats, nil
}

func removeFileCounting(path string) (ClearStats, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ClearStats{}, nil
		}
		return ClearStats{}, err
	}
	if info.IsDir() {
		return removeAllCounting(path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ClearStats{}, err
	}
	return ClearStats{DeletedFiles: 1, DeletedBytes: info.Size()}, nil
}

func validRawName(name string) bool {
	return name != "" &&
		name == filepath.Base(name) &&
		!strings.ContainsAny(name, `/\`) &&
		name != "." &&
		name != ".."
}

// atomicWrite writes data to path via a temp file + rename, creating parents.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cache: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("cache: temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("cache: write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("cache: sync %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("cache: rename %s: %w", path, err)
	}
	return syncDir(dir)
}

func writeSynced(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("cache: create %s: %w", path, err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("cache: write %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("cache: sync %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cache: open directory %s: %w", path, err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("cache: sync directory %s: %w", path, err)
	}
	return nil
}
