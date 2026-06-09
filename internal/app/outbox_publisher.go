package app

import (
	"context"
	"fmt"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
)

type OutboxEventStore interface {
	ListPendingOutboxEvents(ctx context.Context, limit int) ([]outbox.Event, error)
	MarkOutboxPublished(ctx context.Context, id string, publishedAt time.Time) error
	MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) error
}

type OutboxPublisherService struct {
	store     OutboxEventStore
	publisher EventPublisher
	now       func() time.Time
}

// NewOutboxPublisherService создает service повторной публикации outbox событий
func NewOutboxPublisherService(store OutboxEventStore, publisher EventPublisher) *OutboxPublisherService {
	return &OutboxPublisherService{
		store:     store,
		publisher: publisher,
		now:       time.Now,
	}
}

// PublishPending публикует pending outbox события и обновляет их состояние
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
		if err := s.publisher.Publish(ctx, event.Topic, event.Key, event.Payload); err != nil {
			_ = s.store.MarkOutboxPublishAttemptFailed(ctx, event.ID, err, s.now().UTC())
			continue
		}
		if err := s.store.MarkOutboxPublished(ctx, event.ID, s.now().UTC()); err != nil {
			return published, err
		}
		published++
	}

	return published, nil
}
