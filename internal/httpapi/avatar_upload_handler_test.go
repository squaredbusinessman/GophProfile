package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/app"
	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
)

// TestAvatarUploadReturnsCreated проверяет успешный HTTP upload avatar
func TestAvatarUploadReturnsCreated(t *testing.T) {
	uploader := &fakeAvatarUploader{
		result: app.AvatarUploadResult{
			ID:        "4a992fa3-df1a-4b5f-b764-546e99643eb0",
			UserID:    "6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e",
			Status:    avatar.StatusProcessing,
			Width:     2,
			Height:    3,
			CreatedAt: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
		},
	}
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:    "gophprofile",
		Version:        "test",
		Logger:         zerolog.Nop(),
		AvatarUploader: uploader,
	})

	req := validAvatarUploadHTTPRequest(t, "6F3F3C2D-DF58-4E64-91EA-CDF90F4C9C1E")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if uploader.request.UserID != "6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e" {
		t.Fatalf("UserID = %q, want normalized user id", uploader.request.UserID)
	}
	if uploader.request.Width != 2 || uploader.request.Height != 3 {
		t.Fatalf("dimensions = %dx%d, want 2x3", uploader.request.Width, uploader.request.Height)
	}

	var response AvatarUploadResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != "4a992fa3-df1a-4b5f-b764-546e99643eb0" {
		t.Fatalf("ID = %q, want avatar id", response.ID)
	}
	if response.UserID != "6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e" {
		t.Fatalf("UserID = %q, want internal user id", response.UserID)
	}
}

// TestAvatarUploadRejectsMissingUserID проверяет обязательный X-User-ID
func TestAvatarUploadRejectsMissingUserID(t *testing.T) {
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:    "gophprofile",
		Version:        "test",
		Logger:         zerolog.Nop(),
		AvatarUploader: &fakeAvatarUploader{},
	})

	req := validAvatarUploadHTTPRequest(t, "")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestAvatarUploadReturnsNotFoundForMissingUser проверяет 404 для неизвестного user_id
func TestAvatarUploadReturnsNotFoundForMissingUser(t *testing.T) {
	handler := newRouterForTest(t, RouterConfig{
		ServiceName:    "gophprofile",
		Version:        "test",
		Logger:         zerolog.Nop(),
		AvatarUploader: &fakeAvatarUploader{err: app.ErrUserNotFound},
	})

	req := validAvatarUploadHTTPRequest(t, testUserID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestAvatarUploadReturnsUnavailableWithoutService проверяет отсутствие upload service
func TestAvatarUploadReturnsUnavailableWithoutService(t *testing.T) {
	handler := newRouterForTest(t, RouterConfig{
		ServiceName: "gophprofile",
		Version:     "test",
		Logger:      zerolog.Nop(),
	})

	req := validAvatarUploadHTTPRequest(t, testUserID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

type fakeAvatarUploader struct {
	request app.AvatarUploadRequest
	result  app.AvatarUploadResult
	err     error
}

// UploadAvatar запоминает request и возвращает fake-результат
func (f *fakeAvatarUploader) UploadAvatar(ctx context.Context, req app.AvatarUploadRequest) (app.AvatarUploadResult, error) {
	f.request = req
	if f.err != nil {
		return app.AvatarUploadResult{}, f.err
	}
	return f.result, nil
}

// validAvatarUploadHTTPRequest создает валидный multipart upload request
func validAvatarUploadHTTPRequest(t *testing.T, userID string) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="file"; filename="avatar.png"`},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		t.Fatalf("create multipart part: %v", err)
	}
	if _, err := part.Write(validPNG(t)); err != nil {
		t.Fatalf("write multipart part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	return req
}

// validPNG создает маленькое PNG-изображение для API тестов
func validPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})

	var body bytes.Buffer
	if err := png.Encode(&body, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return body.Bytes()
}
