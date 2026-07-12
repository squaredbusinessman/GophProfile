// Package app содержит прикладные сценарии сервера и фонового обработчика
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

// ErrAvatarForbidden сообщает о попытке удалить аватар другого пользователя
var ErrAvatarForbidden = errors.New("avatar belongs to another user")

// AvatarDeleteRepository описывает чтение аватаров для операции удаления
type AvatarDeleteRepository interface {
	// GetAvatarIncludingDeleted возвращает аватар по идентификатору, включая мягко удалённые записи.
	GetAvatarIncludingDeleted(ctx context.Context, id string) (avatar.Avatar, error)
	// ListAvatarsByUser возвращает страницу активных аватаров пользователя.
	ListAvatarsByUser(ctx context.Context, userID string, limit int, offset int) ([]avatar.Avatar, error)
}

// AvatarDeleteOutboxStore описывает атомарное удаление аватара и создание события outbox
type AvatarDeleteOutboxStore interface {
	// SoftDeleteAvatarWithOutbox помечает аватар удаляемым и создаёт событие удаления в одной транзакции.
	SoftDeleteAvatarWithOutbox(ctx context.Context, id string, userID string, deletedAt time.Time, event outbox.Event) error
	// MarkOutboxPublished отмечает событие outbox успешно опубликованным.
	MarkOutboxPublished(ctx context.Context, id string, publishedAt time.Time) error
	// MarkOutboxPublishAttemptFailed сохраняет ошибку публикации события outbox.
	MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) error
}

// AvatarDeleteWorkerRepository описывает доступ обработчика удаления к данным аватара
type AvatarDeleteWorkerRepository interface {
	// GetAvatarIncludingDeleted возвращает аватар по идентификатору, включая мягко удалённые записи.
	GetAvatarIncludingDeleted(ctx context.Context, id string) (avatar.Avatar, error)
	// MarkAvatarDeleted переводит аватар в состояние завершённого удаления объектов.
	MarkAvatarDeleted(ctx context.Context, id string, updatedAt time.Time) error
}

// AvatarDeleteObjectStore описывает удаление объектов аватара из хранилища
type AvatarDeleteObjectStore interface {
	// Delete удаляет объект по ключу и считает отсутствие объекта успешным результатом.
	Delete(ctx context.Context, key string) error
}

// AvatarDeleteService выполняет логическое удаление аватара через outbox
type AvatarDeleteService struct {
	avatars   AvatarDeleteRepository
	outbox    AvatarDeleteOutboxStore
	producer  EventPublisher
	now       func() time.Time
	telemetry businessTelemetry
}

// AvatarDeleteWorkerService удаляет объекты аватара и завершает его удаление
type AvatarDeleteWorkerService struct {
	avatars   AvatarDeleteWorkerRepository
	objects   AvatarDeleteObjectStore
	now       func() time.Time
	telemetry businessTelemetry
}

// AvatarDeleteEvent содержит данные события удаления аватара
type AvatarDeleteEvent struct {
	// AvatarID содержит идентификатор аватара
	AvatarID string `json:"avatar_id"`
	// UserID содержит идентификатор владельца аватара
	UserID string `json:"user_id"`
}

// NewAvatarDeleteService создаёт сервис удаления аватара через outbox
func NewAvatarDeleteService(avatars AvatarDeleteRepository, outbox AvatarDeleteOutboxStore, producer EventPublisher) (*AvatarDeleteService, error) {
	telemetry, err := newBusinessTelemetry()
	if err != nil {
		return nil, fmt.Errorf("create avatar delete telemetry: %w", err)
	}
	return &AvatarDeleteService{
		avatars:   avatars,
		outbox:    outbox,
		producer:  producer,
		now:       time.Now,
		telemetry: telemetry,
	}, nil
}

// NewAvatarDeleteWorkerService создаёт сервис фонового удаления объектов из S3
func NewAvatarDeleteWorkerService(avatars AvatarDeleteWorkerRepository, objects AvatarDeleteObjectStore) (*AvatarDeleteWorkerService, error) {
	telemetry, err := newBusinessTelemetry()
	if err != nil {
		return nil, fmt.Errorf("create avatar delete worker telemetry: %w", err)
	}
	return &AvatarDeleteWorkerService{
		avatars:   avatars,
		objects:   objects,
		now:       time.Now,
		telemetry: telemetry,
	}, nil
}

