package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/app"
)

// TestAvatarReadByIDReturnsBinary проверяет binary ответ avatar endpoint
func TestAvatarReadByIDReturnsBinary(t *testing.T) {
	reader := &fakeAvatarReader{
		result: app.AvatarReadResult{
			Body:        io.NopCloser(strings.NewReader("image")),
			ContentType: "image/png",
			ETag:        "etag",
			Size:        5,
		},
	}
	handler := NewRouter(RouterConfig{
		ServiceName:  "gophprofile",
		Version:      "test",
		Logger:       zerolog.Nop(),
		AvatarReader: reader,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/avatar-1?size=original&format=png", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "image" {
		t.Fatalf("body = %q, want image", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "max-age=86400" {
		t.Fatalf("Cache-Control = %q, want max-age=86400", got)
	}
	if got := rec.Header().Get("ETag"); got != "etag" {
		t.Fatalf("ETag = %q, want etag", got)
	}
	if reader.avatarID != "avatar-1" || reader.size != "original" || reader.format != "png" {
		t.Fatalf("reader args = %q %q %q, want avatar-1 original png", reader.avatarID, reader.size, reader.format)
	}
}

// TestAvatarReadByUserReturnsProcessing проверяет 409 для неготовой thumbnail
func TestAvatarReadByUserReturnsProcessing(t *testing.T) {
	reader := &fakeAvatarReader{err: app.ErrAvatarProcessing}
	handler := NewRouter(RouterConfig{
		ServiceName:  "gophprofile",
		Version:      "test",
		Logger:       zerolog.Nop(),
		AvatarReader: reader,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-1/avatar?size=100x100", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if reader.userID != "user-1" {
		t.Fatalf("userID = %q, want user-1", reader.userID)
	}
}

// TestAvatarReadRejectsUnsupportedFormat проверяет 400 для неподдержанной конвертации
func TestAvatarReadRejectsUnsupportedFormat(t *testing.T) {
	reader := &fakeAvatarReader{err: app.ErrUnsupportedAvatarFormat}
	handler := NewRouter(RouterConfig{
		ServiceName:  "gophprofile",
		Version:      "test",
		Logger:       zerolog.Nop(),
		AvatarReader: reader,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/avatar-1?format=webp", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestAvatarReadReturnsNotFound проверяет 404 для отсутствующей avatar
func TestAvatarReadReturnsNotFound(t *testing.T) {
	reader := &fakeAvatarReader{err: app.ErrAvatarNotFound}
	handler := NewRouter(RouterConfig{
		ServiceName:  "gophprofile",
		Version:      "test",
		Logger:       zerolog.Nop(),
		AvatarReader: reader,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/missing", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

type fakeAvatarReader struct {
	avatarID string
	userID   string
	size     string
	format   string
	result   app.AvatarReadResult
	err      error
}

// GetAvatarByID запоминает fake avatar id запрос
func (f *fakeAvatarReader) GetAvatarByID(ctx context.Context, avatarID string, size string, format string) (app.AvatarReadResult, error) {
	f.avatarID = avatarID
	f.size = size
	f.format = format
	if f.err != nil {
		return app.AvatarReadResult{}, f.err
	}
	return f.result, nil
}

// GetLatestAvatarByUserID запоминает fake user id запрос
func (f *fakeAvatarReader) GetLatestAvatarByUserID(ctx context.Context, userID string, size string, format string) (app.AvatarReadResult, error) {
	f.userID = userID
	f.size = size
	f.format = format
	if f.err != nil {
		return app.AvatarReadResult{}, f.err
	}
	return f.result, nil
}
