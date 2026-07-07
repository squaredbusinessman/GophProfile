package postgres

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
)

// TestCreateUserInsertsAllFields проверяет SQL-вставку пользователя
func TestCreateUserInsertsAllFields(t *testing.T) {
	db, mock := newMockDB(t)
	repo := newUserRepositoryForTest(t, db)
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO users")).
		WithArgs(
			"6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e",
			"user@example.com",
			now,
			now,
			sql.NullTime{},
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.CreateUser(context.Background(), user.User{
		ID:        "6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e",
		Email:     "user@example.com",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}

	assertExpectations(t, mock)
}

// TestGetUserByEmailReturnsActiveUser проверяет поиск пользователя по email
func TestGetUserByEmailReturnsActiveUser(t *testing.T) {
	db, mock := newMockDB(t)
	repo := newUserRepositoryForTest(t, db)
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows(userColumns()).
		AddRow(
			"6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e",
			"user@example.com",
			now,
			now,
			nil,
		)

	mock.ExpectQuery(regexp.QuoteMeta("FROM users")).
		WithArgs("user@example.com").
		WillReturnRows(rows)

	got, err := repo.GetUserByEmail(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail returned error: %v", err)
	}
	if got.ID != "6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e" {
		t.Fatalf("ID = %q, want expected id", got.ID)
	}

	assertExpectations(t, mock)
}

// TestGetUserByEmailReturnsNotFound проверяет маппинг отсутствующего пользователя
func TestGetUserByEmailReturnsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	repo := newUserRepositoryForTest(t, db)

	mock.ExpectQuery(regexp.QuoteMeta("FROM users")).
		WithArgs("missing@example.com").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.GetUserByEmail(context.Background(), "missing@example.com")
	if !errors.Is(err, user.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}

	assertExpectations(t, mock)
}

// TestFindOrCreateUserByEmailReturnsUser проверяет idempotent upsert пользователя
func TestFindOrCreateUserByEmailReturnsUser(t *testing.T) {
	db, mock := newMockDB(t)
	repo := newUserRepositoryForTest(t, db)
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows(userColumns()).
		AddRow(
			"6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e",
			"user@example.com",
			now,
			now,
			nil,
		)

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO users")).
		WithArgs(sqlmock.AnyArg(), "user@example.com", now).
		WillReturnRows(rows)

	got, err := repo.FindOrCreateUserByEmail(context.Background(), "user@example.com", now)
	if err != nil {
		t.Fatalf("FindOrCreateUserByEmail returned error: %v", err)
	}
	if got.Email != "user@example.com" {
		t.Fatalf("Email = %q, want user@example.com", got.Email)
	}

	assertExpectations(t, mock)
}

// userColumns возвращает список колонок пользователя для тестовых строк
func userColumns() []string {
	return []string{
		"id",
		"email",
		"created_at",
		"updated_at",
		"deleted_at",
	}
}
