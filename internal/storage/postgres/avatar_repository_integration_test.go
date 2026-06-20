//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
	"go.opentelemetry.io/otel"
)

// TestIntegrationAvatarRepositorySoftDeleteFiltersActiveReads проверяет repository на реальном PostgreSQL
func TestIntegrationAvatarRepositorySoftDeleteFiltersActiveReads(t *testing.T) {
	db := openIntegrationDB(t)
	cleanupIntegrationTables(t, db)
	t.Cleanup(func() {
		cleanupIntegrationTables(t, db)
	})

	ctx := context.Background()
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	userID := "6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e"
	avatarID := "b3d3a31b-8333-49d7-b921-eccbfdfb5074"

	userRepo := NewUserRepository(db)
	if err := userRepo.CreateUser(ctx, user.User{
		ID:        userID,
		Email:     "user@example.com",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}

	repo := NewAvatarRepository(db)
	item := avatar.Avatar{
		ID:                avatarID,
		UserID:            userID,
		FileName:          "avatar.png",
		MimeType:          "image/png",
		SizeBytes:         128,
		Status:            avatar.StatusProcessing,
		OriginalObjectKey: "avatars/" + userID + "/" + avatarID + "/original",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := repo.CreateAvatar(ctx, item); err != nil {
		t.Fatalf("CreateAvatar returned error: %v", err)
	}

	got, err := repo.GetAvatar(ctx, avatarID)
	if err != nil {
		t.Fatalf("GetAvatar returned error: %v", err)
	}
	if got.ID != avatarID || got.UserID != userID {
		t.Fatalf("avatar = %#v, want created avatar", got)
	}

	active, err := repo.ListAvatarsByUser(ctx, userID, 10, 0)
	if err != nil {
		t.Fatalf("ListAvatarsByUser returned error: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active avatars = %d, want 1", len(active))
	}

	deletedAt := now.Add(time.Minute)
	if err := repo.SoftDeleteAvatar(ctx, avatarID, userID, deletedAt); err != nil {
		t.Fatalf("SoftDeleteAvatar returned error: %v", err)
	}

	_, err = repo.GetAvatar(ctx, avatarID)
	if !errors.Is(err, avatar.ErrNotFound) {
		t.Fatalf("GetAvatar error = %v, want ErrNotFound", err)
	}

	active, err = repo.ListAvatarsByUser(ctx, userID, 10, 0)
	if err != nil {
		t.Fatalf("ListAvatarsByUser after delete returned error: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active avatars after delete = %d, want 0", len(active))
	}

	deleted, err := repo.GetAvatarIncludingDeleted(ctx, avatarID)
	if err != nil {
		t.Fatalf("GetAvatarIncludingDeleted returned error: %v", err)
	}
	if !deleted.IsDeleted() || deleted.Status != avatar.StatusDeleting {
		t.Fatalf("deleted avatar = %#v, want soft deleted deleting status", deleted)
	}
}

// TestIntegrationUserRepositoryFindOrCreateKeepsStableID проверяет email lookup на реальном PostgreSQL
func TestIntegrationUserRepositoryFindOrCreateKeepsStableID(t *testing.T) {
	db := openIntegrationDB(t)
	cleanupIntegrationTables(t, db)
	t.Cleanup(func() {
		cleanupIntegrationTables(t, db)
	})

	ctx := context.Background()
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	repo := NewUserRepository(db)

	first, err := repo.FindOrCreateUserByEmail(ctx, "User@Example.COM", now)
	if err != nil {
		t.Fatalf("first FindOrCreateUserByEmail returned error: %v", err)
	}
	second, err := repo.FindOrCreateUserByEmail(ctx, "user@example.com", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("second FindOrCreateUserByEmail returned error: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("user ids = %q and %q, want stable id", first.ID, second.ID)
	}

	found, err := repo.GetUserByEmail(ctx, "USER@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail returned error: %v", err)
	}
	if found.ID != first.ID {
		t.Fatalf("found user id = %q, want %q", found.ID, first.ID)
	}
}

// TestIntegrationPostgresQueryCreatesChildSpan проверяет trace настоящего PostgreSQL query
func TestIntegrationPostgresQueryCreatesChildSpan(t *testing.T) {
	recorder := installPostgresSpanRecorder(t)
	db := openIntegrationDB(t)
	cleanupIntegrationTables(t, db)
	t.Cleanup(func() {
		cleanupIntegrationTables(t, db)
	})

	ctx, parent := otel.Tracer("integration-test").Start(context.Background(), "upload")
	parentSpanID := parent.SpanContext().SpanID()
	repo := NewUserRepository(db)
	err := repo.CreateUser(ctx, user.User{
		ID:        "0c543858-df6a-4596-a3c5-0c8f2d74f153",
		Email:     "trace@example.com",
		CreatedAt: time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC),
	})
	parent.End()
	if err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}

	for _, span := range recorder.Ended() {
		if span.Name() == "INSERT users" {
			if span.Parent().SpanID() != parentSpanID {
				t.Fatalf("database parent span = %s, want %s", span.Parent().SpanID(), parentSpanID)
			}
			return
		}
	}
	t.Fatal("INSERT users span was not recorded")
}

// openIntegrationDB открывает подключение к PostgreSQL для integration suite
func openIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is required for integration tests")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping db: %v", err)
	}

	return db
}

// cleanupIntegrationTables очищает таблицы между integration тестами
func cleanupIntegrationTables(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, "TRUNCATE outbox_events, avatars, users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate integration tables: %v", err)
	}
}
