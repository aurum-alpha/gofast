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
	// guideURL is used for regionless sources, or single-region when EPGURL is set.
	guideURL string
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
			regs := configuredRegions(settings.Region)
			if len(regs) == 1 {
				guideName = mjhRegionKey(regs[0]) + ".xml.gz"
			}
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

func guideRawKey(region string) string {
	return "guide." + model.NormalizeRegionCode(region) + ".xml.gz"
}

func configuredRegions(regionCSV string) []string {
	return model.ParseRegionList(regionCSV)
}

// mjhRegionKey is the lowercase key i.mjh.nz uses in metadata and filenames.
func mjhRegionKey(region string) string {
	return strings.ToLower(model.NormalizeRegionCode(region))
}

// Fetch downloads channels metadata and one guide per usable region.
func (c *Client) Fetch(ctx context.Context) (provider.Raw, error) {
	channels, err := c.fetch(ctx, c.channelsURL, c.settings.Headers)
	if err != nil {
		return nil, fmt.Errorf("%s channels: %w", c.source.ID, err)
	}
	raw := provider.Raw{RawChannels: channels}

	if c.source.Regionless {
		headers, err := c.headers(channels)
		if err != nil {
			return nil, err
		}
		guide, err := c.fetch(ctx, c.guideURL, headers)
		if err != nil {
			return nil, fmt.Errorf("%s guide: %w", c.source.ID, err)
		}
		raw[RawGuide] = guide
		return raw, nil
	}

	metadata, err := decodeMetadata(channels)
	if err != nil {
		return nil, fmt.Errorf("%s metadata: %w", c.source.ID, err)
	}
	usable := c.usableRegions(metadata)
	if len(usable) == 0 {
		return nil, fmt.Errorf("%s: no usable regions from %q", c.source.ID, c.settings.Region)
	}

	singleOverride := len(usable) == 1 && strings.TrimSpace(c.settings.EPGURL) != ""
	for _, region := range usable {
		headers := c.regionFetchHeaders(metadata, region)
		var guideURL string
		if singleOverride {
			guideURL = c.settings.EPGURL
		} else {
			guideURL = fmt.Sprintf("%s/%s/%s.xml.gz", baseURL, c.source.Directory, mjhRegionKey(region))
		}
		guide, err := c.fetch(ctx, guideURL, headers)
		if err != nil {
			return nil, fmt.Errorf("%s guide %s: %w", c.source.ID, region, err)
		}
		if len(usable) == 1 {
			raw[RawGuide] = guide
		} else {
			raw[guideRawKey(region)] = guide
		}
	}
	return raw, nil
}

