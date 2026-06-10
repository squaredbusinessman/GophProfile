package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
	storages3 "github.com/squaredbusinessman/GophProfile/internal/storage/s3"
)

var (
	ErrAvatarNotFound          = errors.New("avatar not found")
	ErrAvatarProcessing        = errors.New("avatar is still processing")
	ErrUnsupportedAvatarSize   = errors.New("unsupported avatar size")
	ErrUnsupportedAvatarFormat = errors.New("unsupported avatar format")
)

type AvatarReadRepository interface {
	GetAvatar(ctx context.Context, id string) (avatar.Avatar, error)
	ListAvatarsByUser(ctx context.Context, userID string, limit int, offset int) ([]avatar.Avatar, error)
}

type UserEmailLookup interface {
	GetUserByEmail(ctx context.Context, email string) (user.User, error)
}

type AvatarObjectReader interface {
	Get(ctx context.Context, key string) (io.ReadCloser, storages3.ObjectMetadata, error)
}

type AvatarReadService struct {
	users   UserEmailLookup
	avatars AvatarReadRepository
	objects AvatarObjectReader
}

type AvatarReadResult struct {
	Body        io.ReadCloser
	ContentType string
	ETag        string
	Size        int64
}

type AvatarMetadataResult struct {
	Avatar avatar.Avatar
}

type AvatarListResult struct {
	Items  []avatar.Avatar
	Limit  int
	Offset int
}

// NewAvatarReadService создает service чтения avatar
func NewAvatarReadService(avatars AvatarReadRepository, objects AvatarObjectReader) *AvatarReadService {
	return &AvatarReadService{
		avatars: avatars,
		objects: objects,
	}
}

// NewAvatarReadServiceWithUsers создает service чтения avatar с поиском пользователя по email
func NewAvatarReadServiceWithUsers(users UserEmailLookup, avatars AvatarReadRepository, objects AvatarObjectReader) *AvatarReadService {
	return &AvatarReadService{
		users:   users,
		avatars: avatars,
		objects: objects,
	}
}

// GetAvatarByID возвращает stream avatar object по avatar id
func (s *AvatarReadService) GetAvatarByID(ctx context.Context, avatarID string, size string, format string) (AvatarReadResult, error) {
	item, err := s.avatars.GetAvatar(ctx, avatarID)
	if err != nil {
		if errors.Is(err, avatar.ErrNotFound) {
			return AvatarReadResult{}, ErrAvatarNotFound
		}
		return AvatarReadResult{}, fmt.Errorf("get avatar metadata: %w", err)
	}

	return s.getAvatarObject(ctx, item, size, format)
}

// GetLatestAvatarByUserID возвращает stream последней активной avatar пользователя
func (s *AvatarReadService) GetLatestAvatarByUserID(ctx context.Context, userID string, size string, format string) (AvatarReadResult, error) {
	items, err := s.avatars.ListAvatarsByUser(ctx, userID, 1, 0)
	if err != nil {
		return AvatarReadResult{}, fmt.Errorf("list avatars by user: %w", err)
	}
	if len(items) == 0 {
		return AvatarReadResult{}, ErrAvatarNotFound
	}

	return s.getAvatarObject(ctx, items[0], size, format)
}

// GetLatestAvatarByEmail возвращает stream последней активной avatar через сопоставление email с user_id
func (s *AvatarReadService) GetLatestAvatarByEmail(ctx context.Context, email string, size string, format string) (AvatarReadResult, error) {
	if s.users == nil {
		return AvatarReadResult{}, fmt.Errorf("user email lookup is not configured")
	}

	owner, err := s.users.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return AvatarReadResult{}, ErrAvatarNotFound
		}
		return AvatarReadResult{}, fmt.Errorf("get user by email: %w", err)
	}

	return s.GetLatestAvatarByUserID(ctx, owner.ID, size, format)
}

// GetAvatarMetadata возвращает metadata активной avatar по id
func (s *AvatarReadService) GetAvatarMetadata(ctx context.Context, avatarID string) (AvatarMetadataResult, error) {
	item, err := s.avatars.GetAvatar(ctx, avatarID)
	if err != nil {
		if errors.Is(err, avatar.ErrNotFound) {
			return AvatarMetadataResult{}, ErrAvatarNotFound
		}
		return AvatarMetadataResult{}, fmt.Errorf("get avatar metadata: %w", err)
	}

	return AvatarMetadataResult{Avatar: item}, nil
}

// ListAvatarsByUserID возвращает активные avatar пользователя
func (s *AvatarReadService) ListAvatarsByUserID(ctx context.Context, userID string, limit int, offset int) (AvatarListResult, error) {
	items, err := s.avatars.ListAvatarsByUser(ctx, userID, limit, offset)
	if err != nil {
		return AvatarListResult{}, fmt.Errorf("list avatars by user: %w", err)
	}

	return AvatarListResult{
		Items:  items,
		Limit:  limit,
		Offset: offset,
	}, nil
}

// getAvatarObject выбирает object key по size и возвращает stream из S3
func (s *AvatarReadService) getAvatarObject(ctx context.Context, item avatar.Avatar, size string, format string) (AvatarReadResult, error) {
	key, contentType, err := avatarObjectSelection(item, size)
	if err != nil {
		return AvatarReadResult{}, err
	}
	if err := ensureFormatSupported(contentType, format); err != nil {
		return AvatarReadResult{}, err
	}

	body, metadata, err := s.objects.Get(ctx, key)
	if err != nil {
		if errors.Is(err, storages3.ErrNotFound) {
			return AvatarReadResult{}, ErrAvatarNotFound
		}
		return AvatarReadResult{}, fmt.Errorf("get avatar object: %w", err)
	}

	if metadata.ContentType != "" {
		contentType = metadata.ContentType
	}

	return AvatarReadResult{
		Body:        body,
		ContentType: contentType,
		ETag:        metadata.ETag,
		Size:        metadata.Size,
	}, nil
}

// avatarObjectSelection выбирает S3 object key и ожидаемый MIME по size
func avatarObjectSelection(item avatar.Avatar, size string) (string, string, error) {
	normalizedSize := strings.TrimSpace(strings.ToLower(size))
	if normalizedSize == "" {
		normalizedSize = "original"
	}

	switch normalizedSize {
	case "original":
		return item.OriginalObjectKey, item.MimeType, nil
	case "100x100":
		if item.Thumb100ObjectKey == nil || *item.Thumb100ObjectKey == "" {
			return "", "", ErrAvatarProcessing
		}
		return *item.Thumb100ObjectKey, "image/png", nil
	case "300x300":
		if item.Thumb300ObjectKey == nil || *item.Thumb300ObjectKey == "" {
			return "", "", ErrAvatarProcessing
		}
		return *item.Thumb300ObjectKey, "image/png", nil
	default:
		return "", "", ErrUnsupportedAvatarSize
	}
}

// ensureFormatSupported проверяет что запрошенный format совпадает с сохраненным MIME
func ensureFormatSupported(contentType string, format string) error {
	normalized := strings.TrimSpace(strings.ToLower(format))
	if normalized == "" {
		return nil
	}

	expected, ok := map[string]string{
		"jpeg": "image/jpeg",
		"jpg":  "image/jpeg",
		"png":  "image/png",
		"webp": "image/webp",
	}[normalized]
	if !ok {
		return ErrUnsupportedAvatarFormat
	}
	if expected != contentType {
		return ErrUnsupportedAvatarFormat
	}
	return nil
}
