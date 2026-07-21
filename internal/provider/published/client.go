// Package published implements providers backed by maintained M3U/XMLTV pairs.
package published

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/m3u"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/xmltv"
)

const (
	RawGuide        = "guide.xml"
	RawGuideGzip    = "guide.xml.gz"
	RawPlaylist     = "playlist.m3u"
	maxResponseSize = 64 << 20
)

var _ provider.Reader = (*Client)(nil)

// Source describes one compile-time published-pair provider.
type Source struct {
	ID      model.ProviderID
	M3UURL  string
	EPGURL  string
	EPGGzip bool
	// GroupPrefix removes an upstream provider prefix before GoFAST applies its
	// own provider label (for example LocalNow🇺🇸:).
	GroupPrefix string
}

// Client fetches and parses one published M3U/XMLTV pair.
type Client struct {
	source  Source
	client  *httpx.Client
	m3uURL  string
	epgURL  string
	headers map[string]string
}

// New constructs a published-pair client from compile-time URLs and effective settings.
func New(source Source, settings model.ProviderSettings, client *httpx.Client) *Client {
	if client == nil {
		client = httpx.NewClient(0, 0)
	}
	m3uURL := settings.M3UURL
	if m3uURL == "" {
		m3uURL = source.M3UURL
	}
	epgURL := settings.EPGURL
	if epgURL == "" {
		epgURL = source.EPGURL
	}
	headers := makeHeaders(settings.UserAgent, settings.Headers)
	return &Client{
		source:  source,
		client:  client,
		m3uURL:  m3uURL,
		epgURL:  epgURL,
		headers: headers,
	}
}

// Fetch downloads both exact upstream files. A failure in either request
// returns no partial raw snapshot.
func (c *Client) Fetch(ctx context.Context) (provider.Raw, error) {
	playlist, err := c.fetch(ctx, c.m3uURL)
	if err != nil {
		return nil, fmt.Errorf("%s playlist: %w", c.source.ID, err)
	}
	guide, err := c.fetch(ctx, c.epgURL)
	if err != nil {
		return nil, fmt.Errorf("%s guide: %w", c.source.ID, err)
	}
	return provider.Raw{
		RawPlaylist:      playlist,
		c.guideRawName(): guide,
	}, nil
}

// Parse decodes a cached upstream pair without network access.
func (c *Client) Parse(raw provider.Raw) ([]model.Channel, []model.Programme, error) {
	playlist, ok := raw[RawPlaylist]
	if !ok {
		return nil, nil, fmt.Errorf("%s: missing %s", c.source.ID, RawPlaylist)
	}
	guide, ok := raw[c.guideRawName()]
	if !ok {
		return nil, nil, fmt.Errorf("%s: missing %s", c.source.ID, c.guideRawName())
	}

	channels, err := m3u.Parse(bytes.NewReader(playlist))
	if err != nil {
		return nil, nil, fmt.Errorf("%s playlist: %w", c.source.ID, err)
	}
	for index := range channels {
		if c.source.GroupPrefix != "" && strings.HasPrefix(channels[index].Group, c.source.GroupPrefix) {
			channels[index].Group = strings.TrimSpace(strings.TrimPrefix(channels[index].Group, c.source.GroupPrefix))
		}
		channels[index].RequestHeaders = cloneHeaders(c.headers)
	}

	guideReader, closeGuide, err := openGuide(guide)
	if err != nil {
		return nil, nil, fmt.Errorf("%s guide: %w", c.source.ID, err)
	}
	defer closeGuide()
	document, err := xmltv.Parse(guideReader)
	if err != nil {
		return nil, nil, fmt.Errorf("%s guide: %w", c.source.ID, err)
	}
	matched, rate := guideMatch(channels, document.ChannelIDs)
	slog.Info("published-pair guide match",
		"provider", c.source.ID,
		"playlist_channels", len(channels),
		"guide_channels", len(document.ChannelIDs),
		"matched_channels", matched,
		"match_rate", rate,
	)
	return channels, document.Programmes, nil
}

func (c *Client) fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	resp, err := c.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseSize {
		return nil, fmt.Errorf("response exceeds %d bytes", maxResponseSize)
	}
	return body, nil
}

func (c *Client) guideRawName() string {
	if c.source.EPGGzip {
		return RawGuideGzip
	}
	return RawGuide
}

func openGuide(data []byte) (io.Reader, func(), error) {
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return bytes.NewReader(data), func() {}, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, func() {}, err
	}
	return reader, func() { _ = reader.Close() }, nil
}
