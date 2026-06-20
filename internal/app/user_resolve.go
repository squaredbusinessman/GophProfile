package app

import (
	"context"
	"fmt"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
)

// UserEmailResolver описывает получение или создание пользователя по электронной почте
type UserEmailResolver interface {
	FindOrCreateUserByEmail(ctx context.Context, email string, now time.Time) (user.User, error)
}

// UserResolveService сопоставляет внешний адрес электронной почты с пользователем
type UserResolveService struct {
	users UserEmailResolver
	now   func() time.Time
}

// UserResolveResult содержит сведения о найденном или созданном пользователе
type UserResolveResult struct {
	// ID содержит идентификатор пользователя
	ID string
	// Email содержит нормализованный адрес электронной почты
	Email string
	// CreatedAt содержит время создания пользователя
	CreatedAt time.Time
	// UpdatedAt содержит время последнего изменения пользователя
	UpdatedAt time.Time
}

// NewUserResolveService создаёт сервис сопоставления электронной почты с внутренним идентификатором
func NewUserResolveService(users UserEmailResolver) *UserResolveService {
	return &UserResolveService{
		users: users,
		now:   time.Now,
	}
}

// ResolveUserByEmail возвращает существующего пользователя или создаёт нового по электронной почте
func (s *UserResolveService) ResolveUserByEmail(ctx context.Context, email string) (UserResolveResult, error) {
	if s.users == nil {
		return UserResolveResult{}, fmt.Errorf("user resolver is not configured")
	}

	item, err := s.users.FindOrCreateUserByEmail(ctx, email, s.now().UTC())
	if err != nil {
		return UserResolveResult{}, fmt.Errorf("find or create user by email: %w", err)
	}

	return UserResolveResult{
		ID:        item.ID,
		Email:     item.Email,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}, nil
}
