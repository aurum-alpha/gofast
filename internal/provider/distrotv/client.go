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
	geo      string
	feedURL  string
	epgBase  string
}

// New constructs a DistroTV reader from effective settings.
func New(settings model.ProviderSettings, client *httpx.Client) *Client {
	if client == nil {
		client = httpx.NewClient(0, 0)
	}
	geo := NormalizeGeo(settings.Region)
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
		geo:      geo,
		feedURL:  FeedURL(feedURL, geo),
		epgBase:  strings.TrimRight(epgBase, "?&"),
	}
}

// Fetch downloads the live feed and (when channels parse) a short EPG window.
func (c *Client) Fetch(ctx context.Context) (provider.Raw, error) {
	feed, err := c.get(ctx, c.feedURL)
	if err != nil {
		return nil, fmt.Errorf("distrotv feed: %w", err)
	}
	shows, err := ParseFeedLive(feed)
	if err != nil {
		return nil, err
	}
	raw := provider.Raw{RawFeed: feed}
	if len(shows) == 0 {
		return raw, nil
	}
	ids := make([]string, 0, len(shows))
	for _, s := range shows {
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)
	epgURL := c.epgBase + "?id=" + url.QueryEscape(strings.Join(ids, ",")) + "&range=now,24h"
	epg, err := c.get(ctx, epgURL)
	if err != nil {
		// Soft-fail guide: catalog still useful without EPG.
		return raw, nil
	}
	raw[RawEPG] = epg
	return raw, nil
}

// Parse builds channels with DISTRO_RESOLVE opaque StreamURLs and optional programmes.
func (c *Client) Parse(raw provider.Raw) ([]model.Channel, []model.Programme, error) {
	feed := raw[RawFeed]
	if len(feed) == 0 {
		return nil, nil, fmt.Errorf("distrotv: missing %s", RawFeed)
	}
	shows, err := ParseFeedLive(feed)
	if err != nil {
		return nil, nil, err
	}
	headers := c.playHeaders()
	channels := make([]model.Channel, 0, len(shows))
	rawToCatalog := make(map[string]string, len(shows))
	for _, s := range shows {
		catalogID := JoinChannelID(c.geo, s.ID)
		ch := model.Channel{
			Provider:       model.ProviderDistroTV,
			ID:             catalogID,
			Name:           s.Name,
			Group:          s.Group,
			LogoURL:        s.Logo,
			LogoSourceURL:  s.Logo,
			StreamURL:      OpaqueStreamURL(catalogID),
			Classification: model.ClassDistroResolve,
			RequestHeaders: cloneHeaders(headers),
		}
		channels = append(channels, ch)
		rawToCatalog[s.ID] = catalogID
	}
	sort.Slice(channels, func(i, j int) bool {
		return channels[i].ID < channels[j].ID
	})

	var programmes []model.Programme
	if epgRaw := raw[RawEPG]; len(epgRaw) > 0 {
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
			sort.Slice(programmes, func(i, j int) bool {
				if programmes[i].ChannelID != programmes[j].ChannelID {
					return programmes[i].ChannelID < programmes[j].ChannelID
				}
				return programmes[i].Start.Before(programmes[j].Start)
			})
		}
	}
	return channels, programmes, nil
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
