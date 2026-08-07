package proxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/j27-aurum/gofast/internal/classifier"
	"github.com/j27-aurum/gofast/internal/clientaccess"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/distrotv"
)

const (
	EventDemuxStableOpen  = "demux_stable_open"
	EventDemuxStableClose = "demux_stable_close"
	EventDemuxStableFail  = "demux_stable_fail"

	ReasonDemuxStableSlots  = "demux_stable_slots_full"
	ReasonDemuxStableIngest = "demux_stable_ingest"
	ReasonDemuxStableFFmpeg = "demux_stable_ffmpeg"

	defaultDemuxStableMax  = 2
	defaultDemuxStableSize = "1280x720"
	maxDemuxSessionRows    = 16
)

// DemuxStableSession is one active Class B ffmpeg pipe (for snapshots / UI).
type DemuxStableSession struct {
	Provider    string    `json:"provider"`
	ChannelID   string    `json:"channel_id"`
	StartedAt   time.Time `json:"started_at"`
	BytesOut    int64     `json:"bytes_out"`
	BytesPerSec float64   `json:"bytes_per_sec"`
	PID         int       `json:"pid,omitempty"`
	State       string    `json:"state"`
}

type demuxStableSlot struct {
	provider  model.ProviderID
	channelID string
	startedAt time.Time
	bytesOut  atomic.Int64
	pid       atomic.Int32
	state     atomic.Value // string
}

// demuxStableTracker limits concurrent Class B encodes and feeds snapshots.
type demuxStableTracker struct {
	mu     sync.Mutex
	max    int
	active map[uint64]*demuxStableSlot
	nextID uint64
	starts atomic.Uint64
	fails  atomic.Uint64
	bytes  atomic.Uint64
	ffmpeg string
	size   string
}

func newDemuxStableTracker() *demuxStableTracker {
	max := defaultDemuxStableMax
	if v := strings.TrimSpace(os.Getenv("FASTPROXY_DEMUX_STABLE_MAX")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			max = n
		}
	}
	ffmpeg := strings.TrimSpace(os.Getenv("FASTPROXY_FFMPEG"))
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	size := strings.TrimSpace(os.Getenv("FASTPROXY_DEMUX_STABLE_SIZE"))
	if size == "" {
		size = defaultDemuxStableSize
	}
	return &demuxStableTracker{
		max:    max,
		active: make(map[uint64]*demuxStableSlot),
		ffmpeg: ffmpeg,
		size:   size,
	}
}

func (t *demuxStableTracker) acquire(provider model.ProviderID, channelID string) (id uint64, slot *demuxStableSlot, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.max >= 0 && len(t.active) >= t.max {
		return 0, nil, false
	}
	t.nextID++
	id = t.nextID
	slot = &demuxStableSlot{
		provider:  provider,
		channelID: channelID,
		startedAt: time.Now().UTC(),
	}
	slot.state.Store("starting")
	t.active[id] = slot
	t.starts.Add(1)
	return id, slot, true
}

func (t *demuxStableTracker) release(id uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.active, id)
}

func (t *demuxStableTracker) snapshotRows() (active, max int, rateBps float64, sessions []DemuxStableSession) {
	t.mu.Lock()
	defer t.mu.Unlock()
	max = t.max
	active = len(t.active)
	now := time.Now().UTC()
	sessions = make([]DemuxStableSession, 0, min(len(t.active), maxDemuxSessionRows))
	for _, s := range t.active {
		bytes := s.bytesOut.Load()
		bps := 0.0
		elapsed := now.Sub(s.startedAt).Seconds()
		if elapsed > 0.5 {
			bps = float64(bytes) / elapsed
		}
		rateBps += bps
		if len(sessions) >= maxDemuxSessionRows {
			continue
		}
		st, _ := s.state.Load().(string)
		sessions = append(sessions, DemuxStableSession{
			Provider:    string(s.provider),
			ChannelID:   s.channelID,
			StartedAt:   s.startedAt,
			BytesOut:    bytes,
			BytesPerSec: bps,
			PID:         int(s.pid.Load()),
			State:       st,
		})
	}
	return active, max, rateBps, sessions
}

