package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hairy-botter/internal/ai/domain"
)

type mockAI struct {
	err     error
	lastReq domain.Request
	lastUID string
}

func (m *mockAI) HandleMessage(ctx context.Context, userID string, req domain.Request) (string, error) {
	m.lastUID = userID
	m.lastReq = req
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

func TestPostMessagePayloads(t *testing.T) {
	cfg := Config{
		AllowedOrigin:  "*",
		AllowedMethods: "POST, OPTIONS",
		AllowedHeaders: "Content-Type, X-User-ID",
	}

	t.Run("JSON payload", func(t *testing.T) {
		aiMock := &mockAI{}
		srv := New(":8080", aiMock, cfg)
		req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(`{"message":"hello from json"}`))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-User-ID", "user-json")
		w := httptest.NewRecorder()

		srv.h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}
		if aiMock.lastUID != "user-json" {
			t.Errorf("expected userID 'user-json', got %q", aiMock.lastUID)
		}
		if aiMock.lastReq.Message != "hello from json" {
			t.Errorf("expected message 'hello from json', got %q", aiMock.lastReq.Message)
		}

		var resp struct {
			Response string `json:"response"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.Response != "mock response" {
			t.Errorf("expected 'mock response', got %q", resp.Response)
		}
	})

	t.Run("Invalid JSON payload", func(t *testing.T) {
		aiMock := &mockAI{}
		srv := New(":8080", aiMock, cfg)
		req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(`{invalid`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		srv.h.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("Form payload", func(t *testing.T) {
		aiMock := &mockAI{}
		srv := New(":8080", aiMock, cfg)
		req := httptest.NewRequest(http.MethodPost, "/message", strings.NewReader("message=hello+from+form"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		srv.h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}
		if aiMock.lastReq.Message != "hello from form" {
			t.Errorf("expected message 'hello from form', got %q", aiMock.lastReq.Message)
		}
	})
}
