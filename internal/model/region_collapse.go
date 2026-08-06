package model

import (
	"log/slog"
	"strings"
)

// CollapseRegionalTwins drops same-provider regional copies of the same upstream
// channel. Channels with empty UpstreamID or Region are left untouched.
//
// preferred is the ordered system regions list (ISO uppercase). The earliest
// preferred region wins; if none of the twins' regions appear in preferred, the
// first twin in input order is kept.
//
// Certainty is UpstreamID equality only — not title matching. Callers typically
// run this per provider feed (all channels already share one Provider).
func CollapseRegionalTwins(channels []Channel, preferred []string) []Channel {
	if len(channels) < 2 {
		return channels
	}
	prefRank := make(map[string]int, len(preferred))
	for i, r := range preferred {
		r = NormalizeRegionCode(r)
		if r == "" {
			continue
		}
		if _, ok := prefRank[r]; !ok {
			prefRank[r] = i
		}
	}

	type twin struct {
		idx    int
		region string
	}
	groups := map[string][]twin{} // UpstreamID → members
	for i, ch := range channels {
		up := strings.TrimSpace(ch.UpstreamID)
		reg := NormalizeRegionCode(ch.Region)
		if up == "" || reg == "" {
			continue
		}
		groups[up] = append(groups[up], twin{idx: i, region: reg})
	}

	drop := map[int]struct{}{}
	dropped := 0
	for _, members := range groups {
		if len(members) < 2 {
			continue
		}
		keep := members[0].idx
		bestRank := len(prefRank) + len(members) + 1
		if r, ok := prefRank[members[0].region]; ok {
			bestRank = r
		}
		for _, m := range members[1:] {
			rank, ok := prefRank[m.region]
			if !ok {
				rank = len(prefRank) + len(members) + 1
			}
			if rank < bestRank {
				bestRank = rank
				keep = m.idx
			}
		}
		for _, m := range members {
			if m.idx != keep {
				drop[m.idx] = struct{}{}
				dropped++
			}
		}
	}
	if dropped == 0 {
		return channels
	}
	out := make([]Channel, 0, len(channels)-dropped)
	for i, ch := range channels {
		if _, skip := drop[i]; skip {
			continue
		}
		out = append(out, ch)
	}
	slog.Info("collapsed regional channel twins",
		"dropped", dropped,
		"kept", len(out),
	)
	return out
}
