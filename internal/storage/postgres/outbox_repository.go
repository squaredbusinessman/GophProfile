package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
)

// OutboxRepository сохраняет аватары и события outbox в PostgreSQL
type OutboxRepository struct {
	db        *sql.DB
	telemetry postgresTelemetry
}

// ReadOutboxOperationalStats возвращает размер очереди и возраст старейшего события outbox
func (r *OutboxRepository) ReadOutboxOperationalStats(ctx context.Context) (pendingCount int64, oldestAgeSeconds float64, err error) {
	ctx, operation := r.telemetry.startRepositoryOperation(ctx, "SELECT", "outbox_events")
	defer func() { finishRepositoryOperation(operation, err) }()
	err = r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(created_at))), 0)
		FROM outbox_events
		WHERE status = $1
	`, string(outbox.StatusPending)).Scan(&pendingCount, &oldestAgeSeconds)
	if err != nil {
		return 0, 0, fmt.Errorf("read outbox operational stats: %w", err)
	}
	return pendingCount, oldestAgeSeconds, nil
}

// NewOutboxRepository создаёт репозиторий событий outbox
func NewOutboxRepository(db *sql.DB) (*OutboxRepository, error) {
	telemetry, err := newPostgresTelemetry()
	if err != nil {
		return nil, fmt.Errorf("create outbox repository telemetry: %w", err)
	}
	return &OutboxRepository{db: db, telemetry: telemetry}, nil
}

// CreateAvatarWithOutbox атомарно сохраняет аватар и событие outbox
func (r *OutboxRepository) CreateAvatarWithOutbox(ctx context.Context, item avatar.Avatar, event outbox.Event) (err error) {
	ctx, span := r.telemetry.startRepositoryOperation(ctx, "TRANSACTION", "")
	defer func() { finishRepositoryOperation(span, err) }()

	tx, err := r.db.BeginTx(ctx, nil)
	addTransactionEvent(span, "db.transaction.begin", err)
	if err != nil {
		return fmt.Errorf("begin avatar outbox transaction: %w", err)
	}
	committed := false
	defer func() {
		rollbackErr := tx.Rollback()
		if !committed {
			addTransactionEvent(span, "db.transaction.rollback", rollbackErr)
		}
	}()

	if err := insertAvatar(ctx, tx, item); err != nil {
		return err
	}
	if err := insertOutboxEvent(ctx, tx, event); err != nil {
		return err
	}
	commitErr := tx.Commit()
	addTransactionEvent(span, "db.transaction.commit", commitErr)
	if commitErr != nil {
		return fmt.Errorf("commit avatar outbox transaction: %w", commitErr)
	}
	committed = true

	return nil
}

// SoftDeleteAvatarWithOutbox атомарно помечает аватар удаляемым и сохраняет событие outbox
func (r *OutboxRepository) SoftDeleteAvatarWithOutbox(ctx context.Context, id string, userID string, deletedAt time.Time, event outbox.Event) (err error) {
	ctx, span := r.telemetry.startRepositoryOperation(ctx, "TRANSACTION", "")
	defer func() { finishRepositoryOperation(span, err) }()

	tx, err := r.db.BeginTx(ctx, nil)
	addTransactionEvent(span, "db.transaction.begin", err)
	if err != nil {
		return fmt.Errorf("begin avatar delete outbox transaction: %w", err)
	}
	committed := false
	defer func() {
		rollbackErr := tx.Rollback()
		if !committed {
			addTransactionEvent(span, "db.transaction.rollback", rollbackErr)
		}
	}()

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
	commitErr := tx.Commit()
	addTransactionEvent(span, "db.transaction.commit", commitErr)
	if commitErr != nil {
		return fmt.Errorf("commit avatar delete outbox transaction: %w", commitErr)
	}
	committed = true

	return nil
}

// MarkOutboxPublished отмечает событие outbox опубликованным
func (r *OutboxRepository) MarkOutboxPublished(ctx context.Context, id string, publishedAt time.Time) (err error) {
	ctx, span := r.telemetry.startRepositoryOperation(ctx, "UPDATE", "outbox_events")
	defer func() { finishRepositoryOperation(span, err) }()

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

// MarkOutboxPublishAttemptFailed сохраняет ошибку публикации и оставляет событие ожидающим
func (r *OutboxRepository) MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) (err error) {
	ctx, span := r.telemetry.startRepositoryOperation(ctx, "UPDATE", "outbox_events")
	defer func() { finishRepositoryOperation(span, err) }()

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

// ListPendingOutboxEvents возвращает ожидающие события outbox для повторной публикации
func (r *OutboxRepository) ListPendingOutboxEvents(ctx context.Context, limit int) (events []outbox.Event, err error) {
	ctx, span := r.telemetry.startRepositoryOperation(ctx, "SELECT", "outbox_events")
	defer func() { finishRepositoryOperation(span, err) }()

	if limit <= 0 {
		limit = 100
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT
			id,
			topic,
			event_key,
			payload,
			headers,
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
	defer func() {
		_ = rows.Close()
	}()

	events = make([]outbox.Event, 0)
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

// insertOutboxEvent сохраняет событие outbox через указанный исполнитель SQL
func insertOutboxEvent(ctx context.Context, executor sqlExecutor, event outbox.Event) error {
	_, err := executor.ExecContext(ctx, `
		INSERT INTO outbox_events (
			id,
			topic,
			event_key,
			payload,
			headers,
			status,
			attempts,
			last_error,
			created_at,
			updated_at,
			published_at
		)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6, $7, $8, $9, $10, $11)
	`,
		event.ID,
		event.Topic,
		event.Key,
		event.Payload,
		headersJSON(event.Headers),
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

// scanOutboxEvent читает событие outbox из результата SQL-запроса
func scanOutboxEvent(scanner rowScanner) (outbox.Event, error) {
	var event outbox.Event
	var status string
	var headersJSON []byte
	var lastError sql.NullString
	var publishedAt sql.NullTime

	err := scanner.Scan(
		&event.ID,
		&event.Topic,
		&event.Key,
		&event.Payload,
		&headersJSON,
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
	if err := json.Unmarshal(headersJSON, &event.Headers); err != nil {
		return outbox.Event{}, fmt.Errorf("decode outbox headers: %w", err)
	}
	event.LastError = nullStringToStringPtr(lastError)
	event.PublishedAt = nullTimeToTimePtr(publishedAt)
	return event, nil
}

// headersJSON сериализует заголовки Kafka в объект JSON и заменяет nil пустым объектом
func headersJSON(headers map[string]string) []byte {
	if headers == nil {
		headers = map[string]string{}
	}
	data, err := json.Marshal(headers)
	if err != nil {
		return []byte(`{}`)
	}
	return data
}

// expectOutboxAffected проверяет, что SQL-команда изменила строку outbox
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
