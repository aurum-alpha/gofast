// Package logocache downloads channel logos and rewrites LogoURL to local
// /logos/... paths served by fastgen. On-disk storage is owned by internal/cache
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
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
)

const DefaultMaxAge = 7 * 24 * time.Hour

// Cache downloads logos into the shared disk cache and rewrites channel URLs.
type Cache struct {
	store   *cache.Cache
	client  *http.Client
	baseURL string
	maxAge  time.Duration
}

// New returns a logo cache that persists under store ({provider}/logos/).
// baseURL is the public origin with no trailing slash. Zero maxAge uses DefaultMaxAge.
func New(store *cache.Cache, client *http.Client, baseURL string, maxAge time.Duration) *Cache {
	if client == nil {
		client = http.DefaultClient
	}
	if maxAge <= 0 {
		maxAge = DefaultMaxAge
	}
	return &Cache{
		store:   store,
		client:  client,
		baseURL: strings.TrimRight(baseURL, "/"),
		maxAge:  maxAge,
	}
}

// Rewrite updates LogoURL on each channel that has one, preserving the provider
// original in LogoSourceURL. Failures keep the upstream LogoURL except hard
// HTTP 403/404, which clear LogoURL and set LogoError (source URL is retained).
func (c *Cache) Rewrite(ctx context.Context, channels []model.Channel) {
	c.RewriteProgress(ctx, channels, nil)
}

// RewriteProgress is Rewrite with an optional per-logo callback (after each Ensure).
func (c *Cache) RewriteProgress(ctx context.Context, channels []model.Channel, onEach func()) {
	if c == nil {
		return
	}
	for i := range channels {
		if err := ctx.Err(); err != nil {
			return
		}
		if channels[i].LogoURL == "" {
			continue
		}
		channels[i].LogoSourceURL = channels[i].LogoURL
		channels[i].LogoURL, channels[i].LogoError = c.Ensure(ctx, channels[i])
		if onEach != nil {
			onEach()
		}
	}
}

// Ensure downloads or revalidates the channel logo and returns the public local
// URL (or upstream on soft failure). logoError is set when LogoURL is cleared
// after a hard upstream failure (HTTP 403/404).
//
// On each refresh:
//   - upstream URL change → unconditional GET
//   - same URL with ETag/Last-Modified → conditional GET
//   - same URL, no validators, file within maxAge → skip HTTP
func (c *Cache) Ensure(ctx context.Context, ch model.Channel) (logoURL string, logoError string) {
	if c == nil || c.store == nil || ch.LogoURL == "" {
		return ch.LogoURL, ""
	}
	id := ch.NormalizedID
	if id == "" {
		id = model.NormalizeID(ch.ID)
	}
	ext, err := extensionHint(ch.LogoURL)
	if err != nil {
		slog.Warn("logo cache skip: bad logo url",
			"provider", ch.Provider, "id", id, "err", err)
		return ch.LogoURL, ""
	}

	file := id + ext
	public := c.baseURL + "/logos/" + string(ch.Provider) + "/" + file
	meta := c.readMeta(ch.Provider, file)
	_, statErr := c.store.StatLogo(ch.Provider, file)
	hasFile := statErr == nil

	urlChanged := meta.SourceURL != "" && meta.SourceURL != ch.LogoURL
	hasValidators := meta.ETag != "" || meta.LastModified != ""

	if hasFile && !urlChanged {
		if !hasValidators && c.withinMaxAge(ch.Provider, file) {
			return public, ""
		}
		// Validators present (or file past maxAge without them): revalidate below.
	}

	conditional := hasFile && !urlChanged && hasValidators
	body, newExt, newMeta, err := c.fetch(ctx, ch, meta, conditional)
	if err != nil {
		var status httpStatusError
		if errors.As(err, &status) && (status.code == http.StatusForbidden || status.code == http.StatusNotFound) {
			slog.Warn("logo download failed; clearing logo",
				"provider", ch.Provider, "id", id, "err", err)
			return "", status.Error()
		}
		slog.Warn("logo download failed; keeping upstream url",
			"provider", ch.Provider, "id", id, "err", err)
		return ch.LogoURL, ""
	}
	if newExt != "" && newExt != ext {
		ext = newExt
		file = id + ext
		public = c.baseURL + "/logos/" + string(ch.Provider) + "/" + file
	}
	if body != nil {
		if err := c.store.WriteLogo(ch.Provider, file, body); err != nil {
			slog.Warn("logo write failed; keeping upstream url",
				"provider", ch.Provider, "id", id, "err", err)
			return ch.LogoURL, ""
		}
	} else if _, err := c.store.StatLogo(ch.Provider, file); err != nil {
		// 304 without a local file — treat as miss.
		return ch.LogoURL, ""
	}
	newMeta.SourceURL = ch.LogoURL
	_ = c.writeMeta(ch.Provider, file, newMeta)
	return public, ""
}

func (c *Cache) withinMaxAge(id model.ProviderID, file string) bool {
	info, err := c.store.StatLogo(id, file)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) <= c.maxAge
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

type fileMeta struct {
	SourceURL    string `json:"source_url,omitempty"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
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
