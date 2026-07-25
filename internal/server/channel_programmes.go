package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

type channelProgrammesResponse struct {
	Programmes []model.Programme `json:"programmes"`
}

// ChannelProgrammesHandler serves GET .../programmes — in-memory guide rows
// for one channel (diagnostic; includes programmes even when the channel is
// excluded from export).
func ChannelProgrammesHandler(reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		providerID := model.ProviderID(r.PathValue("provider"))
		normalizedID := r.PathValue("normalizedId")
		feed, _, ok := lookupChannel(reg, providerID, normalizedID)
		if !ok {
			http.NotFound(w, r)
			return
		}

		now := time.Now().UTC()
		from, to, err := programmesWindow(r, now)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		out := make([]model.Programme, 0)
		for _, p := range feed.Programmes() {
			if p.ChannelID != normalizedID || !p.IsValid() {
				continue
			}
			if !p.Start.Before(to) || !p.Stop.After(from) {
				continue
			}
			out = append(out, p)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Start.Equal(out[j].Start) {
				return out[i].Stop.Before(out[j].Stop)
			}
			return out[i].Start.Before(out[j].Start)
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(channelProgrammesResponse{Programmes: out})
	}
}

func programmesWindow(r *http.Request, now time.Time) (from, to time.Time, err error) {
	from = now.Add(-1 * time.Hour)
	to = now.Add(12 * time.Hour)
	if raw := r.URL.Query().Get("from"); raw != "" {
		from, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, errInvalidProgrammesTime("from")
		}
		from = from.UTC()
	}
	if raw := r.URL.Query().Get("to"); raw != "" {
		to, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, errInvalidProgrammesTime("to")
		}
		to = to.UTC()
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, errInvertedProgrammesWindow
	}
	return from, to, nil
}

type programmesQueryError string

func (e programmesQueryError) Error() string { return string(e) }

func errInvalidProgrammesTime(field string) error {
	return programmesQueryError("invalid " + field + ": want RFC3339")
}

const errInvertedProgrammesWindow programmesQueryError = "from must be before to"
