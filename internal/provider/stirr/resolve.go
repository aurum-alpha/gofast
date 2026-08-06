package stirr

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
)

const resolveCacheTTL = 3 * time.Minute

var (
	bracketMacro = regexp.MustCompile(`\[[^\]]+\]`)
	// ErrDeadSSAI means STIRR still lists the channel but the SSAI/CDN config
	// is gone (Aniview 422 error=CON). Callers should fail the tune-in.
	ErrDeadSSAI = errors.New("stirr: SSAI content unavailable (CON)")
)

// Resolver POSTs STIRR /playable at tune-in and returns a master HLS URL.
type Resolver struct {
	client      *httpx.Client
	playableTpl string
	ua          string
	origin      string
	referer     string

	mu    sync.Mutex
	cache map[string]resolveCacheEntry
}

type resolveCacheEntry struct {
	at  time.Time
	url string
}

// NewResolver builds a tune-in resolver. playableTpl empty → DefaultPlayableURLTemplate.
func NewResolver(client *httpx.Client, playableTpl, userAgent string) *Resolver {
	if client == nil {
		client = httpx.NewClient(0, 0)
	}
	if strings.TrimSpace(playableTpl) == "" {
		playableTpl = DefaultPlayableURLTemplate
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = BrowserUA
	}
	return &Resolver{
		client:      client,
		playableTpl: playableTpl,
		ua:          userAgent,
		origin:      "https://stirr.com",
		referer:     "https://stirr.com/",
		cache:       map[string]resolveCacheEntry{},
	}
}

// Resolve returns a playable HLS master URL for an opaque StreamURL or videoid.
// Fills [vx_nonce] (and blanks other [macros]); returns the master URL only —
// do not bake short-lived SSAI session variants into the catalog.
// Aniview masters that return 422 error=CON are rejected (ErrDeadSSAI).
func (r *Resolver) Resolve(ctx context.Context, opaqueOrID string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("stirr: nil resolver")
	}
	id := strings.TrimSpace(opaqueOrID)
	if parsed, ok := ParseOpaque(id); ok {
		id = parsed
	}
	if id == "" {
		return "", fmt.Errorf("stirr: empty channel id")
	}

	r.mu.Lock()
	if ent, ok := r.cache[id]; ok && time.Since(ent.at) < resolveCacheTTL && ent.url != "" {
		u := ent.url
		r.mu.Unlock()
		return u, nil
	}
	r.mu.Unlock()

	playURL, err := r.fetchPlayable(ctx, id)
	if err != nil {
		return "", err
	}
	playURL = FillMacros(playURL)
	if err := r.probeMaster(ctx, playURL); err != nil {
		return "", err
	}

	r.mu.Lock()
	r.cache[id] = resolveCacheEntry{at: time.Now(), url: playURL}
	r.mu.Unlock()
	return playURL, nil
}

func (r *Resolver) fetchPlayable(ctx context.Context, videoID string) (string, error) {
	rawURL := fmt.Sprintf(r.playableTpl, videoID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", r.ua)
	req.Header.Set("Origin", r.origin)
	req.Header.Set("Referer", r.referer)
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := r.client.Do(ctx, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("stirr playable HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRawSize+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxRawSize {
		return "", fmt.Errorf("stirr playable too large")
	}
	media, err := ExtractMediaURL(body)
	if err != nil {
		return "", err
	}
	return media, nil
}

// probeMaster soft-checks the resolved master for deleted Aniview configs
// (HTTP 422 + error=CON). Network/5xx errors are ignored so transient CDN
// blips do not fail tune-in.
func (r *Resolver) probeMaster(ctx context.Context, playURL string) error {
	if urlHost(playURL) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, playURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", r.ua)
	req.Header.Set("Origin", r.origin)
	req.Header.Set("Referer", r.referer)
	req.Header.Set("Accept", "*/*")

	resp, err := r.client.Do(ctx, req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode == http.StatusUnprocessableEntity && isAniviewCON(body) {
		return ErrDeadSSAI
	}
	return nil
}

func isAniviewCON(body []byte) bool {
	s := string(body)
	if strings.Contains(s, `"error":"CON"`) || strings.Contains(s, `"error": "CON"`) {
		return true
	}
	return strings.Contains(s, "can not get a content")
}

func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// ExtractMediaURL pulls data[0].media[0] from a playable JSON payload.
func ExtractMediaURL(body []byte) (string, error) {
	var payload struct {
		Data []struct {
			Media []json.RawMessage `json:"media"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("stirr playable json: %w", err)
	}
	if len(payload.Data) == 0 || len(payload.Data[0].Media) == 0 {
		return "", fmt.Errorf("stirr playable: no media")
	}
	raw := payload.Data[0].Media[0]
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "http") {
			return s, nil
		}
	}
	return "", fmt.Errorf("stirr playable: media is not an http URL")
}

// FillMacros replaces [vx_nonce] with a random hex token and blanks other [macros].
func FillMacros(rawURL string) string {
	if !strings.Contains(rawURL, "[") {
		return rawURL
	}
	nonce := randomHex(16)
	out := strings.ReplaceAll(rawURL, "[vx_nonce]", nonce)
	out = bracketMacro.ReplaceAllStringFunc(out, func(m string) string {
		if m == "[vx_nonce]" {
			return nonce
		}
		return ""
	})
	return out
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}
