package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// TestHealthReturnsOK проверяет успешный ответ healthcheck
func TestHealthReturnsOK(t *testing.T) {
	handler := NewRouter(RouterConfig{
		ServiceName: "gophprofile",
		Version:     "test",
		Logger:      zerolog.Nop(),
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var response HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if response.Status != "ok" {
		t.Fatalf("Status = %q, want ok", response.Status)
	}
	if response.Service != "gophprofile" {
		t.Fatalf("Service = %q, want gophprofile", response.Service)
	}
}

// TestHealthRejectsUnsupportedMethod проверяет отказ для неподдержанного метода
func TestHealthRejectsUnsupportedMethod(t *testing.T) {
	handler := NewRouter(RouterConfig{
		ServiceName: "gophprofile",
		Version:     "test",
		Logger:      zerolog.Nop(),
	})

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow = %q, want GET", got)
	}
}

// TestHTTPAccessLogUsesInfoForSuccess проверяет info-уровень для успешного запроса
func TestHTTPAccessLogUsesInfoForSuccess(t *testing.T) {
	var logs bytes.Buffer
	handler := NewRouter(RouterConfig{
		ServiceName: "gophprofile",
		Version:     "test",
		Logger:      zerolog.New(&logs),
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !strings.Contains(logs.String(), `"level":"info"`) {
		t.Fatalf("access log = %s, want info level", logs.String())
	}
	if strings.Contains(logs.String(), `"level":"warn"`) {
		t.Fatalf("access log = %s, should not warn for successful request", logs.String())
	}
}

// TestHTTPAccessLogUsesWarnForClientError проверяет warn-уровень для клиентской ошибки
func TestHTTPAccessLogUsesWarnForClientError(t *testing.T) {
	var logs bytes.Buffer
	handler := NewRouter(RouterConfig{
		ServiceName: "gophprofile",
		Version:     "test",
		Logger:      zerolog.New(&logs),
	})

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !strings.Contains(logs.String(), `"level":"warn"`) {
		t.Fatalf("access log = %s, want warn level", logs.String())
	}
}
