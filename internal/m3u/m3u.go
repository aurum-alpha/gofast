// Package m3u writes #EXTM3U playlists from normalized channels.
package m3u

import (
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/j27-aurum/gofast/internal/format"
	"github.com/j27-aurum/gofast/internal/model"
)

// Source is one provider's channels for a playlist.
type Source struct {
	Provider string // provider id; used to namespace tvg-ids when namespaceIDs is true
	Label    string
	Channels []model.Channel
}

// Write writes a single-provider #EXTM3U playlist with bare (un-namespaced) ids.
// Used for the per-provider playlist. Excluded channels are omitted.
func Write(w io.Writer, channels []model.Channel, label string) error {
	return WriteAll(w, []Source{{Label: label, Channels: channels}}, false)
}

// WriteAll writes one or more provider sources into a single #EXTM3U playlist,
// each source's channels sorted by OffsetNumber (then NormalizedID). Excluded
// channels are omitted. When namespaceIDs is true, each tvg-id is prefixed with
// the provider via format.CombinedID (globally-unique ids for a combined doc).
func WriteAll(w io.Writer, sources []Source, namespaceIDs bool) error {
	if _, err := io.WriteString(w, "#EXTM3U\n"); err != nil {
		return err
	}
	for _, src := range sources {
		chs := model.ForExport(src.Channels)
		sort.SliceStable(chs, func(i, j int) bool {
			if chs[i].OffsetNumber != chs[j].OffsetNumber {
				return chs[i].OffsetNumber < chs[j].OffsetNumber
			}
			return chs[i].NormalizedID < chs[j].NormalizedID
		})
		for _, ch := range chs {
			id := ch.NormalizedID
			if namespaceIDs {
				id = format.CombinedID(src.Provider, ch.NormalizedID)
			}
			name := format.StripQuotes(ch.Name)
			display := format.FormatDisplayName(ch.Name, src.Label)
			group := format.FormatGroupTitle(src.Label, ch.Group)
			logo := format.StripQuotes(ch.LogoURL)
			line := fmt.Sprintf(`#EXTINF:-1 tvg-id="%s" tvg-name="%s"`, id, name)
			if ch.OffsetNumber > 0 {
				line += fmt.Sprintf(` tvg-chno="%s"`, strconv.Itoa(ch.OffsetNumber))
			}
			if logo != "" {
				line += fmt.Sprintf(` tvg-logo="%s"`, logo)
			}
			if group != "" {
				line += fmt.Sprintf(` group-title="%s"`, group)
			}
			line += fmt.Sprintf(",%s\n%s\n", display, ch.OutputURL())
			if _, err := io.WriteString(w, line); err != nil {
				return err
			}
		}
	}
	return nil
}