// serveDemuxStable handles GET /stable/{provider}/{id}: Class B MPEG-TS pipe.
func (h *Handler) serveDemuxStable(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	provider := model.ProviderID(r.PathValue("provider"))
	id := strings.TrimSuffix(r.PathValue("id"), ".ts")
	clientIP := clientaccess.ClientIP(r)
	ua := truncateUA(r.UserAgent())

	origin, err := h.Origin.Lookup(r.Context(), provider, id)
	if err != nil {
		logEvent(slog.LevelWarn, EventOriginMiss,
			"provider", provider, "channel", id, "client_ip", clientIP, "err", err.Error())
		h.emit(Event{Kind: EventOriginMiss, Provider: string(provider), ChannelID: id, Reason: ReasonOriginMiss, Message: err.Error()})
		http.NotFound(w, r)
		return
	}

	if h.demux == nil {
		h.demux = newDemuxStableTracker()
	}

	ingestURL, headers, err := h.resolveDemuxIngest(r.Context(), provider, id, origin)
	if err != nil {
		logEvent(slog.LevelWarn, EventDemuxStableFail,
			"provider", provider, "channel", id, "reason", ReasonDemuxStableIngest, "err", err.Error())
		h.emit(Event{
			Kind: EventDemuxStableFail, Provider: string(provider), ChannelID: id,
			Reason: ReasonDemuxStableIngest, Message: err.Error(),
			Status: http.StatusBadGateway, DurationMS: time.Since(start).Milliseconds(),
		})
		h.demux.fails.Add(1)
		http.Error(w, "demux-stable ingest failed", http.StatusBadGateway)
		return
	}

	slotID, slot, ok := h.demux.acquire(provider, id)
	if !ok {
		logEvent(slog.LevelWarn, EventDemuxStableFail,
			"provider", provider, "channel", id, "reason", ReasonDemuxStableSlots)
		h.emit(Event{
			Kind: EventDemuxStableFail, Provider: string(provider), ChannelID: id,
			Reason: ReasonDemuxStableSlots, Message: "encode slots full",
			Status: http.StatusServiceUnavailable, DurationMS: time.Since(start).Milliseconds(),
		})
		h.demux.fails.Add(1)
		http.Error(w, "demux-stable slots full", http.StatusServiceUnavailable)
		return
	}
	defer h.demux.release(slotID)

	logEvent(slog.LevelInfo, EventDemuxStableOpen,
		"provider", provider, "channel", id, "client_ip", clientIP, "ua", ua,
		"ingest", urlHostPath(ingestURL), "size", h.demux.size)
	h.emit(Event{
		Kind: EventDemuxStableOpen, Provider: string(provider), ChannelID: id,
		Attrs: map[string]any{"ingest": urlHostPath(ingestURL), "size": h.demux.size},
	})

	ctx := r.Context()
	cmd := h.demuxFFmpegCmd(ctx, ingestURL, headers)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		h.failDemuxStart(w, provider, id, start, err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		h.failDemuxStart(w, provider, id, start, err)
		return
	}
	if err := cmd.Start(); err != nil {
		h.failDemuxStart(w, provider, id, start, err)
		return
	}
	if cmd.Process != nil {
		slot.pid.Store(int32(cmd.Process.Pid))
	}
	slot.state.Store("streaming")

	go func() {
		buf, _ := io.ReadAll(io.LimitReader(stderr, 64<<10))
		if len(buf) > 0 {
			msg := strings.TrimSpace(string(buf))
			if len(msg) > 400 {
				msg = msg[:400] + "…"
			}
			logEvent(slog.LevelDebug, "demux_stable_ffmpeg_stderr",
				"provider", provider, "channel", id, "stderr", msg)
		}
	}()

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	n, copyErr := io.Copy(&countingWriter{w: w, slot: slot, total: &h.demux.bytes}, stdout)
	_ = cmd.Process.Kill()
	waitErr := cmd.Wait()
	slot.state.Store("ending")

	reason := ""
	if copyErr != nil && ctx.Err() == nil {
		reason = ReasonDemuxStableFFmpeg
	}
	if ctx.Err() != nil {
		reason = ReasonClientCancel
	}
	logEvent(slog.LevelInfo, EventDemuxStableClose,
		"provider", provider, "channel", id, "bytes", n,
		"duration_ms", time.Since(start).Milliseconds(), "reason", reason,
		"wait_err", errString(waitErr), "copy_err", errString(copyErr))
	h.emit(Event{
		Kind: EventDemuxStableClose, Provider: string(provider), ChannelID: id,
		Reason: reason, Bytes: n, DurationMS: time.Since(start).Milliseconds(),
	})
}

func (h *Handler) failDemuxStart(w http.ResponseWriter, provider model.ProviderID, id string, start time.Time, err error) {
	if h.demux != nil {
		h.demux.fails.Add(1)
	}
	logEvent(slog.LevelWarn, EventDemuxStableFail,
		"provider", provider, "channel", id, "reason", ReasonDemuxStableFFmpeg, "err", err.Error())
	h.emit(Event{
		Kind: EventDemuxStableFail, Provider: string(provider), ChannelID: id,
		Reason: ReasonDemuxStableFFmpeg, Message: err.Error(),
		Status: http.StatusBadGateway, DurationMS: time.Since(start).Milliseconds(),
	})
	http.Error(w, "demux-stable ffmpeg failed", http.StatusBadGateway)
}

