package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/refresh"
)

// ProviderRefreshHandler serves POST /api/providers/{id}/refresh — accept an
// on-demand network refresh (fetch → classify → commit → notify) and return 202.
// Concurrent refreshes for the same provider return 409. Unknown/disabled → 404.
func ProviderRefreshHandler(svc *refresh.Service, runCtx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := model.ProviderID(r.PathValue("id"))
		err := svc.TriggerAsync(runCtx, id)
		if errors.Is(err, refresh.ErrUnknownProvider) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, refresh.ErrRefreshInFlight) {
			http.Error(w, "refresh already in progress", http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}
