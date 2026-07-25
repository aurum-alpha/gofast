// Package mjh implements the shared i.mjh.nz channel metadata and XMLTV format.
package mjh

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

const (
	RawChannels = "channels.json.gz"
	RawGuide    = "guide.xml.gz"

	baseURL    = "https://i.mjh.nz"
	streamBase = "https://jmp2.uk/"
	maxRawSize = 64 << 20
)

var _ provider.Reader = (*Client)(nil)

// Source describes one compile-time i.mjh.nz provider shape.
type Source struct {
	ID         model.ProviderID
	Directory  string
	Regionless bool
	// TaggedRegions means channels live at the top-level map and each channel
	// lists membership in a regions[] array (Plex). Region metadata is still
	// required for headers / EPG path ({region}.xml.gz); nested region.channels
	// maps are ignored.
	TaggedRegions bool
	DefaultSlug   string
}

// Client fetches and parses one MJH source.
type Client struct {
	source      Source
	settings    model.ProviderSettings
	client      *httpx.Client
	channelsURL string
	guideURL    string
}

// New constructs an MJH client from a compile-time source and effective settings.
func New(source Source, settings model.ProviderSettings, client *httpx.Client) *Client {
	if client == nil {
		client = httpx.NewClient(0, 0)
	}
	channelsURL := settings.ChannelsURL
	if channelsURL == "" {
		channelsURL = fmt.Sprintf("%s/%s/.channels.json.gz", baseURL, source.Directory)
	}
	guideURL := settings.EPGURL
	if guideURL == "" {
		guideName := "all.xml.gz"
		if !source.Regionless {
			guideName = settings.Region + ".xml.gz"
		}
		guideURL = fmt.Sprintf("%s/%s/%s", baseURL, source.Directory, guideName)
	}
	return &Client{
		source:      source,
		settings:    settings,
		client:      client,
		channelsURL: channelsURL,
		guideURL:    guideURL,
	}
}

// Fetch downloads both exact compressed upstream payloads. Neither becomes
// visible unless the complete refresh later commits successfully.
func (c *Client) Fetch(ctx context.Context) (provider.Raw, error) {
	channels, err := c.fetch(ctx, c.channelsURL, c.settings.Headers)
	if err != nil {
		return nil, fmt.Errorf("%s channels: %w", c.source.ID, err)
	}
	headers, err := c.headers(channels)
	if err != nil {
		return nil, err
	}
	guide, err := c.fetch(ctx, c.guideURL, headers)
	if err != nil {
		return nil, fmt.Errorf("%s guide: %w", c.source.ID, err)
	}
	return provider.Raw{
		RawChannels: channels,
		RawGuide:    guide,
	}, nil
}

// Parse decodes cached compressed metadata and XMLTV without network access.
func (c *Client) Parse(raw provider.Raw) ([]model.Channel, []model.Programme, error) {
	channelsRaw, ok := raw[RawChannels]
	if !ok {
		return nil, nil, fmt.Errorf("%s: missing %s", c.source.ID, RawChannels)
	}
	guideRaw, ok := raw[RawGuide]
	if !ok {
		return nil, nil, fmt.Errorf("%s: missing %s", c.source.ID, RawGuide)
	}
	metadata, err := decodeMetadata(channelsRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("%s metadata: %w", c.source.ID, err)
	}
	channels, err := c.channels(metadata)
	if err != nil {
		return nil, nil, err
	}
	known := make(map[string]struct{}, len(channels))
	for _, channel := range channels {
		known[channel.ID] = struct{}{}
	}
	programmes, err := decodeGuide(guideRaw, known)
	if err != nil {
		return nil, nil, fmt.Errorf("%s guide: %w", c.source.ID, err)
	}
	return channels, programmes, nil
}

