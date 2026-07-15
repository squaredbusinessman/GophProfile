package app

import (
	"context"
	"fmt"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
)

// OutboxEventStore описывает чтение и обновление событий outbox
type OutboxEventStore interface {
	// ListPendingOutboxEvents возвращает ожидающие публикации события outbox.
	ListPendingOutboxEvents(ctx context.Context, limit int) ([]outbox.Event, error)
	// MarkOutboxPublished отмечает событие outbox успешно опубликованным.
	MarkOutboxPublished(ctx context.Context, id string, publishedAt time.Time) error
	// MarkOutboxPublishAttemptFailed сохраняет ошибку публикации события outbox.
	MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) error
}

// OutboxPublisherService повторно публикует ожидающие события outbox
type OutboxPublisherService struct {
	store     OutboxEventStore
	publisher EventPublisher
	now       func() time.Time
	telemetry businessTelemetry
}

// NewOutboxPublisherService создаёт сервис повторной публикации событий outbox
func NewOutboxPublisherService(store OutboxEventStore, publisher EventPublisher) (*OutboxPublisherService, error) {
	telemetry, err := newBusinessTelemetry()
	if err != nil {
		return nil, fmt.Errorf("create outbox publisher telemetry: %w", err)
	}
	return &OutboxPublisherService{
		store:     store,
		publisher: publisher,
		now:       time.Now,
		telemetry: telemetry,
	}, nil
}

// PublishPending публикует ожидающие события outbox и обновляет их состояние
func (s *OutboxPublisherService) PublishPending(ctx context.Context, limit int) (int, error) {
	if s.store == nil || s.publisher == nil {
		return 0, fmt.Errorf("outbox publisher service is not configured")
	}

	events, err := s.store.ListPendingOutboxEvents(ctx, limit)
	if err != nil {
		return 0, err
	}

	published := 0
	for _, event := range events {
		if err := s.publisher.Publish(ctx, event.Topic, event.Key, event.Payload, event.Headers); err != nil {
			s.telemetry.recordOutboxPublish(ctx, outboxPublishModeBackground, outboxPublishResultError)
			LoggerFromContext(ctx).Warn().
				Str("event_id", event.ID).
				Str("topic", event.Topic).
				Str("error_type", ErrorType(err)).
				Msg("outbox event publish failed")
			if markErr := s.store.MarkOutboxPublishAttemptFailed(ctx, event.ID, err, s.now().UTC()); markErr != nil {
				logOutboxStateUpdateFailed(ctx, event, "mark_publish_attempt_failed", markErr)
			}
			continue
		}
		if err := s.store.MarkOutboxPublished(ctx, event.ID, s.now().UTC()); err != nil {
			s.telemetry.recordOutboxPublish(ctx, outboxPublishModeBackground, outboxPublishResultError)
			return published, err
		}
		s.telemetry.recordOutboxPublish(ctx, outboxPublishModeBackground, outboxPublishResultSuccess)
		published++
	}

	return published, nil
}
