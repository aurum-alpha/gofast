package mjh

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

func decodeGuide(data []byte, known map[string]struct{}) ([]model.Programme, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	decoder := xml.NewDecoder(gz)
	programmes := make([]model.Programme, 0)
	skipped := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "programme" {
			continue
		}
		var raw xmlProgramme
		if err := decoder.DecodeElement(&raw, &start); err != nil {
			return nil, err
		}
		if _, ok := known[raw.Channel]; !ok {
			provider.SkipMalformed(&skipped)
			continue
		}
		startTime, startErr := parseXMLTVTime(raw.Start)
		stopTime, stopErr := parseXMLTVTime(raw.Stop)
		title := firstText(raw.Titles)
		if startErr != nil || stopErr != nil || title == "" || !stopTime.After(startTime) {
			provider.SkipMalformed(&skipped)
			continue
		}
		programmes = append(programmes, model.Programme{
			ChannelID: raw.Channel,
			Title:     title,
			Desc:      firstText(raw.Descriptions),
			Start:     startTime,
			Stop:      stopTime,
		})
	}
	return programmes, nil
}

func firstText(values []xmlText) string {
	for _, value := range values {
		if text := strings.TrimSpace(value.Value); text != "" {
			return text
		}
	}
	return ""
}

func parseXMLTVTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	layouts := []string{
		"20060102150405 -0700",
		"200601021504 -0700",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid XMLTV time %q", value)
}

type xmlProgramme struct {
	Channel      string    `xml:"channel,attr"`
	Start        string    `xml:"start,attr"`
	Stop         string    `xml:"stop,attr"`
	Titles       []xmlText `xml:"title"`
	Descriptions []xmlText `xml:"desc"`
}

type xmlText struct {
	Value string `xml:",chardata"`
}
