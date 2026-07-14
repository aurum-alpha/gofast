// Package xmltv writes XMLTV documents from normalized channels and programmes.
package xmltv

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/j27-aurum/gofast/internal/format"
	"github.com/j27-aurum/gofast/internal/model"
)

// Write writes an XMLTV document using encoding/xml, then re-parses it before
// returning success (invalid output must not be published).
func Write(w io.Writer, channels []model.Channel, programmes []model.Programme, label string) error {
	chs := model.ForExport(channels)
	sort.SliceStable(chs, func(i, j int) bool {
		if chs[i].OffsetNumber != chs[j].OffsetNumber {
			return chs[i].OffsetNumber < chs[j].OffsetNumber
		}
		return chs[i].NormalizedID < chs[j].NormalizedID
	})
	keep := make(map[string]struct{}, len(chs))
	for _, ch := range chs {
		keep[ch.NormalizedID] = struct{}{}
	}

	doc := tv{
		Generator: "gofast",
	}
	for _, ch := range chs {
		c := xmlChannel{ID: ch.NormalizedID}
		c.DisplayNames = append(c.DisplayNames, xmlDisplayName{Value: format.FormatDisplayName(ch.Name, label)})
		if ch.OffsetNumber > 0 {
			c.LCN = append(c.LCN, xmlLCN{Value: fmt.Sprintf("%d", ch.OffsetNumber)})
		}
		if logo := format.StripQuotes(ch.LogoURL); logo != "" {
			c.Icons = append(c.Icons, xmlIcon{Src: logo})
		}
		doc.Channels = append(doc.Channels, c)
	}
	for _, p := range programmes {
		if _, ok := keep[p.ChannelID]; !ok {
			continue
		}
		title := format.StripQuotes(p.Title)
		if title == "" || p.Stop.Before(p.Start) || p.Stop.Equal(p.Start) {
			continue
		}
		prog := xmlProgramme{
			Channel: p.ChannelID,
			Start:   formatTime(p.Start),
			Stop:    formatTime(p.Stop),
			Title:   []xmlTitle{{Value: title}},
		}
		if desc := format.StripQuotes(p.Desc); desc != "" {
			prog.Desc = []xmlDesc{{Value: desc}}
		}
		doc.Programmes = append(doc.Programmes, prog)
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("xmltv encode: %w", err)
	}
	if err := enc.Flush(); err != nil {
		return err
	}
	// Gate: re-parse before publish.
	var check tv
	if err := xml.Unmarshal(buf.Bytes(), &check); err != nil {
		return fmt.Errorf("xmltv re-parse: %w", err)
	}
	_, err := w.Write(buf.Bytes())
	return err
}

func formatTime(t time.Time) string {
	return t.UTC().Format("20060102150405 +0000")
}

type tv struct {
	XMLName    xml.Name       `xml:"tv"`
	Generator  string         `xml:"generator-info-name,attr"`
	Channels   []xmlChannel   `xml:"channel"`
	Programmes []xmlProgramme `xml:"programme"`
}

type xmlChannel struct {
	ID           string           `xml:"id,attr"`
	DisplayNames []xmlDisplayName `xml:"display-name"`
	LCN          []xmlLCN         `xml:"lcn"`
	Icons        []xmlIcon        `xml:"icon"`
}

type xmlDisplayName struct {
	Value string `xml:",chardata"`
}

type xmlLCN struct {
	Value string `xml:",chardata"`
}

type xmlIcon struct {
	Src string `xml:"src,attr"`
}

type xmlProgramme struct {
	Channel string     `xml:"channel,attr"`
	Start   string     `xml:"start,attr"`
	Stop    string     `xml:"stop,attr"`
	Title   []xmlTitle `xml:"title"`
	Desc    []xmlDesc  `xml:"desc,omitempty"`
}

type xmlTitle struct {
	Value string `xml:",chardata"`
}

type xmlDesc struct {
	Value string `xml:",chardata"`
}
