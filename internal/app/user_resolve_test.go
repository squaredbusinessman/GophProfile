package app

import (
	"context"
	"testing"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
)

// TestResolveUserByEmailReturnsStableUser проверяет создание или поиск пользователя по email
func TestResolveUserByEmailReturnsStableUser(t *testing.T) {
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	repo := &fakeUserEmailResolver{
		result: user.User{
			ID:        "6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e",
			Email:     "user@example.com",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	service := NewUserResolveService(repo)
	service.now = func() time.Time {
		return now
	}

	result, err := service.ResolveUserByEmail(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("ResolveUserByEmail returned error: %v", err)
	}
	if repo.email != "user@example.com" || repo.now != now {
		t.Fatalf("repo args = %q %s, want email and now", repo.email, repo.now)
	}
	if result.ID != "6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e" || result.Email != "user@example.com" {
		t.Fatalf("result = %#v, want user identity", result)
	}
}

type fakeUserEmailResolver struct {
	email  string
	now    time.Time
	result user.User
	err    error
}

// FindOrCreateUserByEmail запоминает email и возвращает fake пользователя
func (f *fakeUserEmailResolver) FindOrCreateUserByEmail(ctx context.Context, email string, now time.Time) (user.User, error) {
	f.email = email
	f.now = now
	if f.err != nil {
		return user.User{}, f.err
	}
	return f.result, nil
}
