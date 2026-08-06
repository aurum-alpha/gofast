package stirr

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"
)

// catalogRow is one live channel from videos.category_videos.
type catalogRow struct {
	VideoID       json.Number `json:"videoid"`
	Title         string      `json:"title"`
	Description   string      `json:"description"`
	ChannelNumber json.Number `json:"channel_number"`
	EPGURL        string      `json:"epg_url"`
	EPGChannelID  string      `json:"epg_channel_id"`
	DRMProtected  string      `json:"drm_protected"`
	Categories    []struct {
		Name string `json:"category_name"`
	} `json:"categories"`
	Thumbs       json.RawMessage `json:"thumbs"`
	SquareThumbs json.RawMessage `json:"square_thumbs"`
}

type listPayload struct {
	Videos struct {
		CategoryVideos []json.RawMessage `json:"category_videos"`
	} `json:"videos"`
}

// ParseCatalogRows extracts live channel rows from the list JSON.
func ParseCatalogRows(raw []byte) ([]catalogRow, error) {
	var payload listPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("stirr list: %w", err)
	}
	var rows []catalogRow
	for _, chunk := range payload.Videos.CategoryVideos {
		var nested []catalogRow
		if err := json.Unmarshal(chunk, &nested); err == nil && len(nested) > 0 {
			rows = append(rows, nested...)
			continue
		}
		var one catalogRow
		if err := json.Unmarshal(chunk, &one); err == nil && one.ID() != "" {
			rows = append(rows, one)
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("stirr list: no channels")
	}
	return rows, nil
}

func (r catalogRow) ID() string {
	return strings.TrimSpace(r.VideoID.String())
}

func (r catalogRow) Number() int {
	n, err := r.ChannelNumber.Int64()
	if err != nil || n <= 0 {
		return 0
	}
	return int(n)
}

func (r catalogRow) Group() string {
	for _, c := range r.Categories {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		if mapped, ok := categoryMap[name]; ok {
			return mapped
		}
		return name
	}
	return "General"
}

func (r catalogRow) Logo() string {
	for _, raw := range []json.RawMessage{r.SquareThumbs, r.Thumbs} {
		if u := thumbURL(raw); u != "" {
			return u
		}
	}
	return ""
}

func thumbURL(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err == nil {
		for _, key := range []string{"300x300", "512x512", "416x260", "original"} {
			if u := strings.TrimSpace(m[key]); strings.HasPrefix(u, "http") {
				return u
			}
		}
		for _, u := range m {
			if strings.HasPrefix(strings.TrimSpace(u), "http") {
				return strings.TrimSpace(u)
			}
		}
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		for _, u := range arr {
			if strings.HasPrefix(strings.TrimSpace(u), "http") {
				return strings.TrimSpace(u)
			}
		}
	}
	return ""
}

func (r catalogRow) IsDRM() bool {
	s := strings.ToLower(strings.TrimSpace(r.DRMProtected))
	return s == "yes" || s == "1" || s == "true"
}

var categoryMap = map[string]string{
	"News Flash Live":             "News",
	"Sports Live":                 "Sports",
	"Entertainment Live":          "Entertainment",
	"Music Live":                  "Music",
	"Food and Fitness Live":       "Lifestyle",
	"Comedy Live":                 "Comedy",
	"Shopping Live":               "Shopping",
	"Default Category":            "General",
	"Crime Files":                 "Crime",
	"Documentary Series":          "Documentary",
	"STIRR Kids":                  "Kids",
	"Finance and Business":        "Business",
	"Paranormal Series":           "Entertainment",
	"Science to Space, Amplified": "Science",
	"Pack your Bag Travel":        "Travel",
}

// bulkEPGChannel is one entry under data.channels from /api/epg.
type bulkEPGChannel struct {
	ChannelID json.Number `json:"channel_id"`
	Name      string      `json:"name"`
	Programs  []bulkProg  `json:"programs"`
}

type bulkProg struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	StartEPG    string `json:"start_epg_time"`
	EndEPG      string `json:"end_epg_time"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time"`
}

type bulkEPGPayload struct {
	Data struct {
		Channels []bulkEPGChannel `json:"channels"`
	} `json:"data"`
}

// ParseBulkEPG maps videoid → programmes (ChannelID left as videoid string).
func ParseBulkEPG(raw []byte) (map[string][]progSlot, error) {
	var payload bulkEPGPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("stirr epg: %w", err)
	}
	out := make(map[string][]progSlot, len(payload.Data.Channels))
	for _, ch := range payload.Data.Channels {
		id := strings.TrimSpace(ch.ChannelID.String())
		if id == "" {
			continue
		}
		slots := make([]progSlot, 0, len(ch.Programs))
		for _, p := range ch.Programs {
			start, stop, ok := progTimes(p)
			if !ok || strings.TrimSpace(p.Title) == "" {
				continue
			}
			slots = append(slots, progSlot{
				Title: strings.TrimSpace(p.Title),
				Desc:  strings.TrimSpace(p.Description),
				Start: start,
				Stop:  stop,
			})
		}
		if len(slots) > 0 {
			out[id] = slots
		}
	}
	return out, nil
}

