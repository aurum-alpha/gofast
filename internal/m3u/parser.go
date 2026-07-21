package m3u

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/j27-aurum/gofast/internal/model"
)

const maxLineBytes = 4 << 20

// Parse reads an extended M3U playlist into domain channels. The first channel
// for each normalized tvg-id wins so upstream aliases cannot produce duplicate
// M3U/XMLTV identities.
func Parse(r io.Reader) ([]model.Channel, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)

	channels := make([]model.Channel, 0)
	seen := make(map[string]struct{})
	var pending *model.Channel
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\uFEFF"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			channel, ok := parseEXTINF(line)
			if !ok {
				pending = nil
				continue
			}
			pending = &channel
			continue
		}
		if strings.HasPrefix(line, "#") || pending == nil {
			continue
		}

		pending.StreamURL = line
		normalizedID := model.NormalizeID(pending.ID)
		if normalizedID != "" {
			if _, duplicate := seen[normalizedID]; !duplicate {
				seen[normalizedID] = struct{}{}
				channels = append(channels, *pending)
			}
		}
		pending = nil
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("m3u line %d: %w", lineNumber, err)
	}
	return channels, nil
}

func parseAttributes(value string) map[string]string {
	attributes := make(map[string]string)
	for index := 0; index < len(value); {
		for index < len(value) && unicode.IsSpace(rune(value[index])) {
			index++
		}
		keyStart := index
		for index < len(value) && !unicode.IsSpace(rune(value[index])) && value[index] != '=' {
			index++
		}
		if keyStart == index || index >= len(value) || value[index] != '=' {
			for index < len(value) && !unicode.IsSpace(rune(value[index])) {
				index++
			}
			continue
		}
		key := strings.ToLower(value[keyStart:index])
		index++

		var attribute string
		if index < len(value) && (value[index] == '"' || value[index] == '\'') {
			quote := value[index]
			index++
			start := index
			for index < len(value) && value[index] != quote {
				index++
			}
			attribute = value[start:index]
			if index < len(value) {
				index++
			}
		} else {
			start := index
			for index < len(value) && !unicode.IsSpace(rune(value[index])) {
				index++
			}
			attribute = value[start:index]
		}
		attributes[key] = attribute
	}
	return attributes
}

func parseEXTINF(line string) (model.Channel, bool) {
	payload := strings.TrimPrefix(line, "#EXTINF:")
	comma := commaOutsideQuotes(payload)
	if comma < 0 {
		return model.Channel{}, false
	}
	header := strings.TrimSpace(payload[:comma])
	displayName := strings.TrimSpace(payload[comma+1:])

	space := strings.IndexFunc(header, unicode.IsSpace)
	if space < 0 {
		return model.Channel{}, false
	}
	attributes := parseAttributes(header[space+1:])
	id := strings.TrimSpace(attributes["tvg-id"])
	if id == "" {
		return model.Channel{}, false
	}
	name := strings.TrimSpace(attributes["tvg-name"])
	if name == "" {
		name = displayName
	}
	if name == "" {
		return model.Channel{}, false
	}
	number, _ := strconv.Atoi(strings.TrimSpace(attributes["tvg-chno"]))
	return model.Channel{
		ID:      id,
		Name:    name,
		Group:   strings.TrimSpace(attributes["group-title"]),
		Number:  number,
		LogoURL: strings.TrimSpace(attributes["tvg-logo"]),
	}, true
}

func commaOutsideQuotes(value string) int {
	var quote byte
	for index := 0; index < len(value); index++ {
		switch {
		case quote != 0 && value[index] == quote:
			quote = 0
		case quote == 0 && (value[index] == '"' || value[index] == '\''):
			quote = value[index]
		case quote == 0 && value[index] == ',':
			return index
		}
	}
	return -1
}
