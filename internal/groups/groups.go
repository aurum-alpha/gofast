// Package groups implements the operator-defined cross-provider group taxonomy:
// merge upstream provider groups into named canonical groups and disable groups
// (globally or per provider) so their channels drop from export. It is applied
// at emit time — upstream Channel.Group is never mutated, only Channel.EmittedGroup.
package groups

import (
	"strings"

	"github.com/j27-aurum/gofast/internal/model"
)

// Doc is the operator-authored taxonomy, serialized under the config.yaml
// "groups" key. It is app-managed but lives in config.yaml as the single config
// home (written back via the config writer).
type Doc struct {
	Enabled  bool     `yaml:"enabled" json:"enabled"`
	Merges   []Merge  `yaml:"merges,omitempty" json:"merges"`
	Disabled []string `yaml:"disabled,omitempty" json:"disabled"`
}

// Merge names a canonical group and the upstream group strings folded into it.
type Merge struct {
	Name    string   `yaml:"name" json:"name"`
	Members []string `yaml:"members" json:"members"`
	// Enabled defaults to true when nil. False disables the whole merged group.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// IsZero reports whether the document carries no taxonomy (used by config merge).
func (d Doc) IsZero() bool {
	return !d.Enabled && len(d.Merges) == 0 && len(d.Disabled) == 0
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
			if m.Enabled != nil {
				v := *m.Enabled
				cm.Enabled = &v
			}
			out.Merges[i] = cm
		}
	}
	if len(d.Disabled) > 0 {
		out.Disabled = append([]string(nil), d.Disabled...)
	}
	return out
}

// Policy is a compiled, immutable snapshot of a Doc for fast per-channel lookup.
type Policy struct {
	enabled          bool
	byUpstream       map[string]merged // normalized upstream group -> canonical
	disabledGlobal   map[string]bool   // normalized upstream group
	disabledProvider map[string]bool   // providerKey(id, group)
}

type merged struct {
	name    string
	enabled bool
}

// Compile builds a Policy from a Doc. The result is safe for concurrent reads.
func Compile(doc Doc) *Policy {
	p := &Policy{
		enabled:          doc.Enabled,
		byUpstream:       make(map[string]merged),
		disabledGlobal:   make(map[string]bool),
		disabledProvider: make(map[string]bool),
	}
	for _, m := range doc.Merges {
		name := strings.TrimSpace(m.Name)
		if name == "" {
			continue
		}
		enabled := m.Enabled == nil || *m.Enabled
		for _, member := range m.Members {
			k := normalizeKey(member)
			if k == "" {
				continue
			}
			p.byUpstream[k] = merged{name: name, enabled: enabled}
		}
	}
	for _, sel := range doc.Disabled {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			continue
		}
		if id, group, ok := splitSelector(sel); ok {
			p.disabledProvider[providerKey(id, group)] = true
			continue
		}
		p.disabledGlobal[normalizeKey(sel)] = true
	}
	return p
}

// Enabled reports whether the taxonomy is active.
func (p *Policy) Enabled() bool { return p != nil && p.enabled }

// Lookup resolves an upstream group for one provider into its effective
// group-title, whether it is mapped to a canonical group, and whether it is
// disabled (channels dropped from export).
func (p *Policy) Lookup(providerID model.ProviderID, upstream string) (title string, mapped, disabled bool) {
	if p == nil {
		return strings.TrimSpace(upstream), false, false
	}
	k := normalizeKey(upstream)
	m, mapped := p.byUpstream[k]
	if mapped && !m.enabled {
		disabled = true
	}
	if p.disabledGlobal[k] {
		disabled = true
	}
	if p.disabledProvider[providerKey(providerID, upstream)] {
		disabled = true
	}
	if mapped {
		title = m.name
	} else {
		title = strings.TrimSpace(upstream)
	}
	return title, mapped, disabled
}

// AssignedName returns the canonical group an upstream string is merged into,
// or ("", false) when it is unassigned.
func (p *Policy) AssignedName(upstream string) (string, bool) {
	if p == nil {
		return "", false
	}
	m, ok := p.byUpstream[normalizeKey(upstream)]
	if !ok {
		return "", false
	}
	return m.name, true
}

// Apply sets EmittedGroup on exportable channels and marks channels in disabled
// groups with a disabled-group FilterReason (leaving upstream Channel.Group
// untouched). It is a no-op when the taxonomy is off, so emit falls back to the
// legacy "{label}: {group}". Reasons accumulate — already-excluded channels
// still gain the disabled-group reason when applicable.
func Apply(chs []model.Channel, providerID model.ProviderID, p *Policy) []model.Channel {
	if p == nil || !p.enabled {
		return chs
	}
	out := make([]model.Channel, len(chs))
	copy(out, chs)
	for i := range out {
		title, _, disabled := p.Lookup(providerID, out[i].Group)
		if disabled {
			name := title
			if name == "" {
				name = strings.TrimSpace(out[i].Group)
			}
			out[i].AddFilterReason(model.DisabledGroupReason(name))
			continue
		}
		if !out[i].Excluded {
			out[i].EmittedGroup = title
		}
	}
	return out
}

func normalizeKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// splitSelector splits "providerID/groupName" on the first slash. A selector
// with no slash is a global (all-provider) upstream group name.
func splitSelector(sel string) (id model.ProviderID, group string, ok bool) {
	i := strings.IndexByte(sel, '/')
	if i < 0 {
		return "", "", false
	}
	return model.ProviderID(strings.TrimSpace(sel[:i])), strings.TrimSpace(sel[i+1:]), true
}

func providerKey(id model.ProviderID, group string) string {
	return string(id) + "\x00" + normalizeKey(group)
}
