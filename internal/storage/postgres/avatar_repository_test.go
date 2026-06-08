package postgres

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
)

// TestCreateAvatarInsertsAllFields проверяет SQL-вставку avatar
func TestCreateAvatarInsertsAllFields(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAvatarRepository(db)
	now := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO avatars")).
		WithArgs(
			"4a992fa3-df1a-4b5f-b764-546e99643eb0",
			"user@example.com",
			"avatar.jpg",
			"image/jpeg",
			int64(128),
			sql.NullInt64{},
			sql.NullInt64{},
			string(avatar.StatusProcessing),
			"avatars/user@example.com/avatar/original",
			sql.NullString{},
			sql.NullString{},
			now,
			now,
			sql.NullTime{},
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.CreateAvatar(context.Background(), avatar.Avatar{
		ID:                "4a992fa3-df1a-4b5f-b764-546e99643eb0",
		UserEmail:         "user@example.com",
		FileName:          "avatar.jpg",
		MimeType:          "image/jpeg",
		SizeBytes:         128,
		Status:            avatar.StatusProcessing,
		OriginalObjectKey: "avatars/user@example.com/avatar/original",
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	if err != nil {
		t.Fatalf("CreateAvatar returned error: %v", err)
	}

	assertExpectations(t, mock)
}

// TestGetAvatarFiltersSoftDeleted проверяет фильтр soft delete при чтении
func TestGetAvatarFiltersSoftDeleted(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAvatarRepository(db)
	now := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows(avatarColumns()).
		AddRow(
			"4a992fa3-df1a-4b5f-b764-546e99643eb0",
			"user@example.com",
			"avatar.jpg",
			"image/jpeg",
			int64(128),
			nil,
			nil,
			string(avatar.StatusProcessing),
			"avatars/user@example.com/avatar/original",
			nil,
			nil,
			now,
			now,
			nil,
		)

	mock.ExpectQuery(regexp.QuoteMeta("FROM avatars")).
		WithArgs("4a992fa3-df1a-4b5f-b764-546e99643eb0").
		WillReturnRows(rows)

	got, err := repo.GetAvatar(context.Background(), "4a992fa3-df1a-4b5f-b764-546e99643eb0")
	if err != nil {
		t.Fatalf("GetAvatar returned error: %v", err)
	}
	if got.ID != "4a992fa3-df1a-4b5f-b764-546e99643eb0" {
		t.Fatalf("ID = %q, want expected id", got.ID)
	}
	if got.IsDeleted() {
		t.Fatal("avatar should not be marked as deleted")
	}

	assertExpectations(t, mock)
}

// TestGetAvatarReturnsNotFound проверяет маппинг отсутствующей строки
func TestGetAvatarReturnsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAvatarRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta("FROM avatars")).
		WithArgs("missing-id").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.GetAvatar(context.Background(), "missing-id")
	if !errors.Is(err, avatar.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}

	assertExpectations(t, mock)
}

// TestListAvatarsByUserEmailFiltersSoftDeleted проверяет список активных avatar по email пользователя
func TestListAvatarsByUserEmailFiltersSoftDeleted(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAvatarRepository(db)
	now := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows(avatarColumns()).
		AddRow(
			"4a992fa3-df1a-4b5f-b764-546e99643eb0",
			"user@example.com",
			"avatar.jpg",
			"image/jpeg",
			int64(128),
			nil,
			nil,
			string(avatar.StatusProcessing),
			"avatars/user@example.com/avatar/original",
			nil,
			nil,
			now,
			now,
			nil,
		)

	mock.ExpectQuery(regexp.QuoteMeta("FROM avatars")).
		WithArgs("user@example.com", 25, 5).
		WillReturnRows(rows)

	items, err := repo.ListAvatarsByUserEmail(context.Background(), "user@example.com", 25, 5)
	if err != nil {
		t.Fatalf("ListAvatarsByUserEmail returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}

	assertExpectations(t, mock)
}

// TestUpdateAvatarStatusRejectsInvalidStatus проверяет валидацию статуса
func TestUpdateAvatarStatusRejectsInvalidStatus(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAvatarRepository(db)

	err := repo.UpdateAvatarStatus(context.Background(), "avatar-id", avatar.Status("unknown"), time.Now())
	if !errors.Is(err, avatar.ErrInvalidStatus) {
		t.Fatalf("error = %v, want ErrInvalidStatus", err)
	}

	assertExpectations(t, mock)
}

// TestUpdateAvatarStatusReturnsNotFound проверяет отсутствие активной строки
func TestUpdateAvatarStatusReturnsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAvatarRepository(db)
	now := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta("UPDATE avatars")).
		WithArgs("avatar-id", string(avatar.StatusFailed), now).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.UpdateAvatarStatus(context.Background(), "avatar-id", avatar.StatusFailed, now)
	if !errors.Is(err, avatar.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}

	assertExpectations(t, mock)
}

// TestSoftDeleteAvatarMarksDeleting проверяет мягкое удаление avatar
func TestSoftDeleteAvatarMarksDeleting(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAvatarRepository(db)
	deletedAt := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta("UPDATE avatars")).
		WithArgs("avatar-id", "user@example.com", string(avatar.StatusDeleting), deletedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.SoftDeleteAvatar(context.Background(), "avatar-id", "user@example.com", deletedAt)
	if err != nil {
		t.Fatalf("SoftDeleteAvatar returned error: %v", err)
	}

	assertExpectations(t, mock)
}

// newMockDB создает мок PostgreSQL connection pool
func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db, mock
}

// assertExpectations проверяет выполнение всех ожидаемых SQL-команд
func assertExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations were not met: %v", err)
	}
}

// avatarColumns возвращает список колонок avatar для тестовых строк
func avatarColumns() []string {
	return []string{
		"id",
		"user_id",
		"file_name",
		"mime_type",
		"size_bytes",
		"width",
		"height",
		"status",
		"original_object_key",
		"thumb_100_object_key",
		"thumb_300_object_key",
		"created_at",
		"updated_at",
		"deleted_at",
	}
}
