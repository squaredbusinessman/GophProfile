package httpapi

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestValidateAvatarUploadAcceptsJPEG проверяет успешную валидацию JPEG
func TestValidateAvatarUploadAcceptsJPEG(t *testing.T) {
	req := avatarUploadRequest(t, "User+Avatar@Example.COM", "file", "avatar.jpg", "image/jpeg", jpegBytes())
	rec := httptest.NewRecorder()

	upload, err := ValidateAvatarUploadRequest(rec, req)
	if err != nil {
		t.Fatalf("ValidateAvatarUploadRequest returned error: %v", err)
	}
	defer upload.Close()

	if upload.UserEmail != "user+avatar@example.com" {
		t.Fatalf("UserEmail = %q, want normalized email", upload.UserEmail)
	}
	if upload.FileName != "avatar.jpg" {
		t.Fatalf("FileName = %q, want avatar.jpg", upload.FileName)
	}
	if upload.ContentType != "image/jpeg" {
		t.Fatalf("ContentType = %q, want image/jpeg", upload.ContentType)
	}
	if upload.Size != int64(len(jpegBytes())) {
		t.Fatalf("Size = %d, want %d", upload.Size, len(jpegBytes()))
	}
}

// TestValidateAvatarUploadAcceptsPNG проверяет успешную валидацию PNG
func TestValidateAvatarUploadAcceptsPNG(t *testing.T) {
	req := avatarUploadRequest(t, "user@example.com", "file", "avatar.png", "image/png", pngBytes())
	rec := httptest.NewRecorder()

	upload, err := ValidateAvatarUploadRequest(rec, req)
	if err != nil {
		t.Fatalf("ValidateAvatarUploadRequest returned error: %v", err)
	}
	defer upload.Close()

	if upload.ContentType != "image/png" {
		t.Fatalf("ContentType = %q, want image/png", upload.ContentType)
	}
}

// TestValidateAvatarUploadAcceptsWebP проверяет успешную валидацию WebP
func TestValidateAvatarUploadAcceptsWebP(t *testing.T) {
	req := avatarUploadRequest(t, "user@example.com", "file", "avatar.webp", "image/webp", webpBytes())
	rec := httptest.NewRecorder()

	upload, err := ValidateAvatarUploadRequest(rec, req)
	if err != nil {
		t.Fatalf("ValidateAvatarUploadRequest returned error: %v", err)
	}
	defer upload.Close()

	if upload.ContentType != "image/webp" {
		t.Fatalf("ContentType = %q, want image/webp", upload.ContentType)
	}
}

// TestValidateAvatarUploadRequiresUserEmail проверяет обязательный X-User-ID
func TestValidateAvatarUploadRequiresUserEmail(t *testing.T) {
	req := avatarUploadRequest(t, "", "file", "avatar.jpg", "image/jpeg", jpegBytes())
	rec := httptest.NewRecorder()

	_, err := ValidateAvatarUploadRequest(rec, req)
	assertValidationError(t, err, http.StatusBadRequest, "Missing X-User-ID")
}

// TestValidateAvatarUploadRejectsInvalidUserEmail проверяет формат email в X-User-ID
func TestValidateAvatarUploadRejectsInvalidUserEmail(t *testing.T) {
	req := avatarUploadRequest(t, "bad/user", "file", "avatar.jpg", "image/jpeg", jpegBytes())
	rec := httptest.NewRecorder()

	_, err := ValidateAvatarUploadRequest(rec, req)
	assertValidationError(t, err, http.StatusBadRequest, "Invalid X-User-ID")
}

// TestValidateAvatarUploadRejectsEmailWithoutDomainDot проверяет домен email
func TestValidateAvatarUploadRejectsEmailWithoutDomainDot(t *testing.T) {
	req := avatarUploadRequest(t, "user@example", "file", "avatar.jpg", "image/jpeg", jpegBytes())
	rec := httptest.NewRecorder()

	_, err := ValidateAvatarUploadRequest(rec, req)
	assertValidationError(t, err, http.StatusBadRequest, "Invalid X-User-ID")
}

// TestValidateAvatarUploadRejectsLongUserEmail проверяет длину email в X-User-ID
func TestValidateAvatarUploadRejectsLongUserEmail(t *testing.T) {
	req := avatarUploadRequest(t, strings.Repeat("a", maxUserEmailLength)+"@example.com", "file", "avatar.jpg", "image/jpeg", jpegBytes())
	rec := httptest.NewRecorder()

	_, err := ValidateAvatarUploadRequest(rec, req)
	assertValidationError(t, err, http.StatusBadRequest, "Invalid X-User-ID")
}

