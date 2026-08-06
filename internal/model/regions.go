package model

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultRegions is the system-wide regions default when unset (ISO 3166-1
// alpha-2 uppercase). Distro fantasy geo QQ is also accepted when listed.
const DefaultRegions = "US"

// NormalizeRegionCode returns the canonical region token: trimmed uppercase
// ISO 3166-1 alpha-2 style (US, CA, GB, …). Non-ISO tokens like Distro's QQ
// are uppercased the same way so filters never show "us" and "US" as distinct.
func NormalizeRegionCode(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// ParseRegionList splits a comma-separated regions string into ordered unique
// tokens (trim, uppercase, drop empties, case-insensitive dedupe).
func ParseRegionList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		tok := NormalizeRegionCode(part)
		if tok == "" {
			continue
		}
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

// JoinRegionList joins region tokens with commas (no extra spaces).
func JoinRegionList(regions []string) string {
	if len(regions) == 0 {
		return ""
	}
	return strings.Join(regions, ",")
}

// NormalizeRegionsCSV parses s and returns a canonical comma-joined list.
// Empty input becomes DefaultRegions.
func NormalizeRegionsCSV(s string) string {
	list := ParseRegionList(s)
	if len(list) == 0 {
		return DefaultRegions
	}
	return JoinRegionList(list)
}

// UnmarshalRegionsYAML decodes a YAML scalar or sequence into a regions CSV string.
func UnmarshalRegionsYAML(value *yaml.Node) (string, error) {
	if value == nil || value.Kind == 0 {
		return "", nil
	}
	switch value.Kind {
	case yaml.ScalarNode, yaml.AliasNode:
		var s string
		if err := value.Decode(&s); err != nil {
			return "", err
		}
		return JoinRegionList(ParseRegionList(s)), nil
	case yaml.SequenceNode:
		var parts []string
		if err := value.Decode(&parts); err != nil {
			return "", err
		}
		return JoinRegionList(ParseRegionList(strings.Join(parts, ","))), nil
	default:
		return "", fmt.Errorf("regions: expected string or sequence")
	}
}
