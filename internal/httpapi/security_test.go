package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/app"
)

// TestCORSPreflightAllowsConfiguredOrigin проверяет preflight для разрешенного origin
func TestCORSPreflightAllowsConfiguredOrigin(t *testing.T) {
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:    "gophprofile",
		Version:        "test",
		Logger:         zerolog.Nop(),
		AllowedOrigins: []string{"https://app.example.com"},
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/avatars", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want configured origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPost) {
		t.Fatalf("Access-Control-Allow-Methods = %q, want POST", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-User-ID") {
		t.Fatalf("Access-Control-Allow-Headers = %q, want X-User-ID", got)
	}
}

// TestCORSRejectsUnconfiguredOrigin проверяет отказ для origin вне allowlist
func TestCORSRejectsUnconfiguredOrigin(t *testing.T) {
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:    "gophprofile",
		Version:        "test",
		Logger:         zerolog.Nop(),
		AllowedOrigins: []string{"https://app.example.com"},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}

// TestRateLimitRejectsAPIBurst проверяет лимит запросов на API routes
func TestRateLimitRejectsAPIBurst(t *testing.T) {
	reader := &fakeAvatarReader{err: app.ErrAvatarNotFound}
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:    "gophprofile",
		Version:        "test",
		Logger:         zerolog.Nop(),
		RateLimitRPS:   1,
		RateLimitBurst: 1,
		AvatarReader:   reader,
	})

	first := httptest.NewRequest(http.MethodGet, "/api/v1/avatar?email=user%40example.com", nil)
	first.RemoteAddr = "203.0.113.10:12345"
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, first)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", firstRec.Code, http.StatusOK)
	}

	second := httptest.NewRequest(http.MethodGet, "/api/v1/avatar?email=user%40example.com", nil)
	second.RemoteAddr = "203.0.113.10:54321"
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", secondRec.Code, http.StatusTooManyRequests)
	}
	if got := secondRec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}
}

// TestRateLimitSkipsHealth проверяет что healthcheck не зависит от API limiter
func TestRateLimitSkipsHealth(t *testing.T) {
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:    "gophprofile",
		Version:        "test",
		Logger:         zerolog.Nop(),
		RateLimitRPS:   1,
		RateLimitBurst: 1,
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "203.0.113.10:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status[%d] = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}
}

// TestAvatarReadErrorDoesNotLeakInternalDetails проверяет что read error не раскрывает секреты
func TestAvatarReadErrorDoesNotLeakInternalDetails(t *testing.T) {
	reader := &fakeAvatarReader{err: sensitiveInternalError()}
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:  "gophprofile",
		Version:      "test",
		Logger:       zerolog.Nop(),
		AvatarReader: reader,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/avatar-1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	assertNoSensitiveDetails(t, rec.Body.String())
}

// TestAvatarUploadErrorDoesNotLeakInternalDetails проверяет что upload error не раскрывает секреты
func TestAvatarUploadErrorDoesNotLeakInternalDetails(t *testing.T) {
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:    "gophprofile",
		Version:        "test",
		Logger:         zerolog.Nop(),
		AvatarUploader: &fakeAvatarUploader{err: sensitiveInternalError()},
	})

	req := validAvatarUploadHTTPRequest(t, testUserID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	assertNoSensitiveDetails(t, rec.Body.String())
}

// TestAvatarDeleteErrorDoesNotLeakInternalDetails проверяет что delete error не раскрывает секреты
func TestAvatarDeleteErrorDoesNotLeakInternalDetails(t *testing.T) {
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:   "gophprofile",
		Version:       "test",
		Logger:        zerolog.Nop(),
		AvatarDeleter: &fakeAvatarDeleter{err: sensitiveInternalError()},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/avatar-1", nil)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	assertNoSensitiveDetails(t, rec.Body.String())
}

// sensitiveInternalError возвращает ошибку с деталями которые нельзя отдавать клиенту
func sensitiveInternalError() error {
	return errors.New("postgres://secret:pass@db:5432/app avatars/user/avatar/original stack trace")
}

// assertNoSensitiveDetails проверяет отсутствие внутренних деталей в HTTP body
func assertNoSensitiveDetails(t *testing.T, body string) {
	t.Helper()

	for _, forbidden := range []string{"postgres://", "secret", "avatars/", "stack trace"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("body = %q, should not contain %q", body, forbidden)
		}
	}
}