func (h *Handler) demuxFFmpegCmd(ctx context.Context, ingestURL string, headers map[string]string) *exec.Cmd {
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-fflags", "+genpts+discardcorrupt",
		"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "2",
	}
	if hdr := ffmpegHeadersArg(headers); hdr != "" {
		args = append(args, "-headers", hdr)
	}
	geom := strings.ReplaceAll(h.demux.size, "x", ":")
	args = append(args,
		"-i", ingestURL,
		"-map", "0:v:0?", "-map", "0:a:0?",
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency",
		"-vf", "scale="+geom+":force_original_aspect_ratio=decrease,pad="+geom+":(ow-iw)/2:(oh-ih)/2",
		"-c:a", "aac", "-ac", "2", "-ar", "48000", "-b:a", "128k",
		"-f", "mpegts", "pipe:1",
	)
	cmd := exec.CommandContext(ctx, h.demux.ffmpeg, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func ffmpegHeadersArg(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	var b strings.Builder
	for k, v := range headers {
		if k == "" || v == "" {
			continue
		}
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	return b.String()
}

// resolveDemuxIngest returns a URL ffmpeg can open (Class A / resolve / mint as needed).
func (h *Handler) resolveDemuxIngest(ctx context.Context, provider model.ProviderID, id string, origin ChannelOrigin) (string, map[string]string, error) {
	headers := origin.RequestHeaders
	kind := origin.Classification.Canonical().ProxyKind()
	switch kind {
	case model.ProxySessionMint:
		u, err := h.mintPlayableURL(ctx, provider, id, origin)
		return u, headers, err
	case model.ProxyDistroResolve:
		if h.Distro == nil {
			return "", nil, fmt.Errorf("distro resolver not configured")
		}
		token := origin.StreamURL
		if token == "" {
			token = id
		}
		u, err := h.Distro.Resolve(ctx, token)
		if err != nil {
			return "", nil, err
		}
		if needsClassAIngest(u) {
			return h.loopbackStreamURL(provider, id), nil, nil
		}
		return u, headers, nil
	case model.ProxyStirrResolve:
		if h.Stirr == nil {
			return "", nil, fmt.Errorf("stirr resolver not configured")
		}
		token := origin.StreamURL
		if token == "" {
			token = id
		}
		u, err := h.Stirr.Resolve(ctx, token)
		if err != nil {
			return "", nil, err
		}
		if needsClassAIngest(u) {
			return h.loopbackStreamURL(provider, id), nil, nil
		}
		return u, headers, nil
	case model.ProxyAmagiRewrite:
		return h.loopbackStreamURL(provider, id), nil, nil
	default:
		if strings.TrimSpace(origin.StreamURL) == "" {
			return "", nil, fmt.Errorf("empty stream_url")
		}
		return origin.StreamURL, headers, nil
	}
}

func needsClassAIngest(playURL string) bool {
	if class, ok := classifier.FromURL(playURL); ok && class.ProxyKind() == model.ProxyAmagiRewrite {
		return true
	}
	return distrotv.NeedsPlaylistProxy(playURL)
}

func (h *Handler) loopbackStreamURL(provider model.ProviderID, id string) string {
	base := strings.TrimRight(strings.TrimSpace(h.LoopbackBase), "/")
	if base == "" {
		port := strings.TrimSpace(os.Getenv("PORT"))
		if port == "" {
			port = "8181"
		}
		port = strings.TrimPrefix(port, ":")
		base = "http://127.0.0.1:" + port
	}
	return fmt.Sprintf("%s/stream/%s/%s.m3u8", base, provider, id)
}

func (h *Handler) mintPlayableURL(ctx context.Context, provider model.ProviderID, id string, origin ChannelOrigin) (string, error) {
	if manifest, ok := h.Store.GetMintedManifest(provider, id); ok {
		return manifest, nil
	}
	eventID, err := daiEventID(origin.StreamURL)
	if err != nil {
		return "", err
	}
	manifest, _, err := h.mint.mint(ctx, eventID, origin.RequestHeaders)
	if err != nil {
		return "", err
	}
	h.Store.PutMintedManifest(provider, id, manifest)
	return manifest, nil
}

type countingWriter struct {
	w     io.Writer
	slot  *demuxStableSlot
	total *atomic.Uint64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 {
		c.slot.bytesOut.Add(int64(n))
		if c.total != nil {
			c.total.Add(uint64(n))
		}
	}
	return n, err
}
