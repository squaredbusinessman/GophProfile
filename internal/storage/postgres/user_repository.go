package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
)

// userRepository сохраняет и читает пользователей в PostgreSQL
type userRepository struct {
	db        *sql.DB
	telemetry postgresTelemetry
}

// CreateUser сохраняет нового пользователя
func (r *userRepository) CreateUser(ctx context.Context, item user.User) (err error) {
	ctx, operation := r.telemetry.startRepositoryOperation(ctx, "INSERT", "users")
	defer func() { finishRepositoryOperation(operation, err) }()

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO users (
			id,
			email,
			created_at,
			updated_at,
			deleted_at
		)
		VALUES ($1, $2, $3, $4, $5)
	`,
		item.ID,
		item.Email,
		item.CreatedAt,
		item.UpdatedAt,
		timePtrToNullTime(item.DeletedAt),
	)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	return nil
}

// GetUser возвращает активного пользователя по внутреннему UUID
func (r *userRepository) GetUser(ctx context.Context, id string) (item user.User, err error) {
	ctx, operation := r.telemetry.startRepositoryOperation(ctx, "SELECT", "users")
	defer func() { finishRepositoryOperation(operation, err) }()

	row := r.db.QueryRowContext(ctx, `
		SELECT
			id,
			email,
			created_at,
			updated_at,
			deleted_at
		FROM users
		WHERE id = $1
			AND deleted_at IS NULL
	`, id)

	return scanUser(row)
}

// GetUserByEmail возвращает активного пользователя по нормализованной электронной почте
func (r *userRepository) GetUserByEmail(ctx context.Context, email string) (item user.User, err error) {
	ctx, operation := r.telemetry.startRepositoryOperation(ctx, "SELECT", "users")
	defer func() { finishRepositoryOperation(operation, err) }()

	row := r.db.QueryRowContext(ctx, `
		SELECT
			id,
			email,
			created_at,
			updated_at,
			deleted_at
		FROM users
		WHERE lower(email) = lower($1)
			AND deleted_at IS NULL
	`, email)

	return scanUser(row)
}

// FindOrCreateUserByEmail возвращает пользователя по email или создает нового
func (r *userRepository) FindOrCreateUserByEmail(ctx context.Context, email string, now time.Time) (item user.User, err error) {
	ctx, operation := r.telemetry.startRepositoryOperation(ctx, "INSERT", "users")
	defer func() { finishRepositoryOperation(operation, err) }()

	row := r.db.QueryRowContext(ctx, `
		INSERT INTO users (
			id,
			email,
			created_at,
			updated_at,
			deleted_at
		)
		VALUES ($1, $2, $3, $3, NULL)
		ON CONFLICT (lower(email)) WHERE deleted_at IS NULL
		DO UPDATE SET email = EXCLUDED.email
		RETURNING
			id,
			email,
			created_at,
			updated_at,
			deleted_at
	`, uuid.NewString(), email, now)

	return scanUser(row)
}

// scanUser читает пользователя из результата SQL-запроса
func scanUser(scanner rowScanner) (user.User, error) {
	var item user.User
	var deletedAt sql.NullTime

	err := scanner.Scan(
		&item.ID,
		&item.Email,
		&item.CreatedAt,
		&item.UpdatedAt,
		&deletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return user.User{}, user.ErrNotFound
	}
	if err != nil {
		return user.User{}, fmt.Errorf("scan user: %w", err)
	}

	item.DeletedAt = nullTimeToTimePtr(deletedAt)
	return item, nil
}
