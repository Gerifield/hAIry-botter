package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"hairy-botter/internal/ai/domain"
)

type mockAI struct {
	err error
}

func (m *mockAI) HandleMessage(ctx context.Context, userID string, req domain.Request) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return "mock response", nil
}

func checkCORSHeaders(t *testing.T, w *httptest.ResponseRecorder, cfg Config) {
	t.Helper()
	if origin := w.Header().Get("Access-Control-Allow-Origin"); origin != cfg.AllowedOrigin {
		t.Errorf("expected origin %s, got %s", cfg.AllowedOrigin, origin)
	}
	if methods := w.Header().Get("Access-Control-Allow-Methods"); methods != cfg.AllowedMethods {
		t.Errorf("expected methods %s, got %s", cfg.AllowedMethods, methods)
	}
	if headers := w.Header().Get("Access-Control-Allow-Headers"); headers != cfg.AllowedHeaders {
		t.Errorf("expected headers %s, got %s", cfg.AllowedHeaders, headers)
	}
}

func TestCORSHeaders(t *testing.T) {
	cfg := Config{
		AllowedOrigin:  "https://example.com",
		AllowedMethods: "GET, POST",
		AllowedHeaders: "X-Custom-Header",
	}

	t.Run("OPTIONS request", func(t *testing.T) {
		srv := New(":8080", &mockAI{}, cfg)
		req := httptest.NewRequest(http.MethodOptions, "/message", nil)
		w := httptest.NewRecorder()
		srv.h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status OK, got %d", w.Code)
		}
		checkCORSHeaders(t, w, cfg)
	})

	t.Run("POST request success", func(t *testing.T) {
		srv := New(":8080", &mockAI{}, cfg)
		req := httptest.NewRequest(http.MethodPost, "/message", nil)
		w := httptest.NewRecorder()
		srv.h.ServeHTTP(w, req)

		checkCORSHeaders(t, w, cfg)
	})

	t.Run("POST request error", func(t *testing.T) {
		srv := New(":8080", &mockAI{err: errors.New("handler error")}, cfg)
		req := httptest.NewRequest(http.MethodPost, "/message", nil)
		w := httptest.NewRecorder()
		srv.h.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected status InternalServerError, got %d", w.Code)
		}
		checkCORSHeaders(t, w, cfg)
	})
}
