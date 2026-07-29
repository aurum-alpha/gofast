package model

import (
	"fmt"
	"regexp"
	"strings"
)

// FilterReason is why a channel is out of M3U/XMLTV emission.
type FilterReason string

const (
	FilterReasonDRM                       FilterReason = "DRM"
	FilterReasonNeedsFASTProxy            FilterReason = "needs FASTProxy (proxy_base_url not configured)"
	FilterReasonUnhealthy                 FilterReason = "unhealthy (exclude_unhealthy)"
	FilterReasonEmitDisabled              FilterReason = "emit disabled"
	FilterReasonDuplicate                 FilterReason = "duplicate"
	FilterReasonMissingIdentity           FilterReason = "missing identity"
	FilterReasonMissingStream             FilterReason = "missing stream"
	FilterReasonUnsupportedClassification FilterReason = "unsupported classification"

	// FilterReasonDisabledGroupPrefix begins the reason for channels dropped
	// because their group was disabled in the taxonomy.
	FilterReasonDisabledGroupPrefix = "disabled group"
)

// FilterReasonKind is a stable UI / status-filter code for a FilterReason.
type FilterReasonKind string

const (
	FilterKindDRM           FilterReasonKind = "drm"
	FilterKindNeedsProxy    FilterReasonKind = "needs-proxy"
	FilterKindUnhealthy     FilterReasonKind = "unhealthy"
	FilterKindEmitDisabled  FilterReasonKind = "emit-disabled"
	FilterKindDuplicate     FilterReasonKind = "duplicate"
	FilterKindMissingID     FilterReasonKind = "missing-identity"
	FilterKindMissingStream FilterReasonKind = "missing-stream"
	FilterKindUnsupported   FilterReasonKind = "unsupported"
	FilterKindDisabledGroup FilterReasonKind = "disabled-group"
	FilterKindExclusion     FilterReasonKind = "exclusion"
	FilterKindOther         FilterReasonKind = "excluded"
)

// String returns the wire form.
func (r FilterReason) String() string { return string(r) }

// IsSoft reports whether force-include / export:enabled may clear this reason.
func (r FilterReason) IsSoft() bool {
	if r == "" {
		return false
	}
	if r == FilterReasonUnhealthy || r == FilterReasonEmitDisabled || r == FilterReasonDuplicate {
		return true
	}
	if strings.HasPrefix(string(r), FilterReasonDisabledGroupPrefix) {
		return true
	}
	return strings.HasPrefix(string(r), "exclusion ")
}

// IsHard reports whether this reason cannot be cleared by force-include.
func (r FilterReason) IsHard() bool {
	switch r {
	case FilterReasonDRM,
		FilterReasonNeedsFASTProxy,
		FilterReasonMissingIdentity,
		FilterReasonMissingStream,
		FilterReasonUnsupportedClassification:
		return true
	default:
		return false
	}
}

// Kind returns the stable UI/filter code for r.
func (r FilterReason) Kind() FilterReasonKind {
	switch {
	case r == FilterReasonDRM:
		return FilterKindDRM
	case r == FilterReasonNeedsFASTProxy:
		return FilterKindNeedsProxy
	case r == FilterReasonUnhealthy:
		return FilterKindUnhealthy
	case r == FilterReasonEmitDisabled:
		return FilterKindEmitDisabled
	case r == FilterReasonDuplicate:
		return FilterKindDuplicate
	case r == FilterReasonMissingIdentity:
		return FilterKindMissingID
	case r == FilterReasonMissingStream:
		return FilterKindMissingStream
	case r == FilterReasonUnsupportedClassification:
		return FilterKindUnsupported
	case strings.HasPrefix(string(r), FilterReasonDisabledGroupPrefix):
		return FilterKindDisabledGroup
	case strings.HasPrefix(string(r), "exclusion "):
		return FilterKindExclusion
	case r == "":
		return ""
	default:
		return FilterKindOther
	}
}

