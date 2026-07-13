package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Stub serves a minimal HTTP API until full gen/proxy handlers land.
type Stub struct {
	Addr string
}

// Handler returns the stub HTTP handler (healthz only for now).
func (s *Stub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	return mux
}

// Run listens until ctx is cancelled, then shuts down gracefully.
func (s *Stub) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.Addr,
		Handler: s.Handler(),
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", s.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-errCh; err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		return err
	}
}

func healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
