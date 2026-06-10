package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
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
	defer func() {
		_ = result.Body.Close()
	}()

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
	defer func() {
		_ = result.Body.Close()
	}()
	if result.ContentType != "image/png" {
		t.Fatalf("ContentType = %q, want image/png", result.ContentType)
	}
}

// TestGetLatestAvatarByEmailResolvesUserID проверяет сопоставление email с user_id
func TestGetLatestAvatarByEmailResolvesUserID(t *testing.T) {
	service := NewAvatarReadServiceWithUsers(
		&fakeUserEmailLookup{item: user.User{ID: "user-1", Email: "user@example.com"}},
		&fakeAvatarReadRepository{
			items: []avatar.Avatar{
				{
					ID:                "avatar-1",
					UserID:            "user-1",
					MimeType:          "image/png",
					Status:            avatar.StatusReady,
					OriginalObjectKey: "avatars/user-1/avatar-1/original",
				},
			},
		},
		&fakeAvatarObjectReader{
			body: []byte("original"),
			metadata: storages3.ObjectMetadata{
				ContentType: "image/png",
				Size:        8,
			},
		},
	)

	result, err := service.GetLatestAvatarByEmail(context.Background(), "user@example.com", "original", "png")
	if err != nil {
		t.Fatalf("GetLatestAvatarByEmail returned error: %v", err)
	}
	defer func() {
		_ = result.Body.Close()
	}()
	if result.ContentType != "image/png" {
		t.Fatalf("ContentType = %q, want image/png", result.ContentType)
	}
}

// TestGetLatestAvatarByEmailMapsMissingUser проверяет отсутствие пользователя по email
func TestGetLatestAvatarByEmailMapsMissingUser(t *testing.T) {
	service := NewAvatarReadServiceWithUsers(
		&fakeUserEmailLookup{err: user.ErrNotFound},
		&fakeAvatarReadRepository{},
		&fakeAvatarObjectReader{},
	)

	_, err := service.GetLatestAvatarByEmail(context.Background(), "missing@example.com", "original", "")
	if !errors.Is(err, ErrAvatarNotFound) {
		t.Fatalf("error = %v, want ErrAvatarNotFound", err)
	}
}

// TestGetAvatarMetadataMapsNotFound проверяет ошибку отсутствующей avatar
func TestGetAvatarMetadataMapsNotFound(t *testing.T) {
	service := NewAvatarReadService(&fakeAvatarReadRepository{
		err: avatar.ErrNotFound,
	}, &fakeAvatarObjectReader{})

	_, err := service.GetAvatarMetadata(context.Background(), "missing")
	if !errors.Is(err, ErrAvatarNotFound) {
		t.Fatalf("error = %v, want ErrAvatarNotFound", err)
	}
}

// TestListAvatarsByUserIDPassesPagination проверяет передачу pagination в repository
func TestListAvatarsByUserIDPassesPagination(t *testing.T) {
	repo := &fakeAvatarReadRepository{
		items: []avatar.Avatar{
			{
				ID:                "avatar-1",
				UserID:            "user-1",
				MimeType:          "image/png",
				Status:            avatar.StatusFailed,
				OriginalObjectKey: "avatars/user-1/avatar-1/original",
			},
		},
	}
	service := NewAvatarReadService(repo, &fakeAvatarObjectReader{})

	result, err := service.ListAvatarsByUserID(context.Background(), "user-1", 25, 5)
	if err != nil {
		t.Fatalf("ListAvatarsByUserID returned error: %v", err)
	}
	if repo.userID != "user-1" || repo.limit != 25 || repo.offset != 5 {
		t.Fatalf("repo args = %q %d %d, want user-1 25 5", repo.userID, repo.limit, repo.offset)
	}
	if result.Limit != 25 || result.Offset != 5 || len(result.Items) != 1 {
		t.Fatalf("result = %#v, want pagination and one item", result)
	}
}

type fakeAvatarReadRepository struct {
	item   avatar.Avatar
	items  []avatar.Avatar
	userID string
	limit  int
	offset int
	err    error
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
	f.userID = userID
	f.limit = limit
	f.offset = offset
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

type fakeUserEmailLookup struct {
	item user.User
	err  error
}

// GetUserByEmail возвращает fake пользователя по email
func (f *fakeUserEmailLookup) GetUserByEmail(ctx context.Context, email string) (user.User, error) {
	if f.err != nil {
		return user.User{}, f.err
	}
	return f.item, nil
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
