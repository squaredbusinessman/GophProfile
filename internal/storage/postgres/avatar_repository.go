package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
)

type AvatarRepository struct {
	db *sql.DB
}

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// NewAvatarRepository создает repository для работы с avatar в PostgreSQL
func NewAvatarRepository(db *sql.DB) *AvatarRepository {
	return &AvatarRepository{db: db}
}

// CreateAvatar сохраняет новую avatar со статусом processing
func (r *AvatarRepository) CreateAvatar(ctx context.Context, item avatar.Avatar) (err error) {
	ctx, span := startRepositorySpan(ctx, "INSERT", "avatars")
	defer func() { finishRepositorySpan(span, err) }()
	return insertAvatar(ctx, r.db, item)
}

// insertAvatar сохраняет avatar через указанный SQL executor
func insertAvatar(ctx context.Context, executor sqlExecutor, item avatar.Avatar) error {
	if err := avatar.ValidateStatus(item.Status); err != nil {
		return err
	}

	_, err := executor.ExecContext(ctx, `
		INSERT INTO avatars (
			id,
			user_id,
			file_name,
			mime_type,
			size_bytes,
			width,
			height,
			status,
			original_object_key,
			thumb_100_object_key,
			thumb_300_object_key,
			created_at,
			updated_at,
			deleted_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`,
		item.ID,
		item.UserID,
		item.FileName,
		item.MimeType,
		item.SizeBytes,
		intPtrToNullInt64(item.Width),
		intPtrToNullInt64(item.Height),
		string(item.Status),
		item.OriginalObjectKey,
		stringPtrToNullString(item.Thumb100ObjectKey),
		stringPtrToNullString(item.Thumb300ObjectKey),
		item.CreatedAt,
		item.UpdatedAt,
		timePtrToNullTime(item.DeletedAt),
	)
	if err != nil {
		return fmt.Errorf("create avatar: %w", err)
	}

	return nil
}

// GetAvatar возвращает активную avatar по id
func (r *AvatarRepository) GetAvatar(ctx context.Context, id string) (item avatar.Avatar, err error) {
	ctx, span := startRepositorySpan(ctx, "SELECT", "avatars")
	defer func() { finishRepositorySpan(span, err) }()

	row := r.db.QueryRowContext(ctx, `
		SELECT
			id,
			user_id,
			file_name,
			mime_type,
			size_bytes,
			width,
			height,
			status,
			original_object_key,
			thumb_100_object_key,
			thumb_300_object_key,
			created_at,
			updated_at,
			deleted_at
		FROM avatars
		WHERE id = $1
			AND deleted_at IS NULL
	`, id)

	item, err = scanAvatar(row)
	if err != nil {
		return avatar.Avatar{}, err
	}

	return item, nil
}

// GetAvatarIncludingDeleted возвращает avatar по id включая мягко удаленные записи
func (r *AvatarRepository) GetAvatarIncludingDeleted(ctx context.Context, id string) (item avatar.Avatar, err error) {
	ctx, span := startRepositorySpan(ctx, "SELECT", "avatars")
	defer func() { finishRepositorySpan(span, err) }()

	row := r.db.QueryRowContext(ctx, `
		SELECT
			id,
			user_id,
			file_name,
			mime_type,
			size_bytes,
			width,
			height,
			status,
			original_object_key,
			thumb_100_object_key,
			thumb_300_object_key,
			created_at,
			updated_at,
			deleted_at
		FROM avatars
		WHERE id = $1
	`, id)

	item, err = scanAvatar(row)
	if err != nil {
		return avatar.Avatar{}, err
	}

	return item, nil
}

