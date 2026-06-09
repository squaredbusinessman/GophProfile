package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
)

type OutboxRepository struct {
	db *sql.DB
}

// NewOutboxRepository создает repository для outbox событий
func NewOutboxRepository(db *sql.DB) *OutboxRepository {
	return &OutboxRepository{db: db}
}

// CreateAvatarWithOutbox атомарно сохраняет avatar и outbox событие
func (r *OutboxRepository) CreateAvatarWithOutbox(ctx context.Context, item avatar.Avatar, event outbox.Event) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin avatar outbox transaction: %w", err)
	}
	defer tx.Rollback()

	if err := insertAvatar(ctx, tx, item); err != nil {
		return err
	}
	if err := insertOutboxEvent(ctx, tx, event); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit avatar outbox transaction: %w", err)
	}

	return nil
}

// SoftDeleteAvatarWithOutbox атомарно помечает avatar удаляемой и сохраняет outbox событие
func (r *OutboxRepository) SoftDeleteAvatarWithOutbox(ctx context.Context, id string, userID string, deletedAt time.Time, event outbox.Event) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin avatar delete outbox transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE avatars
		SET status = $3,
			deleted_at = $4,
			updated_at = $4
		WHERE id = $1
			AND user_id = $2
			AND deleted_at IS NULL
	`, id, userID, string(avatar.StatusDeleting), deletedAt)
	if err != nil {
		return fmt.Errorf("soft delete avatar: %w", err)
	}
	if err := expectOneAffected(result); err != nil {
		return err
	}
	if err := insertOutboxEvent(ctx, tx, event); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit avatar delete outbox transaction: %w", err)
	}

	return nil
}

// MarkOutboxPublished отмечает outbox событие опубликованным
func (r *OutboxRepository) MarkOutboxPublished(ctx context.Context, id string, publishedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE outbox_events
		SET status = $2,
			published_at = $3,
			updated_at = $3
		WHERE id = $1
			AND status = $4
	`, id, string(outbox.StatusPublished), publishedAt, string(outbox.StatusPending))
	if err != nil {
		return fmt.Errorf("mark outbox published: %w", err)
	}

	return expectOutboxAffected(result)
}

// MarkOutboxPublishAttemptFailed сохраняет ошибку публикации и оставляет событие pending
func (r *OutboxRepository) MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE outbox_events
		SET attempts = attempts + 1,
			last_error = $2,
			updated_at = $3
		WHERE id = $1
			AND status = $4
	`, id, publishErr.Error(), updatedAt, string(outbox.StatusPending))
	if err != nil {
		return fmt.Errorf("mark outbox publish attempt failed: %w", err)
	}

	return expectOutboxAffected(result)
}

// ListPendingOutboxEvents возвращает pending outbox события для retry publisher
func (r *OutboxRepository) ListPendingOutboxEvents(ctx context.Context, limit int) ([]outbox.Event, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT
			id,
			topic,
			event_key,
			payload,
			status,
			attempts,
			last_error,
			created_at,
			updated_at,
			published_at
		FROM outbox_events
		WHERE status = $1
		ORDER BY created_at ASC
		LIMIT $2
	`, string(outbox.StatusPending), limit)
	if err != nil {
		return nil, fmt.Errorf("list pending outbox events: %w", err)
	}
	defer rows.Close()

	events := make([]outbox.Event, 0)
	for rows.Next() {
		event, err := scanOutboxEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending outbox events: %w", err)
	}

	return events, nil
}

// insertOutboxEvent сохраняет outbox событие через указанный SQL executor
func insertOutboxEvent(ctx context.Context, executor sqlExecutor, event outbox.Event) error {
	_, err := executor.ExecContext(ctx, `
		INSERT INTO outbox_events (
			id,
			topic,
			event_key,
			payload,
			status,
			attempts,
			last_error,
			created_at,
			updated_at,
			published_at
		)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10)
	`,
		event.ID,
		event.Topic,
		event.Key,
		event.Payload,
		string(event.Status),
		event.Attempts,
		stringPtrToNullString(event.LastError),
		event.CreatedAt,
		event.UpdatedAt,
		timePtrToNullTime(event.PublishedAt),
	)
	if err != nil {
		return fmt.Errorf("create outbox event: %w", err)
	}

	return nil
}

// scanOutboxEvent читает outbox событие из результата SQL-запроса
func scanOutboxEvent(scanner rowScanner) (outbox.Event, error) {
	var event outbox.Event
	var status string
	var lastError sql.NullString
	var publishedAt sql.NullTime

	err := scanner.Scan(
		&event.ID,
		&event.Topic,
		&event.Key,
		&event.Payload,
		&status,
		&event.Attempts,
		&lastError,
		&event.CreatedAt,
		&event.UpdatedAt,
		&publishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return outbox.Event{}, outbox.ErrNotFound
	}
	if err != nil {
		return outbox.Event{}, fmt.Errorf("scan outbox event: %w", err)
	}

	event.Status = outbox.Status(status)
	event.LastError = nullStringToStringPtr(lastError)
	event.PublishedAt = nullTimeToTimePtr(publishedAt)
	return event, nil
}

// expectOutboxAffected проверяет что SQL-команда изменила outbox строку
func expectOutboxAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if affected == 0 {
		return outbox.ErrNotFound
	}
	return nil
}
