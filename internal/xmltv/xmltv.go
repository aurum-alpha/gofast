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

// Source is one provider's channels and programmes for an XMLTV document.
type Source struct {
	Provider   string // provider id; used to namespace ids when namespaceIDs is true
	Label      string
	Channels   []model.Channel
	Programmes []model.Programme
}

// Write writes a single-provider XMLTV document with bare (un-namespaced) ids,
// export-filtered. Used for the per-provider /{provider}.xml artifact.
func Write(w io.Writer, channels []model.Channel, programmes []model.Programme, label string) error {
	return WriteAll(w, []Source{{Label: label, Channels: channels, Programmes: programmes}}, false, false)
}

// WriteAll writes one or more provider sources into a single XMLTV document,
// then re-parses it before returning success (invalid output must not be published).
//
// includeAll=false emits only exportable channels (model.ForExport); true also
// includes excluded/DRM channels (diagnostic view).
//
// namespaceIDs=false emits bare normalized ids (correct for a per-provider
// document); true prefixes each channel id and programme ref with the provider
// via format.CombinedID, so a combined all-provider document has globally-unique
// tvg-ids.
func WriteAll(w io.Writer, sources []Source, includeAll, namespaceIDs bool) error {
	doc := buildTV(sources, includeAll, namespaceIDs)

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

func buildTV(sources []Source, includeAll, namespaceIDs bool) tv {
	doc := tv{Generator: "gofast"}
	for _, src := range sources {
		chs := selectChannels(src.Channels, includeAll)
		sort.SliceStable(chs, func(i, j int) bool {
			if chs[i].OffsetNumber != chs[j].OffsetNumber {
				return chs[i].OffsetNumber < chs[j].OffsetNumber
			}
			return chs[i].NormalizedID < chs[j].NormalizedID
		})

		// emitted maps a channel's bare normalized id (what programmes reference)
		// to the id actually written (bare or provider-namespaced).
		emitted := make(map[string]string, len(chs))
		for _, ch := range chs {
			id := ch.NormalizedID
			if namespaceIDs {
				id = format.CombinedID(src.Provider, ch.NormalizedID)
			}
			emitted[ch.NormalizedID] = id

			c := xmlChannel{ID: id}
			c.DisplayNames = append(c.DisplayNames, xmlDisplayName{Value: format.FormatDisplayName(ch.Name, src.Label)})
			if ch.OffsetNumber > 0 {
				c.LCN = append(c.LCN, xmlLCN{Value: fmt.Sprintf("%d", ch.OffsetNumber)})
			}
			if logo := format.StripQuotes(ch.LogoURL); logo != "" {
				c.Icons = append(c.Icons, xmlIcon{Src: logo})
			}
			doc.Channels = append(doc.Channels, c)
		}

		for _, p := range src.Programmes {
			id, ok := emitted[p.ChannelID]
			if !ok {
				continue
			}
			title := format.StripQuotes(p.Title)
			if title == "" || p.Stop.Before(p.Start) || p.Stop.Equal(p.Start) {
				continue
			}
			prog := xmlProgramme{
				Channel: id,
				Start:   formatTime(p.Start),
				Stop:    formatTime(p.Stop),
				Title:   []xmlTitle{{Value: title}},
			}
			if desc := format.StripQuotes(p.Desc); desc != "" {
				prog.Desc = []xmlDesc{{Value: desc}}
			}
			doc.Programmes = append(doc.Programmes, prog)
		}
	}
	return doc
}

// selectChannels picks the channels to emit: exportable only, unless includeAll
// (then any channel with a normalized id, so excluded/DRM appear for diagnostics).
func selectChannels(channels []model.Channel, includeAll bool) []model.Channel {
	if !includeAll {
		return model.ForExport(channels)
	}
	out := make([]model.Channel, 0, len(channels))
	for _, ch := range channels {
		if ch.NormalizedID == "" {
			continue
		}
		out = append(out, ch)
	}
	return out
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
