package lg

import (
	"net/url"
	"regexp"
	"strings"
)

// Bracketed client macros in LG/Xumo SSAI query values (e.g. [IFA], [LMT], [GDPR]).
var clientMacroRE = regexp.MustCompile(`(?i)\[[A-Z0-9_]+\]`)

// normalizeMediaURL prepares mediaStaticUrl for export.
// Without ads.* SSAI keys, the entire query is stripped (legacy LG behavior).
// With any ads.* key, those keys are kept (CloudFront/Xumo origin interpolation)
// and bracketed client macros in values are neutralized; other query keys drop.
func normalizeMediaURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		if i := strings.IndexByte(raw, '?'); i >= 0 {
			return raw[:i]
		}
		return raw
	}
	u.Fragment = ""
	q := u.Query()
	hasAds := false
	for key := range q {
		if strings.HasPrefix(strings.ToLower(key), "ads.") {
			hasAds = true
			break
		}
	}
	if !hasAds {
		u.RawQuery = ""
		return u.String()
	}
	kept := make(url.Values)
	for key, vals := range q {
		if !strings.HasPrefix(strings.ToLower(key), "ads.") {
			continue
		}
		for _, v := range vals {
			kept.Add(key, neutralizeClientMacros(v))
		}
	}
	u.RawQuery = kept.Encode()
	return u.String()
}

func neutralizeClientMacros(v string) string {
	return strings.TrimSpace(clientMacroRE.ReplaceAllString(v, ""))
}
