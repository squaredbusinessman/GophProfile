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

const uploadTestUserID = "6f3f3c2d-df58-4e64-91ea-cdf90f4c9c1e"

// TestUploadAvatarStoresOriginalCreatesAvatarAndPublishesEvent проверяет успешный поток upload
func TestUploadAvatarStoresOriginalCreatesAvatarAndPublishesEvent(t *testing.T) {
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	avatarOutbox := &fakeAvatarOutboxStore{}
	objects := &fakeObjectStore{}
	publisher := &fakeEventPublisher{}
	service := NewAvatarUploadService(&fakeUserLookup{item: user.User{ID: uploadTestUserID}}, avatarOutbox, objects, publisher)
	service.now = func() time.Time { return now }

	result, err := service.UploadAvatar(context.Background(), AvatarUploadRequest{
		UserID:      uploadTestUserID,
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

	if result.UserID != uploadTestUserID {
		t.Fatalf("UserID = %q, want request user id", result.UserID)
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
	if avatarOutbox.created.UserID != uploadTestUserID {
		t.Fatalf("created UserID = %q, want request user id", avatarOutbox.created.UserID)
	}
	if avatarOutbox.createdEvent.Topic != queuekafka.TopicAvatarProcess {
		t.Fatalf("outbox topic = %q, want avatar process topic", avatarOutbox.createdEvent.Topic)
	}
}

// TestUploadAvatarDoesNotCreateDBRecordWhenS3Fails проверяет порядок S3 до БД
func TestUploadAvatarDoesNotCreateDBRecordWhenS3Fails(t *testing.T) {
	avatarOutbox := &fakeAvatarOutboxStore{}
	service := NewAvatarUploadService(
		&fakeUserLookup{item: user.User{ID: uploadTestUserID}},
		avatarOutbox,
		&fakeObjectStore{putErr: errors.New("s3 down")},
		&fakeEventPublisher{},
	)

	_, err := service.UploadAvatar(context.Background(), AvatarUploadRequest{
		UserID:      uploadTestUserID,
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
		&fakeUserLookup{item: user.User{ID: uploadTestUserID}},
		avatarOutbox,
		&fakeObjectStore{},
		publisher,
	)

	_, err := service.UploadAvatar(context.Background(), AvatarUploadRequest{
		UserID:      uploadTestUserID,
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

// TestUploadAvatarReturnsUserNotFound проверяет отсутствие пользователя по UUID
func TestUploadAvatarReturnsUserNotFound(t *testing.T) {
	avatarOutbox := &fakeAvatarOutboxStore{}
	objects := &fakeObjectStore{}
	service := NewAvatarUploadService(
		&fakeUserLookup{err: user.ErrNotFound},
		avatarOutbox,
		objects,
		&fakeEventPublisher{},
	)

	_, err := service.UploadAvatar(context.Background(), AvatarUploadRequest{
		UserID:      uploadTestUserID,
		ContentType: "image/png",
		Reader:      bytes.NewReader([]byte("payload")),
	})
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("error = %v, want ErrUserNotFound", err)
	}
	if objects.putCalled || avatarOutbox.createCalled {
		t.Fatal("upload should stop before S3 and DB for missing user")
	}
}

type fakeUserLookup struct {
	item user.User
	err  error
}

// GetUser возвращает тестового пользователя по UUID
func (f *fakeUserLookup) GetUser(ctx context.Context, id string) (user.User, error) {
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

// CreateAvatarWithOutbox запоминает тестовые записи аватара и события outbox
func (f *fakeAvatarOutboxStore) CreateAvatarWithOutbox(ctx context.Context, item avatar.Avatar, event outbox.Event) error {
	f.createCalled = true
	f.created = item
	f.createdEvent = event
	return f.createErr
}

// MarkOutboxPublished запоминает успешную публикацию тестового события outbox
func (f *fakeAvatarOutboxStore) MarkOutboxPublished(ctx context.Context, id string, publishedAt time.Time) error {
	f.markPublishedCalled = true
	return nil
}

// MarkOutboxPublishAttemptFailed запоминает ошибку публикации тестового события outbox
func (f *fakeAvatarOutboxStore) MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) error {
	f.markFailedAttemptCalled = true
	return nil
}

type fakeObjectStore struct {
	putCalled bool
	putErr    error
}

// Put запоминает тестовую загрузку в объектное хранилище
func (f *fakeObjectStore) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	f.putCalled = true
	return f.putErr
}

type fakeEventPublisher struct {
	publishCalled bool
	publishCalls  int
	publishErr    error
	topic         string
	key           string
	payload       []byte
	headers       map[string]string
}

// Publish запоминает тестовую публикацию события
func (f *fakeEventPublisher) Publish(ctx context.Context, topic string, key string, payload []byte, headers map[string]string) error {
	f.publishCalled = true
	f.publishCalls++
	f.topic = topic
	f.key = key
	f.payload = append([]byte(nil), payload...)
	f.headers = make(map[string]string, len(headers))
	for name, value := range headers {
		f.headers[name] = value
	}
	return f.publishErr
}
