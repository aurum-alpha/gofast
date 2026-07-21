package xmltv

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

// Document is the provider-facing subset of an upstream XMLTV document.
type Document struct {
	ChannelIDs []string
	Programmes []model.Programme
}

// Parse reads upstream XMLTV, repairing bare ampersands before strict XML
// decoding. Unknown/malformed programme rows are skipped; malformed XML fails
// the document so last-known-good publication remains intact.
func Parse(r io.Reader) (Document, error) {
	decoder := xml.NewDecoder(newAmpersandReader(r))
	document := Document{}
	seenChannels := make(map[string]struct{})
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return document, nil
		}
		if err != nil {
			return Document{}, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "channel":
			id := attribute(start.Attr, "id")
			if id != "" {
				if _, duplicate := seenChannels[id]; !duplicate {
					seenChannels[id] = struct{}{}
					document.ChannelIDs = append(document.ChannelIDs, id)
				}
			}
		case "programme":
			var raw parsedProgramme
			if err := decoder.DecodeElement(&raw, &start); err != nil {
				return Document{}, err
			}
			startTime, startErr := parseTime(raw.Start)
			stopTime, stopErr := parseTime(raw.Stop)
			title := firstParsedText(raw.Titles)
			if raw.Channel == "" || startErr != nil || stopErr != nil || title == "" || !stopTime.After(startTime) {
				continue
			}
			document.Programmes = append(document.Programmes, model.Programme{
				ChannelID: raw.Channel,
				Title:     title,
				Desc:      firstParsedText(raw.Descriptions),
				Start:     startTime,
				Stop:      stopTime,
			})
		}
	}
}

type ampersandReader struct {
	reader  *bufio.Reader
	pending []byte
	err     error
}

func newAmpersandReader(r io.Reader) io.Reader {
	return &ampersandReader{reader: bufio.NewReader(r)}
}

func (r *ampersandReader) Read(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		if len(r.pending) == 0 {
			r.fill()
		}
		if len(r.pending) == 0 {
			if written > 0 {
				return written, nil
			}
			return 0, r.err
		}
		copied := copy(p[written:], r.pending)
		written += copied
		r.pending = r.pending[copied:]
	}
	return written, nil
}

func (r *ampersandReader) fill() {
	if r.err != nil {
		return
	}
	value, err := r.reader.ReadByte()
	if err != nil {
		r.err = err
		return
	}
	if value != '&' {
		r.pending = append(r.pending, value)
		return
	}

	entity := make([]byte, 0, 16)
	for len(entity) < 32 {
		value, err = r.reader.ReadByte()
		if err != nil {
			r.err = err
			break
		}
		if value == '&' || value == '<' {
			_ = r.reader.UnreadByte()
			break
		}
		entity = append(entity, value)
		if value == ';' {
			break
		}
	}
	if validEntity(entity) {
		r.pending = append(r.pending, '&')
	} else {
		r.pending = append(r.pending, []byte("&amp;")...)
	}
	r.pending = append(r.pending, entity...)
}

func validEntity(entity []byte) bool {
	value := string(entity)
	switch value {
	case "amp;", "lt;", "gt;", "quot;", "apos;":
		return true
	}
	if len(value) < 3 || value[0] != '#' || value[len(value)-1] != ';' {
		return false
	}
	digits := value[1 : len(value)-1]
	base := 10
	if strings.HasPrefix(digits, "x") || strings.HasPrefix(digits, "X") {
		base = 16
		digits = digits[1:]
	}
	if digits == "" {
		return false
	}
	for _, digit := range digits {
		if base == 10 && (digit < '0' || digit > '9') {
			return false
		}
		if base == 16 && !((digit >= '0' && digit <= '9') || (digit >= 'a' && digit <= 'f') || (digit >= 'A' && digit <= 'F')) {
			return false
		}
	}
	return true
}

func attribute(attributes []xml.Attr, name string) string {
	for _, attribute := range attributes {
		if attribute.Name.Local == name {
			return strings.TrimSpace(attribute.Value)
		}
	}
	return ""
}

func firstParsedText(values []parsedText) string {
	for _, value := range values {
		if text := strings.TrimSpace(value.Value); text != "" {
			return text
		}
	}
	return ""
}

func parseTime(value string) (time.Time, error) {
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

type parsedProgramme struct {
	Channel      string       `xml:"channel,attr"`
	Start        string       `xml:"start,attr"`
	Stop         string       `xml:"stop,attr"`
	Titles       []parsedText `xml:"title"`
	Descriptions []parsedText `xml:"desc"`
}

type parsedText struct {
	Value string `xml:",chardata"`
}
