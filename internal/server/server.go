package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Server is the HTTP process for fastgen/fastproxy: listen address, route
// registration, request logging, and graceful shutdown.
type Server struct {
	Addr   string
	Routes func(mux *http.ServeMux) // optional; called after /healthz is registered
}

// Handler returns the HTTP handler for this server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", Healthz)
	if s.Routes != nil {
		s.Routes(mux)
	}
	return WithRequestLog(mux)
}

// WithRequestLog logs method, path, and duration for each request.
func WithRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", time.Since(start),
		)
	})
}

// Run listens until ctx is cancelled, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
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
