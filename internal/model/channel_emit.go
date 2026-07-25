package model

import (
	"fmt"
	"strings"
)

const (
	// FilterReasonEmitDisabled is set when the operator excludes a channel from emit.
	FilterReasonEmitDisabled = "emit disabled"
)

// ExportMode controls whether a channel is included in the exported lineup.
type ExportMode string

const (
	ExportAuto     ExportMode = "auto"
	ExportEnabled  ExportMode = "enabled"
	ExportDisabled ExportMode = "disabled"
)

// ChannelEmit is operator customization of what fastgen emits for one channel.
// Omitted fields mean "use the fastgen-produced default." It does not change
// upstream fetch/raw data.
type ChannelEmit struct {
	Export  ExportMode `json:"export,omitempty" yaml:"export,omitempty"`
	Name    string     `json:"name,omitempty" yaml:"name,omitempty"`
	Group   string     `json:"group,omitempty" yaml:"group,omitempty"`
	Number  *int       `json:"number,omitempty" yaml:"number,omitempty"`
	LogoURL string     `json:"logo_url,omitempty" yaml:"logo_url,omitempty"`
}

// EmitDefaults are the fastgen-produced export values before per-field customs.
// Painted for the channel detail UI so unchecked Customize boxes show the default.
type EmitDefaults struct {
	Name    string `json:"name"`
	Group   string `json:"group"`
	Number  int    `json:"number"`
	LogoURL string `json:"logo_url"`
}

// IsZero reports whether no emit customization is set.
func (e ChannelEmit) IsZero() bool {
	return e.ExportMode() == ExportAuto &&
		strings.TrimSpace(e.Name) == "" &&
		strings.TrimSpace(e.Group) == "" &&
		e.Number == nil &&
		strings.TrimSpace(e.LogoURL) == ""
}

// ExportMode returns the effective export mode (empty → auto).
func (e ChannelEmit) ExportMode() ExportMode {
	switch ExportMode(strings.ToLower(strings.TrimSpace(string(e.Export)))) {
	case ExportEnabled:
		return ExportEnabled
	case ExportDisabled:
		return ExportDisabled
	default:
		return ExportAuto
	}
}

// Normalized returns a trimmed copy suitable for persistence. Zero rows become IsZero.
func (e ChannelEmit) Normalized() ChannelEmit {
	out := ChannelEmit{
		Name:    strings.TrimSpace(e.Name),
		Group:   strings.TrimSpace(e.Group),
		LogoURL: strings.TrimSpace(e.LogoURL),
	}
	switch e.ExportMode() {
	case ExportEnabled:
		out.Export = ExportEnabled
	case ExportDisabled:
		out.Export = ExportDisabled
	}
	if e.Number != nil {
		n := *e.Number
		out.Number = &n
	}
	return out
}

// Validate checks a single emit row (map key validated separately).
func (e ChannelEmit) Validate() error {
	raw := strings.ToLower(strings.TrimSpace(string(e.Export)))
	if raw != "" && raw != string(ExportAuto) && raw != string(ExportEnabled) && raw != string(ExportDisabled) {
		return fmt.Errorf("export: invalid value %q", e.Export)
	}
	if e.Number != nil && *e.Number < 0 {
		return fmt.Errorf("number: must be >= 0")
	}
	return nil
}

// Equal reports whether two emit rows are the same.
func (e ChannelEmit) Equal(o ChannelEmit) bool {
	if e.ExportMode() != o.ExportMode() {
		return false
	}
	if e.Name != o.Name || e.Group != o.Group || e.LogoURL != o.LogoURL {
		return false
	}
	if (e.Number == nil) != (o.Number == nil) {
		return false
	}
	if e.Number != nil && *e.Number != *o.Number {
		return false
	}
	return true
}

// ValidateChannelEmitMap validates every key/row in a channel_emit map.
func ValidateChannelEmitMap(m map[string]ChannelEmit) error {
	for key, row := range m {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("channel_emit: empty key")
		}
		if err := row.Validate(); err != nil {
			return fmt.Errorf("channel_emit.%s: %w", key, err)
		}
	}
	return nil
}

// ChannelEmitMapsEqual reports deep equality of two emit maps.
func ChannelEmitMapsEqual(a, b map[string]ChannelEmit) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || !av.Equal(bv) {
			return false
		}
	}
	return true
}

// DisplayName returns the emitted display name (custom or upstream Name).
func (c Channel) DisplayName() string {
	if strings.TrimSpace(c.EmittedName) != "" {
		return c.EmittedName
	}
	return c.Name
}

