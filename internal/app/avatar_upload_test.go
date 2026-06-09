package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
)

// TestUploadAvatarStoresOriginalCreatesAvatarAndPublishesEvent проверяет успешный поток upload
func TestUploadAvatarStoresOriginalCreatesAvatarAndPublishesEvent(t *testing.T) {
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	users := &fakeUserResolver{
		item: user.User{
			ID:    "6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e",
			Email: "user@example.com",
		},
	}
	avatarOutbox := &fakeAvatarOutboxStore{}
	objects := &fakeObjectStore{}
	publisher := &fakeEventPublisher{}
	service := NewAvatarUploadService(users, avatarOutbox, objects, publisher)
	service.now = func() time.Time { return now }

	result, err := service.UploadAvatar(context.Background(), AvatarUploadRequest{
		UserEmail:   "user@example.com",
		FileName:    "avatar.png",
		ContentType: "image/png",
		Size:        7,
		Width:       10,
		Height:      20,
		Reader:      bytes.NewReader([]byte("payload")),
	})
	if err != nil {
		t.Fatalf("UploadAvatar returned error: %v", err)
	}

	if result.UserID != "6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e" {
		t.Fatalf("UserID = %q, want resolved user id", result.UserID)
	}
	if !objects.putCalled {
		t.Fatal("S3 Put should be called")
	}
	if !avatarOutbox.createCalled {
		t.Fatal("CreateAvatarWithOutbox should be called")
	}
	if !publisher.publishCalled {
		t.Fatal("Publish should be called")
	}
	if publisher.topic != queuekafka.TopicAvatarProcess {
		t.Fatalf("topic = %q, want avatar process topic", publisher.topic)
	}
	if !avatarOutbox.markPublishedCalled {
		t.Fatal("MarkOutboxPublished should be called after successful publish")
	}
	if avatarOutbox.created.Status != avatar.StatusProcessing {
		t.Fatalf("created status = %q, want processing", avatarOutbox.created.Status)
	}
	if avatarOutbox.createdEvent.Topic != queuekafka.TopicAvatarProcess {
		t.Fatalf("outbox topic = %q, want avatar process topic", avatarOutbox.createdEvent.Topic)
	}
}

// TestUploadAvatarDoesNotCreateDBRecordWhenS3Fails проверяет порядок S3 до БД
func TestUploadAvatarDoesNotCreateDBRecordWhenS3Fails(t *testing.T) {
	avatarOutbox := &fakeAvatarOutboxStore{}
	service := NewAvatarUploadService(
		&fakeUserResolver{item: user.User{ID: "user-id", Email: "user@example.com"}},
		avatarOutbox,
		&fakeObjectStore{putErr: errors.New("s3 down")},
		&fakeEventPublisher{},
	)

	_, err := service.UploadAvatar(context.Background(), AvatarUploadRequest{
		UserEmail:   "user@example.com",
		ContentType: "image/png",
		Reader:      bytes.NewReader([]byte("payload")),
	})
	if err == nil {
		t.Fatal("UploadAvatar should return error")
	}

	if avatarOutbox.createCalled {
		t.Fatal("CreateAvatarWithOutbox should not be called after S3 failure")
	}
}

// TestUploadAvatarKeepsOutboxPendingWhenPublishFails проверяет outbox компенсацию после ошибки publish
func TestUploadAvatarKeepsOutboxPendingWhenPublishFails(t *testing.T) {
	avatarOutbox := &fakeAvatarOutboxStore{}
	publisher := &fakeEventPublisher{publishErr: errors.New("kafka down")}
	service := NewAvatarUploadService(
		&fakeUserResolver{item: user.User{ID: "user-id", Email: "user@example.com"}},
		avatarOutbox,
		&fakeObjectStore{},
		publisher,
	)

	_, err := service.UploadAvatar(context.Background(), AvatarUploadRequest{
		UserEmail:   "user@example.com",
		ContentType: "image/png",
		Reader:      bytes.NewReader([]byte("payload")),
	})
	if err != nil {
		t.Fatalf("UploadAvatar returned error: %v", err)
	}
	if !avatarOutbox.markFailedAttemptCalled {
		t.Fatal("MarkOutboxPublishAttemptFailed should be called")
	}
	if publisher.publishCalls != 1 {
		t.Fatalf("publishCalls = %d, want best effort single publish", publisher.publishCalls)
	}
}

type fakeUserResolver struct {
	item user.User
	err  error
}

// FindOrCreateUserByEmail возвращает fake-пользователя
func (f *fakeUserResolver) FindOrCreateUserByEmail(ctx context.Context, email string, now time.Time) (user.User, error) {
	if f.err != nil {
		return user.User{}, f.err
	}
	return f.item, nil
}

type fakeAvatarOutboxStore struct {
	createCalled            bool
	created                 avatar.Avatar
	createdEvent            outbox.Event
	createErr               error
	markPublishedCalled     bool
	markFailedAttemptCalled bool
}

// CreateAvatarWithOutbox запоминает fake-запись avatar и outbox событие
func (f *fakeAvatarOutboxStore) CreateAvatarWithOutbox(ctx context.Context, item avatar.Avatar, event outbox.Event) error {
	f.createCalled = true
	f.created = item
	f.createdEvent = event
	return f.createErr
}

// MarkOutboxPublished запоминает fake-успешную публикацию outbox
func (f *fakeAvatarOutboxStore) MarkOutboxPublished(ctx context.Context, id string, publishedAt time.Time) error {
	f.markPublishedCalled = true
	return nil
}

// MarkOutboxPublishAttemptFailed запоминает fake-ошибку публикации outbox
func (f *fakeAvatarOutboxStore) MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) error {
	f.markFailedAttemptCalled = true
	return nil
}

type fakeObjectStore struct {
	putCalled bool
	putErr    error
}

// Put запоминает fake-загрузку object storage
func (f *fakeObjectStore) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	f.putCalled = true
	return f.putErr
}

type fakeEventPublisher struct {
	publishCalled bool
	publishCalls  int
	publishErr    error
	topic         string
}

// Publish запоминает fake-публикацию события
func (f *fakeEventPublisher) Publish(ctx context.Context, topic string, key string, payload []byte) error {
	f.publishCalled = true
	f.publishCalls++
	f.topic = topic
	return f.publishErr
}
