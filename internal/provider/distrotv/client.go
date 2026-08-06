package distrotv

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

const maxRawSize = 64 << 20

var _ provider.Reader = (*Client)(nil)

// Client fetches DistroTV's jsrdn live feed + EPG and emits opaque catalog URLs.
type Client struct {
	settings model.ProviderSettings
	client   *httpx.Client
	geos     []string
	feedBase string
	epgBase  string
}

// New constructs a DistroTV reader from effective settings (Region is the
// system-wide regions CSV injected at bootstrap).
func New(settings model.ProviderSettings, client *httpx.Client) *Client {
	if client == nil {
		client = httpx.NewClient(0, 0)
	}
	feedURL := settings.ChannelsURL
	if feedURL == "" {
		feedURL = DefaultFeedURL
	}
	epgBase := settings.EPGURL
	if epgBase == "" {
		epgBase = DefaultEPGURL
	}
	return &Client{
		settings: settings,
		client:   client,
		geos:     configuredGeos(settings.Region),
		feedBase: feedURL,
		epgBase:  strings.TrimRight(epgBase, "?&"),
	}
}

func configuredGeos(regionCSV string) []string {
	list := model.ParseRegionList(regionCSV)
	if len(list) == 0 {
		return []string{DefaultGeo}
	}
	out := make([]string, 0, len(list))
	seen := make(map[string]struct{}, len(list))
	for _, r := range list {
		g := strings.ToUpper(strings.TrimSpace(r))
		if g == "" {
			continue
		}
		if _, ok := seen[g]; ok {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	if len(out) == 0 {
		return []string{DefaultGeo}
	}
	return out
}

func feedRawKey(geo string) string { return "feed." + geo }
func epgRawKey(geo string) string  { return "epg." + geo }

// Fetch downloads the live feed and (when channels parse) a short EPG window
// for each configured geo.
func (c *Client) Fetch(ctx context.Context) (provider.Raw, error) {
	raw := provider.Raw{}
	anyFeed := false
	for _, geo := range c.geos {
		feedURL := FeedURL(c.feedBase, geo)
		if len(c.geos) == 1 && strings.TrimSpace(c.settings.ChannelsURL) != "" {
			feedURL = c.settings.ChannelsURL
		}
		feed, err := c.get(ctx, feedURL)
		if err != nil {
			return nil, fmt.Errorf("distrotv feed geo=%s: %w", geo, err)
		}
		shows, err := ParseFeedLive(feed)
		if err != nil {
			return nil, fmt.Errorf("distrotv feed geo=%s: %w", geo, err)
		}
		if len(c.geos) == 1 {
			raw[RawFeed] = feed
		} else {
			raw[feedRawKey(geo)] = feed
		}
		anyFeed = true
		if len(shows) == 0 {
			continue
		}
		ids := make([]string, 0, len(shows))
		for _, s := range shows {
			ids = append(ids, s.ID)
		}
		sort.Strings(ids)
		epgURL := c.epgBase + "?id=" + url.QueryEscape(strings.Join(ids, ",")) + "&range=now,24h"
		epg, err := c.get(ctx, epgURL)
		if err != nil {
			continue // soft-fail guide
		}
		if len(c.geos) == 1 {
			raw[RawEPG] = epg
		} else {
			raw[epgRawKey(geo)] = epg
		}
	}
	if !anyFeed {
		return nil, fmt.Errorf("distrotv: no feeds for geos %v", c.geos)
	}
	return raw, nil
}

// Parse builds channels with DISTRO_RESOLVE opaque StreamURLs and optional programmes.
func (c *Client) Parse(raw provider.Raw) ([]model.Channel, []model.Programme, error) {
	var channels []model.Channel
	var programmes []model.Programme
	parsedAny := false
	for _, geo := range c.geos {
		feed, ok := c.feedBytes(raw, geo)
		if !ok {
			continue
		}
		parsedAny = true
		shows, err := ParseFeedLive(feed)
		if err != nil {
			return nil, nil, fmt.Errorf("distrotv geo=%s: %w", geo, err)
		}
		headers := c.playHeaders()
		rawToCatalog := make(map[string]string, len(shows))
		for _, s := range shows {
			catalogID := JoinChannelID(geo, s.ID)
			ch := model.Channel{
				Provider:       model.ProviderDistroTV,
				ID:             catalogID,
				UpstreamID:     s.ID,
				Name:           s.Name,
				Group:          s.Group,
				Region:         geo,
				LogoURL:        s.Logo,
				LogoSourceURL:  s.Logo,
				StreamURL:      OpaqueStreamURL(catalogID),
				Classification: model.ClassDistroResolve,
				RequestHeaders: cloneHeaders(headers),
			}
			channels = append(channels, ch)
			rawToCatalog[s.ID] = catalogID
		}
		if epgRaw, ok := c.epgBytes(raw, geo); ok {
			slots, err := ParseEPGSlots(epgRaw)
			if err == nil {
				for rawID, list := range slots {
					catalogID, ok := rawToCatalog[rawID]
					if !ok {
						continue
					}
					for _, s := range list {
						programmes = append(programmes, model.Programme{
							ChannelID: catalogID,
							Title:     s.Title,
							Desc:      s.Description,
							Start:     s.Start,
							Stop:      s.End,
						})
					}
				}
			}
		}
	}
	if !parsedAny {
		return nil, nil, fmt.Errorf("distrotv: missing feed data for geos %v", c.geos)
	}
	sort.Slice(channels, func(i, j int) bool {
		return channels[i].ID < channels[j].ID
	})
	sort.Slice(programmes, func(i, j int) bool {
		if programmes[i].ChannelID != programmes[j].ChannelID {
			return programmes[i].ChannelID < programmes[j].ChannelID
		}
		return programmes[i].Start.Before(programmes[j].Start)
	})
	return channels, programmes, nil
}

func (c *Client) feedBytes(raw provider.Raw, geo string) ([]byte, bool) {
	if b := raw[feedRawKey(geo)]; len(b) > 0 {
		return b, true
	}
	if len(c.geos) == 1 {
		if b := raw[RawFeed]; len(b) > 0 {
			return b, true
		}
	}
	return nil, false
}

func (c *Client) epgBytes(raw provider.Raw, geo string) ([]byte, bool) {
	if b := raw[epgRawKey(geo)]; len(b) > 0 {
		return b, true
	}
	if len(c.geos) == 1 {
		if b := raw[RawEPG]; len(b) > 0 {
			return b, true
		}
	}
	return nil, false
}

func (c *Client) playHeaders() map[string]string {
	ua := strings.TrimSpace(c.settings.UserAgent)
	if ua == "" {
		ua = BrowserUA
	}
	out := map[string]string{
		"User-Agent": ua,
		"Origin":     "https://distro.tv",
		"Referer":    "https://distro.tv/",
	}
	for k, v := range c.settings.Headers {
		if k == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	ua := strings.TrimSpace(c.settings.UserAgent)
	if ua == "" {
		ua = AndroidUA
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json,*/*")
	for k, v := range c.settings.Headers {
		if k == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
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

func cloneHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
