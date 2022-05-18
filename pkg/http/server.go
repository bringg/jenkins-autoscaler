package http

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const MetricsEndpoint = "/metrics"

type (
	Server struct {
		srv *http.Server
	}
)

// NewServer returns a new Server with defined handlers.
func NewServer(addr string) *Server {
	mux := http.NewServeMux()
	mux.Handle(MetricsEndpoint, promhttp.Handler())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, MetricsEndpoint, http.StatusTemporaryRedirect)
	})

	return &Server{srv: &http.Server{Addr: addr, Handler: mux}}
}

// Start starts http server.
func (s *Server) Start() error {
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

// Shutdown stop http server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