type progSlot struct {
	Title string
	Desc  string
	Start time.Time
	Stop  time.Time
}

func progTimes(p bulkProg) (start, stop time.Time, ok bool) {
	if t, err := time.Parse("2006-01-02 15:04:05 -07:00", strings.TrimSpace(p.StartEPG)); err == nil {
		start = t.UTC()
	} else if p.StartTime > 0 {
		start = time.Unix(p.StartTime, 0).UTC()
	}
	if t, err := time.Parse("2006-01-02 15:04:05 -07:00", strings.TrimSpace(p.EndEPG)); err == nil {
		stop = t.UTC()
	} else if p.EndTime > 0 {
		stop = time.Unix(p.EndTime, 0).UTC()
	}
	if start.IsZero() || stop.IsZero() || !stop.After(start) {
		return time.Time{}, time.Time{}, false
	}
	return start, stop, true
}

// SanitizeURL adds https when missing and rejects non-http(s) values.
func SanitizeURL(raw string) string {
	u := html.UnescapeString(strings.TrimSpace(raw))
	u = strings.TrimRight(u, "\u00a0")
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	if parsed.Scheme == "" {
		u = "https://" + u
		parsed, err = url.Parse(u)
		if err != nil {
			return ""
		}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || !strings.Contains(host, ".") {
		return ""
	}
	return u
}

// ParseWurlSchedules extracts programmes from a Wurl JSON EPG payload.
func ParseWurlSchedules(raw []byte) ([]progSlot, error) {
	var payload struct {
		Schedules []struct {
			ID            string `json:"id"`
			StartDateTime string `json:"startDateTime"`
			DurSecs       int    `json:"durSecs"`
		} `json:"schedules"`
		Movies          []wurlItem `json:"movies"`
		ShortFormVideos []wurlItem `json:"shortFormVideos"`
		TvSpecials      []wurlItem `json:"tvSpecials"`
		Series          []struct {
			Seasons []struct {
				Episodes []wurlItem `json:"episodes"`
			} `json:"seasons"`
		} `json:"series"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	items := map[string]wurlItem{}
	for _, m := range payload.Movies {
		if id := m.id(); id != "" {
			items[id] = m
		}
	}
	for _, m := range payload.ShortFormVideos {
		if id := m.id(); id != "" {
			items[id] = m
		}
	}
	for _, m := range payload.TvSpecials {
		if id := m.id(); id != "" {
			items[id] = m
		}
	}
	for _, show := range payload.Series {
		for _, season := range show.Seasons {
			for _, ep := range season.Episodes {
				if id := ep.id(); id != "" {
					items[id] = ep
				}
			}
		}
	}
	var out []progSlot
	for _, sched := range payload.Schedules {
		sid := strings.TrimSpace(sched.ID)
		if sid == "" || sched.DurSecs <= 0 {
			continue
		}
		start, err := time.Parse(time.RFC3339, sched.StartDateTime)
		if err != nil {
			start, err = time.Parse("2006-01-02T15:04:05Z", sched.StartDateTime)
		}
		if err != nil {
			continue
		}
		item := items[sid]
		title := item.title()
		if title == "" {
			title = sid
		}
		out = append(out, progSlot{
			Title: title,
			Desc:  item.desc(),
			Start: start.UTC(),
			Stop:  start.UTC().Add(time.Duration(sched.DurSecs) * time.Second),
		})
	}
	return out, nil
}

type wurlItem struct {
	ID          json.RawMessage `json:"id"`
	Title       json.RawMessage `json:"title"`
	Description json.RawMessage `json:"description"`
	Name        string          `json:"name"`
}

func (w wurlItem) id() string {
	return jsonStringish(w.ID)
}

func (w wurlItem) title() string {
	if t := jsonTitle(w.Title); t != "" {
		return t
	}
	return strings.TrimSpace(w.Name)
}

func (w wurlItem) desc() string {
	return jsonTitle(w.Description)
}

func jsonStringish(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return strings.TrimSpace(n.String())
	}
	return strings.TrimSpace(string(raw))
}

func jsonTitle(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var obj struct {
		Value string `json:"value"`
		Text  string `json:"text"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		if strings.TrimSpace(obj.Value) != "" {
			return strings.TrimSpace(obj.Value)
		}
		return strings.TrimSpace(obj.Text)
	}
	return ""
}

// providerEPGKey builds a stable raw key for a soft-fetched provider EPG URL.
func providerEPGKey(videoID string) string {
	return "provider_epg." + videoID
}
