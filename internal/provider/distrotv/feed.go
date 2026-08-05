package distrotv

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultGeo     = "QQ"
	DefaultFeedURL = "https://tv.jsrdn.com/tv_v5/getfeed.php?type=live"
	DefaultEPGURL  = "https://tv.jsrdn.com/epg/query.php"
	AndroidUA      = "Dalvik/2.1.0 (Linux; U; Android 9; AFTT Build/STT9.221129.002) GTV/AFTT DistroTV/2.0.9"
	BrowserUA      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

	RawFeed = "feed.json"
	RawEPG  = "epg.json"
)

// NormalizeGeo uppercases a Distro geo code (US, QQ, CA, …).
func NormalizeGeo(geo string) string {
	g := strings.ToUpper(strings.TrimSpace(geo))
	if g == "" {
		return DefaultGeo
	}
	return g
}

// FeedURL builds the live catalog URL for geo. US/default omits &geo= (server
// geolocates); other codes append &geo=.
func FeedURL(base, geo string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = DefaultFeedURL
	}
	geo = NormalizeGeo(geo)
	if geo == "US" {
		return base
	}
	u, err := url.Parse(base)
	if err != nil {
		return base + "&geo=" + url.QueryEscape(geo)
	}
	q := u.Query()
	q.Set("geo", geo)
	u.RawQuery = q.Encode()
	return u.String()
}

// liveShow is the subset of Distro feed JSON we need for a live channel.
type liveShow struct {
	ID    string
	Name  string
	Logo  string
	Group string
	URL   string
}

type feedFile struct {
	Shows json.RawMessage `json:"shows"`
}

type showObj struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	ImgLogo string `json:"img_logo"`
	Genre   string `json:"genre"`
	Seasons []struct {
		Episodes []struct {
			ID      any `json:"id"`
			Content struct {
				URL string `json:"url"`
			} `json:"content"`
		} `json:"episodes"`
	} `json:"seasons"`
}

// ParseFeedLive extracts live channels from a Distro getfeed.php JSON body.
func ParseFeedLive(raw []byte) ([]liveShow, error) {
	var root feedFile
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("distrotv: feed json: %w", err)
	}
	shows, err := decodeShows(root.Shows)
	if err != nil {
		return nil, err
	}
	out := make([]liveShow, 0, len(shows))
	for _, s := range shows {
		if !strings.EqualFold(strings.TrimSpace(s.Type), "live") {
			continue
		}
		if len(s.Seasons) == 0 || len(s.Seasons[0].Episodes) == 0 {
			continue
		}
		ep := s.Seasons[0].Episodes[0]
		id := anyString(ep.ID)
		upstream := strings.TrimSpace(ep.Content.URL)
		name := strings.TrimSpace(s.Title)
		if id == "" || upstream == "" || name == "" {
			continue
		}
		group := primaryGenre(s.Genre)
		out = append(out, liveShow{
			ID:    id,
			Name:  name,
			Logo:  strings.TrimSpace(s.ImgLogo),
			Group: group,
			URL:   upstream,
		})
	}
	return out, nil
}

func decodeShows(raw json.RawMessage) ([]showObj, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	switch raw[0] {
	case '{':
		var m map[string]showObj
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("distrotv: shows object: %w", err)
		}
		out := make([]showObj, 0, len(m))
		for _, s := range m {
			out = append(out, s)
		}
		return out, nil
	case '[':
		var out []showObj
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("distrotv: shows array: %w", err)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("distrotv: unexpected shows json")
	}
}

func anyString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		return fmt.Sprintf("%.0f", t)
	case json.Number:
		return t.String()
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func primaryGenre(raw string) string {
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			return p
		}
	}
	return "DistroTV"
}

// URLMapFromFeed maps raw episode id → upstream stream URL.
func URLMapFromFeed(raw []byte) (map[string]string, error) {
	shows, err := ParseFeedLive(raw)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(shows))
	for _, s := range shows {
		out[s.ID] = s.URL
	}
	return out, nil
}

type epgFile struct {
	EPG map[string]struct {
		Slots []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Start       string `json:"start"`
			End         string `json:"end"`
		} `json:"slots"`
	} `json:"epg"`
}

const epgTimeLayout = "2006-01-02 15:04:05"

// ParseEPGSlots maps raw Distro episode id → programme rows (channel id left blank).
func ParseEPGSlots(raw []byte) (map[string][]epgSlot, error) {
	var root epgFile
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("distrotv: epg json: %w", err)
	}
	out := make(map[string][]epgSlot, len(root.EPG))
	for id, ch := range root.EPG {
		slots := make([]epgSlot, 0, len(ch.Slots))
		for _, s := range ch.Slots {
			start, err1 := time.ParseInLocation(epgTimeLayout, s.Start, time.UTC)
			end, err2 := time.ParseInLocation(epgTimeLayout, s.End, time.UTC)
			if err1 != nil || err2 != nil {
				continue
			}
			title := strings.TrimSpace(s.Title)
			if title == "" {
				title = "Unknown"
			}
			slots = append(slots, epgSlot{
				Title:       title,
				Description: strings.TrimSpace(s.Description),
				Start:       start,
				End:         end,
			})
		}
		if len(slots) > 0 {
			out[id] = slots
		}
	}
	return out, nil
}

type epgSlot struct {
	Title       string
	Description string
	Start       time.Time
	End         time.Time
}
