// Package xmltv writes XMLTV documents from normalized channels and programmes.
package xmltv

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/format"
	"github.com/j27-aurum/gofast/internal/model"
)

const timeLayout = "20060102150405 -0700"

// Options controls document-wide XMLTV rendering.
type Options struct {
	IncludeExcluded bool
	NamespaceIDs    bool
}

// Source is one provider's channels and programmes for an XMLTV document.
type Source struct {
	Provider   model.ProviderID
	Label      string
	Channels   []model.Channel
	Programmes []model.Programme
}

type channelEntry struct {
	channel  model.Channel
	id       string
	label    string
	provider model.ProviderID
}

type programmeEntry struct {
	channelRank int
	id          string
	programme   model.Programme
}

// Marshal renders a single-provider XMLTV document with bare ids.
func Marshal(channels []model.Channel, programmes []model.Programme, label string) ([]byte, error) {
	return MarshalAll(
		[]Source{{Label: label, Channels: channels, Programmes: programmes}},
		Options{},
	)
}

// MarshalAll renders, re-parses, and semantically validates a complete XMLTV
// document before returning bytes suitable for publication.
func MarshalAll(sources []Source, options Options) ([]byte, error) {
	document, err := buildTV(sources, options)
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	buffer.WriteString(xml.Header)
	encoder := xml.NewEncoder(&buffer)
	encoder.Indent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("xmltv encode: %w", err)
	}
	if err := encoder.Flush(); err != nil {
		return nil, fmt.Errorf("xmltv flush: %w", err)
	}
	buffer.WriteByte('\n')

	var check tv
	if err := xml.Unmarshal(buffer.Bytes(), &check); err != nil {
		return nil, fmt.Errorf("xmltv re-parse: %w", err)
	}
	if err := validateTV(check); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Write writes a single-provider XMLTV document with bare ids.
func Write(w io.Writer, channels []model.Channel, programmes []model.Programme, label string) error {
	data, err := Marshal(channels, programmes, label)
	if err != nil {
		return err
	}
	return writeAll(w, data)
}

// WriteAll writes a complete multi-source XMLTV document.
func WriteAll(w io.Writer, sources []Source, options Options) error {
	data, err := MarshalAll(sources, options)
	if err != nil {
		return err
	}
	return writeAll(w, data)
}

func buildTV(sources []Source, options Options) (tv, error) {
	entries, emittedBySource, err := collectChannels(sources, options)
	if err != nil {
		return tv{}, err
	}

	document := tv{Generator: "gofast"}
	channelRank := make(map[string]int, len(entries))
	for index, item := range entries {
		channelRank[item.id] = index
		channel := xmlChannel{
			ID: item.id,
			DisplayNames: []xmlDisplayName{{
				Value: format.FormatDisplayName(item.channel.Name, item.label),
			}},
		}
		if item.channel.OffsetNumber > 0 {
			channel.LCN = []xmlLCN{{Value: fmt.Sprintf("%d", item.channel.OffsetNumber)}}
		}
		if logo := strings.TrimSpace(item.channel.LogoURL); logo != "" {
			channel.Icons = []xmlIcon{{Src: logo}}
		}
		document.Channels = append(document.Channels, channel)
	}

	var programmes []programmeEntry
	for sourceIndex, source := range sources {
		emitted := emittedBySource[sourceIndex]
		for _, programme := range source.Programmes {
			id, ok := emitted[programme.ChannelID]
			if !ok || !programme.IsValid() {
				continue
			}
			programmes = append(programmes, programmeEntry{
				channelRank: channelRank[id],
				id:          id,
				programme:   programme,
			})
		}
	}
	sort.Slice(programmes, func(i, j int) bool {
		left, right := programmes[i], programmes[j]
		if left.channelRank != right.channelRank {
			return left.channelRank < right.channelRank
		}
		if !left.programme.Start.Equal(right.programme.Start) {
			return left.programme.Start.Before(right.programme.Start)
		}
		if !left.programme.Stop.Equal(right.programme.Stop) {
			return left.programme.Stop.Before(right.programme.Stop)
		}
		if left.programme.Title != right.programme.Title {
			return left.programme.Title < right.programme.Title
		}
		return left.programme.Desc < right.programme.Desc
	})
	for _, item := range programmes {
		programme := xmlProgramme{
			Channel: item.id,
			Start:   formatTime(item.programme.Start),
			Stop:    formatTime(item.programme.Stop),
			Title:   []xmlTitle{{Value: strings.TrimSpace(item.programme.Title)}},
		}
		if description := strings.TrimSpace(item.programme.Desc); description != "" {
			programme.Desc = []xmlDesc{{Value: description}}
		}
		document.Programmes = append(document.Programmes, programme)
	}
	return document, nil
}