// TestValidateAvatarUploadRequiresFile проверяет обязательное поле file
func TestValidateAvatarUploadRequiresFile(t *testing.T) {
	req := avatarUploadRequest(t, "user@example.com", "image", "avatar.jpg", "image/jpeg", jpegBytes())
	rec := httptest.NewRecorder()

	_, err := ValidateAvatarUploadRequest(rec, req)
	assertValidationError(t, err, http.StatusBadRequest, "Missing file")
}

// TestValidateAvatarUploadRejectsEmptyFile проверяет отказ для пустого файла
func TestValidateAvatarUploadRejectsEmptyFile(t *testing.T) {
	req := avatarUploadRequest(t, "user@example.com", "file", "avatar.jpg", "image/jpeg", nil)
	rec := httptest.NewRecorder()

	_, err := ValidateAvatarUploadRequest(rec, req)
	assertValidationError(t, err, http.StatusBadRequest, "Invalid file format")
}

// TestValidateAvatarUploadRejectsUnsupportedMIME проверяет отказ для неподдержанного MIME
func TestValidateAvatarUploadRejectsUnsupportedMIME(t *testing.T) {
	req := avatarUploadRequest(t, "user@example.com", "file", "avatar.gif", "image/gif", []byte("GIF89a"))
	rec := httptest.NewRecorder()

	_, err := ValidateAvatarUploadRequest(rec, req)
	assertValidationError(t, err, http.StatusBadRequest, "Invalid file format")
}

// TestValidateAvatarUploadRejectsMIMEMagicMismatch проверяет совпадение MIME и magic bytes
func TestValidateAvatarUploadRejectsMIMEMagicMismatch(t *testing.T) {
	req := avatarUploadRequest(t, "user@example.com", "file", "avatar.png", "image/png", jpegBytes())
	rec := httptest.NewRecorder()

	_, err := ValidateAvatarUploadRequest(rec, req)
	assertValidationError(t, err, http.StatusBadRequest, "Invalid file format")
}

// TestValidateAvatarUploadRejectsInvalidMagicBytes проверяет отказ для неверных magic bytes
func TestValidateAvatarUploadRejectsInvalidMagicBytes(t *testing.T) {
	req := avatarUploadRequest(t, "user@example.com", "file", "avatar.jpg", "image/jpeg", []byte("not-image"))
	rec := httptest.NewRecorder()

	_, err := ValidateAvatarUploadRequest(rec, req)
	assertValidationError(t, err, http.StatusBadRequest, "Invalid file format")
}

// TestValidateAvatarUploadRejectsTooLargeFile проверяет отказ для файла больше 10MB
func TestValidateAvatarUploadRejectsTooLargeFile(t *testing.T) {
	data := append(jpegBytes(), bytes.Repeat([]byte{0}, int(MaxAvatarFileSize))...)
	req := avatarUploadRequest(t, "user@example.com", "file", "avatar.jpg", "image/jpeg", data)
	rec := httptest.NewRecorder()

	_, err := ValidateAvatarUploadRequest(rec, req)
	assertValidationError(t, err, http.StatusRequestEntityTooLarge, "File too large")
}

// TestValidateAvatarUploadRejectsTooLargeBodyBeforeParse проверяет ограничение всего multipart body
func TestValidateAvatarUploadRejectsTooLargeBodyBeforeParse(t *testing.T) {
	data := append(jpegBytes(), bytes.Repeat([]byte{0}, int(avatarUploadBodyLimit))...)
	req := avatarUploadRequest(t, "user@example.com", "file", "avatar.jpg", "image/jpeg", data)
	rec := httptest.NewRecorder()

	_, err := ValidateAvatarUploadRequest(rec, req)
	assertValidationError(t, err, http.StatusRequestEntityTooLarge, "File too large")
}

// avatarUploadRequest создает multipart request для тестов загрузки avatar
func avatarUploadRequest(t *testing.T, userEmail string, fieldName string, fileName string, contentType string, data []byte) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="` + fieldName + `"; filename="` + fileName + `"`},
		"Content-Type":        {contentType},
	})
	if err != nil {
		t.Fatalf("create multipart part: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write multipart part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if userEmail != "" {
		req.Header.Set("X-User-ID", userEmail)
	}
	return req
}

// assertValidationError проверяет статус и сообщение ошибки валидации
func assertValidationError(t *testing.T, err error, statusCode int, message string) {
	t.Helper()

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if validationErr.StatusCode != statusCode {
		t.Fatalf("StatusCode = %d, want %d", validationErr.StatusCode, statusCode)
	}
	if validationErr.Message != message {
		t.Fatalf("Message = %q, want %q", validationErr.Message, message)
	}
}

// jpegBytes возвращает минимальный набор bytes с JPEG magic
func jpegBytes() []byte {
	return []byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43, 0x00}
}

// pngBytes возвращает минимальный набор bytes с PNG magic
func pngBytes() []byte {
	return []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}
}

// webpBytes возвращает минимальный набор bytes с WebP magic
func webpBytes() []byte {
	return []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'}
}
