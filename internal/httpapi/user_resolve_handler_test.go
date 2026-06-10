package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/app"
)

// TestUserResolveReturnsUserID проверяет сопоставление email с user_id
func TestUserResolveReturnsUserID(t *testing.T) {
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	resolver := &fakeUserResolver{
		result: app.UserResolveResult{
			ID:        testUserID,
			Email:     "user@example.com",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	handler := NewRouter(RouterConfig{
		ServiceName:  "gophprofile",
		Version:      "test",
		Logger:       zerolog.Nop(),
		UserResolver: resolver,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/resolve", strings.NewReader(`{"email":"User@Example.COM"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if resolver.email != "user@example.com" {
		t.Fatalf("email = %q, want normalized email", resolver.email)
	}

	var response UserResolveResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.UserID != testUserID || response.Email != "user@example.com" {
		t.Fatalf("response = %#v, want user id and email", response)
	}
}

// TestUserResolveRejectsInvalidEmail проверяет валидацию email
func TestUserResolveRejectsInvalidEmail(t *testing.T) {
	handler := NewRouter(RouterConfig{
		ServiceName:  "gophprofile",
		Version:      "test",
		Logger:       zerolog.Nop(),
		UserResolver: &fakeUserResolver{},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/resolve", strings.NewReader(`{"email":"bad email"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

type fakeUserResolver struct {
	email  string
	result app.UserResolveResult
	err    error
}

// ResolveUserByEmail запоминает email и возвращает fake user resolve result
func (f *fakeUserResolver) ResolveUserByEmail(ctx context.Context, email string) (app.UserResolveResult, error) {
	f.email = email
	if f.err != nil {
		return app.UserResolveResult{}, f.err
	}
	return f.result, nil
}
