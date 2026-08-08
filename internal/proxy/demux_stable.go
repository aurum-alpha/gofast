package proxy

import (
	"context"
	"errors"
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
	EventDemuxStableStall = "demux_stable_stall"

	ReasonDemuxStableSlots  = "demux_stable_slots_full"
	ReasonDemuxStableIngest = "demux_stable_ingest"
	ReasonDemuxStableFFmpeg = "demux_stable_ffmpeg"
	ReasonFFmpegExit        = "ffmpeg_exit"

	defaultDemuxStableMax  = 2
	defaultDemuxStableSize = "1280x720"
	maxDemuxSessionRows    = 16

	demuxStderrLogLimit   = 800
	demuxStallCheckEvery  = 5 * time.Second
	demuxStallQuietFor    = 15 * time.Second
	demuxStallLogInterval = 30 * time.Second
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

	ffArgs := h.demuxFFmpegArgs(ingestURL, headers)
	logEvent(slog.LevelInfo, EventDemuxStableOpen,
		"provider", provider, "channel", id, "client_ip", clientIP, "ua", ua,
		"ingest", urlHostPath(ingestURL), "size", h.demux.size,
		"ffmpeg", h.demux.ffmpeg, "argv", redactFFmpegArgv(ffArgs))
	h.emit(Event{
		Kind: EventDemuxStableOpen, Provider: string(provider), ChannelID: id,
		Attrs: map[string]any{
			"ingest": urlHostPath(ingestURL),
			"size":   h.demux.size,
			"ffmpeg": h.demux.ffmpeg,
			"argv":   redactFFmpegArgv(ffArgs),
		},
	})

	ctx := r.Context()
	cmd := h.demuxFFmpegCmd(ctx, ffArgs)
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

	stderrCh := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(io.LimitReader(stderr, 64<<10))
		msg := truncateDemuxLog(strings.TrimSpace(string(buf)), demuxStderrLogLimit)
		if msg != "" {
			logEvent(slog.LevelWarn, "demux_stable_ffmpeg_stderr",
				"provider", provider, "channel", id, "stderr", msg)
		}
		stderrCh <- msg
	}()

	stallStop := make(chan struct{})
	go h.watchDemuxStall(stallStop, slot, provider, id)

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	n, copyErr := io.Copy(&countingWriter{w: w, slot: slot, total: &h.demux.bytes}, stdout)
	close(stallStop)
	_ = cmd.Process.Kill()
	waitErr := cmd.Wait()
	slot.state.Store("ending")

	stderrMsg := ""
	select {
	case stderrMsg = <-stderrCh:
	case <-time.After(500 * time.Millisecond):
	}

	reason := demuxCloseReason(ctx.Err() != nil, copyErr, waitErr)
	exitCode, signal := ffmpegWaitInfo(waitErr)
	elapsed := time.Since(start)
	bps := 0.0
	if elapsed.Seconds() > 0.5 {
		bps = float64(n) / elapsed.Seconds()
	}
	logEvent(slog.LevelInfo, EventDemuxStableClose,
		"provider", provider, "channel", id, "bytes", n,
		"duration_ms", elapsed.Milliseconds(), "bytes_per_sec", int64(bps),
		"reason", reason,
		"wait_err", errString(waitErr), "copy_err", errString(copyErr),
		"exit_code", exitCode, "signal", signal, "stderr", stderrMsg)
	h.emit(Event{
		Kind: EventDemuxStableClose, Provider: string(provider), ChannelID: id,
		Reason: reason, Message: stderrMsg, Bytes: n, DurationMS: elapsed.Milliseconds(),
		Attrs: map[string]any{
			"bytes_per_sec": int64(bps),
			"exit_code":     exitCode,
			"signal":        signal,
			"wait_err":      errString(waitErr),
			"copy_err":      errString(copyErr),
		},
	})
}

// watchDemuxStall logs observe-only warnings when the encode produces no bytes
// for demuxStallQuietFor. It never kills ffmpeg (#60 instrumentation).
func (h *Handler) watchDemuxStall(stop <-chan struct{}, slot *demuxStableSlot, provider model.ProviderID, id string) {
	ticker := time.NewTicker(demuxStallCheckEvery)
	defer ticker.Stop()
	var lastBytes int64
	lastProgress := time.Now()
	var lastWarn time.Time
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			st, _ := slot.state.Load().(string)
			if st != "streaming" {
				continue
			}
			n := slot.bytesOut.Load()
			if n > lastBytes {
				lastBytes = n
				lastProgress = time.Now()
				continue
			}
			quiet := time.Since(lastProgress)
			if quiet < demuxStallQuietFor {
				continue
			}
			if !lastWarn.IsZero() && time.Since(lastWarn) < demuxStallLogInterval {
				continue
			}
			lastWarn = time.Now()
			elapsedMS := time.Since(slot.startedAt).Milliseconds()
			logEvent(slog.LevelWarn, EventDemuxStableStall,
				"provider", provider, "channel", id,
				"bytes", n, "quiet_ms", quiet.Milliseconds(),
				"elapsed_ms", elapsedMS, "pid", slot.pid.Load())
			h.emit(Event{
				Kind: EventDemuxStableStall, Provider: string(provider), ChannelID: id,
				Reason:  EventDemuxStableStall,
				Message: fmt.Sprintf("no demux-stable bytes for %ds", int(quiet.Seconds())),
				Bytes:   n, DurationMS: elapsedMS,
			})
		}
	}
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

func (h *Handler) demuxFFmpegArgs(ingestURL string, headers map[string]string) []string {
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
	return args
}

func (h *Handler) demuxFFmpegCmd(ctx context.Context, args []string) *exec.Cmd {
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

// redactFFmpegArgv joins argv for logs; -headers values are redacted.
func redactFFmpegArgv(args []string) string {
	if len(args) == 0 {
		return ""
	}
	out := make([]string, len(args))
	copy(out, args)
	for i := 0; i < len(out); i++ {
		if out[i] == "-headers" && i+1 < len(out) {
			out[i+1] = "[redacted]"
			i++
		}
	}
	return strings.Join(out, " ")
}

func truncateDemuxLog(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}

// demuxCloseReason classifies why a Class B pipe ended (for soak diagnosis #60).
func demuxCloseReason(clientCancelled bool, copyErr, waitErr error) string {
	if clientCancelled {
		return ReasonClientCancel
	}
	_, sig := ffmpegWaitInfo(waitErr)
	if waitErr != nil {
		if isKillSignal(sig) {
			if copyErr != nil {
				return ReasonFFmpegExit
			}
			// We reaped with Kill after stdout EOF — normal end.
			return ""
		}
		return ReasonFFmpegExit
	}
	if copyErr != nil {
		return ReasonFFmpegExit
	}
	return ""
}

func ffmpegWaitInfo(err error) (exitCode int, signal string) {
	if err == nil {
		return 0, ""
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return -1, ""
	}
	if status, ok := ee.Sys().(syscall.WaitStatus); ok {
		if status.Signaled() {
			return -1, status.Signal().String()
		}
		return status.ExitStatus(), ""
	}
	return ee.ExitCode(), ""
}

func isKillSignal(sig string) bool {
	switch strings.ToLower(sig) {
	case "killed", "sigkill":
		return true
	default:
		return false
	}
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
