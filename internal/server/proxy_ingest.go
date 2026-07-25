package server

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/j27-aurum/gofast/internal/proxyactivity"
)

const proxyIngestMaxBytes = 1 << 20

type proxyIngestEnvelope struct {
	SchemaVersion int                     `json:"schema_version"`
	ProxyID       string                  `json:"proxy_id"`
	SentAt        time.Time               `json:"sent_at"`
	Events        []proxyIngestEvent      `json:"events"`
	Snapshot      *proxyactivity.Snapshot `json:"snapshot"`
}

type proxyIngestEvent struct {
	Kind       string         `json:"kind"`
	At         time.Time      `json:"at"`
	Provider   string         `json:"provider"`
	ChannelID  string         `json:"channel_id"`
	Reason     string         `json:"reason"`
	Message    string         `json:"message"`
	Status     int            `json:"status"`
	DurationMS int64          `json:"duration_ms"`
	Bytes      int64          `json:"bytes"`
	Attrs      map[string]any `json:"attrs"`
}

// ProxyEventsHandler serves POST /api/proxy/events (server-to-server from FASTProxy).
func ProxyEventsHandler(store *proxyactivity.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if store == nil {
			http.Error(w, "proxy activity unavailable", http.StatusServiceUnavailable)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, proxyIngestMaxBytes+1))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		if len(body) > proxyIngestMaxBytes {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		var env proxyIngestEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		events := make([]proxyactivity.Event, 0, len(env.Events))
		for _, e := range env.Events {
			var attrs json.RawMessage
			if len(e.Attrs) > 0 {
				attrs, _ = json.Marshal(e.Attrs)
			}
			events = append(events, proxyactivity.Event{
				Kind: e.Kind, At: e.At, Provider: e.Provider, ChannelID: e.ChannelID,
				Reason: e.Reason, Message: e.Message, Status: e.Status,
				DurationMS: e.DurationMS, Bytes: e.Bytes, Attrs: attrs,
			})
		}
		if err := store.IngestBatch(env.ProxyID, events, env.Snapshot); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ProxyStatusHandler serves GET /api/proxy/status for the Status UI glass.
func ProxyStatusHandler(store *proxyactivity.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if store == nil {
			http.Error(w, "proxy activity unavailable", http.StatusServiceUnavailable)
			return
		}
		st, err := store.StatusView()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	}
}
