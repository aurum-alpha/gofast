// Package dedupe finds cross-provider channels that share a normalized display
// title so operators can prefer/drop duplicates for export. Matching is
// deterministic (no LLM): strip the GoFAST " · {label}" suffix, then fold case /
// punctuation. Brand tokens (Pluto TV, Her, Plus, …) are kept so BET variants
// stay distinct. Clusters require ≥2 distinct providers.
package dedupe

import (
	"sort"
	"strings"
	"unicode"

	"github.com/j27-aurum/gofast/internal/model"
)

// Doc is the operator-authored dedupe preferences under config.yaml "dedupe".
type Doc struct {
	// PreferredProviders is the ordered provider preference for Keep preferred.
	PreferredProviders []model.ProviderID `yaml:"preferred_providers,omitempty" json:"preferred_providers,omitempty"`
	// KeepAllKeys are cluster keys the operator chose to keep all members for
	// (hidden from the default "Needs review" filter).
	KeepAllKeys []string `yaml:"keep_all_keys,omitempty" json:"keep_all_keys,omitempty"`
}

// IsZero reports whether the document carries no preferences.
func (d Doc) IsZero() bool {
	return len(d.PreferredProviders) == 0 && len(d.KeepAllKeys) == 0
}

// Clone returns a deep copy.
func (d Doc) Clone() Doc {
	out := Doc{}
	if len(d.PreferredProviders) > 0 {
		out.PreferredProviders = append([]model.ProviderID(nil), d.PreferredProviders...)
	}
	if len(d.KeepAllKeys) > 0 {
		out.KeepAllKeys = append([]string(nil), d.KeepAllKeys...)
	}
	return out
}

// Normalized returns trimmed provider ids and cluster keys (empty entries dropped).
func (d Doc) Normalized() Doc {
	out := Doc{}
	seenProv := map[model.ProviderID]bool{}
	for _, id := range d.PreferredProviders {
		id = model.ProviderID(strings.ToLower(strings.TrimSpace(string(id))))
		if id == "" || seenProv[id] {
			continue
		}
		seenProv[id] = true
		out.PreferredProviders = append(out.PreferredProviders, id)
	}
	seenKey := map[string]bool{}
	for _, k := range d.KeepAllKeys {
		k = strings.TrimSpace(k)
		if k == "" || seenKey[k] {
			continue
		}
		seenKey[k] = true
		out.KeepAllKeys = append(out.KeepAllKeys, k)
	}
	return out
}

// Status is the derived review state for one cluster.
type Status string

const (
	StatusUnresolved Status = "unresolved"
	StatusResolved   Status = "resolved"
)

// Member is one channel in a duplicate cluster.
type Member struct {
	Provider     model.ProviderID `json:"provider"`
	ID           string           `json:"id"`
	NormalizedID string           `json:"normalized_id"`
	Name         string           `json:"name"`
	EmittedGroup string           `json:"emitted_group,omitempty"`
	// Region is the scrape geography (ISO 3166-1 alpha-2), when known.
	// Same-provider rows often differ by region under multi-region scrape.
	Region string `json:"region,omitempty"`
	// Number is the provider's upstream channel number (0 if none).
	Number int `json:"number"`
	// OffsetNumber is the emitted/generated number (offset or synthesize).
	OffsetNumber   int                  `json:"offset_number"`
	Classification model.Classification `json:"classification,omitempty"`
	Health         string               `json:"health,omitempty"`
	Exportable     bool                 `json:"exportable"`
	Export         model.ExportMode     `json:"export"`
	FilterReason   model.FilterReason   `json:"filter_reason,omitempty"`
	FilterReasons  []model.FilterReason `json:"filter_reasons,omitempty"`
}

// Cluster is one multi-provider same-title bucket.
type Cluster struct {
	Key             string   `json:"key"`
	Title           string   `json:"title"`
	Status          Status   `json:"status"`
	ExportableCount int      `json:"exportable_count"`
	KeepAll         bool     `json:"keep_all"`
	Members         []Member `json:"members"`
}

// Summary counts for the Dedupes API.
type Summary struct {
	Clusters         int `json:"clusters"`
	Unresolved       int `json:"unresolved"`
	ChannelsInvolved int `json:"channels_involved"`
}

// knownLabels are shipped provider labels used when stripping " · {label}" when
// the per-channel label map is incomplete.
var knownLabels = []string{
	"LG", "Pluto", "Samsung", "Roku", "Plex", "Xumo", "Tubi", "TCL", "DistroTV", "LocalNow",
}

// ClusterKey returns the dedupe bucket key for a channel display name.
// providerLabel is the provider's configured label (may be empty).
func ClusterKey(name, providerLabel string) string {
	return NormalizeTitle(StripProviderSuffix(name, providerLabel))
}

// StripProviderSuffix removes a trailing " · {label}" (Unicode middle dot).
func StripProviderSuffix(name, providerLabel string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	labels := make([]string, 0, 1+len(knownLabels))
	if lab := strings.TrimSpace(providerLabel); lab != "" {
		labels = append(labels, lab)
	}
	labels = append(labels, knownLabels...)
	for _, lab := range labels {
		suffix := " · " + lab
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSpace(name[:len(name)-len(suffix)])
		}
	}
	return name
}

