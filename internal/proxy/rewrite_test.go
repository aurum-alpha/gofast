package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestRewriteBeaconMedia(t *testing.T) {
	t.Parallel()
	body := readTestdata(t, "beacon_media.m3u8")
	store := NewStore()
	got, err := RewritePlaylist(body, "https://cdn.example/live/index.m3u8", "http://proxy.test", "sid1", store, nil, model.ProviderLG, "news")
	if err != nil {
		t.Fatal(err)
	}
	if got.IsMaster {
		t.Fatal("expected media playlist")
	}
	lines := nonCommentURILines(got.Body)
	if len(lines) != 3 {
		t.Fatalf("media lines = %v", lines)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "http://proxy.test/seg/") || !strings.HasSuffix(line, ".ts") {
			t.Fatalf("line not proxied: %s", line)
		}
	}
	if !strings.Contains(got.Body, "#EXT-X-TARGETDURATION:6") {
		t.Fatalf("tags not preserved: %s", got.Body)
	}
}

func TestRewriteRelative(t *testing.T) {
	t.Parallel()
	body := readTestdata(t, "relative_media.m3u8")
	store := NewStore()
	got, err := RewritePlaylist(body, "https://cdn.example/a/b/index.m3u8", "http://proxy.test", "sid1", store, nil, model.ProviderLG, "news")
	if err != nil {
		t.Fatal(err)
	}
	if got.URIRewrites != 2 {
		t.Fatalf("rewrites=%d", got.URIRewrites)
	}
	// Tokens were minted against resolved absolute URLs.
	tok1, ok := store.GetSeg(strings.TrimPrefix(nonCommentURILines(got.Body)[0], "http://proxy.test/seg/"))
	if !ok || !strings.Contains(tok1.UpstreamURL, "/a/b/seg1.ts") {
		t.Fatalf("seg1 resolve: %+v", tok1)
	}
}

func TestRewriteKey(t *testing.T) {
	t.Parallel()
	body := readTestdata(t, "with_key.m3u8")
	store := NewStore()
	got, err := RewritePlaylist(body, "https://cdn.example/live.m3u8", "http://proxy.test", "sid1", store, nil, model.ProviderLG, "news")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Body, `URI="http://proxy.test/seg/`) || !strings.Contains(got.Body, `.key"`) {
		t.Fatalf("KEY not rewritten: %s", got.Body)
	}
}

func TestRewriteMaster(t *testing.T) {
	t.Parallel()
	body := readTestdata(t, "master.m3u8")
	store := NewStore()
	got, err := RewritePlaylist(body, "https://cdn.example/live/master.m3u8", "http://proxy.test", "abc", store, nil, model.ProviderLG, "news")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsMaster || len(got.VariantURLs) != 2 {
		t.Fatalf("master result: %+v", got)
	}
	if !strings.Contains(got.Body, "http://proxy.test/s/abc/0.m3u8") {
		t.Fatalf("variant rewrite missing: %s", got.Body)
	}
	if !strings.HasSuffix(got.VariantURLs[1], "/live/media-low.m3u8") {
		t.Fatalf("relative variant resolve: %s", got.VariantURLs[1])
	}
}

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func nonCommentURILines(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		out = append(out, trim)
	}
	return out
}
