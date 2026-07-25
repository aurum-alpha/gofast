// Package categories implements the operator-defined programme category
// taxonomy: merge upstream XMLTV <category> labels into canonical names.
// Applied at emit time — upstream Programme.Categories is never mutated, only
// Programme.EmittedCategories. There is no disable (labels do not remove airings).
package categories

import (
	"strings"

	"github.com/j27-aurum/gofast/internal/model"
)

// Doc is the operator-authored taxonomy under the config.yaml "categories" key.
type Doc struct {
	Enabled bool    `yaml:"enabled" json:"enabled"`
	Merges  []Merge `yaml:"merges,omitempty" json:"merges"`
}

// Merge names a canonical category and the upstream strings folded into it.
type Merge struct {
	Name    string   `yaml:"name" json:"name"`
	Members []string `yaml:"members" json:"members"`
}

// IsZero reports whether the document carries no taxonomy.
func (d Doc) IsZero() bool {
	return !d.Enabled && len(d.Merges) == 0
}

// Clone returns a deep copy so callers can hold it without aliasing slices.
func (d Doc) Clone() Doc {
	out := Doc{Enabled: d.Enabled}
	if len(d.Merges) > 0 {
		out.Merges = make([]Merge, len(d.Merges))
		for i, m := range d.Merges {
			cm := Merge{Name: m.Name}
			if len(m.Members) > 0 {
				cm.Members = append([]string(nil), m.Members...)
			}
			out.Merges[i] = cm
		}
	}
	return out
}

// Policy is a compiled, immutable snapshot of a Doc for fast lookup.
type Policy struct {
	enabled    bool
	byUpstream map[string]string // normalized upstream -> canonical name
}

// Compile builds a Policy from a Doc. Safe for concurrent reads.
func Compile(doc Doc) *Policy {
	p := &Policy{
		enabled:    doc.Enabled,
		byUpstream: make(map[string]string),
	}
	for _, m := range doc.Merges {
		name := strings.TrimSpace(m.Name)
		if name == "" {
			continue
		}
		for _, member := range m.Members {
			k := normalizeKey(member)
			if k == "" {
				continue
			}
			p.byUpstream[k] = name
		}
	}
	return p
}

// Enabled reports whether the taxonomy is active.
func (p *Policy) Enabled() bool { return p != nil && p.enabled }

// Map resolves one upstream category to its effective label.
func (p *Policy) Map(upstream string) string {
	trimmed := strings.TrimSpace(upstream)
	if trimmed == "" {
		return ""
	}
	if p == nil || !p.enabled {
		return trimmed
	}
	if name, ok := p.byUpstream[normalizeKey(trimmed)]; ok {
		return name
	}
	return trimmed
}

// AssignedName returns the canonical category an upstream string is merged into.
func (p *Policy) AssignedName(upstream string) (string, bool) {
	if p == nil {
		return "", false
	}
	name, ok := p.byUpstream[normalizeKey(upstream)]
	return name, ok
}

// MapAll maps a programme's upstream categories, deduping by normalized key
// while preserving first-seen effective display order.
func (p *Policy) MapAll(upstream []string) []string {
	if len(upstream) == 0 {
		return nil
	}
	out := make([]string, 0, len(upstream))
	seen := make(map[string]struct{}, len(upstream))
	for _, u := range upstream {
		title := p.Map(u)
		if title == "" {
			continue
		}
		k := normalizeKey(title)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, title)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Apply sets EmittedCategories on programmes when the taxonomy is enabled.
// No-op when off (marshal falls back to Categories via ExportCategories).
func Apply(progs []model.Programme, p *Policy) []model.Programme {
	if p == nil || !p.enabled {
		return progs
	}
	out := make([]model.Programme, len(progs))
	copy(out, progs)
	for i := range out {
		out[i].EmittedCategories = p.MapAll(out[i].Categories)
	}
	return out
}

func normalizeKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
