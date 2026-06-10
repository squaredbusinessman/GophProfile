package app

import (
	"context"
	"fmt"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
)

type UserEmailResolver interface {
	FindOrCreateUserByEmail(ctx context.Context, email string, now time.Time) (user.User, error)
}

type UserResolveService struct {
	users UserEmailResolver
	now   func() time.Time
}

type UserResolveResult struct {
	ID        string
	Email     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewUserResolveService создает service сопоставления email с внутренним user_id
func NewUserResolveService(users UserEmailResolver) *UserResolveService {
	return &UserResolveService{
		users: users,
		now:   time.Now,
	}
}

// ResolveUserByEmail возвращает существующего пользователя или создает нового по email
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
