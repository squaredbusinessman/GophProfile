package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/app"
	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
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
	handler := newRouterForTest(t, RouterConfig{
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
	handler := newRouterForTest(t, RouterConfig{
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

// TestAvatarReadByUserReturnsDefaultForMissingAvatar проверяет заглушку для пользователя без avatar
func TestAvatarReadByUserReturnsDefaultForMissingAvatar(t *testing.T) {
	defaultAvatar := NewDefaultAvatar([]byte("default-image"))
	reader := &fakeAvatarReader{err: app.ErrAvatarNotFound}
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:   "gophprofile",
		Version:       "test",
		Logger:        zerolog.Nop(),
		DefaultAvatar: defaultAvatar,
		AvatarReader:  reader,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-1/avatar?size=100x100&format=png", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if got := rec.Header().Get("ETag"); got != defaultAvatar.ETag {
		t.Fatalf("ETag = %q, want %q", got, defaultAvatar.ETag)
	}
	if rec.Body.String() != "default-image" {
		t.Fatalf("body = %q, want default-image", rec.Body.String())
	}
	if reader.userID != "user-1" || reader.size != "100x100" || reader.format != "png" {
		t.Fatalf("reader args = %q %q %q, want user-1 100x100 png", reader.userID, reader.size, reader.format)
	}
}

// TestAvatarReadRejectsUnsupportedFormat проверяет 400 для неподдержанной конвертации
func TestAvatarReadRejectsUnsupportedFormat(t *testing.T) {
	reader := &fakeAvatarReader{err: app.ErrUnsupportedAvatarFormat}
	handler := newRouterForTest(t, RouterConfig{
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
	handler := newRouterForTest(t, RouterConfig{
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

// TestPublicAvatarByEmailReturnsBinary проверяет публичный lookup avatar по email
func TestPublicAvatarByEmailReturnsBinary(t *testing.T) {
	reader := &fakeAvatarReader{
		result: app.AvatarReadResult{
			Body:        io.NopCloser(strings.NewReader("image")),
			ContentType: "image/png",
			Size:        5,
		},
	}
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:  "gophprofile",
		Version:      "test",
		Logger:       zerolog.Nop(),
		AvatarReader: reader,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatar?email=User%40Example.COM&size=original&format=png", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "image" {
		t.Fatalf("body = %q, want image", rec.Body.String())
	}
	if reader.email != "user@example.com" || reader.size != "original" || reader.format != "png" {
		t.Fatalf("reader args = %q %q %q, want normalized email original png", reader.email, reader.size, reader.format)
	}
}

// TestPublicAvatarByEmailReturnsDefaultForMissingUser проверяет заглушку для неизвестного email
func TestPublicAvatarByEmailReturnsDefaultForMissingUser(t *testing.T) {
	defaultAvatar := NewDefaultAvatar([]byte("default-image"))
	reader := &fakeAvatarReader{err: app.ErrAvatarNotFound}
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:   "gophprofile",
		Version:       "test",
		Logger:        zerolog.Nop(),
		DefaultAvatar: defaultAvatar,
		AvatarReader:  reader,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatar?email=missing%40example.com", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if got := rec.Header().Get("ETag"); got != defaultAvatar.ETag {
		t.Fatalf("ETag = %q, want %q", got, defaultAvatar.ETag)
	}
	if rec.Body.String() != "default-image" {
		t.Fatalf("body = %q, want default-image", rec.Body.String())
	}
}

// TestPublicAvatarByEmailRequiresEmail проверяет обязательный email query
func TestPublicAvatarByEmailRequiresEmail(t *testing.T) {
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:  "gophprofile",
		Version:      "test",
		Logger:       zerolog.Nop(),
		AvatarReader: &fakeAvatarReader{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatar", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestAvatarMetadataReturnsReadyJSON проверяет metadata готовой avatar
func TestAvatarMetadataReturnsReadyJSON(t *testing.T) {
	width := 640
	height := 480
	thumb100 := "avatars/user-1/avatar-1/100x100"
	thumb300 := "avatars/user-1/avatar-1/300x300"
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	reader := &fakeAvatarReader{
		metadataResult: app.AvatarMetadataResult{
			Avatar: avatar.Avatar{
				ID:                "avatar-1",
				UserID:            "user-1",
				FileName:          "avatar.png",
				MimeType:          "image/png",
				SizeBytes:         128,
				Width:             &width,
				Height:            &height,
				Status:            avatar.StatusReady,
				OriginalObjectKey: "avatars/user-1/avatar-1/original",
				Thumb100ObjectKey: &thumb100,
				Thumb300ObjectKey: &thumb300,
				CreatedAt:         now,
				UpdatedAt:         now,
			},
		},
	}
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:  "gophprofile",
		Version:      "test",
		Logger:       zerolog.Nop(),
		AvatarReader: reader,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/avatar-1/metadata", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var response AvatarMetadataResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if reader.metadataAvatarID != "avatar-1" {
		t.Fatalf("metadataAvatarID = %q, want avatar-1", reader.metadataAvatarID)
	}
	if response.ID != "avatar-1" || response.UserID != "user-1" || response.Status != string(avatar.StatusReady) {
		t.Fatalf("response identity = %#v, want ready avatar metadata", response)
	}
	if response.SizeBytes != 128 || response.Width == nil || *response.Width != width || response.Height == nil || *response.Height != height {
		t.Fatalf("response size/dimensions = %#v, want size and dimensions", response)
	}
	if response.URL != "/api/v1/avatars/avatar-1" {
		t.Fatalf("url = %q, want internal avatar URL", response.URL)
	}
	if len(response.Thumbnails) != 2 {
		t.Fatalf("len(thumbnails) = %d, want 2", len(response.Thumbnails))
	}
}

// TestAvatarMetadataReflectsNonReadyStates проверяет metadata avatar без готовых thumbnails
func TestAvatarMetadataReflectsNonReadyStates(t *testing.T) {
	for _, status := range []avatar.Status{avatar.StatusProcessing, avatar.StatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
			reader := &fakeAvatarReader{
				metadataResult: app.AvatarMetadataResult{
					Avatar: avatar.Avatar{
						ID:                "avatar-1",
						UserID:            "user-1",
						FileName:          "avatar.png",
						MimeType:          "image/png",
						SizeBytes:         128,
						Status:            status,
						OriginalObjectKey: "avatars/user-1/avatar-1/original",
						CreatedAt:         now,
						UpdatedAt:         now,
					},
				},
			}
			handler := newRouterForTest(t, RouterConfig{
				ServiceName:  "gophprofile",
				Version:      "test",
				Logger:       zerolog.Nop(),
				AvatarReader: reader,
			})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/avatar-1/metadata", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}

			var response AvatarMetadataResponse
			if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Status != string(status) {
				t.Fatalf("status = %q, want %s", response.Status, status)
			}
			if len(response.Thumbnails) != 0 {
				t.Fatalf("len(thumbnails) = %d, want 0", len(response.Thumbnails))
			}
		})
	}
}

// TestUserAvatarListReturnsPagination проверяет список avatar пользователя
func TestUserAvatarListReturnsPagination(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	reader := &fakeAvatarReader{
		listResult: app.AvatarListResult{
			Items: []avatar.Avatar{
				{
					ID:                "avatar-2",
					UserID:            "user-1",
					FileName:          "second.png",
					MimeType:          "image/png",
					SizeBytes:         256,
					Status:            avatar.StatusFailed,
					OriginalObjectKey: "avatars/user-1/avatar-2/original",
					CreatedAt:         now.Add(time.Minute),
					UpdatedAt:         now.Add(time.Minute),
				},
				{
					ID:                "avatar-1",
					UserID:            "user-1",
					FileName:          "first.png",
					MimeType:          "image/png",
					SizeBytes:         128,
					Status:            avatar.StatusProcessing,
					OriginalObjectKey: "avatars/user-1/avatar-1/original",
					CreatedAt:         now,
					UpdatedAt:         now,
				},
			},
		},
	}
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:  "gophprofile",
		Version:      "test",
		Logger:       zerolog.Nop(),
		AvatarReader: reader,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-1/avatars?limit=2&offset=1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var response AvatarListResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if reader.listUserID != "user-1" || reader.limit != 2 || reader.offset != 1 {
		t.Fatalf("list args = %q %d %d, want user-1 2 1", reader.listUserID, reader.limit, reader.offset)
	}
	if response.Limit != 2 || response.Offset != 1 {
		t.Fatalf("pagination = %d %d, want 2 1", response.Limit, response.Offset)
	}
	if len(response.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(response.Items))
	}
	if response.Items[0].ID != "avatar-2" || response.Items[0].Status != string(avatar.StatusFailed) {
		t.Fatalf("first item = %#v, want failed avatar-2", response.Items[0])
	}
}

type fakeAvatarReader struct {
	avatarID         string
	userID           string
	email            string
	metadataAvatarID string
	listUserID       string
	size             string
	format           string
	limit            int
	offset           int
	result           app.AvatarReadResult
	metadataResult   app.AvatarMetadataResult
	listResult       app.AvatarListResult
	err              error
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

// GetLatestAvatarByEmail запоминает fake email lookup запрос
func (f *fakeAvatarReader) GetLatestAvatarByEmail(ctx context.Context, email string, size string, format string) (app.AvatarReadResult, error) {
	f.email = email
	f.size = size
	f.format = format
	if f.err != nil {
		return app.AvatarReadResult{}, f.err
	}
	return f.result, nil
}

// GetAvatarMetadata запоминает fake avatar id metadata запроса
func (f *fakeAvatarReader) GetAvatarMetadata(ctx context.Context, avatarID string) (app.AvatarMetadataResult, error) {
	f.metadataAvatarID = avatarID
	if f.err != nil {
		return app.AvatarMetadataResult{}, f.err
	}
	return f.metadataResult, nil
}

// ListAvatarsByUserID запоминает fake user id и pagination запроса
func (f *fakeAvatarReader) ListAvatarsByUserID(ctx context.Context, userID string, limit int, offset int) (app.AvatarListResult, error) {
	f.listUserID = userID
	f.limit = limit
	f.offset = offset
	if f.err != nil {
		return app.AvatarListResult{}, f.err
	}
	return f.listResult, nil
}
