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
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
	storages3 "github.com/squaredbusinessman/GophProfile/internal/storage/s3"
)

type UserResolver interface {
	FindOrCreateUserByEmail(ctx context.Context, email string, now time.Time) (user.User, error)
}

type AvatarOutboxStore interface {
	CreateAvatarWithOutbox(ctx context.Context, item avatar.Avatar, event outbox.Event) error
	MarkOutboxPublished(ctx context.Context, id string, publishedAt time.Time) error
	MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) error
}

type ObjectStore interface {
	Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
}

type EventPublisher interface {
	Publish(ctx context.Context, topic string, key string, payload []byte) error
}

type AvatarUploadService struct {
	users        UserResolver
	avatarOutbox AvatarOutboxStore
	objects      ObjectStore
	publisher    EventPublisher
	now          func() time.Time
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
func NewAvatarUploadService(users UserResolver, avatarOutbox AvatarOutboxStore, objects ObjectStore, publisher EventPublisher) *AvatarUploadService {
	return &AvatarUploadService{
		users:        users,
		avatarOutbox: avatarOutbox,
		objects:      objects,
		publisher:    publisher,
		now:          time.Now,
	}
}

// UploadAvatar сохраняет original в S3 и атомарно создает avatar с outbox событием
func (s *AvatarUploadService) UploadAvatar(ctx context.Context, req AvatarUploadRequest) (AvatarUploadResult, error) {
	if s.users == nil || s.avatarOutbox == nil || s.objects == nil || s.publisher == nil {
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
	event := AvatarProcessEvent{
		AvatarID:          avatarID,
		UserID:            owner.ID,
		Email:             owner.Email,
		OriginalObjectKey: originalKey,
		Thumb100ObjectKey: thumb100Key,
		Thumb300ObjectKey: thumb300Key,
		ContentType:       req.ContentType,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return AvatarUploadResult{}, fmt.Errorf("marshal avatar process event: %w", err)
	}

	outboxEvent := outbox.Event{
		ID:        uuid.NewString(),
		Topic:     queuekafka.TopicAvatarProcess,
		Key:       avatarID,
		Payload:   payload,
		Status:    outbox.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.avatarOutbox.CreateAvatarWithOutbox(ctx, item, outboxEvent); err != nil {
		return AvatarUploadResult{}, fmt.Errorf("create avatar metadata and outbox event: %w", err)
	}

	s.publishOutboxEvent(ctx, outboxEvent)

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

// publishOutboxEvent пытается быстро опубликовать outbox событие после commit
func (s *AvatarUploadService) publishOutboxEvent(ctx context.Context, event outbox.Event) {
	if err := s.publisher.Publish(ctx, event.Topic, event.Key, event.Payload); err != nil {
		_ = s.avatarOutbox.MarkOutboxPublishAttemptFailed(ctx, event.ID, err, s.now().UTC())
		return
	}

	_ = s.avatarOutbox.MarkOutboxPublished(ctx, event.ID, s.now().UTC())
}

// intPtr возвращает указатель на положительный int или nil
func intPtr(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}
