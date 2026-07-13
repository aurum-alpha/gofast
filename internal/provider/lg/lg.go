// Package lg implements the LG Channels US schedulelist provider.
package lg

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

const (
	defaultURL = "https://api.lgchannels.com/api/v1.0/schedulelist"
	chromeUA   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

// Client fetches LG Channels US lineups.
type Client struct {
	id     string
	cfg    model.Provider
	client *httpx.Client
	url    string
}

// New constructs an LG client. id is the config key (typically "lg").
func New(id string, cfg model.Provider, client *httpx.Client) *Client {
	if client == nil {
		client = httpx.NewClient(0, 0)
	}
	u := cfg.ChannelsURL
	if u == "" {
		u = defaultURL
	}
	return &Client{id: id, cfg: cfg, client: client, url: u}
}

// Factory returns a provider.Factory for registry wiring.
func Factory(client *httpx.Client) provider.Factory {
	return func(id string, cfg model.Provider) (provider.Reader, error) {
		return New(id, cfg, client), nil
	}
}

func (c *Client) ID() string { return c.id }

func (c *Client) Fetch(ctx context.Context) ([]model.Channel, []model.Programme, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", chromeUA)
	if c.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	req.Header.Set("x-device-country", "US")
	req.Header.Set("x-device-language", "en")
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, nil, fmt.Errorf("lg: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return ParseSchedule(resp.Body)
}

// ParseSchedule decodes an LG schedulelist JSON document.
func ParseSchedule(r io.Reader) ([]model.Channel, []model.Programme, error) {
	var root scheduleRoot
	dec := json.NewDecoder(r)
	if err := dec.Decode(&root); err != nil {
		return nil, nil, fmt.Errorf("lg: decode: %w", err)
	}

	type acc struct {
		ch    model.Channel
		progs []model.Programme
	}
	byID := make(map[string]*acc)
	order := make([]string, 0)
	skipped := 0

	for _, cat := range root.Categories {
		for _, raw := range cat.Channels {
			id := strings.TrimSpace(raw.ChannelID)
			stream := stripQuery(strings.TrimSpace(raw.MediaStaticURL))
			if id == "" || stream == "" {
				provider.SkipMalformed(&skipped)
				continue
			}
			a, ok := byID[id]
			if !ok {
				num := 0
				if n, err := strconv.Atoi(strings.TrimSpace(raw.ChannelNumber)); err == nil {
					num = n
				}
				a = &acc{ch: model.Channel{
					ID:        id,
					Name:      strings.TrimSpace(raw.ChannelName),
					Group:     strings.TrimSpace(raw.ChannelGenreName),
					Number:    num,
					StreamURL: stream,
					LogoURL:   strings.TrimSpace(raw.ChannelLogoURL),
				}}
				byID[id] = a
				order = append(order, id)
			}
			for _, pr := range raw.Programs {
				title := strings.TrimSpace(pr.ProgramTitle)
				start, err1 := parseLGTime(pr.StartDateTime)
				stop, err2 := parseLGTime(pr.EndDateTime)
				if title == "" || err1 != nil || err2 != nil || !stop.After(start) {
					provider.SkipMalformed(&skipped)
					continue
				}
				a.progs = append(a.progs, model.Programme{
					ChannelID: id, // normalized later by registry
					Title:     title,
					Desc:      strings.TrimSpace(pr.Description),
					Start:     start,
					Stop:      stop,
				})
			}
		}
	}

	channels := make([]model.Channel, 0, len(order))
	programmes := make([]model.Programme, 0)
	for _, id := range order {
		a := byID[id]
		channels = append(channels, a.ch)
		programmes = append(programmes, a.progs...)
	}
	_ = skipped
	return channels, programmes, nil
}

func stripQuery(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		if i := strings.IndexByte(raw, '?'); i >= 0 {
			return raw[:i]
		}
		return raw
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func parseLGTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.ParseInLocation("2006-01-02T15:04:05Z", s, time.UTC)
}

type scheduleRoot struct {
	Categories []scheduleCategory `json:"categories"`
}

type scheduleCategory struct {
	Channels []scheduleChannel `json:"channels"`
}

type scheduleChannel struct {
	ChannelID        string            `json:"channelId"`
	ChannelName      string            `json:"channelName"`
	ChannelNumber    string            `json:"channelNumber"`
	ChannelLogoURL   string            `json:"channelLogoUrl"`
	MediaStaticURL   string            `json:"mediaStaticUrl"`
	ProviderID       string            `json:"providerId"`
	ChannelGenreName string            `json:"channelGenreName"`
	Programs         []scheduleProgram `json:"programs"`
}

type scheduleProgram struct {
	ProgramTitle  string `json:"programTitle"`
	Description   string `json:"description"`
	StartDateTime string `json:"startDateTime"`
	EndDateTime   string `json:"endDateTime"`
}
