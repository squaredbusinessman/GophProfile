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
	// ErrAvatarNotFound сообщает об отсутствии доступного аватара
	ErrAvatarNotFound = errors.New("avatar not found")
	// ErrAvatarProcessing сообщает, что обработка аватара ещё не завершена
	ErrAvatarProcessing = errors.New("avatar is still processing")
	// ErrUnsupportedAvatarSize сообщает о неподдерживаемом размере изображения
	ErrUnsupportedAvatarSize = errors.New("unsupported avatar size")
	// ErrUnsupportedAvatarFormat сообщает о неподдерживаемом формате изображения
	ErrUnsupportedAvatarFormat = errors.New("unsupported avatar format")
)

// AvatarReadRepository описывает чтение метаданных аватаров
type AvatarReadRepository interface {
	// GetAvatar возвращает активный аватар по идентификатору.
	GetAvatar(ctx context.Context, id string) (avatar.Avatar, error)
	// ListAvatarsByUser возвращает страницу активных аватаров пользователя.
	ListAvatarsByUser(ctx context.Context, userID string, limit int, offset int) ([]avatar.Avatar, error)
}

// UserEmailLookup описывает поиск пользователя по адресу электронной почты
type UserEmailLookup interface {
	// GetUserByEmail возвращает активного пользователя по нормализованному email.
	GetUserByEmail(ctx context.Context, email string) (user.User, error)
}

// AvatarObjectReader описывает чтение объекта аватара
type AvatarObjectReader interface {
	// Get открывает объект аватара и возвращает поток вместе с HTTP-метаданными.
	Get(ctx context.Context, key string) (io.ReadCloser, storages3.ObjectMetadata, error)
}

// AvatarReadService предоставляет чтение объектов и метаданных аватаров
type AvatarReadService struct {
	users   UserEmailLookup
	avatars AvatarReadRepository
	objects AvatarObjectReader
}

// AvatarReadResult содержит поток объекта и его HTTP-метаданные
type AvatarReadResult struct {
	// Body предоставляет содержимое объекта
	Body io.ReadCloser
	// ContentType содержит MIME-тип объекта
	ContentType string
	// ETag содержит идентификатор версии объекта
	ETag string
	// Size содержит размер объекта в байтах
	Size int64
}

// AvatarMetadataResult содержит метаданные аватара
type AvatarMetadataResult struct {
	// Avatar содержит доменную модель аватара
	Avatar avatar.Avatar
}

// AvatarListResult содержит страницу аватаров пользователя
type AvatarListResult struct {
	// Items содержит аватары текущей страницы
	Items []avatar.Avatar
	// Limit содержит максимальное число элементов страницы
	Limit int
	// Offset содержит смещение страницы
	Offset int
}

// NewAvatarReadService создаёт сервис чтения аватаров
func NewAvatarReadService(avatars AvatarReadRepository, objects AvatarObjectReader) *AvatarReadService {
	return &AvatarReadService{
		avatars: avatars,
		objects: objects,
	}
}

// NewAvatarReadServiceWithUsers создаёт сервис чтения аватаров с поиском пользователя по электронной почте
func NewAvatarReadServiceWithUsers(users UserEmailLookup, avatars AvatarReadRepository, objects AvatarObjectReader) *AvatarReadService {
	return &AvatarReadService{
		users:   users,
		avatars: avatars,
		objects: objects,
	}
}

// GetAvatarByID возвращает поток объекта аватара по его идентификатору
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

// GetLatestAvatarByUserID возвращает поток последнего активного аватара пользователя
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

// GetLatestAvatarByEmail возвращает поток последнего активного аватара по электронной почте пользователя
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

// GetAvatarMetadata возвращает метаданные активного аватара по идентификатору
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

// ListAvatarsByUserID возвращает активные аватары пользователя
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

// getAvatarObject выбирает ключ объекта по размеру и возвращает поток из S3
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

// avatarObjectSelection выбирает ключ объекта S3 и ожидаемый MIME-тип по размеру
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

// ensureFormatSupported проверяет, что запрошенный формат совпадает с сохранённым MIME-типом
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
