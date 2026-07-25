package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/clientaccess"
)

type clientAccessResponse struct {
	Summary []clientAccessSummary `json:"summary"`
	Recent  []clientAccessEvent   `json:"recent"`
}

type clientAccessSummary struct {
	File       string `json:"file"`
	Hits30d    int    `json:"hits_30d"`
	LastAt     string `json:"last_at,omitempty"`
	LastIP     string `json:"last_ip,omitempty"`
	LastStatus int    `json:"last_status,omitempty"`
}

type clientAccessEvent struct {
	File      string `json:"file"`
	At        string `json:"at"`
	IP        string `json:"ip"`
	Status    int    `json:"status"`
	UserAgent string `json:"user_agent,omitempty"`
}

// ClientAccessHandler serves GET /api/client-access — emit pull summary + recent history.
// Query: limit (default 50), file, ip (substring), status (200|304).
func ClientAccessHandler(access *clientaccess.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		out := clientAccessResponse{
			Summary: []clientAccessSummary{},
			Recent:  []clientAccessEvent{},
		}
		if access != nil {
			sum, err := access.Summary()
			if err != nil {
				http.Error(w, "read error", http.StatusInternalServerError)
				return
			}
			for _, s := range sum {
				row := clientAccessSummary{
					File:       s.File,
					Hits30d:    s.Hits30d,
					LastIP:     s.LastIP,
					LastStatus: s.LastStatus,
				}
				if !s.LastAt.IsZero() {
					row.LastAt = s.LastAt.UTC().Format(time.RFC3339)
				}
				out.Summary = append(out.Summary, row)
			}
			q := clientaccess.Query{
				File: strings.TrimSpace(r.URL.Query().Get("file")),
				IP:   strings.TrimSpace(r.URL.Query().Get("ip")),
			}
			if lim := r.URL.Query().Get("limit"); lim != "" {
				n, err := strconv.Atoi(lim)
				if err != nil || n < 0 {
					http.Error(w, "invalid limit", http.StatusBadRequest)
					return
				}
				q.Limit = n
			}
			if st := r.URL.Query().Get("status"); st != "" {
				n, err := strconv.Atoi(st)
				if err != nil || (n != 200 && n != 304) {
					http.Error(w, "invalid status", http.StatusBadRequest)
					return
				}
				q.Status = n
			}
			recent, err := access.Recent(q)
			if err != nil {
				http.Error(w, "read error", http.StatusInternalServerError)
				return
			}
			for _, e := range recent {
				out.Recent = append(out.Recent, clientAccessEvent{
					File:      e.File,
					At:        e.At.UTC().Format(time.RFC3339),
					IP:        e.IP,
					Status:    e.Status,
					UserAgent: e.UserAgent,
				})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}
