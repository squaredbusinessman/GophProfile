package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/resilience"
)

// AvatarRepository добавляет защиту от отказов к репозиторию аватаров
type AvatarRepository struct {
	next    *avatarRepository
	breaker *resilience.CircuitBreaker
}

// NewAvatarRepository создаёт защищённый репозиторий аватаров в PostgreSQL
func NewAvatarRepository(db *sql.DB, breakerCfg ...resilience.CircuitBreakerConfig) (*AvatarRepository, error) {
	telemetry, err := newPostgresTelemetry()
	if err != nil {
		return nil, fmt.Errorf("create avatar repository telemetry: %w", err)
	}
	return &AvatarRepository{
		next:    &avatarRepository{db: db, telemetry: telemetry},
		breaker: newPostgresBreaker(breakerCfg),
	}, nil
}

// ReadAvatarOperationalStats возвращает количество аватаров по состояниям и размер оригиналов
func (r *AvatarRepository) ReadAvatarOperationalStats(ctx context.Context) (map[avatar.Status]int64, int64, error) {
	type operationalStats struct {
		countByStatus       map[avatar.Status]int64
		originalStorageSize int64
	}
	result, err := executePostgres(r.breaker, func() (operationalStats, error) {
		countByStatus, originalStorageSize, operationErr := r.next.ReadAvatarOperationalStats(ctx)
		return operationalStats{
			countByStatus:       countByStatus,
			originalStorageSize: originalStorageSize,
		}, operationErr
	})
	return result.countByStatus, result.originalStorageSize, err
}

// CreateAvatar сохраняет новый аватар в состоянии обработки
func (r *AvatarRepository) CreateAvatar(ctx context.Context, item avatar.Avatar) error {
	return executePostgresCommand(r.breaker, func() error {
		return r.next.CreateAvatar(ctx, item)
	})
}

// GetAvatar возвращает активный аватар по идентификатору
func (r *AvatarRepository) GetAvatar(ctx context.Context, id string) (avatar.Avatar, error) {
	return executePostgres(r.breaker, func() (avatar.Avatar, error) {
		return r.next.GetAvatar(ctx, id)
	})
}

// GetAvatarIncludingDeleted возвращает аватар по идентификатору вместе с мягко удалёнными записями
func (r *AvatarRepository) GetAvatarIncludingDeleted(ctx context.Context, id string) (avatar.Avatar, error) {
	return executePostgres(r.breaker, func() (avatar.Avatar, error) {
		return r.next.GetAvatarIncludingDeleted(ctx, id)
	})
}

// ListAvatarsByUser возвращает активные аватары пользователя
func (r *AvatarRepository) ListAvatarsByUser(ctx context.Context, userID string, limit int, offset int) ([]avatar.Avatar, error) {
	return executePostgres(r.breaker, func() ([]avatar.Avatar, error) {
		return r.next.ListAvatarsByUser(ctx, userID, limit, offset)
	})
}

// UpdateAvatarStatus обновляет состояние активного аватара
func (r *AvatarRepository) UpdateAvatarStatus(ctx context.Context, id string, status avatar.Status, updatedAt time.Time) error {
	return executePostgresCommand(r.breaker, func() error {
		return r.next.UpdateAvatarStatus(ctx, id, status, updatedAt)
	})
}

// MarkAvatarReady сохраняет размеры и ключи миниатюр после обработки
func (r *AvatarRepository) MarkAvatarReady(ctx context.Context, id string, width int, height int, thumb100Key string, thumb300Key string, updatedAt time.Time) error {
	return executePostgresCommand(r.breaker, func() error {
		return r.next.MarkAvatarReady(ctx, id, width, height, thumb100Key, thumb300Key, updatedAt)
	})
}

// SoftDeleteAvatar выполняет мягкое удаление аватара пользователя
func (r *AvatarRepository) SoftDeleteAvatar(ctx context.Context, id string, userID string, deletedAt time.Time) error {
	return executePostgresCommand(r.breaker, func() error {
		return r.next.SoftDeleteAvatar(ctx, id, userID, deletedAt)
	})
}

// MarkAvatarDeleted переводит аватар в состояние удалённых объектов S3
func (r *AvatarRepository) MarkAvatarDeleted(ctx context.Context, id string, updatedAt time.Time) error {
	return executePostgresCommand(r.breaker, func() error {
		return r.next.MarkAvatarDeleted(ctx, id, updatedAt)
	})
}
