package mjh

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type metadata struct {
	Slug     string                `json:"slug"`
	Headers  map[string]string     `json:"headers"`
	Regions  map[string]region     `json:"regions"`
	Channels map[string]rawChannel `json:"channels"`
}

type region struct {
	Headers  map[string]string     `json:"headers"`
	Channels map[string]rawChannel `json:"channels"`
}

type rawChannel struct {
	ChannelNumber json.RawMessage `json:"chno"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Group         string          `json:"group"`
	Groups        []string        `json:"groups"`
	Logo          string          `json:"logo"`
	LicenseURL    string          `json:"license_url"`
	// Regions lists which lineup regions include this channel (Plex-shaped
	// top-level channel maps). Empty means "no regional tag".
	Regions []string `json:"regions"`
}

func cloneHeaders(headers map[string]string) map[string]string {
	result := make(map[string]string, len(headers))
	mergeHeaders(result, headers)
	return result
}

func mergeHeaders(dst, src map[string]string) {
	for key, value := range src {
		dst[http.CanonicalHeaderKey(key)] = value
	}
}

func channelInRegion(raw rawChannel, region string) bool {
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "" {
		return false
	}
	for _, value := range raw.Regions {
		if strings.ToLower(strings.TrimSpace(value)) == region {
			return true
		}
	}
	return false
}

func parseChannelNumber(raw json.RawMessage) (int, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return 0, nil
	}
	if strings.HasPrefix(value, `"`) {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, err
		}
		value = strings.TrimSpace(text)
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid chno %q", value)
	}
	return number, nil
}
