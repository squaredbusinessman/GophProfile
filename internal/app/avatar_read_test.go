package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	storages3 "github.com/squaredbusinessman/GophProfile/internal/storage/s3"
)

// TestGetAvatarByIDReturnsOriginal проверяет выдачу original сразу после upload
func TestGetAvatarByIDReturnsOriginal(t *testing.T) {
	repo := &fakeAvatarReadRepository{
		item: avatar.Avatar{
			ID:                "avatar-1",
			UserID:            "user-1",
			MimeType:          "image/jpeg",
			Status:            avatar.StatusProcessing,
			OriginalObjectKey: "avatars/user-1/avatar-1/original",
		},
	}
	objects := &fakeAvatarObjectReader{
		body: []byte("original"),
		metadata: storages3.ObjectMetadata{
			ContentType: "image/jpeg",
			ETag:        "etag-original",
			Size:        8,
		},
	}
	service := NewAvatarReadService(repo, objects)

	result, err := service.GetAvatarByID(context.Background(), "avatar-1", "original", "jpeg")
	if err != nil {
		t.Fatalf("GetAvatarByID returned error: %v", err)
	}
	defer result.Body.Close()

	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "original" {
		t.Fatalf("body = %q, want original", string(body))
	}
	if objects.key != "avatars/user-1/avatar-1/original" {
		t.Fatalf("key = %q, want original key", objects.key)
	}
	if result.ContentType != "image/jpeg" || result.ETag != "etag-original" {
		t.Fatalf("result = %#v, want metadata from object", result)
	}
}

// TestGetAvatarByIDReturnsProcessingForMissingThumbnail проверяет статус обработки thumbnails
func TestGetAvatarByIDReturnsProcessingForMissingThumbnail(t *testing.T) {
	service := NewAvatarReadService(&fakeAvatarReadRepository{
		item: avatar.Avatar{
			ID:                "avatar-1",
			UserID:            "user-1",
			MimeType:          "image/png",
			Status:            avatar.StatusProcessing,
			OriginalObjectKey: "avatars/user-1/avatar-1/original",
		},
	}, &fakeAvatarObjectReader{})

	_, err := service.GetAvatarByID(context.Background(), "avatar-1", "100x100", "")
	if !errors.Is(err, ErrAvatarProcessing) {
		t.Fatalf("error = %v, want ErrAvatarProcessing", err)
	}
}

// TestGetAvatarByIDRejectsUnsupportedFormatConversion проверяет отказ от конвертации
func TestGetAvatarByIDRejectsUnsupportedFormatConversion(t *testing.T) {
	service := NewAvatarReadService(&fakeAvatarReadRepository{
		item: avatar.Avatar{
			ID:                "avatar-1",
			UserID:            "user-1",
			MimeType:          "image/jpeg",
			Status:            avatar.StatusReady,
			OriginalObjectKey: "avatars/user-1/avatar-1/original",
		},
	}, &fakeAvatarObjectReader{})

	_, err := service.GetAvatarByID(context.Background(), "avatar-1", "original", "webp")
	if !errors.Is(err, ErrUnsupportedAvatarFormat) {
		t.Fatalf("error = %v, want ErrUnsupportedAvatarFormat", err)
	}
}

// TestGetLatestAvatarByUserIDUsesLatestActiveAvatar проверяет выбор последней активной avatar пользователя
func TestGetLatestAvatarByUserIDUsesLatestActiveAvatar(t *testing.T) {
	thumb100 := "avatars/user-1/avatar-1/100x100"
	service := NewAvatarReadService(&fakeAvatarReadRepository{
		items: []avatar.Avatar{
			{
				ID:                "avatar-1",
				UserID:            "user-1",
				MimeType:          "image/png",
				Status:            avatar.StatusReady,
				OriginalObjectKey: "avatars/user-1/avatar-1/original",
				Thumb100ObjectKey: &thumb100,
			},
		},
	}, &fakeAvatarObjectReader{
		body: []byte("thumb"),
		metadata: storages3.ObjectMetadata{
			ContentType: "image/png",
			Size:        5,
		},
	})

	result, err := service.GetLatestAvatarByUserID(context.Background(), "user-1", "100x100", "png")
	if err != nil {
		t.Fatalf("GetLatestAvatarByUserID returned error: %v", err)
	}
	defer result.Body.Close()
	if result.ContentType != "image/png" {
		t.Fatalf("ContentType = %q, want image/png", result.ContentType)
	}
}

type fakeAvatarReadRepository struct {
	item  avatar.Avatar
	items []avatar.Avatar
	err   error
}

// GetAvatar возвращает fake avatar по id
func (f *fakeAvatarReadRepository) GetAvatar(ctx context.Context, id string) (avatar.Avatar, error) {
	if f.err != nil {
		return avatar.Avatar{}, f.err
	}
	if f.item.ID == "" {
		return avatar.Avatar{}, avatar.ErrNotFound
	}
	return f.item, nil
}

// ListAvatarsByUser возвращает fake список avatar пользователя
func (f *fakeAvatarReadRepository) ListAvatarsByUser(ctx context.Context, userID string, limit int, offset int) ([]avatar.Avatar, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

type fakeAvatarObjectReader struct {
	key      string
	body     []byte
	metadata storages3.ObjectMetadata
	err      error
}

// Get возвращает fake stream avatar object
func (f *fakeAvatarObjectReader) Get(ctx context.Context, key string) (io.ReadCloser, storages3.ObjectMetadata, error) {
	f.key = key
	if f.err != nil {
		return nil, storages3.ObjectMetadata{}, f.err
	}
	return io.NopCloser(bytes.NewReader(f.body)), f.metadata, nil
}
