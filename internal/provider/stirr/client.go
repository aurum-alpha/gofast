package stirr

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

const (
	maxRawSize         = 64 << 20
	providerEPGWorkers = 8
)

var _ provider.Reader = (*Client)(nil)

// Client fetches STIRR list + bulk EPG and emits opaque STIRR_RESOLVE StreamURLs.
type Client struct {
	settings    model.ProviderSettings
	client      *httpx.Client
	channelsURL string
	epgURL      string
}

// New constructs a STIRR reader from effective settings.
func New(settings model.ProviderSettings, client *httpx.Client) *Client {
	if client == nil {
		client = httpx.NewClient(0, 0)
	}
	channelsURL := strings.TrimSpace(settings.ChannelsURL)
	if channelsURL == "" {
		channelsURL = DefaultChannelsURL
	}
	epgURL := strings.TrimSpace(settings.EPGURL)
	if epgURL == "" {
		epgURL = DefaultEPGURL
	}
	return &Client{
		settings:    settings,
		client:      client,
		channelsURL: channelsURL,
		epgURL:      epgURL,
	}
}

// Fetch downloads the channel list, bulk EPG, soft-fetches provider EPG URLs
// for channels missing from the bulk guide, and soft-audits Aniview CON dead
// SSAI configs into dead.json.
func (c *Client) Fetch(ctx context.Context) (provider.Raw, error) {
	list, err := c.get(ctx, c.channelsURL)
	if err != nil {
		return nil, fmt.Errorf("stirr list: %w", err)
	}
	rows, err := ParseCatalogRows(list)
	if err != nil {
		return nil, err
	}
	raw := provider.Raw{RawList: list}

	epg, err := c.get(ctx, c.epgURL)
	if err == nil {
		raw[RawEPG] = epg
	}

	bulkIDs := map[string]struct{}{}
	if len(epg) > 0 {
		if slots, err := ParseBulkEPG(epg); err == nil {
			for id := range slots {
				bulkIDs[id] = struct{}{}
			}
		}
	}

	type job struct {
		id  string
		url string
	}
	var jobs []job
	seenURL := map[string]struct{}{}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		id := row.ID()
		if id == "" {
			continue
		}
		ids = append(ids, id)
		if _, ok := bulkIDs[id]; ok {
			continue
		}
		u := SanitizeURL(row.EPGURL)
		if u == "" {
			continue
		}
		if _, ok := seenURL[u]; ok {
			continue
		}
		seenURL[u] = struct{}{}
		jobs = append(jobs, job{id: id, url: u})
	}

	var mu sync.Mutex
	sem := make(chan struct{}, providerEPGWorkers)
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			body, err := c.get(ctx, j.url)
			if err != nil || len(body) == 0 {
				return
			}
			mu.Lock()
			raw[providerEPGKey(j.id)] = body
			mu.Unlock()
		}(j)
	}
	wg.Wait()

	ua := strings.TrimSpace(c.settings.UserAgent)
	resolver := NewResolver(c.client, "", ua)
	raw[RawDead] = encodeDeadIDs(auditDeadIDs(ctx, resolver, ids))
	return raw, nil
}

// Parse builds channels with STIRR_RESOLVE opaque StreamURLs and programmes.
// Videoids present in dead.json get a hard dead-SSAI filter reason (excluded
// from M3U/XMLTV; still visible in the Channels UI).
func (c *Client) Parse(raw provider.Raw) ([]model.Channel, []model.Programme, error) {
	list := raw[RawList]
	if len(list) == 0 {
		return nil, nil, fmt.Errorf("stirr: missing %s", RawList)
	}
	rows, err := ParseCatalogRows(list)
	if err != nil {
		return nil, nil, err
	}

	bulk := map[string][]progSlot{}
	if epg := raw[RawEPG]; len(epg) > 0 {
		if slots, err := ParseBulkEPG(epg); err == nil {
			bulk = slots
		}
	}
	dead := decodeDeadIDs(raw[RawDead])

	headers := c.playHeaders()
	channels := make([]model.Channel, 0, len(rows))
	var programmes []model.Programme
	seen := map[string]struct{}{}

	for _, row := range rows {
		id := row.ID()
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		class := model.ClassStirrResolve
		if row.IsDRM() {
			class = model.ClassDRM
		}
		logo := row.Logo()
		ch := model.Channel{
			Provider:       model.ProviderSTIRR,
			ID:             id,
			UpstreamID:     id,
			Name:           strings.TrimSpace(row.Title),
			Group:          row.Group(),
			Number:         row.Number(),
			LogoURL:        logo,
			LogoSourceURL:  logo,
			StreamURL:      OpaqueStreamURL(id),
			Classification: class,
			RequestHeaders: cloneHeaders(headers),
		}
		if ch.Name == "" {
			ch.Name = "STIRR " + id
		}
		if _, ok := dead[id]; ok {
			ch.AddFilterReason(model.FilterReasonDeadSSAI)
		}
		channels = append(channels, ch)

		slots := bulk[id]
		if len(slots) == 0 {
			if body := raw[providerEPGKey(id)]; len(body) > 0 {
				if wurl, err := ParseWurlSchedules(body); err == nil {
					slots = wurl
				}
			}
		}
		for _, s := range slots {
			programmes = append(programmes, model.Programme{
				ChannelID: id,
				Title:     s.Title,
				Desc:      s.Desc,
				Start:     s.Start,
				Stop:      s.Stop,
			})
		}
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

func (c *Client) playHeaders() map[string]string {
	ua := strings.TrimSpace(c.settings.UserAgent)
	if ua == "" {
		ua = BrowserUA
	}
	out := map[string]string{
		"User-Agent": ua,
		"Origin":     "https://stirr.com",
		"Referer":    "https://stirr.com/",
		"Accept":     "application/json, text/plain, */*",
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
	for k, v := range c.playHeaders() {
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
