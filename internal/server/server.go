// Package server is the HTTP server implementation for the main logic
package server

import (
	"context"
	"hairy-botter/internal/logic"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Server .
type Server struct {
	h     *chi.Mux
	srv   *http.Server
	logic *logic.Logic
}

// New .
func New(addr string, logic *logic.Logic) *Server {
	h := chi.NewMux()
	s := &Server{
		h:     h,
		srv:   &http.Server{Addr: addr, Handler: h},
		logic: logic,
	}
	s.addRoutes()

	return s
}

func (s *Server) addRoutes() {
	s.h.Post("/message", s.postMessage)
}

// Start .
func (s *Server) Start() error {
	return s.srv.ListenAndServe()
}

// Stop .
func (s *Server) Stop(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
