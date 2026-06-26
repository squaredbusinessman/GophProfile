package postgres

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
)

// TestReadOutboxOperationalStatsReturnsPersistentBacklog проверяет агрегаты outbox из БД
func TestReadOutboxOperationalStatsReturnsPersistentBacklog(t *testing.T) {
	db, mock := newMockDB(t)
	repo := newOutboxRepositoryForTest(t, db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT\n\t\t\tCOUNT(*),")).
		WithArgs(string(outbox.StatusPending)).
		WillReturnRows(sqlmock.NewRows([]string{"count", "oldest_age"}).AddRow(int64(5), 37.5))

	pendingCount, oldestAgeSeconds, err := repo.ReadOutboxOperationalStats(context.Background())
	if err != nil {
		t.Fatalf("ReadOutboxOperationalStats() error = %v", err)
	}
	if pendingCount != 5 || oldestAgeSeconds != 37.5 {
		t.Fatalf("outbox stats = count %d age %f", pendingCount, oldestAgeSeconds)
	}
	assertExpectations(t, mock)
}

// TestCreateAvatarWithOutboxWritesBothRecordsInTransaction проверяет атомарную запись avatar и outbox
func TestCreateAvatarWithOutboxWritesBothRecordsInTransaction(t *testing.T) {
	recorder := installPostgresSpanRecorder(t)
	db, mock := newMockDB(t)
	repo := newOutboxRepositoryForTest(t, db)
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO avatars")).
		WithArgs(
			"4a992fa3-df1a-4b5f-b764-546e99643eb0",
			"6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e",
			"avatar.png",
			"image/png",
			int64(128),
			sql.NullInt64{Int64: 10, Valid: true},
			sql.NullInt64{Int64: 20, Valid: true},
			string(avatar.StatusProcessing),
			"avatars/6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e/avatar/original",
			sql.NullString{},
			sql.NullString{},
			now,
			now,
			sql.NullTime{},
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox_events")).
		WithArgs(
			"7e9b73db-f6d6-466d-aaee-34d4e9e76615",
			"avatar.process.v1",
			"4a992fa3-df1a-4b5f-b764-546e99643eb0",
			[]byte(`{"avatar_id":"4a992fa3-df1a-4b5f-b764-546e99643eb0"}`),
			[]byte(`{"traceparent":"00-11111111111111111111111111111111-2222222222222222-01"}`),
			string(outbox.StatusPending),
			0,
			sql.NullString{},
			now,
			now,
			sql.NullTime{},
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	width := 10
	height := 20
	err := repo.CreateAvatarWithOutbox(context.Background(), avatar.Avatar{
		ID:                "4a992fa3-df1a-4b5f-b764-546e99643eb0",
		UserID:            "6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e",
		FileName:          "avatar.png",
		MimeType:          "image/png",
		SizeBytes:         128,
		Width:             &width,
		Height:            &height,
		Status:            avatar.StatusProcessing,
		OriginalObjectKey: "avatars/6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e/avatar/original",
		CreatedAt:         now,
		UpdatedAt:         now,
	}, outbox.Event{
		ID:      "7e9b73db-f6d6-466d-aaee-34d4e9e76615",
		Topic:   "avatar.process.v1",
		Key:     "4a992fa3-df1a-4b5f-b764-546e99643eb0",
		Payload: []byte(`{"avatar_id":"4a992fa3-df1a-4b5f-b764-546e99643eb0"}`),
		Headers: map[string]string{
			"traceparent": "00-11111111111111111111111111111111-2222222222222222-01",
		},
		Status:    outbox.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateAvatarWithOutbox returned error: %v", err)
	}

	assertExpectations(t, mock)
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	names := eventNames(spans[0].Events())
	if !names["db.transaction.begin"] || !names["db.transaction.commit"] || names["db.transaction.rollback"] {
		t.Fatalf("unexpected transaction events: %v", names)
	}
}

// TestCreateAvatarWithOutboxRecordsRollback проверяет событие rollback при отмене транзакции
func TestCreateAvatarWithOutboxRecordsRollback(t *testing.T) {
	recorder := installPostgresSpanRecorder(t)
	db, mock := newMockDB(t)
	repo := newOutboxRepositoryForTest(t, db)

	mock.ExpectBegin()
	mock.ExpectRollback()

	err := repo.CreateAvatarWithOutbox(context.Background(), avatar.Avatar{Status: avatar.Status("invalid")}, outbox.Event{})
	if err == nil {
		t.Fatal("CreateAvatarWithOutbox() error = nil")
	}
	assertExpectations(t, mock)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	names := eventNames(spans[0].Events())
	if !names["db.transaction.begin"] || !names["db.transaction.rollback"] || names["db.transaction.commit"] {
		t.Fatalf("unexpected transaction events: %v", names)
	}
}

// TestSoftDeleteAvatarWithOutboxWritesBothRecordsInTransaction проверяет атомарный soft delete и outbox
func TestSoftDeleteAvatarWithOutboxWritesBothRecordsInTransaction(t *testing.T) {
	db, mock := newMockDB(t)
	repo := newOutboxRepositoryForTest(t, db)
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE avatars")).
		WithArgs(
			"4a992fa3-df1a-4b5f-b764-546e99643eb0",
			"6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e",
			string(avatar.StatusDeleting),
			now,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox_events")).
		WithArgs(
			"7e9b73db-f6d6-466d-aaee-34d4e9e76615",
			"avatar.delete.v1",
			"4a992fa3-df1a-4b5f-b764-546e99643eb0",
			[]byte(`{"avatar_id":"4a992fa3-df1a-4b5f-b764-546e99643eb0"}`),
			[]byte(`{}`),
			string(outbox.StatusPending),
			0,
			sql.NullString{},
			now,
			now,
			sql.NullTime{},
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.SoftDeleteAvatarWithOutbox(context.Background(),
		"4a992fa3-df1a-4b5f-b764-546e99643eb0",
		"6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e",
		now,
		outbox.Event{
			ID:        "7e9b73db-f6d6-466d-aaee-34d4e9e76615",
			Topic:     "avatar.delete.v1",
			Key:       "4a992fa3-df1a-4b5f-b764-546e99643eb0",
			Payload:   []byte(`{"avatar_id":"4a992fa3-df1a-4b5f-b764-546e99643eb0"}`),
			Status:    outbox.StatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		},
	)
	if err != nil {
		t.Fatalf("SoftDeleteAvatarWithOutbox returned error: %v", err)
	}

	assertExpectations(t, mock)
}

// TestListPendingOutboxEventsRestoresHeaders проверяет чтение сохранённого carrier
func TestListPendingOutboxEventsRestoresHeaders(t *testing.T) {
	db, mock := newMockDB(t)
	repo := newOutboxRepositoryForTest(t, db)
	now := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	columns := []string{
		"id", "topic", "event_key", "payload", "headers", "status", "attempts",
		"last_error", "created_at", "updated_at", "published_at",
	}
	mock.ExpectQuery(regexp.QuoteMeta("FROM outbox_events")).
		WithArgs(string(outbox.StatusPending), 10).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			"event-id",
			"avatar.process.v1",
			"avatar-id",
			[]byte(`{"avatar_id":"avatar-id"}`),
			[]byte(`{"traceparent":"00-11111111111111111111111111111111-2222222222222222-01","x-custom":"preserved"}`),
			string(outbox.StatusPending),
			0,
			nil,
			now,
			now,
			nil,
		))

	events, err := repo.ListPendingOutboxEvents(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListPendingOutboxEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Headers["x-custom"] != "preserved" || events[0].Headers["traceparent"] == "" {
		t.Fatalf("restored headers = %#v", events)
	}
	assertExpectations(t, mock)
}

// TestMarkOutboxPublishedUpdatesPendingEvent проверяет отметку успешной публикации
func TestMarkOutboxPublishedUpdatesPendingEvent(t *testing.T) {
	db, mock := newMockDB(t)
	repo := newOutboxRepositoryForTest(t, db)
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta("UPDATE outbox_events")).
		WithArgs("event-id", string(outbox.StatusPublished), now, string(outbox.StatusPending)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.MarkOutboxPublished(context.Background(), "event-id", now); err != nil {
		t.Fatalf("MarkOutboxPublished returned error: %v", err)
	}

	assertExpectations(t, mock)
}

// TestMarkOutboxPublishAttemptFailedKeepsEventPending проверяет запись ошибки публикации
func TestMarkOutboxPublishAttemptFailedKeepsEventPending(t *testing.T) {
	db, mock := newMockDB(t)
	repo := newOutboxRepositoryForTest(t, db)
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta("UPDATE outbox_events")).
		WithArgs("event-id", "kafka down", now, string(outbox.StatusPending)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.MarkOutboxPublishAttemptFailed(context.Background(), "event-id", assertError("kafka down"), now)
	if err != nil {
		t.Fatalf("MarkOutboxPublishAttemptFailed returned error: %v", err)
	}

	assertExpectations(t, mock)
}

type assertError string

// Error возвращает текст тестовой ошибки
func (e assertError) Error() string {
	return string(e)
}
