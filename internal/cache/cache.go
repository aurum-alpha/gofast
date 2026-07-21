// Package cache is the sole owner of the on-disk generated artifacts. It maps a
// provider id to its files and exposes typed reads/writes; no other package
// knows the layout or touches disk, so persistence is controlled centrally.
//
// Layout under root:
//
//	<id>/raw          debug backup of the raw upstream response
//	<id>/playlist.m3u per-provider playlist (bare ids)
//	<id>/guide.xml    per-provider XMLTV (bare ids)
//	<id>/meta.json    provider.Lineup
//	playlist.m3u      aggregate playlist (provider-namespaced ids)
//	epg.xml           aggregate XMLTV (provider-namespaced ids)
package cache

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

const (
	fileM3U  = "playlist.m3u"
	fileXML  = "guide.xml"
	fileMeta = "meta.json"
	fileRaw  = "raw"

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

// WriteProvider atomically writes a provider's playlist, guide, and meta.json.
// meta.json is written last so it acts as the commit marker for the set.
func (c *Cache) WriteProvider(id model.ProviderID, m3u M3U, xml XMLTV, meta provider.Meta) error {
	dir, err := c.providerDir(id)
	if err != nil {
		return err
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("cache: marshal meta: %w", err)
	}
	if err := atomicWrite(filepath.Join(dir, fileM3U), m3u); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(dir, fileXML), xml); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, fileMeta), metaBytes)
}

// WriteRaw archives a provider's raw upstream response — the canonical snapshot
// re-parsed on boot to rebuild channels/programmes without a network call.
func (c *Cache) WriteRaw(id model.ProviderID, raw []byte) error {
	dir, err := c.providerDir(id)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, fileRaw), raw)
}

// ReadRaw returns a provider's last raw upstream response (fs.ErrNotExist if none).
func (c *Cache) ReadRaw(id model.ProviderID) ([]byte, error) {
	dir, err := c.providerDir(id)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(dir, fileRaw))
}

// ReadM3U returns a provider's playlist (fs.ErrNotExist if not yet generated).
func (c *Cache) ReadM3U(id model.ProviderID) (M3U, error) {
	dir, err := c.providerDir(id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, fileM3U))
	return M3U(b), err
}

// ReadXMLTV returns a provider's guide (fs.ErrNotExist if not yet generated).
func (c *Cache) ReadXMLTV(id model.ProviderID) (XMLTV, error) {
	dir, err := c.providerDir(id)
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
	dir, err := c.providerDir(id)
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

// providerDir returns {root}/{id}, rejecting ids that could escape the root.
func (c *Cache) providerDir(id model.ProviderID) (string, error) {
	name := string(id)
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fs.ErrNotExist
	}
	return filepath.Join(c.root, name), nil
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
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("cache: rename %s: %w", path, err)
	}
	return nil
}