// NormalizeTitle folds a display title for exact cluster matching.
// Does not strip brand tokens (Pluto TV, Her, Plus, USA, Channel, …).
func NormalizeTitle(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.ToLower(name)
	var b strings.Builder
	b.Grow(len(name))
	prevSpace := false
	for _, r := range name {
		switch {
		case r == '&' || r == '+':
			if !prevSpace {
				b.WriteByte(' ')
			}
			b.WriteString("and")
			b.WriteByte(' ')
			prevSpace = true
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevSpace = false
		default:
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// Scan buckets painted channels into multi-provider duplicate clusters.
// labels maps provider id → display label for suffix stripping.
// keepAll is the set of cluster keys marked keep-all in config.
func Scan(channels []model.Channel, labels map[model.ProviderID]string, keepAll map[string]bool) []Cluster {
	type row struct {
		ch    model.Channel
		key   string
		title string
	}
	buckets := map[string][]row{}
	for _, ch := range channels {
		lab := labels[ch.Provider]
		stripped := StripProviderSuffix(ch.Name, lab)
		if stripped == "" {
			stripped = ch.Name
		}
		key := NormalizeTitle(stripped)
		if key == "" {
			continue
		}
		buckets[key] = append(buckets[key], row{ch: ch, key: key, title: stripped})
	}

	out := make([]Cluster, 0)
	for key, rows := range buckets {
		providers := map[model.ProviderID]bool{}
		for _, r := range rows {
			providers[r.ch.Provider] = true
		}
		if len(providers) < 2 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].ch.Provider != rows[j].ch.Provider {
				return rows[i].ch.Provider < rows[j].ch.Provider
			}
			if rows[i].ch.Region != rows[j].ch.Region {
				return rows[i].ch.Region < rows[j].ch.Region
			}
			if rows[i].ch.Name != rows[j].ch.Name {
				return rows[i].ch.Name < rows[j].ch.Name
			}
			return rows[i].ch.NormalizedID < rows[j].ch.NormalizedID
		})
		titleCounts := map[string]int{}
		members := make([]Member, 0, len(rows))
		exportable := 0
		for _, r := range rows {
			titleCounts[r.title]++
			exportMode := model.ExportAuto
			if r.ch.Emit != nil {
				exportMode = r.ch.Emit.ExportMode()
			}
			exp := !r.ch.Excluded
			if exp {
				exportable++
			}
			members = append(members, Member{
				Provider:       r.ch.Provider,
				ID:             r.ch.ID,
				NormalizedID:   r.ch.NormalizedID,
				Name:           r.ch.Name, // original provider display name
				EmittedGroup:   r.ch.EmittedGroup,
				Region:         r.ch.Region,
				Number:         r.ch.Number,
				OffsetNumber:   r.ch.OffsetNumber,
				Classification: r.ch.Classification,
				Health:         string(r.ch.Health.StatusOrUntested()),
				Exportable:     exp,
				Export:         exportMode,
				FilterReason:   r.ch.FilterReason,
				FilterReasons:  append([]model.FilterReason(nil), r.ch.EffectiveFilterReasons()...),
			})
		}
		title := bestTitle(titleCounts, rows[0].title)
		status := StatusResolved
		if exportable >= 2 {
			status = StatusUnresolved
		}
		out = append(out, Cluster{
			Key:             key,
			Title:           title,
			Status:          status,
			ExportableCount: exportable,
			KeepAll:         keepAll[key],
			Members:         members,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Members) != len(out[j].Members) {
			return len(out[i].Members) > len(out[j].Members)
		}
		return out[i].Title < out[j].Title
	})
	return out
}

// Summarize counts clusters for the API envelope.
func Summarize(clusters []Cluster, keepAll map[string]bool) Summary {
	s := Summary{Clusters: len(clusters)}
	for _, c := range clusters {
		s.ChannelsInvolved += len(c.Members)
		if c.Status == StatusUnresolved && !keepAll[c.Key] {
			s.Unresolved++
		}
	}
	return s
}

// PickPreferred returns the member index to keep given preferred provider order,
// or -1 if none of the exportable members match.
func PickPreferred(members []Member, preferred []model.ProviderID) int {
	if len(preferred) == 0 {
		return -1
	}
	rank := map[model.ProviderID]int{}
	for i, id := range preferred {
		rank[id] = i
	}
	best := -1
	bestRank := len(preferred) + 1
	for i, m := range members {
		if !m.Exportable {
			continue
		}
		r, ok := rank[m.Provider]
		if !ok {
			continue
		}
		if r < bestRank {
			bestRank = r
			best = i
		}
	}
	return best
}

func bestTitle(counts map[string]int, fallback string) string {
	best := fallback
	n := -1
	for t, c := range counts {
		if c > n || (c == n && t < best) {
			best = t
			n = c
		}
	}
	return best
}
