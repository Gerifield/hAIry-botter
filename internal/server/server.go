// Package server is the HTTP server implementation for the main logic
package server

import (
	"context"
	"hairy-botter/internal/ai/domain"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type ai interface {
	HandleMessage(ctx context.Context, userID string, req domain.Request) (string, error)
}

// Config .
type Config struct {
	AllowedOrigin  string
	AllowedMethods string
	AllowedHeaders string
}

// Server .
type Server struct {
	h       *chi.Mux
	srv     *http.Server
	logic   ai
	cfg     Config
	connMgr *ConnectionManager
}

// New .
func New(addr string, aiLogic ai, cfg Config) *Server {
	h := chi.NewMux()
	s := &Server{
		h:       h,
		srv:     &http.Server{Addr: addr, Handler: h},
		logic:   aiLogic,
		cfg:     cfg,
		connMgr: newConnectionManager(),
	}
	s.addRoutes()

	return s
}

func (s *Server) addRoutes() {
	s.h.Post("/message", s.postMessage)
	s.h.Get("/ws/{session_id}", s.wsHandler)

	// CORS preflight request handler
	s.h.Options("/*", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.cfg.AllowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", s.cfg.AllowedMethods)
		w.Header().Set("Access-Control-Allow-Headers", s.cfg.AllowedHeaders)
		w.WriteHeader(http.StatusOK)
	})
}

// Broadcast sends an event message to all WebSocket clients connected to sessionID.
func (s *Server) Broadcast(ctx context.Context, sessionID string, msg any) {
	s.connMgr.Broadcast(ctx, sessionID, msg)
}

// Start .
func (s *Server) Start() error {
	return s.srv.ListenAndServe()
}

// Stop .
func (s *Server) Stop(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
