package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
	storages3 "github.com/squaredbusinessman/GophProfile/internal/storage/s3"
)

const avatarPublishAttempts = 3

type UserResolver interface {
	FindOrCreateUserByEmail(ctx context.Context, email string, now time.Time) (user.User, error)
}

type AvatarStore interface {
	CreateAvatar(ctx context.Context, item avatar.Avatar) error
	UpdateAvatarStatus(ctx context.Context, id string, status avatar.Status, updatedAt time.Time) error
}

type ObjectStore interface {
	Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
}

type EventPublisher interface {
	Publish(ctx context.Context, topic string, key string, payload []byte) error
}

type AvatarUploadService struct {
	users     UserResolver
	avatars   AvatarStore
	objects   ObjectStore
	publisher EventPublisher
	now       func() time.Time
}

type AvatarUploadRequest struct {
	UserEmail   string
	FileName    string
	ContentType string
	Size        int64
	Width       int
	Height      int
	Reader      io.Reader
}

type AvatarUploadResult struct {
	ID                string
	UserID            string
	Email             string
	FileName          string
	ContentType       string
	Size              int64
	Width             int
	Height            int
	Status            avatar.Status
	OriginalObjectKey string
	CreatedAt         time.Time
}

type AvatarProcessEvent struct {
	AvatarID          string `json:"avatar_id"`
	UserID            string `json:"user_id"`
	Email             string `json:"email"`
	OriginalObjectKey string `json:"original_object_key"`
	Thumb100ObjectKey string `json:"thumb_100_object_key"`
	Thumb300ObjectKey string `json:"thumb_300_object_key"`
	ContentType       string `json:"content_type"`
}

// NewAvatarUploadService создает service загрузки avatar
func NewAvatarUploadService(users UserResolver, avatars AvatarStore, objects ObjectStore, publisher EventPublisher) *AvatarUploadService {
	return &AvatarUploadService{
		users:     users,
		avatars:   avatars,
		objects:   objects,
		publisher: publisher,
		now:       time.Now,
	}
}

// UploadAvatar сохраняет original в S3 создает запись в БД и публикует задачу обработки
func (s *AvatarUploadService) UploadAvatar(ctx context.Context, req AvatarUploadRequest) (AvatarUploadResult, error) {
	if s.users == nil || s.avatars == nil || s.objects == nil || s.publisher == nil {
		return AvatarUploadResult{}, fmt.Errorf("avatar upload service is not configured")
	}

	now := s.now().UTC()
	owner, err := s.users.FindOrCreateUserByEmail(ctx, req.UserEmail, now)
	if err != nil {
		return AvatarUploadResult{}, fmt.Errorf("resolve user: %w", err)
	}

	avatarID := uuid.NewString()
	originalKey := storages3.OriginalObjectKey(owner.ID, avatarID)
	thumb100Key := storages3.Thumb100ObjectKey(owner.ID, avatarID)
	thumb300Key := storages3.Thumb300ObjectKey(owner.ID, avatarID)

	body, err := io.ReadAll(req.Reader)
	if err != nil {
		return AvatarUploadResult{}, fmt.Errorf("read avatar body: %w", err)
	}
	size := int64(len(body))
	if req.Size > 0 {
		size = req.Size
	}

	if err := s.objects.Put(ctx, originalKey, bytes.NewReader(body), size, req.ContentType); err != nil {
		return AvatarUploadResult{}, fmt.Errorf("put original object: %w", err)
	}

	item := avatar.Avatar{
		ID:                avatarID,
		UserID:            owner.ID,
		FileName:          req.FileName,
		MimeType:          req.ContentType,
		SizeBytes:         size,
		Width:             intPtr(req.Width),
		Height:            intPtr(req.Height),
		Status:            avatar.StatusProcessing,
		OriginalObjectKey: originalKey,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.avatars.CreateAvatar(ctx, item); err != nil {
		return AvatarUploadResult{}, fmt.Errorf("create avatar metadata: %w", err)
	}

	event := AvatarProcessEvent{
		AvatarID:          avatarID,
		UserID:            owner.ID,
		Email:             owner.Email,
		OriginalObjectKey: originalKey,
		Thumb100ObjectKey: thumb100Key,
		Thumb300ObjectKey: thumb300Key,
		ContentType:       req.ContentType,
	}
	if err := s.publishProcessEvent(ctx, event); err != nil {
		_ = s.avatars.UpdateAvatarStatus(ctx, avatarID, avatar.StatusFailed, s.now().UTC())
		return AvatarUploadResult{}, fmt.Errorf("publish avatar process event: %w", err)
	}

	return AvatarUploadResult{
		ID:                avatarID,
		UserID:            owner.ID,
		Email:             owner.Email,
		FileName:          req.FileName,
		ContentType:       req.ContentType,
		Size:              size,
		Width:             req.Width,
		Height:            req.Height,
		Status:            avatar.StatusProcessing,
		OriginalObjectKey: originalKey,
		CreatedAt:         now,
	}, nil
}

// publishProcessEvent публикует Kafka-событие с коротким retry для MVP
func (s *AvatarUploadService) publishProcessEvent(ctx context.Context, event AvatarProcessEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt < avatarPublishAttempts; attempt++ {
		if err := s.publisher.Publish(ctx, queuekafka.TopicAvatarProcess, event.AvatarID, payload); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

// intPtr возвращает указатель на положительный int или nil
func intPtr(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}
