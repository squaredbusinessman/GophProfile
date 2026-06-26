package postgres

import (
	"database/sql"
	"testing"
)

// newUserRepositoryForTest создаёт репозиторий пользователей и завершает тест при ошибке телеметрии
func newUserRepositoryForTest(t *testing.T, db *sql.DB) *UserRepository {
	t.Helper()
	repo, err := NewUserRepository(db)
	if err != nil {
		t.Fatalf("NewUserRepository() error = %v", err)
	}
	return repo
}

// newAvatarRepositoryForTest создаёт репозиторий аватаров и завершает тест при ошибке телеметрии
func newAvatarRepositoryForTest(t *testing.T, db *sql.DB) *AvatarRepository {
	t.Helper()
	repo, err := NewAvatarRepository(db)
	if err != nil {
		t.Fatalf("NewAvatarRepository() error = %v", err)
	}
	return repo
}

// newOutboxRepositoryForTest создаёт репозиторий outbox и завершает тест при ошибке телеметрии
func newOutboxRepositoryForTest(t *testing.T, db *sql.DB) *OutboxRepository {
	t.Helper()
	repo, err := NewOutboxRepository(db)
	if err != nil {
		t.Fatalf("NewOutboxRepository() error = %v", err)
	}
	return repo
}
