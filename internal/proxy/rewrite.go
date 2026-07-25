package proxy

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/j27-aurum/gofast/internal/model"
)

var uriAttrRE = regexp.MustCompile(`(?i)\bURI="([^"]*)"`)

// RewriteResult is a rewritten playlist plus discovered upstream media/variant URIs.
type RewriteResult struct {
	Body        string
	VariantURLs []string // absolute upstream variant URIs (master only)
	MediaURLs   []string // absolute upstream media URIs (media playlist)
	IsMaster    bool
	URIRewrites int
}

// RewritePlaylist rewrites playable URIs to proxy targets while preserving other tags.
//
// publicBase is the absolute proxy origin (no trailing slash). For masters,
// variants become {publicBase}/s/{sid}/{n}.m3u8. For media playlists, media and
// KEY/MAP URIs become {publicBase}/seg/{token}.
//
// Why .ts on tokens: ffmpeg's allowed_segment_extensions rejects extensionless
// beacon URLs — the whole reason this proxy exists. Token = {hex}.ts satisfies
// that check while the path stays /seg/{token}.
func RewritePlaylist(body, playlistURL, publicBase, sid string, store *Store, headers map[string]string, provider model.ProviderID, channelID string) (RewriteResult, error) {
	base, err := url.Parse(playlistURL)
	if err != nil {
		return RewriteResult{}, err
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	isMaster := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#EXT-X-STREAM-INF") {
			isMaster = true
			break
		}
	}

	out := make([]string, 0, len(lines))
	var variants, media []string
	rewrites := 0
	expectVariant := false
	expectMedia := false
	basePublic := strings.TrimRight(publicBase, "/")

	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "#EXT-X-STREAM-INF") {
			expectVariant = true
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trim, "#EXTINF") {
			expectMedia = true
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trim, "#EXT-X-KEY") || strings.HasPrefix(trim, "#EXT-X-MAP") {
			rewritten, n, abs := rewriteURIAttr(line, base, basePublic, store, headers, provider, channelID, ".key")
			out = append(out, rewritten)
			rewrites += n
			if abs != "" {
				media = append(media, abs)
			}
			continue
		}
		if trim == "" || strings.HasPrefix(trim, "#") {
			out = append(out, line)
			expectVariant = false
			expectMedia = false
			continue
		}

		abs, err := resolveURI(base, trim)
		if err != nil {
			out = append(out, line)
			expectVariant = false
			expectMedia = false
			continue
		}
		if expectVariant {
			idx := len(variants)
			variants = append(variants, abs)
			out = append(out, basePublic+"/s/"+sid+"/"+strconv.Itoa(idx)+".m3u8")
			rewrites++
			expectVariant = false
			continue
		}
		if expectMedia || !isMaster {
			token := store.MintSeg(abs, ".ts", headers, provider, channelID)
			out = append(out, basePublic+"/seg/"+token)
			media = append(media, abs)
			rewrites++
			expectMedia = false
			continue
		}
		out = append(out, line)
	}

	return RewriteResult{
		Body:        strings.Join(out, "\n"),
		VariantURLs: variants,
		MediaURLs:   media,
		IsMaster:    isMaster,
		URIRewrites: rewrites,
	}, nil
}

func rewriteURIAttr(line string, base *url.URL, publicBase string, store *Store, headers map[string]string, provider model.ProviderID, channelID, ext string) (string, int, string) {
	m := uriAttrRE.FindStringSubmatchIndex(line)
	if m == nil {
		return line, 0, ""
	}
	raw := line[m[2]:m[3]]
	abs, err := resolveURI(base, raw)
	if err != nil {
		return line, 0, ""
	}
	token := store.MintSeg(abs, ext, headers, provider, channelID)
	newURI := publicBase + "/seg/" + token
	return line[:m[2]] + newURI + line[m[3]:], 1, abs
}

func resolveURI(base *url.URL, ref string) (string, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(u).String(), nil
}
