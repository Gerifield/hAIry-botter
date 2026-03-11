package server

import (
	"context"
	"hairy-botter/internal/ai/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockAI struct{}

func (m *mockAI) HandleMessage(ctx context.Context, userID string, req domain.Request) (string, error) {
	return "mock response", nil
}

func TestCORSHeaders(t *testing.T) {
	cfg := Config{
		AllowedOrigin:  "https://example.com",
		AllowedMethods: "GET, POST",
		AllowedHeaders: "X-Custom-Header",
	}
	aiLogic := &mockAI{}
	srv := New(":8080", aiLogic, cfg)

	t.Run("OPTIONS request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/message", nil)
		w := httptest.NewRecorder()
		srv.h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status OK, got %d", w.Code)
		}

		if origin := w.Header().Get("Access-Control-Allow-Origin"); origin != cfg.AllowedOrigin {
			t.Errorf("expected origin %s, got %s", cfg.AllowedOrigin, origin)
		}
		if methods := w.Header().Get("Access-Control-Allow-Methods"); methods != cfg.AllowedMethods {
			t.Errorf("expected methods %s, got %s", cfg.AllowedMethods, methods)
		}
		if headers := w.Header().Get("Access-Control-Allow-Headers"); headers != cfg.AllowedHeaders {
			t.Errorf("expected headers %s, got %s", cfg.AllowedHeaders, headers)
		}
	})

	t.Run("POST request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/message", nil)
		w := httptest.NewRecorder()
		srv.h.ServeHTTP(w, req)

		if origin := w.Header().Get("Access-Control-Allow-Origin"); origin != cfg.AllowedOrigin {
			t.Errorf("expected origin %s, got %s", cfg.AllowedOrigin, origin)
		}
		if methods := w.Header().Get("Access-Control-Allow-Methods"); methods != cfg.AllowedMethods {
			t.Errorf("expected methods %s, got %s", cfg.AllowedMethods, methods)
		}
		if headers := w.Header().Get("Access-Control-Allow-Headers"); headers != cfg.AllowedHeaders {
			t.Errorf("expected headers %s, got %s", cfg.AllowedHeaders, headers)
		}
	})
}
