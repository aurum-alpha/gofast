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

// Write writes an #EXTM3U playlist. Excluded channels are omitted.
// Channels are sorted by OffsetNumber (then NormalizedID).
func Write(w io.Writer, channels []model.Channel, label string) error {
	if _, err := io.WriteString(w, "#EXTM3U\n"); err != nil {
		return err
	}
	chs := model.ForExport(channels)
	sort.SliceStable(chs, func(i, j int) bool {
		if chs[i].OffsetNumber != chs[j].OffsetNumber {
			return chs[i].OffsetNumber < chs[j].OffsetNumber
		}
		return chs[i].NormalizedID < chs[j].NormalizedID
	})
	for _, ch := range chs {
		id := ch.NormalizedID
		name := format.StripQuotes(ch.Name)
		display := format.FormatDisplayName(ch.Name, label)
		group := format.FormatGroupTitle(label, ch.Group)
		logo := format.StripQuotes(ch.LogoURL)
		line := fmt.Sprintf(
			`#EXTINF:-1 tvg-id="%s" tvg-name="%s"`,
			id, name,
		)
		if ch.OffsetNumber > 0 {
			line += fmt.Sprintf(` tvg-chno="%s"`, strconv.Itoa(ch.OffsetNumber))
		}
		if logo != "" {
			line += fmt.Sprintf(` tvg-logo="%s"`, logo)
		}
		if group != "" {
			line += fmt.Sprintf(` group-title="%s"`, group)
		}
		line += fmt.Sprintf(",%s\n%s\n", display, ch.StreamURL)
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
	}
	return nil
}
