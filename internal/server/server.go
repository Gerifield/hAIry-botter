// Package server is the HTTP server implementation for the main logic
package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type ai interface {
	HandleMessage(ctx context.Context, userID string, msg string) (string, error)
}

// Server .
type Server struct {
	h     *chi.Mux
	srv   *http.Server
	logic ai
}

// New .
func New(addr string, aiLogic ai) *Server {
	h := chi.NewMux()
	s := &Server{
		h:     h,
		srv:   &http.Server{Addr: addr, Handler: h},
		logic: aiLogic,
	}
	s.addRoutes()

	return s
}

func (s *Server) addRoutes() {
	s.h.Post("/message", s.postMessage)

	// CORS preflight request handler
	// TODO: make configurable
	s.h.Options("/*", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-User-ID")
		w.WriteHeader(http.StatusOK)
	})
}

// Start .
func (s *Server) Start() error {
	return s.srv.ListenAndServe()
}

// Stop .
func (s *Server) Stop(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
