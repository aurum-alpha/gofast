// Package cache is the sole owner of the on-disk generated artifacts. It maps a
// provider id to its files and exposes typed reads/writes; no other package
// knows the layout or touches disk, so persistence is controlled centrally.
//
// Layout under root:
//
//	<id>/current                         selected generation name
//	<id>/status.json                     refresh attempt/error state
//	<id>/generations/<generation>/raw    provider-native response
//	<id>/generations/<generation>/*.m3u  per-provider playlist
//	<id>/generations/<generation>/*.xml  per-provider guide
//	<id>/generations/<generation>/meta.json
//	playlist.m3u      aggregate playlist (provider-namespaced ids)
//	epg.xml           aggregate XMLTV (provider-namespaced ids)
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
	fileRaw        = "raw"
	fileStatus     = "status.json"
	fileCurrent    = "current"
	dirGenerations = "generations"

	aggM3U = "playlist.m3u"
	aggXML = "epg.xml"
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
func (c *Cache) CommitProvider(id model.ProviderID, raw []byte, m3u M3U, xml XMLTV, meta provider.Meta) error {
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
	files := []struct {
		name string
		data []byte
	}{
		{fileRaw, raw},
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

// ReadRaw returns a provider's last raw upstream response (fs.ErrNotExist if none).
func (c *Cache) ReadRaw(id model.ProviderID) ([]byte, error) {
	dir, _, err := c.selectedDir(id)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(dir, fileRaw))
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

// WriteAggregate atomically writes the combined playlist + guide.
func (c *Cache) WriteAggregate(m3u M3U, xml XMLTV) error {
	if err := atomicWrite(filepath.Join(c.root, aggM3U), m3u); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(c.root, aggXML), xml)
}

// ReadAggregateM3U returns the combined playlist (fs.ErrNotExist if not generated).
func (c *Cache) ReadAggregateM3U() (M3U, error) {
	b, err := os.ReadFile(filepath.Join(c.root, aggM3U))
	return M3U(b), err
}

// ReadAggregateXMLTV returns the combined guide (fs.ErrNotExist if not generated).
func (c *Cache) ReadAggregateXMLTV() (XMLTV, error) {
	b, err := os.ReadFile(filepath.Join(c.root, aggXML))
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
func (c *Cache) LoadProvider(id model.ProviderID) ([]byte, provider.Meta, bool, error) {
	dir, legacy, err := c.selectedDir(id)
	if err != nil {
		return nil, provider.Meta{}, false, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, fileRaw))
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
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".staging-") {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			generations = append(generations, generation{entry.Name(), info.ModTime().UnixNano()})
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
