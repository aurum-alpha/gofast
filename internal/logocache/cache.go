// Package logocache rewrites channel logos to local /logos/... URLs and
// lazily fetches artwork on request. On-disk storage is owned by internal/cache
// under {provider}/logos/. Artwork TLS exceptions apply only to this package's
// HTTP client — never to stream or EPG clients.
package logocache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
)

// DefaultMaxAge is how long a cached logo is served without revalidation.
const DefaultMaxAge = 24 * time.Hour

const defaultWorkers = 10

// SourceFunc resolves the upstream logo URL (and optional request headers) for
// a provider/channel when the disk meta has no SourceURL.
type SourceFunc func(provider model.ProviderID, channelID string) (sourceURL string, headers map[string]string, ok bool)

// Cache rewrites emit URLs and lazily fills disk logos via a worker pool.
type Cache struct {
	store   *cache.Cache
	client  *http.Client
	baseURL string
	maxAge  time.Duration

	jobs     chan *fetchJob
	stopOnce sync.Once
	stop     chan struct{}
	wg       sync.WaitGroup

	mu       sync.Mutex
	inflight map[string]*fetchWait
}

// New returns a logo cache. Zero maxAge uses DefaultMaxAge. Starts defaultWorkers
// fetch workers; call Close when replacing the cache (e.g. base_url reload).
func New(store *cache.Cache, client *http.Client, baseURL string, maxAge time.Duration) *Cache {
	if client == nil {
		client = http.DefaultClient
	}
	if maxAge <= 0 {
		maxAge = DefaultMaxAge
	}
	c := &Cache{
		store:    store,
		client:   client,
		baseURL:  strings.TrimRight(baseURL, "/"),
		maxAge:   maxAge,
		jobs:     make(chan *fetchJob),
		stop:     make(chan struct{}),
		inflight: make(map[string]*fetchWait),
	}
	c.startWorkers(defaultWorkers)
	return c
}

// Close stops fetch workers. Safe to call once.
func (c *Cache) Close() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() {
		close(c.stop)
	})
	c.wg.Wait()
}

// BaseURL returns the public origin used in rewritten logo URLs.
func (c *Cache) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// RewriteURLs sets LogoSourceURL from the upstream LogoURL (when needed) and
// points LogoURL at {base}/logos/{provider}/{id}{ext}. No HTTP. Hard-invalidates
// disk bytes when the upstream source URL changed since the last meta.
func (c *Cache) RewriteURLs(channels []model.Channel) {
	if c == nil {
		return
	}
	for i := range channels {
		src := emitSourceURL(channels[i], c.baseURL)
		if src == "" {
			continue
		}
		id := channels[i].NormalizedID
		if id == "" {
			id = model.NormalizeID(channels[i].ID)
		}
		ext, err := extensionHint(src)
		if err != nil {
			slog.Warn("logo cache skip: bad logo url",
				"provider", channels[i].Provider, "id", id, "err", err)
			continue
		}
		file := id + ext
		c.invalidateIfSourceChanged(channels[i].Provider, id, src)
		channels[i].LogoSourceURL = src
		channels[i].LogoURL = c.publicURL(channels[i].Provider, file)
		channels[i].LogoError = ""
		meta := c.readMeta(channels[i].Provider, file)
		meta.SourceURL = src
		_ = c.writeMeta(channels[i].Provider, file, meta)
	}
}

func emitSourceURL(ch model.Channel, base string) string {
	u := strings.TrimSpace(ch.LogoURL)
	if u == "" {
		return strings.TrimSpace(ch.LogoSourceURL)
	}
	if isLocalLogoURL(u, base) {
		return strings.TrimSpace(ch.LogoSourceURL)
	}
	return u
}

func isLocalLogoURL(raw, base string) bool {
	base = strings.TrimRight(base, "/")
	if base == "" {
		return false
	}
	return strings.HasPrefix(raw, base+"/logos/")
}

func (c *Cache) publicURL(provider model.ProviderID, file string) string {
	return c.baseURL + "/logos/" + string(provider) + "/" + file
}

func (c *Cache) invalidateIfSourceChanged(provider model.ProviderID, channelID, newSource string) {
	if c.store == nil || newSource == "" || channelID == "" {
		return
	}
	file := c.findLogoFile(provider, channelID)
	if file == "" {
		return
	}
	meta := c.readMeta(provider, file)
	if meta.SourceURL != "" && meta.SourceURL != newSource {
		_, _ = c.store.DeleteChannelLogos(provider, channelID)
	}
}

func (c *Cache) withinMaxAge(id model.ProviderID, file string) bool {
	info, err := c.store.StatLogo(id, file)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) <= c.maxAge
}

// Ensure fetches or revalidates one logo (used by tests and the worker pool).
// LogoURL on ch must be the upstream source URL. Returns public local URL.
func (c *Cache) Ensure(ctx context.Context, ch model.Channel) (logoURL string, logoError string) {
	if c == nil || c.store == nil {
		return ch.LogoURL, ""
	}
	src := strings.TrimSpace(ch.LogoURL)
	if src == "" {
		src = strings.TrimSpace(ch.LogoSourceURL)
	}
	if src == "" {
		return "", ""
	}
	id := ch.NormalizedID
	if id == "" {
		id = model.NormalizeID(ch.ID)
	}
	ext, err := extensionHint(src)
	if err != nil {
		return "", ""
	}
	file := id + ext
	ch.LogoURL = src
	public := c.publicURL(ch.Provider, file)
	meta := c.readMeta(ch.Provider, file)
	_, statErr := c.store.StatLogo(ch.Provider, file)
	hasFile := statErr == nil
	urlChanged := meta.SourceURL != "" && meta.SourceURL != src
	if hasFile && !urlChanged {
		hasValidators := meta.ETag != "" || meta.LastModified != ""
		if !hasValidators && c.freshAge(ch.Provider, file, meta) {
			return public, ""
		}
	}
	forceFull := !hasFile || urlChanged
	if err := c.fetchToDisk(ctx, ch.Provider, file, src, ch.RequestHeaders, forceFull); err != nil {
		var status httpStatusError
		if errors.As(err, &status) && (status.code == http.StatusForbidden || status.code == http.StatusNotFound) {
			return "", status.Error()
		}
		slog.Warn("logo download failed; keeping upstream url",
			"provider", ch.Provider, "id", id, "err", err)
		return src, ""
	}
	if _, err := c.store.StatLogo(ch.Provider, file); err != nil {
		if alt := c.findLogoFile(ch.Provider, id); alt != "" {
			file = alt
		}
	}
	return c.publicURL(ch.Provider, file), ""
}

