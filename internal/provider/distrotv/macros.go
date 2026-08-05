package distrotv

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var macroRE = regexp.MustCompile(`__[^_].*?__`)

// SanitizeURL substitutes Distro ad-targeting macros and drops unknown __…__ values.
func SanitizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for k := range q {
		vals := q[k]
		for i, v := range vals {
			if repl, ok := macroValue(v); ok {
				vals[i] = repl
				continue
			}
			if macroRE.MatchString(v) {
				vals[i] = ""
			}
		}
		q[k] = vals
	}
	u.RawQuery = q.Encode()
	// FastChannels: /d/distro001a/ paths break when ad params are present.
	if strings.HasPrefix(u.Path, "/d/distro001a/") {
		u.RawQuery = ""
	}
	return u.String()
}

func macroValue(v string) (string, bool) {
	switch v {
	case "__CACHE_BUSTER__":
		return strconv.FormatInt(time.Now().UnixMilli(), 10), true
	case "__DEVICE_ID__", "__ADVERTISING_ID__":
		return randomID(), true
	case "__LIMIT_AD_TRACKING__", "__IS_GDPR__", "__IS_CCPA__", "__APP_VERSION__":
		return "0", true
	case "__GEO_COUNTRY__":
		return "US", true
	case "__PAGEURL_ESC__", "__STORE_URL__":
		return "https%3A%2F%2Fdistro.tv%2F", true
	case "__APP_BUNDLE__":
		return "distro.tv", true
	case "__WIDTH__":
		return "1920", true
	case "__HEIGHT__":
		return "1080", true
	case "__DEVICE__":
		return "Linux", true
	case "__DEVICE_ID_TYPE__":
		return "uuid", true
	case "__DEVICE_CATEGORY__":
		return "desktop", true
	case "__env.i__", "__env.u__":
		return "web", true
	case "__LATITUDE__", "__LONGITUDE__", "__GEO_DMA__", "__GEO_TYPE__",
		"__APP_CATEGORY__", "__DEVICE_CONNECTION_TYPE__", "__PALN__",
		"__GDPR_CONSENT__", "__CLIENT_IP__":
		return "", true
	default:
		return "", false
	}
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}
