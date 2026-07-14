// Package emit renders M3U and XMLTV from normalized channels/programmes.
package emit

import (
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/j27-aurum/gofast/internal/model"
)

// M3U writes an #EXTM3U playlist. Excluded channels are omitted.
// Channels are sorted by Number (then NormalizedID).
func M3U(w io.Writer, channels []model.Channel, label string) error {
	if _, err := io.WriteString(w, "#EXTM3U\n"); err != nil {
		return err
	}
	chs := exportable(channels)
	sort.SliceStable(chs, func(i, j int) bool {
		if chs[i].Number != chs[j].Number {
			return chs[i].Number < chs[j].Number
		}
		return chs[i].NormalizedID < chs[j].NormalizedID
	})
	for _, ch := range chs {
		id := ch.NormalizedID
		name := model.StripQuotes(ch.Name)
		display := model.DisplayName(ch.Name, label)
		group := model.GroupTitle(label, ch.Group)
		logo := model.StripQuotes(ch.LogoURL)
		line := fmt.Sprintf(
			`#EXTINF:-1 tvg-id="%s" tvg-name="%s"`,
			id, name,
		)
		if ch.Number > 0 {
			line += fmt.Sprintf(` tvg-chno="%s"`, strconv.Itoa(ch.Number))
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

func exportable(channels []model.Channel) []model.Channel {
	out := make([]model.Channel, 0, len(channels))
	for _, ch := range channels {
		if ch.Excluded || ch.NormalizedID == "" || ch.StreamURL == "" {
			continue
		}
		out = append(out, ch)
	}
	return out
}