func (c *Client) channels(metadata metadata) ([]model.Channel, error) {
	rawChannels, headers, err := c.selectChannels(metadata)
	if err != nil {
		return nil, err
	}
	mergeHeaders(headers, c.settings.Headers)

	slugTemplate := c.settings.SlugTemplate
	if slugTemplate == "" {
		slugTemplate = metadata.Slug
	}
	if slugTemplate == "" {
		slugTemplate = c.source.DefaultSlug
	}
	if slugTemplate == "" {
		err := fmt.Errorf("%s: missing slug and slug_template", c.source.ID)
		slog.Warn("mjh provider has no stream slug", "provider", c.source.ID, "err", err)
		return nil, err
	}

	ids := make([]string, 0, len(rawChannels))
	for id := range rawChannels {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	channels := make([]model.Channel, 0, len(ids))
	skipped := 0
	for _, id := range ids {
		raw := rawChannels[id]
		if strings.TrimSpace(id) == "" || strings.TrimSpace(raw.Name) == "" {
			provider.SkipMalformed(&skipped)
			continue
		}
		number, err := parseChannelNumber(raw.ChannelNumber)
		if err != nil {
			provider.SkipMalformed(&skipped)
			continue
		}
		group := raw.Group
		if group == "" && len(raw.Groups) > 0 {
			group = raw.Groups[0]
		}
		channel := model.Channel{
			ID:             id,
			Name:           raw.Name,
			Description:    raw.Description,
			Group:          group,
			Number:         number,
			StreamURL:      streamBase + strings.ReplaceAll(slugTemplate, "{id}", id),
			LogoURL:        raw.Logo,
			LicenseURL:     raw.LicenseURL,
			RequestHeaders: cloneHeaders(headers),
		}
		if raw.LicenseURL != "" {
			channel.Classification = model.ClassDRM
		}
		channels = append(channels, channel)
	}
	if skipped > 0 {
		slog.Warn("mjh skipped malformed channels", "provider", c.source.ID, "count", skipped)
	}
	return channels, nil
}

// selectChannels returns the channel map and base request headers for this
// source shape (region-nested, tagged top-level, or regionless).
func (c *Client) selectChannels(metadata metadata) (map[string]rawChannel, map[string]string, error) {
	headers := cloneHeaders(metadata.Headers)
	switch {
	case c.source.TaggedRegions:
		regionName := strings.TrimSpace(c.settings.Region)
		if regionName == "" {
			return nil, nil, fmt.Errorf("%s: region required for tagged-region metadata", c.source.ID)
		}
		region, ok := metadata.Regions[regionName]
		if !ok {
			return nil, nil, fmt.Errorf("%s: region %q not found", c.source.ID, regionName)
		}
		mergeHeaders(headers, region.Headers)
		filtered := make(map[string]rawChannel)
		for id, raw := range metadata.Channels {
			if channelInRegion(raw, regionName) {
				filtered[id] = raw
			}
		}
		return filtered, headers, nil
	case !c.source.Regionless:
		region, ok := metadata.Regions[c.settings.Region]
		if !ok {
			return nil, nil, fmt.Errorf("%s: region %q not found", c.source.ID, c.settings.Region)
		}
		mergeHeaders(headers, region.Headers)
		return region.Channels, headers, nil
	default:
		return metadata.Channels, headers, nil
	}
}

func (c *Client) fetch(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRawSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxRawSize {
		return nil, fmt.Errorf("response exceeds %d bytes", maxRawSize)
	}
	return body, nil
}

func (c *Client) headers(channelsRaw []byte) (map[string]string, error) {
	metadata, err := decodeMetadata(channelsRaw)
	if err != nil {
		return nil, fmt.Errorf("%s metadata: %w", c.source.ID, err)
	}
	_, headers, err := c.selectChannels(metadata)
	if err != nil {
		return nil, err
	}
	mergeHeaders(headers, c.settings.Headers)
	return headers, nil
}

func decodeGzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func decodeMetadata(data []byte) (metadata, error) {
	decoded, err := decodeGzip(data)
	if err != nil {
		return metadata{}, err
	}
	var result metadata
	if err := json.Unmarshal(decoded, &result); err != nil {
		return metadata{}, err
	}
	return result, nil
}
