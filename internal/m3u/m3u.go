// Package m3u writes #EXTM3U playlists from normalized channels.
package m3u

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/j27-aurum/gofast/internal/format"
	"github.com/j27-aurum/gofast/internal/model"
)

// Options controls document-wide M3U rendering.
type Options struct {
	NamespaceIDs bool
}

// Source is one provider's channels for a playlist.
type Source struct {
	Provider model.ProviderID
	Label    string
	Channels []model.Channel
}

type entry struct {
	channel  model.Channel
	id       string
	label    string
	provider model.ProviderID
}

// Marshal renders a single-provider playlist with bare ids.
func Marshal(channels []model.Channel, label string) ([]byte, error) {
	return MarshalAll([]Source{{Label: label, Channels: channels}}, Options{})
}

// MarshalAll renders a complete playlist before returning it. Exportable
// channels from every source are globally sorted by final channel number, with
// unnumbered channels last. Namespaced ids are used for aggregate documents.
func MarshalAll(sources []Source, options Options) ([]byte, error) {
	entries, err := collectEntries(sources, options)
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	buffer.WriteString("#EXTM3U\n")
	for _, item := range entries {
		channel := item.channel
		displayName := channel.DisplayName()
		line := fmt.Sprintf(
			`#EXTINF:-1 tvg-id="%s" tvg-name="%s"`,
			item.id,
			format.M3UAttribute(displayName),
		)
		if channel.OffsetNumber > 0 {
			line += fmt.Sprintf(` tvg-chno="%s"`, strconv.Itoa(channel.OffsetNumber))
		}
		if logo := format.M3UAttribute(channel.LogoURL); logo != "" {
			line += fmt.Sprintf(` tvg-logo="%s"`, logo)
		}
		groupTitle := channel.EmittedGroup
		if groupTitle == "" {
			groupTitle = format.FormatGroupTitle(item.label, channel.Group)
		}
		if group := format.M3UAttribute(groupTitle); group != "" {
			line += fmt.Sprintf(` group-title="%s"`, group)
		}
		line += "," + format.M3UText(format.FormatDisplayName(displayName, item.label))
		buffer.WriteString(line)
		buffer.WriteByte('\n')
		buffer.WriteString(channel.OutputURL())
		buffer.WriteByte('\n')
	}
	return buffer.Bytes(), nil
}

// Write writes a single-provider playlist with bare ids.
func Write(w io.Writer, channels []model.Channel, label string) error {
	data, err := Marshal(channels, label)
	if err != nil {
		return err
	}
	return writeAll(w, data)
}

// WriteAll writes a complete multi-source playlist.
func WriteAll(w io.Writer, sources []Source, options Options) error {
	data, err := MarshalAll(sources, options)
	if err != nil {
		return err
	}
	return writeAll(w, data)
}

func collectEntries(sources []Source, options Options) ([]entry, error) {
	var entries []entry
	seen := make(map[string]model.Channel)
	for _, source := range sources {
		for _, channel := range model.ForExport(source.Channels) {
			if !format.ValidM3ULine(channel.OutputURL()) {
				return nil, fmt.Errorf("m3u: channel %q has invalid playback URL", channel.NormalizedID)
			}
			id := channel.NormalizedID
			if options.NamespaceIDs {
				if source.Provider == "" {
					return nil, fmt.Errorf("m3u: channel %q has no provider for namespaced id", channel.NormalizedID)
				}
				id = format.CombinedID(string(source.Provider), channel.NormalizedID)
			}
			if previous, duplicate := seen[id]; duplicate {
				return nil, fmt.Errorf(
					"m3u: duplicate emitted channel id %q from upstream ids %q and %q",
					id,
					previous.ID,
					channel.ID,
				)
			}
			seen[id] = channel
			entries = append(entries, entry{
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
	return entries, nil
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
