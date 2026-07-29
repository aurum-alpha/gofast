package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/opsreport"
)

// OpsReportScheduleHandler serves GET /api/ops-report/schedule.
func OpsReportScheduleHandler(sched *opsreport.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if sched == nil {
			http.Error(w, "ops report unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sched.Snapshot())
	}
}

// OpsReportArchivesHandler serves GET /api/ops-report/archives.
func OpsReportArchivesHandler(sched *opsreport.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if sched == nil {
			http.Error(w, "ops report unavailable", http.StatusServiceUnavailable)
			return
		}
		list, err := sched.ListArchives(50)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []opsreport.ArchiveMeta{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"archives": list})
	}
}

// OpsReportArchiveHandler serves GET /api/ops-report/archives/{id}.
func OpsReportArchiveHandler(sched *opsreport.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if sched == nil {
			http.Error(w, "ops report unavailable", http.StatusServiceUnavailable)
			return
		}
		id := r.PathValue("id")
		a, err := sched.GetArchive(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(a)
	}
}

// OpsReportResendHandler serves POST /api/ops-report/archives/{id}/resend.
func OpsReportResendHandler(sched *opsreport.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		if sched == nil {
			http.Error(w, "ops report unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := sched.Resend(r.Context(), r.PathValue("id")); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// OpsReportTestSMTPHandler serves POST /api/ops-report/test-smtp.
func OpsReportTestSMTPHandler(sched *opsreport.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		if sched == nil {
			http.Error(w, "ops report unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := sched.TestSMTP(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// OpsReportSendPreviewHandler serves POST /api/ops-report/send-preview.
func OpsReportSendPreviewHandler(sched *opsreport.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		if sched == nil {
			http.Error(w, "ops report unavailable", http.StatusServiceUnavailable)
			return
		}
		meta, err := sched.SendPreview(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(meta)
	}
}