// DeleteAvatarByID логически удаляет аватар по идентификатору при совпадении владельца
func (s *AvatarDeleteService) DeleteAvatarByID(ctx context.Context, avatarID string, requesterUserID string) error {
	startedAt := time.Now()
	result := deleteResultError
	defer func() { s.telemetry.recordDelete(ctx, startedAt, deletePhaseRequest, result) }()

	if s.avatars == nil || s.outbox == nil || s.producer == nil {
		return fmt.Errorf("avatar delete service is not configured")
	}

	item, err := s.avatars.GetAvatarIncludingDeleted(ctx, avatarID)
	if err != nil {
		if errors.Is(err, avatar.ErrNotFound) {
			result = deleteResultRejected
			return ErrAvatarNotFound
		}
		return fmt.Errorf("get avatar for delete: %w", err)
	}

	result, err = s.deleteAvatar(ctx, item, requesterUserID)
	return err
}

// DeleteLatestAvatarByUserID логически удаляет последний активный аватар пользователя
func (s *AvatarDeleteService) DeleteLatestAvatarByUserID(ctx context.Context, targetUserID string, requesterUserID string) error {
	startedAt := time.Now()
	result := deleteResultError
	defer func() { s.telemetry.recordDelete(ctx, startedAt, deletePhaseRequest, result) }()

	if s.avatars == nil || s.outbox == nil || s.producer == nil {
		return fmt.Errorf("avatar delete service is not configured")
	}
	if targetUserID != requesterUserID {
		result = deleteResultRejected
		return ErrAvatarForbidden
	}

	items, err := s.avatars.ListAvatarsByUser(ctx, targetUserID, 1, 0)
	if err != nil {
		return fmt.Errorf("list avatars for delete: %w", err)
	}
	if len(items) == 0 {
		result = deleteResultIdempotentSkip
		return nil
	}

	result, err = s.deleteAvatar(ctx, items[0], requesterUserID)
	return err
}

// HandleDeleteMessage обрабатывает тело сообщения Kafka из темы avatar.delete
func (s *AvatarDeleteWorkerService) HandleDeleteMessage(ctx context.Context, payload []byte) error {
	startedAt := time.Now()
	result := deleteResultError
	defer func() { s.telemetry.recordDelete(ctx, startedAt, deletePhaseExecute, result) }()

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
			result = deleteResultIdempotentSkip
			return nil
		}
		return fmt.Errorf("get avatar for object delete: %w", err)
	}
	if item.Status == avatar.StatusDeleted {
		result = deleteResultIdempotentSkip
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
	result = deleteResultCompleted
	return nil
}

// deleteAvatar проверяет владельца и создаёт событие удаления в outbox
func (s *AvatarDeleteService) deleteAvatar(ctx context.Context, item avatar.Avatar, requesterUserID string) (string, error) {
	if item.UserID != requesterUserID {
		return deleteResultRejected, ErrAvatarForbidden
	}
	if item.DeletedAt != nil || item.Status == avatar.StatusDeleting || item.Status == avatar.StatusDeleted {
		return deleteResultIdempotentSkip, nil
	}

	now := s.now().UTC()
	eventPayload, err := json.Marshal(AvatarDeleteEvent{
		AvatarID: item.ID,
		UserID:   item.UserID,
	})
	if err != nil {
		return deleteResultError, fmt.Errorf("marshal avatar delete event: %w", err)
	}

	event := outbox.Event{
		ID:        uuid.NewString(),
		Topic:     queuekafka.TopicAvatarDelete,
		Key:       item.ID,
		Payload:   eventPayload,
		Headers:   queuekafka.InjectTraceContext(ctx, nil),
		Status:    outbox.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.outbox.SoftDeleteAvatarWithOutbox(ctx, item.ID, item.UserID, now, event); err != nil {
		if errors.Is(err, avatar.ErrNotFound) {
			return deleteResultIdempotentSkip, nil
		}
		return deleteResultError, fmt.Errorf("soft delete avatar with outbox: %w", err)
	}

	s.publishOutboxEvent(ctx, event)
	return deleteResultAccepted, nil
}

// publishOutboxEvent пытается опубликовать событие outbox сразу после фиксации транзакции
func (s *AvatarDeleteService) publishOutboxEvent(ctx context.Context, event outbox.Event) {
	if err := s.producer.Publish(ctx, event.Topic, event.Key, event.Payload, event.Headers); err != nil {
		s.telemetry.recordOutboxPublish(ctx, outboxPublishModeImmediate, outboxPublishResultError)
		if markErr := s.outbox.MarkOutboxPublishAttemptFailed(ctx, event.ID, err, s.now().UTC()); markErr != nil {
			logOutboxStateUpdateFailed(ctx, event, "mark_publish_attempt_failed", markErr)
		}
		return
	}

	if err := s.outbox.MarkOutboxPublished(ctx, event.ID, s.now().UTC()); err != nil {
		s.telemetry.recordOutboxPublish(ctx, outboxPublishModeImmediate, outboxPublishResultError)
		logOutboxStateUpdateFailed(ctx, event, "mark_published", err)
		return
	}
	s.telemetry.recordOutboxPublish(ctx, outboxPublishModeImmediate, outboxPublishResultSuccess)
}
