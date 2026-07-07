package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/app"
)

// TestHealthReturnsOK проверяет успешный ответ healthcheck
func TestHealthReturnsOK(t *testing.T) {
	handler := newRouterForTest(t, RouterConfig{
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

// TestHealthReturnsDependencyChecks проверяет успешные статусы внешних зависимостей
func TestHealthReturnsDependencyChecks(t *testing.T) {
	handler := newRouterForTest(t, RouterConfig{
		ServiceName: "gophprofile",
		Version:     "test",
		Logger:      zerolog.Nop(),
		HealthChecks: map[string]HealthCheck{
			"postgres": func(ctx context.Context) error {
				return nil
			},
			"s3": func(ctx context.Context) error {
				return nil
			},
			"kafka": func(ctx context.Context) error {
				return nil
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var response HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if response.Status != "ok" {
		t.Fatalf("Status = %q, want ok", response.Status)
	}
	for _, name := range []string{"postgres", "s3", "kafka"} {
		if response.Checks[name] != "ok" {
			t.Fatalf("Checks[%s] = %q, want ok", name, response.Checks[name])
		}
	}
}

// TestHealthReturnsServiceUnavailableForFailedCheck проверяет degraded статус при ошибке зависимости
func TestHealthReturnsServiceUnavailableForFailedCheck(t *testing.T) {
	handler := newRouterForTest(t, RouterConfig{
		ServiceName: "gophprofile",
		Version:     "test",
		Logger:      zerolog.Nop(),
		HealthChecks: map[string]HealthCheck{
			"postgres": func(ctx context.Context) error {
				return errors.New("database is down")
			},
			"s3": func(ctx context.Context) error {
				return nil
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var response HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if response.Status != "degraded" {
		t.Fatalf("Status = %q, want degraded", response.Status)
	}
	if response.Checks["postgres"] != "error" {
		t.Fatalf("postgres check = %q, want error", response.Checks["postgres"])
	}
	if response.Checks["s3"] != "ok" {
		t.Fatalf("s3 check = %q, want ok", response.Checks["s3"])
	}
}

// TestHealthRejectsUnsupportedMethod проверяет отказ для неподдержанного метода
func TestHealthRejectsUnsupportedMethod(t *testing.T) {
	handler := newRouterForTest(t, RouterConfig{
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
	handler := newRouterForTest(t, RouterConfig{
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
	handler := newRouterForTest(t, RouterConfig{
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

// TestRequestIDIsStoredInHandlerContext проверяет передачу request_id в context обработчика
func TestRequestIDIsStoredInHandlerContext(t *testing.T) {
	var requestID string
	handler := newRouterForTest(t, RouterConfig{
		Logger: zerolog.Nop(),
		HealthChecks: map[string]HealthCheck{
			"context": func(ctx context.Context) error {
				requestID = app.RequestIDFromContext(ctx)
				return nil
			},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-ID", "request-test-123")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if requestID != "request-test-123" {
		t.Fatalf("request_id = %q, ожидался request-test-123", requestID)
	}
}

// TestValidationErrorDoesNotProduceInternalErrorLog проверяет уровень validation ошибки
func TestValidationErrorDoesNotProduceInternalErrorLog(t *testing.T) {
	var logs bytes.Buffer
	handler := newRouterForTest(t, RouterConfig{
		Logger:       zerolog.New(&logs),
		UserResolver: failingUserResolver{},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/resolve", strings.NewReader(`{"unknown":true}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, ожидался 400", rec.Code)
	}
	if strings.Contains(logs.String(), `"level":"error"`) || !strings.Contains(logs.String(), `"level":"warn"`) {
		t.Fatalf("validation log имеет неверный уровень: %s", logs.String())
	}
}

// TestInternalErrorLogDoesNotExposeDSN проверяет защиту секретов во внутренней ошибке
func TestInternalErrorLogDoesNotExposeDSN(t *testing.T) {
	const secretDSN = "postgres://secret:password@db:5432/gophprofile"
	var logs bytes.Buffer
	handler := newRouterForTest(t, RouterConfig{
		Logger:       zerolog.New(&logs),
		UserResolver: failingUserResolver{err: errors.New(secretDSN)},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/resolve", strings.NewReader(`{"email":"user@example.com"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, ожидался 500", rec.Code)
	}
	if strings.Contains(logs.String(), secretDSN) || strings.Contains(logs.String(), "password") {
		t.Fatalf("error log содержит DSN или секрет: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"level":"error"`) || !strings.Contains(logs.String(), `"error_type":"*errors.errorString"`) {
		t.Fatalf("внутренняя ошибка не классифицирована: %s", logs.String())
	}
}

// failingUserResolver возвращает настроенную ошибку разрешения пользователя
type failingUserResolver struct {
	// err содержит возвращаемую тестовую ошибку
	err error
}

// ResolveUserByEmail возвращает ошибку тестового разрешения пользователя
func (r failingUserResolver) ResolveUserByEmail(context.Context, string) (app.UserResolveResult, error) {
	return app.UserResolveResult{}, r.err
}
