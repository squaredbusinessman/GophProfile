package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
)

// TestOutboxPublisherPublishesPendingEvents проверяет успешную публикацию pending-событий
func TestOutboxPublisherPublishesPendingEvents(t *testing.T) {
	store := &fakeOutboxEventStore{
		events: []outbox.Event{
			{
				ID:      "event-1",
				Topic:   "avatar.process.v1",
				Key:     "avatar-1",
				Payload: []byte(`{"avatar_id":"avatar-1"}`),
			},
		},
	}
	publisher := &fakeEventPublisher{}
	service := newOutboxPublisherServiceForTest(t, store, publisher)
	service.now = func() time.Time { return time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC) }

	published, err := service.PublishPending(context.Background(), 100)
	if err != nil {
		t.Fatalf("PublishPending returned error: %v", err)
	}
	if published != 1 {
		t.Fatalf("published = %d, want 1", published)
	}
	if !store.markPublishedCalled {
		t.Fatal("MarkOutboxPublished should be called")
	}
}

// TestOutboxPublisherKeepsEventPendingWhenPublishFails проверяет сохранение ошибки Kafka
func TestOutboxPublisherKeepsEventPendingWhenPublishFails(t *testing.T) {
	store := &fakeOutboxEventStore{
		events: []outbox.Event{
			{
				ID:      "event-1",
				Topic:   "avatar.process.v1",
				Key:     "avatar-1",
				Payload: []byte(`{"avatar_id":"avatar-1"}`),
			},
		},
	}
	const secret = "postgres://secret:password@db:5432/gophprofile"
	publisher := &fakeEventPublisher{publishErr: errors.New(secret)}
	service := newOutboxPublisherServiceForTest(t, store, publisher)
	var logs bytes.Buffer
	ctx := ContextWithLogger(context.Background(), zerolog.New(&logs))

	published, err := service.PublishPending(ctx, 100)
	if err != nil {
		t.Fatalf("PublishPending returned error: %v", err)
	}
	if published != 0 {
		t.Fatalf("published = %d, want 0", published)
	}
	if !store.markFailedAttemptCalled {
		t.Fatal("MarkOutboxPublishAttemptFailed should be called")
	}
	if !strings.Contains(logs.String(), `"level":"warn"`) || strings.Contains(logs.String(), secret) {
		t.Fatalf("recoverable log имеет неверный уровень или содержит секрет: %s", logs.String())
	}
}

// TestOutboxPublisherLogsStateUpdateError проверяет журналирование ошибки записи publish attempt
func TestOutboxPublisherLogsStateUpdateError(t *testing.T) {
	const secret = "postgres://secret:password@db:5432/gophprofile"
	store := &fakeOutboxEventStore{
		events: []outbox.Event{
			{
				ID:      "event-1",
				Topic:   "avatar.process.v1",
				Key:     "avatar-1",
				Payload: []byte(`{"avatar_id":"avatar-1"}`),
			},
		},
		markFailedAttemptErr: errors.New(secret),
	}
	publisher := &fakeEventPublisher{publishErr: errors.New("kafka down")}
	service := newOutboxPublisherServiceForTest(t, store, publisher)
	var logs bytes.Buffer
	ctx := ContextWithLogger(context.Background(), zerolog.New(&logs))

	published, err := service.PublishPending(ctx, 100)
	if err != nil {
		t.Fatalf("PublishPending returned error: %v", err)
	}
	if published != 0 {
		t.Fatalf("published = %d, want 0", published)
	}
	logText := logs.String()
	if !strings.Contains(logText, `"level":"error"`) || !strings.Contains(logText, `"operation":"mark_publish_attempt_failed"`) {
		t.Fatalf("log does not contain expected outbox state error: %s", logText)
	}
	if strings.Contains(logText, secret) || strings.Contains(logText, `{"avatar_id"`) || strings.Contains(logText, "avatar-1") {
		t.Fatalf("log contains sensitive message data: %s", logText)
	}
}

type fakeOutboxEventStore struct {
	events                  []outbox.Event
	listErr                 error
	markPublishedCalled     bool
	markFailedAttemptCalled bool
	markPublishedErr        error
	markFailedAttemptErr    error
}

// ListPendingOutboxEvents возвращает fake pending outbox события
func (f *fakeOutboxEventStore) ListPendingOutboxEvents(ctx context.Context, limit int) ([]outbox.Event, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.events, nil
}

// MarkOutboxPublished запоминает fake-успешную публикацию
func (f *fakeOutboxEventStore) MarkOutboxPublished(ctx context.Context, id string, publishedAt time.Time) error {
	f.markPublishedCalled = true
	return f.markPublishedErr
}

// MarkOutboxPublishAttemptFailed запоминает fake-ошибку публикации
func (f *fakeOutboxEventStore) MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) error {
	f.markFailedAttemptCalled = true
	return f.markFailedAttemptErr
}
