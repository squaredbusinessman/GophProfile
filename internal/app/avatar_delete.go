package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
)

var ErrAvatarForbidden = errors.New("avatar belongs to another user")

type AvatarDeleteRepository interface {
	GetAvatarIncludingDeleted(ctx context.Context, id string) (avatar.Avatar, error)
	ListAvatarsByUser(ctx context.Context, userID string, limit int, offset int) ([]avatar.Avatar, error)
}

type AvatarDeleteOutboxStore interface {
	SoftDeleteAvatarWithOutbox(ctx context.Context, id string, userID string, deletedAt time.Time, event outbox.Event) error
	MarkOutboxPublished(ctx context.Context, id string, publishedAt time.Time) error
	MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) error
}

type AvatarDeleteWorkerRepository interface {
	GetAvatarIncludingDeleted(ctx context.Context, id string) (avatar.Avatar, error)
	MarkAvatarDeleted(ctx context.Context, id string, updatedAt time.Time) error
}

type AvatarDeleteObjectStore interface {
	Delete(ctx context.Context, key string) error
}

type AvatarDeleteService struct {
	avatars  AvatarDeleteRepository
	outbox   AvatarDeleteOutboxStore
	producer EventPublisher
	now      func() time.Time
}

type AvatarDeleteWorkerService struct {
	avatars AvatarDeleteWorkerRepository
	objects AvatarDeleteObjectStore
	now     func() time.Time
}

type AvatarDeleteEvent struct {
	AvatarID string `json:"avatar_id"`
	UserID   string `json:"user_id"`
}

// NewAvatarDeleteService создает service удаления avatar через outbox
func NewAvatarDeleteService(avatars AvatarDeleteRepository, outbox AvatarDeleteOutboxStore, producer EventPublisher) *AvatarDeleteService {
	return &AvatarDeleteService{
		avatars:  avatars,
		outbox:   outbox,
		producer: producer,
		now:      time.Now,
	}
}

// NewAvatarDeleteWorkerService создает worker service удаления S3 objects
func NewAvatarDeleteWorkerService(avatars AvatarDeleteWorkerRepository, objects AvatarDeleteObjectStore) *AvatarDeleteWorkerService {
	return &AvatarDeleteWorkerService{
		avatars: avatars,
		objects: objects,
		now:     time.Now,
	}
}

// DeleteAvatarByID мягко удаляет avatar по id если requester является владельцем
func (s *AvatarDeleteService) DeleteAvatarByID(ctx context.Context, avatarID string, requesterUserID string) error {
	if s.avatars == nil || s.outbox == nil || s.producer == nil {
		return fmt.Errorf("avatar delete service is not configured")
	}

	item, err := s.avatars.GetAvatarIncludingDeleted(ctx, avatarID)
	if err != nil {
		if errors.Is(err, avatar.ErrNotFound) {
			return ErrAvatarNotFound
		}
		return fmt.Errorf("get avatar for delete: %w", err)
	}

	return s.deleteAvatar(ctx, item, requesterUserID)
}

// DeleteLatestAvatarByUserID мягко удаляет последнюю активную avatar пользователя
func (s *AvatarDeleteService) DeleteLatestAvatarByUserID(ctx context.Context, targetUserID string, requesterUserID string) error {
	if s.avatars == nil || s.outbox == nil || s.producer == nil {
		return fmt.Errorf("avatar delete service is not configured")
	}
	if targetUserID != requesterUserID {
		return ErrAvatarForbidden
	}

	items, err := s.avatars.ListAvatarsByUser(ctx, targetUserID, 1, 0)
	if err != nil {
		return fmt.Errorf("list avatars for delete: %w", err)
	}
	if len(items) == 0 {
		return nil
	}

	return s.deleteAvatar(ctx, items[0], requesterUserID)
}

// HandleDeleteMessage обрабатывает Kafka payload avatar.delete
func (s *AvatarDeleteWorkerService) HandleDeleteMessage(ctx context.Context, payload []byte) error {
	var message AvatarDeleteEvent
	if err := json.Unmarshal(payload, &message); err != nil {
		return fmt.Errorf("decode avatar delete message: %w", err)
	}
	if message.AvatarID == "" {
		return fmt.Errorf("avatar delete message missing avatar_id")
	}
	if s.avatars == nil || s.objects == nil {
		return fmt.Errorf("avatar delete worker is not configured")
	}

	item, err := s.avatars.GetAvatarIncludingDeleted(ctx, message.AvatarID)
	if err != nil {
		if errors.Is(err, avatar.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("get avatar for object delete: %w", err)
	}
	if item.Status == avatar.StatusDeleted {
		return nil
	}

	for _, key := range item.ObjectKeys() {
		if err := s.objects.Delete(ctx, key); err != nil {
			return fmt.Errorf("delete avatar object %s: %w", key, err)
		}
	}

	if err := s.avatars.MarkAvatarDeleted(ctx, item.ID, s.now().UTC()); err != nil {
		return fmt.Errorf("mark avatar deleted: %w", err)
	}
	return nil
}

// deleteAvatar выполняет owner check и создает delete outbox событие
func (s *AvatarDeleteService) deleteAvatar(ctx context.Context, item avatar.Avatar, requesterUserID string) error {
	if item.UserID != requesterUserID {
		return ErrAvatarForbidden
	}
	if item.DeletedAt != nil || item.Status == avatar.StatusDeleting || item.Status == avatar.StatusDeleted {
		return nil
	}

	now := s.now().UTC()
	eventPayload, err := json.Marshal(AvatarDeleteEvent{
		AvatarID: item.ID,
		UserID:   item.UserID,
	})
	if err != nil {
		return fmt.Errorf("marshal avatar delete event: %w", err)
	}

	event := outbox.Event{
		ID:        uuid.NewString(),
		Topic:     queuekafka.TopicAvatarDelete,
		Key:       item.ID,
		Payload:   eventPayload,
		Status:    outbox.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.outbox.SoftDeleteAvatarWithOutbox(ctx, item.ID, item.UserID, now, event); err != nil {
		if errors.Is(err, avatar.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("soft delete avatar with outbox: %w", err)
	}

	s.publishOutboxEvent(ctx, event)
	return nil
}

// publishOutboxEvent пытается быстро опубликовать outbox событие после commit
func (s *AvatarDeleteService) publishOutboxEvent(ctx context.Context, event outbox.Event) {
	if err := s.producer.Publish(ctx, event.Topic, event.Key, event.Payload); err != nil {
		_ = s.outbox.MarkOutboxPublishAttemptFailed(ctx, event.ID, err, s.now().UTC())
		return
	}

	_ = s.outbox.MarkOutboxPublished(ctx, event.ID, s.now().UTC())
}