// ListAvatarsByUser возвращает активные avatar пользователя по внутреннему UUID
func (r *AvatarRepository) ListAvatarsByUser(ctx context.Context, userID string, limit int, offset int) (items []avatar.Avatar, err error) {
	ctx, span := startRepositorySpan(ctx, "SELECT", "avatars")
	defer func() { finishRepositorySpan(span, err) }()

	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT
			id,
			user_id,
			file_name,
			mime_type,
			size_bytes,
			width,
			height,
			status,
			original_object_key,
			thumb_100_object_key,
			thumb_300_object_key,
			created_at,
			updated_at,
			deleted_at
		FROM avatars
		WHERE user_id = $1
			AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list avatars by user: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	items = make([]avatar.Avatar, 0)
	for rows.Next() {
		item, err := scanAvatar(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate avatars by user: %w", err)
	}

	return items, nil
}

// UpdateAvatarStatus обновляет статус активной avatar
func (r *AvatarRepository) UpdateAvatarStatus(ctx context.Context, id string, status avatar.Status, updatedAt time.Time) (err error) {
	if err := avatar.ValidateStatus(status); err != nil {
		return err
	}
	ctx, span := startRepositorySpan(ctx, "UPDATE", "avatars")
	defer func() { finishRepositorySpan(span, err) }()

	result, err := r.db.ExecContext(ctx, `
		UPDATE avatars
		SET status = $2,
			updated_at = $3
		WHERE id = $1
			AND deleted_at IS NULL
	`, id, string(status), updatedAt)
	if err != nil {
		return fmt.Errorf("update avatar status: %w", err)
	}

	return expectOneAffected(result)
}

// MarkAvatarReady сохраняет размеры и ключи миниатюр после обработки
func (r *AvatarRepository) MarkAvatarReady(ctx context.Context, id string, width int, height int, thumb100Key string, thumb300Key string, updatedAt time.Time) (err error) {
	ctx, span := startRepositorySpan(ctx, "UPDATE", "avatars")
	defer func() { finishRepositorySpan(span, err) }()

	result, err := r.db.ExecContext(ctx, `
		UPDATE avatars
		SET status = $2,
			width = $3,
			height = $4,
			thumb_100_object_key = $5,
			thumb_300_object_key = $6,
			updated_at = $7
		WHERE id = $1
			AND deleted_at IS NULL
	`, id, string(avatar.StatusReady), width, height, thumb100Key, thumb300Key, updatedAt)
	if err != nil {
		return fmt.Errorf("mark avatar ready: %w", err)
	}

	return expectOneAffected(result)
}

// SoftDeleteAvatar выполняет мягкое удаление avatar пользователя по внутреннему UUID
func (r *AvatarRepository) SoftDeleteAvatar(ctx context.Context, id string, userID string, deletedAt time.Time) (err error) {
	ctx, span := startRepositorySpan(ctx, "UPDATE", "avatars")
	defer func() { finishRepositorySpan(span, err) }()

	result, err := r.db.ExecContext(ctx, `
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

	return expectOneAffected(result)
}

// MarkAvatarDeleted переводит avatar в состояние удаленных S3 objects
func (r *AvatarRepository) MarkAvatarDeleted(ctx context.Context, id string, updatedAt time.Time) (err error) {
	ctx, span := startRepositorySpan(ctx, "UPDATE", "avatars")
	defer func() { finishRepositorySpan(span, err) }()

	result, err := r.db.ExecContext(ctx, `
		UPDATE avatars
		SET status = $2,
			updated_at = $3
		WHERE id = $1
			AND deleted_at IS NOT NULL
	`, id, string(avatar.StatusDeleted), updatedAt)
	if err != nil {
		return fmt.Errorf("mark avatar deleted: %w", err)
	}

	return expectOneAffected(result)
}

type rowScanner interface {
	Scan(dest ...any) error
}

// scanAvatar читает avatar из результата SQL-запроса
func scanAvatar(scanner rowScanner) (avatar.Avatar, error) {
	var item avatar.Avatar
	var width sql.NullInt64
	var height sql.NullInt64
	var status string
	var thumb100 sql.NullString
	var thumb300 sql.NullString
	var deletedAt sql.NullTime

	err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&item.FileName,
		&item.MimeType,
		&item.SizeBytes,
		&width,
		&height,
		&status,
		&item.OriginalObjectKey,
		&thumb100,
		&thumb300,
		&item.CreatedAt,
		&item.UpdatedAt,
		&deletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return avatar.Avatar{}, avatar.ErrNotFound
	}
	if err != nil {
		return avatar.Avatar{}, fmt.Errorf("scan avatar: %w", err)
	}

	item.Status = avatar.Status(status)
	if err := avatar.ValidateStatus(item.Status); err != nil {
		return avatar.Avatar{}, err
	}
	item.Width = nullInt64ToIntPtr(width)
	item.Height = nullInt64ToIntPtr(height)
	item.Thumb100ObjectKey = nullStringToStringPtr(thumb100)
	item.Thumb300ObjectKey = nullStringToStringPtr(thumb300)
	item.DeletedAt = nullTimeToTimePtr(deletedAt)

	return item, nil
}

// expectOneAffected проверяет что SQL-команда изменила одну строку
func expectOneAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if affected == 0 {
		return avatar.ErrNotFound
	}
	return nil
}

// intPtrToNullInt64 конвертирует указатель int в nullable SQL-значение
func intPtrToNullInt64(value *int) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*value), Valid: true}
}

// stringPtrToNullString конвертирует указатель string в nullable SQL-значение
func stringPtrToNullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

// timePtrToNullTime конвертирует указатель time в nullable SQL-значение
func timePtrToNullTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *value, Valid: true}
}

// nullInt64ToIntPtr конвертирует nullable SQL-int в указатель int
func nullInt64ToIntPtr(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int64)
	return &converted
}

// nullStringToStringPtr конвертирует nullable SQL-string в указатель string
func nullStringToStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

// nullTimeToTimePtr конвертирует nullable SQL-time в указатель time
func nullTimeToTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
