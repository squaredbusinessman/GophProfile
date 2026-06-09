package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/app"
)

const testUserID = "6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e"

// TestAvatarDeleteByIDReturnsNoContent проверяет успешное удаление avatar по id
func TestAvatarDeleteByIDReturnsNoContent(t *testing.T) {
	deleter := &fakeAvatarDeleter{}
	handler := NewRouter(RouterConfig{
		ServiceName:   "gophprofile",
		Version:       "test",
		Logger:        zerolog.Nop(),
		AvatarDeleter: deleter,
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/avatar-1", nil)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if deleter.avatarID != "avatar-1" || deleter.requesterUserID != testUserID {
		t.Fatalf("delete args = %q %q, want avatar-1 requester", deleter.avatarID, deleter.requesterUserID)
	}
}

// TestAvatarDeleteRejectsForeignOwner проверяет 403 для чужой avatar
func TestAvatarDeleteRejectsForeignOwner(t *testing.T) {
	deleter := &fakeAvatarDeleter{err: app.ErrAvatarForbidden}
	handler := NewRouter(RouterConfig{
		ServiceName:   "gophprofile",
		Version:       "test",
		Logger:        zerolog.Nop(),
		AvatarDeleter: deleter,
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/avatar-1", nil)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// TestAvatarDeleteRequiresUserID проверяет обязательный X-User-ID для удаления
func TestAvatarDeleteRequiresUserID(t *testing.T) {
	handler := NewRouter(RouterConfig{
		ServiceName:   "gophprofile",
		Version:       "test",
		Logger:        zerolog.Nop(),
		AvatarDeleter: &fakeAvatarDeleter{},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/avatar-1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestLatestAvatarDeleteByUserReturnsNoContent проверяет удаление последней avatar пользователя
func TestLatestAvatarDeleteByUserReturnsNoContent(t *testing.T) {
	deleter := &fakeAvatarDeleter{}
	handler := NewRouter(RouterConfig{
		ServiceName:   "gophprofile",
		Version:       "test",
		Logger:        zerolog.Nop(),
		AvatarDeleter: deleter,
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+testUserID+"/avatar", nil)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if deleter.targetUserID != testUserID || deleter.requesterUserID != testUserID {
		t.Fatalf("delete args = %q %q, want target and requester", deleter.targetUserID, deleter.requesterUserID)
	}
}

// TestAvatarMetadataRejectsDeleteMethod проверяет запрет DELETE для metadata route
func TestAvatarMetadataRejectsDeleteMethod(t *testing.T) {
	handler := NewRouter(RouterConfig{
		ServiceName:   "gophprofile",
		Version:       "test",
		Logger:        zerolog.Nop(),
		AvatarDeleter: &fakeAvatarDeleter{},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/avatar-1/metadata", nil)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

type fakeAvatarDeleter struct {
	avatarID        string
	targetUserID    string
	requesterUserID string
	err             error
}

// DeleteAvatarByID запоминает fake delete by id запрос
func (f *fakeAvatarDeleter) DeleteAvatarByID(ctx context.Context, avatarID string, requesterUserID string) error {
	f.avatarID = avatarID
	f.requesterUserID = requesterUserID
	return f.err
}

// DeleteLatestAvatarByUserID запоминает fake delete by user запрос
func (f *fakeAvatarDeleter) DeleteLatestAvatarByUserID(ctx context.Context, targetUserID string, requesterUserID string) error {
	f.targetUserID = targetUserID
	f.requesterUserID = requesterUserID
	return f.err
}