func (c *Cache) findLogoFile(provider model.ProviderID, channelID string) string {
	for _, ext := range []string{".png", ".jpg", ".gif", ".webp", ".svg"} {
		file := channelID + ext
		if _, err := c.store.StatLogo(provider, file); err == nil {
			return file
		}
	}
	return ""
}

// fetchToDisk performs full or conditional GET and writes the result.
// forceFull skips conditional headers (e.g. after hard invalidate / miss).
func (c *Cache) fetchToDisk(ctx context.Context, provider model.ProviderID, file, sourceURL string, headers map[string]string, forceFull bool) error {
	meta := c.readMeta(provider, file)
	_, statErr := c.store.StatLogo(provider, file)
	hasFile := statErr == nil
	conditional := !forceFull && hasFile && meta.SourceURL == sourceURL && (meta.ETag != "" || meta.LastModified != "")

	ch := model.Channel{
		Provider:       provider,
		LogoURL:        sourceURL,
		RequestHeaders: headers,
	}
	body, newExt, newMeta, err := c.fetch(ctx, ch, meta, conditional)
	if err != nil {
		return err
	}
	id, _ := logoChannelID(file)
	ext := filepath.Ext(file)
	if newExt != "" && newExt != ext {
		if id != "" {
			_, _ = c.store.DeleteLogo(provider, file)
			file = id + newExt
		}
	}
	if body != nil {
		if err := c.store.WriteLogo(provider, file, body); err != nil {
			return err
		}
	} else if !hasFile {
		if _, err := c.store.StatLogo(provider, file); err != nil {
			return fmt.Errorf("304 without local file")
		}
	} else {
		// 304: touch mtime by rewriting meta; bump freshness via WriteLogoMeta only.
		// Stat mtime won't change — update a FetchedAt in meta for age checks.
	}
	newMeta.SourceURL = sourceURL
	newMeta.FetchedAt = time.Now().UTC()
	if err := c.writeMeta(provider, file, newMeta); err != nil {
		return err
	}
	// Touch file mtime on 304 so withinMaxAge advances.
	if body == nil && hasFile {
		if data, err := c.store.ReadLogo(provider, file); err == nil {
			_ = c.store.WriteLogo(provider, file, data)
		}
	}
	return nil
}

func (c *Cache) fetch(ctx context.Context, ch model.Channel, meta fileMeta, conditional bool) (body []byte, ext string, out fileMeta, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ch.LogoURL, nil)
	if err != nil {
		return nil, "", fileMeta{}, err
	}
	for k, v := range ch.RequestHeaders {
		req.Header.Set(k, v)
	}
	if conditional {
		if meta.ETag != "" {
			req.Header.Set("If-None-Match", meta.ETag)
		}
		if meta.LastModified != "" {
			req.Header.Set("If-Modified-Since", meta.LastModified)
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", fileMeta{}, err
	}
	defer resp.Body.Close()

	out.SourceURL = ch.LogoURL
	out.ETag = resp.Header.Get("ETag")
	out.LastModified = resp.Header.Get("Last-Modified")
	if out.ETag == "" {
		out.ETag = meta.ETag
	}
	if out.LastModified == "" {
		out.LastModified = meta.LastModified
	}

	switch resp.StatusCode {
	case http.StatusNotModified:
		return nil, "", out, nil
	case http.StatusOK:
		data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return nil, "", fileMeta{}, err
		}
		ext = extensionFromContentType(resp.Header.Get("Content-Type"))
		if ext == "" {
			ext, _ = extensionHint(ch.LogoURL)
		}
		return data, ext, out, nil
	default:
		return nil, "", fileMeta{}, httpStatusError{code: resp.StatusCode}
	}
}

type httpStatusError struct {
	code int
}

func (e httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d", e.code)
}

func (e httpStatusError) HTTPStatus() int { return e.code }

type fileMeta struct {
	SourceURL    string    `json:"source_url,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	FetchedAt    time.Time `json:"fetched_at,omitempty"`
}

func (c *Cache) readMeta(id model.ProviderID, file string) fileMeta {
	data, err := c.store.ReadLogoMeta(id, file)
	if err != nil {
		return fileMeta{}
	}
	var m fileMeta
	if json.Unmarshal(data, &m) != nil {
		return fileMeta{}
	}
	return m
}

func (c *Cache) writeMeta(id model.ProviderID, file string, m fileMeta) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return c.store.WriteLogoMeta(id, file, data)
}

func logoChannelID(filename string) (string, bool) {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	if base == "" || base != filepath.Base(base) {
		return "", false
	}
	return base, true
}

func extensionHint(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	ext := strings.ToLower(filepath.Ext(u.Path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		if ext == ".jpeg" {
			return ".jpg", nil
		}
		return ext, nil
	default:
		return ".png", nil
	}
}

func extensionFromContentType(ct string) string {
	ct = strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	switch ct {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	default:
		return ""
	}
}