// ApplyChannelEmitPresentation snapshots emit defaults for name/number/logo and
// applies those customs onto emit-facing fields (EmittedName, OffsetNumber, LogoURL).
func ApplyChannelEmitPresentation(channels []Channel, emits map[string]ChannelEmit) []Channel {
	if len(channels) == 0 {
		return channels
	}
	out := make([]Channel, len(channels))
	copy(out, channels)
	for i := range out {
		d := out[i].emitDefaults()
		d.Name = out[i].Name
		d.Number = out[i].OffsetNumber
		d.LogoURL = out[i].LogoURL
		out[i].EmitDefaults = d

		em, ok := emits[out[i].NormalizedID]
		if !ok {
			continue
		}
		em = em.Normalized()
		if em.Name != "" {
			out[i].EmittedName = em.Name
		}
		if em.Number != nil {
			out[i].OffsetNumber = *em.Number
		}
		if em.LogoURL != "" {
			out[i].LogoURL = em.LogoURL
			out[i].LogoError = ""
		}
	}
	return out
}

// ApplyChannelEmitGroup snapshots the default emitted group-title and applies a
// custom group (wins over taxonomy). Force-include soft-clears disabled-group.
func ApplyChannelEmitGroup(channels []Channel, emits map[string]ChannelEmit) []Channel {
	if len(channels) == 0 {
		return channels
	}
	out := make([]Channel, len(channels))
	copy(out, channels)
	for i := range out {
		d := out[i].emitDefaults()
		if out[i].EmittedGroup != "" {
			d.Group = out[i].EmittedGroup
		} else {
			d.Group = out[i].Group
		}
		out[i].EmitDefaults = d

		em, ok := emits[out[i].NormalizedID]
		if !ok {
			continue
		}
		em = em.Normalized()
		if em.Group != "" {
			out[i].EmittedGroup = em.Group
		}
		if em.ExportMode() == ExportEnabled && out[i].Excluded && isSoftEmitReason(out[i].FilterReason) {
			out[i].Excluded = false
			out[i].FilterReason = ""
		}
	}
	return out
}

// ApplyChannelEmitPreExport applies export mode before emission policy: disabled
// excludes the channel; enabled soft-clears operator exclusions and marks
// ForceInclude so unhealthy prune can be skipped.
func ApplyChannelEmitPreExport(channels []Channel, emits map[string]ChannelEmit) []Channel {
	if len(emits) == 0 {
		return channels
	}
	out := make([]Channel, len(channels))
	copy(out, channels)
	for i := range out {
		em, ok := emits[out[i].NormalizedID]
		if !ok {
			continue
		}
		switch em.ExportMode() {
		case ExportDisabled:
			out[i].Excluded = true
			out[i].FilterReason = FilterReasonEmitDisabled
			out[i].ForceInclude = false
		case ExportEnabled:
			out[i].ForceInclude = true
			if out[i].Excluded && isSoftEmitReason(out[i].FilterReason) {
				out[i].Excluded = false
				out[i].FilterReason = ""
			}
		}
	}
	return out
}

// PaintChannelEmit attaches the configured emit row (if any) for API/UI.
func PaintChannelEmit(channels []Channel, emits map[string]ChannelEmit) []Channel {
	if len(emits) == 0 {
		return channels
	}
	out := make([]Channel, len(channels))
	copy(out, channels)
	for i := range out {
		em, ok := emits[out[i].NormalizedID]
		if !ok {
			continue
		}
		em = em.Normalized()
		if em.IsZero() {
			continue
		}
		cp := em
		out[i].Emit = &cp
	}
	return out
}

func (c *Channel) emitDefaults() *EmitDefaults {
	if c.EmitDefaults == nil {
		c.EmitDefaults = &EmitDefaults{}
	}
	return c.EmitDefaults
}

func isSoftEmitReason(reason string) bool {
	if reason == "" {
		return false
	}
	if reason == FilterReasonUnhealthy || reason == FilterReasonEmitDisabled {
		return true
	}
	if strings.HasPrefix(reason, FilterReasonDisabledGroupPrefix) {
		return true
	}
	return strings.HasPrefix(reason, "exclusion ")
}

// IsHardEmitBlock reports whether a filter reason cannot be cleared by force-include.
func IsHardEmitBlock(reason string) bool {
	return reason == FilterReasonDRM || reason == FilterReasonNeedsFASTProxy
}