// DisabledGroupReason is the FilterReason for a channel dropped by a disabled group.
func DisabledGroupReason(name string) FilterReason {
	name = strings.TrimSpace(name)
	if name == "" {
		return FilterReason(FilterReasonDisabledGroupPrefix)
	}
	return FilterReason(fmt.Sprintf("%s %q", FilterReasonDisabledGroupPrefix, name))
}

// ExclusionMatched is the FilterReason when a provider exclusion regex matched.
func ExclusionMatched(re *regexp.Regexp) FilterReason {
	if re == nil {
		return FilterReason("exclusion matched")
	}
	return FilterReason(fmt.Sprintf("exclusion %q matched", re.String()))
}

// primaryFilterReasonOrder is hard-first then soft preference for FilterReason.
var primaryFilterReasonOrder = []FilterReason{
	FilterReasonDRM,
	FilterReasonUnsupportedClassification,
	FilterReasonNeedsFASTProxy,
	FilterReasonMissingIdentity,
	FilterReasonMissingStream,
	FilterReasonDuplicate,
	FilterReasonEmitDisabled,
	FilterReasonUnhealthy,
}

// PrimaryFilterReason picks the badge-driving reason from a set.
func PrimaryFilterReason(reasons []FilterReason) FilterReason {
	if len(reasons) == 0 {
		return ""
	}
	for _, want := range primaryFilterReasonOrder {
		for _, r := range reasons {
			if r == want {
				return r
			}
		}
	}
	for _, r := range reasons {
		if strings.HasPrefix(string(r), FilterReasonDisabledGroupPrefix) {
			return r
		}
	}
	for _, r := range reasons {
		if strings.HasPrefix(string(r), "exclusion ") {
			return r
		}
	}
	return reasons[0]
}

// HasFilterReason reports whether reasons contains r exactly.
func HasFilterReason(reasons []FilterReason, r FilterReason) bool {
	for _, x := range reasons {
		if x == r {
			return true
		}
	}
	return false
}

// HasFilterReasonKind reports whether any reason maps to kind.
func HasFilterReasonKind(reasons []FilterReason, kind FilterReasonKind) bool {
	for _, r := range reasons {
		if r.Kind() == kind {
			return true
		}
	}
	return false
}

// AddFilterReason appends r if non-empty and not already present, then syncs
// Excluded and FilterReason primary from FilterReasons.
func (c *Channel) AddFilterReason(r FilterReason) {
	if c == nil || r == "" {
		return
	}
	c.ensureReasonsSlice()
	if !HasFilterReason(c.FilterReasons, r) {
		c.FilterReasons = append(c.FilterReasons, r)
	}
	c.syncExclusionFromReasons()
}

// ClearSoftFilterReasons removes soft reasons; hard reasons remain.
func (c *Channel) ClearSoftFilterReasons() {
	if c == nil {
		return
	}
	c.ensureReasonsSlice()
	kept := c.FilterReasons[:0]
	for _, r := range c.FilterReasons {
		if r.IsHard() {
			kept = append(kept, r)
		}
	}
	c.FilterReasons = kept
	c.syncExclusionFromReasons()
}

func (c *Channel) ensureReasonsSlice() {
	if len(c.FilterReasons) == 0 && c.FilterReason != "" {
		c.FilterReasons = []FilterReason{c.FilterReason}
	}
}

func (c *Channel) syncExclusionFromReasons() {
	if c == nil {
		return
	}
	c.FilterReason = PrimaryFilterReason(c.FilterReasons)
	c.Excluded = len(c.FilterReasons) > 0
}

// EffectiveFilterReasons returns FilterReasons, or a single-element slice from
// legacy FilterReason when the slice is empty (compat for partially migrated data).
func (c Channel) EffectiveFilterReasons() []FilterReason {
	if len(c.FilterReasons) > 0 {
		return c.FilterReasons
	}
	if c.FilterReason != "" {
		return []FilterReason{c.FilterReason}
	}
	return nil
}

// IsHardEmitBlock reports whether a filter reason cannot be cleared by force-include.
func IsHardEmitBlock(reason FilterReason) bool {
	return reason.IsHard()
}
