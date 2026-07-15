package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
	"github.com/squaredbusinessman/GophProfile/internal/resilience"
)

// UserRepository добавляет защиту от отказов к репозиторию пользователей
type UserRepository struct {
	next    *userRepository
	breaker *resilience.CircuitBreaker
}

// NewUserRepository создаёт защищённый репозиторий пользователей в PostgreSQL
func NewUserRepository(db *sql.DB, breakerCfg ...resilience.CircuitBreakerConfig) (*UserRepository, error) {
	telemetry, err := newPostgresTelemetry()
	if err != nil {
		return nil, fmt.Errorf("create user repository telemetry: %w", err)
	}
	return &UserRepository{
		next:    &userRepository{db: db, telemetry: telemetry},
		breaker: newPostgresBreaker(breakerCfg),
	}, nil
}

// CreateUser сохраняет нового пользователя
func (r *UserRepository) CreateUser(ctx context.Context, item user.User) error {
	return executePostgresCommand(r.breaker, func() error {
		return r.next.CreateUser(ctx, item)
	})
}

// GetUser возвращает активного пользователя по внутреннему идентификатору
func (r *UserRepository) GetUser(ctx context.Context, id string) (user.User, error) {
	return executePostgres(r.breaker, func() (user.User, error) {
		return r.next.GetUser(ctx, id)
	})
}

// GetUserByEmail возвращает активного пользователя по нормализованной электронной почте
func (r *UserRepository) GetUserByEmail(ctx context.Context, email string) (user.User, error) {
	return executePostgres(r.breaker, func() (user.User, error) {
		return r.next.GetUserByEmail(ctx, email)
	})
}

// FindOrCreateUserByEmail возвращает пользователя по электронной почте или создаёт нового
func (r *UserRepository) FindOrCreateUserByEmail(ctx context.Context, email string, now time.Time) (user.User, error) {
	return executePostgres(r.breaker, func() (user.User, error) {
		return r.next.FindOrCreateUserByEmail(ctx, email, now)
	})
}