// Parse decodes cached compressed metadata and XMLTV without network access.
func (c *Client) Parse(raw provider.Raw) ([]model.Channel, []model.Programme, error) {
	channelsRaw, ok := raw[RawChannels]
	if !ok {
		return nil, nil, fmt.Errorf("%s: missing %s", c.source.ID, RawChannels)
	}
	metadata, err := decodeMetadata(channelsRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("%s metadata: %w", c.source.ID, err)
	}

	if c.source.Regionless {
		guideRaw, ok := raw[RawGuide]
		if !ok {
			return nil, nil, fmt.Errorf("%s: missing %s", c.source.ID, RawGuide)
		}
		channels, err := c.buildChannels(metadata, "", false)
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

	usable := c.usableRegions(metadata)
	if len(usable) == 0 {
		return nil, nil, fmt.Errorf("%s: no usable regions from %q", c.source.ID, c.settings.Region)
	}
	multi := len(usable) > 1

	var channels []model.Channel
	var programmes []model.Programme
	for _, region := range usable {
		regionChannels, err := c.buildChannels(metadata, region, multi)
		if err != nil {
			return nil, nil, err
		}
		guideRaw, err := c.guideBytes(raw, region, multi)
		if err != nil {
			return nil, nil, err
		}
		upstreamKnown := make(map[string]struct{}, len(regionChannels))
		catalogByUpstream := make(map[string]string, len(regionChannels))
		for _, ch := range regionChannels {
			up := ch.UpstreamID
			if up == "" {
				up = ch.ID
			}
			upstreamKnown[up] = struct{}{}
			catalogByUpstream[up] = ch.ID
		}
		regionProgrammes, err := decodeGuide(guideRaw, upstreamKnown)
		if err != nil {
			return nil, nil, fmt.Errorf("%s guide %s: %w", c.source.ID, region, err)
		}
		for i := range regionProgrammes {
			if catalog, ok := catalogByUpstream[regionProgrammes[i].ChannelID]; ok {
				regionProgrammes[i].ChannelID = catalog
			}
		}
		channels = append(channels, regionChannels...)
		programmes = append(programmes, regionProgrammes...)
	}
	sort.SliceStable(channels, func(i, j int) bool { return channels[i].ID < channels[j].ID })
	sort.SliceStable(programmes, func(i, j int) bool {
		if programmes[i].ChannelID != programmes[j].ChannelID {
			return programmes[i].ChannelID < programmes[j].ChannelID
		}
		return programmes[i].Start.Before(programmes[j].Start)
	})
	return channels, programmes, nil
}

func (c *Client) guideBytes(raw provider.Raw, region string, multi bool) ([]byte, error) {
	if !multi {
		if g, ok := raw[RawGuide]; ok && len(g) > 0 {
			return g, nil
		}
	}
	if g, ok := raw[guideRawKey(region)]; ok && len(g) > 0 {
		return g, nil
	}
	if !multi {
		return nil, fmt.Errorf("%s: missing %s", c.source.ID, RawGuide)
	}
	return nil, fmt.Errorf("%s: missing %s", c.source.ID, guideRawKey(region))
}

func (c *Client) usableRegions(metadata metadata) []string {
	want := configuredRegions(c.settings.Region)
	if len(want) == 0 {
		want = []string{model.DefaultRegions}
	}
	var out []string
	for _, region := range want {
		key := mjhRegionKey(region)
		if c.regionAvailable(metadata, key) {
			out = append(out, model.NormalizeRegionCode(region))
		} else {
			slog.Info("mjh skipping unknown region", "provider", c.source.ID, "region", region)
		}
	}
	return out
}

func (c *Client) regionAvailable(metadata metadata, region string) bool {
	_, ok := metadata.Regions[region]
	return ok
}

func (c *Client) buildChannels(metadata metadata, region string, multi bool) ([]model.Channel, error) {
	rawChannels, headers, err := c.selectChannels(metadata, region)
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
		catalogID := id
		displayRegion := model.NormalizeRegionCode(region)
		if multi {
			catalogID = displayRegion + "_" + id
		}
		channel := model.Channel{
			ID:             catalogID,
			UpstreamID:     id,
			Name:           raw.Name,
			Description:    raw.Description,
			Group:          group,
			Region:         displayRegion,
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

// selectChannels returns the channel map and base request headers for one region
// (or the full set when regionless — region is ignored).
func (c *Client) selectChannels(metadata metadata, region string) (map[string]rawChannel, map[string]string, error) {
	headers := cloneHeaders(metadata.Headers)
	switch {
	case c.source.Regionless:
		return metadata.Channels, headers, nil
	case c.source.TaggedRegions:
		regionName := mjhRegionKey(region)
		if regionName == "" {
			return nil, nil, fmt.Errorf("%s: region required for tagged-region metadata", c.source.ID)
		}
		regionMeta, ok := metadata.Regions[regionName]
		if !ok {
			return nil, nil, fmt.Errorf("%s: region %q not found", c.source.ID, regionName)
		}
		mergeHeaders(headers, regionMeta.Headers)
		filtered := make(map[string]rawChannel)
		for id, raw := range metadata.Channels {
			if channelInRegion(raw, regionName) {
				filtered[id] = raw
			}
		}
		return filtered, headers, nil
	default:
		regionName := mjhRegionKey(region)
		regionMeta, ok := metadata.Regions[regionName]
		if !ok {
			return nil, nil, fmt.Errorf("%s: region %q not found", c.source.ID, regionName)
		}
		mergeHeaders(headers, regionMeta.Headers)
		return regionMeta.Channels, headers, nil
	}
}

func (c *Client) regionFetchHeaders(metadata metadata, region string) map[string]string {
	_, headers, err := c.selectChannels(metadata, region)
	if err != nil {
		headers = cloneHeaders(metadata.Headers)
	}
	mergeHeaders(headers, c.settings.Headers)
	return headers
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
	_, headers, err := c.selectChannels(metadata, "")
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