func collectChannels(sources []Source, options Options) ([]channelEntry, []map[string]string, error) {
	var entries []channelEntry
	emittedBySource := make([]map[string]string, len(sources))
	seen := make(map[string]model.Channel)
	for sourceIndex, source := range sources {
		emittedBySource[sourceIndex] = make(map[string]string)
		for _, channel := range selectChannels(source.Channels, options.IncludeExcluded) {
			id := channel.NormalizedID
			if options.NamespaceIDs {
				if source.Provider == "" {
					return nil, nil, fmt.Errorf("xmltv: channel %q has no provider for namespaced id", channel.NormalizedID)
				}
				id = format.CombinedID(string(source.Provider), channel.NormalizedID)
			}
			if previous, duplicate := seen[id]; duplicate {
				return nil, nil, fmt.Errorf(
					"xmltv: duplicate emitted channel id %q from upstream ids %q and %q",
					id,
					previous.ID,
					channel.ID,
				)
			}
			seen[id] = channel
			emittedBySource[sourceIndex][channel.NormalizedID] = id
			entries = append(entries, channelEntry{
				channel:  channel,
				id:       id,
				label:    source.Label,
				provider: source.Provider,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		leftNumber, rightNumber := left.channel.OffsetNumber, right.channel.OffsetNumber
		if leftNumber <= 0 && rightNumber > 0 {
			return false
		}
		if leftNumber > 0 && rightNumber <= 0 {
			return true
		}
		if leftNumber != rightNumber {
			return leftNumber < rightNumber
		}
		if left.provider != right.provider {
			return left.provider < right.provider
		}
		return left.id < right.id
	})
	return entries, emittedBySource, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(timeLayout)
}

func selectChannels(channels []model.Channel, includeExcluded bool) []model.Channel {
	if !includeExcluded {
		return model.ForExport(channels)
	}
	out := make([]model.Channel, 0, len(channels))
	for _, channel := range channels {
		if channel.NormalizedID != "" {
			out = append(out, channel)
		}
	}
	return out
}

func validateTV(document tv) error {
	channels := make(map[string]struct{}, len(document.Channels))
	for _, channel := range document.Channels {
		if channel.ID == "" {
			return fmt.Errorf("xmltv validate: empty channel id")
		}
		if _, duplicate := channels[channel.ID]; duplicate {
			return fmt.Errorf("xmltv validate: duplicate channel id %q", channel.ID)
		}
		if len(channel.DisplayNames) == 0 || strings.TrimSpace(channel.DisplayNames[0].Value) == "" {
			return fmt.Errorf("xmltv validate: channel %q has empty display name", channel.ID)
		}
		channels[channel.ID] = struct{}{}
	}
	for _, programme := range document.Programmes {
		if _, ok := channels[programme.Channel]; !ok {
			return fmt.Errorf("xmltv validate: programme references unknown channel %q", programme.Channel)
		}
		start, startErr := time.Parse(timeLayout, programme.Start)
		stop, stopErr := time.Parse(timeLayout, programme.Stop)
		if startErr != nil || stopErr != nil || !stop.After(start) {
			return fmt.Errorf("xmltv validate: programme on %q has invalid times", programme.Channel)
		}
		if len(programme.Title) == 0 || strings.TrimSpace(programme.Title[0].Value) == "" {
			return fmt.Errorf("xmltv validate: programme on %q has empty title", programme.Channel)
		}
	}
	return nil
}

func writeAll(w io.Writer, data []byte) error {
	written, err := w.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return nil
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
