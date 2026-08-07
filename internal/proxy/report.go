package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	eventSchemaVersion = 1
	reportBufferSize   = 512
	busySnapshotEvery  = 8 * time.Second
	idleHeartbeatEvery = 30 * time.Second
)

// Event is one telemetry row pushed to gen (same vocabulary as slog).
type Event struct {
	Kind       string         `json:"kind"`
	At         time.Time      `json:"at"`
	Provider   string         `json:"provider,omitempty"`
	ChannelID  string         `json:"channel_id,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	Message    string         `json:"message,omitempty"`
	Status     int            `json:"status,omitempty"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Bytes      int64          `json:"bytes,omitempty"`
	Attrs      map[string]any `json:"attrs,omitempty"`
}

// Snapshot is a periodic live view of proxy process state.
type Snapshot struct {
	At                    time.Time            `json:"at"`
	ActiveSessions        int                  `json:"active_sessions"`
	ActiveSegTokens       int                  `json:"active_seg_tokens"`
	StreamOpens           uint64               `json:"stream_opens"`
	Stream302s            uint64               `json:"stream_302s"`
	PlaylistOK            uint64               `json:"playlist_ok"`
	PlaylistFail          uint64               `json:"playlist_fail"`
	SegOK                 uint64               `json:"seg_ok"`
	SegFail               uint64               `json:"seg_fail"`
	SegBytes              uint64               `json:"seg_bytes"`
	EventsDropped         uint64               `json:"events_dropped"`
	DemuxStableActive     int                  `json:"demux_stable_active,omitempty"`
	DemuxStableMax        int                  `json:"demux_stable_max,omitempty"`
	DemuxStableBytesTotal uint64               `json:"demux_stable_bytes_total,omitempty"`
	DemuxStableStarts     uint64               `json:"demux_stable_starts,omitempty"`
	DemuxStableFails      uint64               `json:"demux_stable_fails,omitempty"`
	DemuxStableSessions   []DemuxStableSession `json:"demux_stable_sessions,omitempty"`
}

// ingestEnvelope is POST /api/proxy/events body.
type ingestEnvelope struct {
	SchemaVersion int       `json:"schema_version"`
	ProxyID       string    `json:"proxy_id"`
	SentAt        time.Time `json:"sent_at"`
	Events        []Event   `json:"events,omitempty"`
	Snapshot      *Snapshot `json:"snapshot,omitempty"`
}

// Reporter asynchronously pushes events/snapshots to gen. Media path never waits.
type Reporter struct {
	genBase    string
	proxyID    string
	httpClient *http.Client
	store      *Store

	ch       chan Event
	drop     atomic.Uint64
	opens    atomic.Uint64
	n302     atomic.Uint64
	plOK     atomic.Uint64
	plFail   atomic.Uint64
	segOK    atomic.Uint64
	segFail  atomic.Uint64
	segBytes atomic.Uint64

	mu       sync.Mutex
	lastErrs []Event // recent failures for snapshot attrs (capped)
	demux    *demuxStableTracker
}

// NewReporter builds a reporter. genBase may be empty (no-op reporter for tests).
func NewReporter(genBase string, store *Store) *Reporter {
	host, _ := os.Hostname()
	if host == "" {
		host = "fastproxy"
	}
	return &Reporter{
		genBase:    strings.TrimRight(strings.TrimSpace(genBase), "/"),
		proxyID:    host,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		store:      store,
		ch:         make(chan Event, reportBufferSize),
	}
}

// Emit queues an event; drops oldest-side by skipping when full.
func (r *Reporter) Emit(ev Event) {
	if r == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	switch ev.Kind {
	case EventStreamOpen:
		r.opens.Add(1)
	case EventStream302:
		r.n302.Add(1)
	case EventPlaylistOK:
		r.plOK.Add(1)
	case EventPlaylistFail:
		r.plFail.Add(1)
	case EventSegOK:
		r.segOK.Add(1)
		if ev.Bytes > 0 {
			r.segBytes.Add(uint64(ev.Bytes))
		}
	case EventSegFail:
		r.segFail.Add(1)
	case EventDemuxStableFail:
		// demux tracker also increments fails; keep report buffer for glass.
	}
	if ev.Kind == EventPlaylistFail || ev.Kind == EventSegFail || ev.Kind == EventOriginMiss ||
		ev.Kind == EventSessionMintFail || ev.Kind == EventDemuxStableFail {
		r.mu.Lock()
		r.lastErrs = append(r.lastErrs, ev)
		if len(r.lastErrs) > 20 {
			r.lastErrs = r.lastErrs[len(r.lastErrs)-20:]
		}
		r.mu.Unlock()
	}
	select {
	case r.ch <- ev:
	default:
		r.drop.Add(1)
		logEvent(slog.LevelWarn, EventReportDropped, "kind", ev.Kind)
	}
}

// Run flushes events and heartbeats until ctx is done.
func (r *Reporter) Run(ctx context.Context) {
	if r == nil || r.genBase == "" {
		// Drain channel so Emit never blocks forever in tests without Run.
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.ch:
			}
		}
	}
	busy := time.NewTicker(busySnapshotEvery)
	idle := time.NewTicker(idleHeartbeatEvery)
	defer busy.Stop()
	defer idle.Stop()

	batch := make([]Event, 0, 32)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		r.post(ctx, batch, nil)
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case ev := <-r.ch:
			batch = append(batch, ev)
			if len(batch) >= 32 {
				flush()
			}
		case <-busy.C:
			flush()
			if r.store != nil {
				sess, segs := r.store.Stats()
				demuxActive := 0
				if r.demux != nil {
					demuxActive, _, _ = r.demux.snapshotRows()
				}
				if sess+segs > 0 || demuxActive > 0 || r.opens.Load() > 0 {
					snap := r.snapshot()
					r.post(ctx, nil, &snap)
				}
			}
		case <-idle.C:
			flush()
			snap := r.snapshot()
			r.post(ctx, nil, &snap)
		}
	}
}

func (r *Reporter) snapshot() Snapshot {
	sess, segs := 0, 0
	if r.store != nil {
		sess, segs = r.store.Stats()
	}
	snap := Snapshot{
		At:              time.Now().UTC(),
		ActiveSessions:  sess,
		ActiveSegTokens: segs,
		StreamOpens:     r.opens.Load(),
		Stream302s:      r.n302.Load(),
		PlaylistOK:      r.plOK.Load(),
		PlaylistFail:    r.plFail.Load(),
		SegOK:           r.segOK.Load(),
		SegFail:         r.segFail.Load(),
		SegBytes:        r.segBytes.Load(),
		EventsDropped:   r.drop.Load(),
	}
	if r.demux != nil {
		active, max, sessions := r.demux.snapshotRows()
		snap.DemuxStableActive = active
		snap.DemuxStableMax = max
		snap.DemuxStableBytesTotal = r.demux.bytes.Load()
		snap.DemuxStableStarts = r.demux.starts.Load()
		snap.DemuxStableFails = r.demux.fails.Load()
		snap.DemuxStableSessions = sessions
	}
	return snap
}

func (r *Reporter) post(ctx context.Context, events []Event, snap *Snapshot) {
	env := ingestEnvelope{
		SchemaVersion: eventSchemaVersion,
		ProxyID:       r.proxyID,
		SentAt:        time.Now().UTC(),
		Events:        events,
		Snapshot:      snap,
	}
	body, err := json.Marshal(env)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.genBase+"/api/proxy/events", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient.Do(req)
	if err != nil {
		logEvent(slog.LevelWarn, "report_post_fail", "err", err.Error())
		return
	}
	_ = resp.Body.Close()
}
