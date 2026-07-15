package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
	"github.com/squaredbusinessman/GophProfile/internal/resilience"
)

// OutboxRepository добавляет защиту от отказов к репозиторию событий outbox
type OutboxRepository struct {
	next    *outboxRepository
	breaker *resilience.CircuitBreaker
}

// NewOutboxRepository создаёт защищённый репозиторий событий outbox
func NewOutboxRepository(db *sql.DB, breakerCfg ...resilience.CircuitBreakerConfig) (*OutboxRepository, error) {
	telemetry, err := newPostgresTelemetry()
	if err != nil {
		return nil, fmt.Errorf("create outbox repository telemetry: %w", err)
	}
	return &OutboxRepository{
		next:    &outboxRepository{db: db, telemetry: telemetry},
		breaker: newPostgresBreaker(breakerCfg),
	}, nil
}

// ReadOutboxOperationalStats возвращает размер очереди и возраст старейшего события outbox
func (r *OutboxRepository) ReadOutboxOperationalStats(ctx context.Context) (int64, float64, error) {
	type operationalStats struct {
		pendingCount     int64
		oldestAgeSeconds float64
	}
	result, err := executePostgres(r.breaker, func() (operationalStats, error) {
		pendingCount, oldestAgeSeconds, operationErr := r.next.ReadOutboxOperationalStats(ctx)
		return operationalStats{
			pendingCount:     pendingCount,
			oldestAgeSeconds: oldestAgeSeconds,
		}, operationErr
	})
	return result.pendingCount, result.oldestAgeSeconds, err
}

// CreateAvatarWithOutbox атомарно сохраняет аватар и событие outbox
func (r *OutboxRepository) CreateAvatarWithOutbox(ctx context.Context, item avatar.Avatar, event outbox.Event) error {
	return executePostgresCommand(r.breaker, func() error {
		return r.next.CreateAvatarWithOutbox(ctx, item, event)
	})
}

// SoftDeleteAvatarWithOutbox атомарно помечает аватар удаляемым и сохраняет событие outbox
func (r *OutboxRepository) SoftDeleteAvatarWithOutbox(ctx context.Context, id string, userID string, deletedAt time.Time, event outbox.Event) error {
	return executePostgresCommand(r.breaker, func() error {
		return r.next.SoftDeleteAvatarWithOutbox(ctx, id, userID, deletedAt, event)
	})
}

// MarkOutboxPublished отмечает событие outbox опубликованным
func (r *OutboxRepository) MarkOutboxPublished(ctx context.Context, id string, publishedAt time.Time) error {
	return executePostgresCommand(r.breaker, func() error {
		return r.next.MarkOutboxPublished(ctx, id, publishedAt)
	})
}

// MarkOutboxPublishAttemptFailed сохраняет ошибку публикации и оставляет событие ожидающим
func (r *OutboxRepository) MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) error {
	return executePostgresCommand(r.breaker, func() error {
		return r.next.MarkOutboxPublishAttemptFailed(ctx, id, publishErr, updatedAt)
	})
}

// ListPendingOutboxEvents возвращает ожидающие события outbox для повторной публикации
func (r *OutboxRepository) ListPendingOutboxEvents(ctx context.Context, limit int) ([]outbox.Event, error) {
	return executePostgres(r.breaker, func() ([]outbox.Event, error) {
		return r.next.ListPendingOutboxEvents(ctx, limit)
	})
}
