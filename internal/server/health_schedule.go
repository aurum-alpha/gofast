package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/health"
)

// HealthScheduleHandler serves GET /api/health/schedule (global L2/L3 next/last).
func HealthScheduleHandler(sched *health.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if sched == nil {
			http.Error(w, "health scheduler unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sched.Snapshot())
	}
}
