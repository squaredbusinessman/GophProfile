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
	service := NewOutboxPublisherService(store, publisher)
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
	service := NewOutboxPublisherService(store, publisher)
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

type fakeOutboxEventStore struct {
	events                  []outbox.Event
	listErr                 error
	markPublishedCalled     bool
	markFailedAttemptCalled bool
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
	return nil
}

// MarkOutboxPublishAttemptFailed запоминает fake-ошибку публикации
func (f *fakeOutboxEventStore) MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) error {
	f.markFailedAttemptCalled = true
	return nil
}
