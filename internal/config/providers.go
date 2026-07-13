package config

import (
	"fmt"
	"regexp"

	"github.com/j27-aurum/gofast/internal/model"
)

// compileProviders applies per-field defaults and compiles exclusion regexes.
// There are no built-in provider identities — the map comes only from YAML.
func compileProviders(providers map[string]model.Provider) (map[string]model.Provider, error) {
	if len(providers) == 0 {
		return providers, nil
	}
	out := make(map[string]model.Provider, len(providers))
	for id, p := range providers {
		p = model.MergeProviderDefaults(p)
		compiled := make([]*regexp.Regexp, 0, len(p.Exclusions))
		for i, pat := range p.Exclusions {
			re, err := regexp.Compile("(?i)" + pat)
			if err != nil {
				return nil, fmt.Errorf("providers.%s.exclusions[%d]: invalid regex %q: %w", id, i, pat, err)
			}
			compiled = append(compiled, re)
		}
		p.ExclusionRegexes = compiled
		out[id] = p
	}
	return out, nil
}
