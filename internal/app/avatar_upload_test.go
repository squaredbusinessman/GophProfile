package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
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
	avatars := &fakeAvatarStore{}
	objects := &fakeObjectStore{}
	publisher := &fakeEventPublisher{}
	service := NewAvatarUploadService(users, avatars, objects, publisher)
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
	if !avatars.createCalled {
		t.Fatal("CreateAvatar should be called")
	}
	if !publisher.publishCalled {
		t.Fatal("Publish should be called")
	}
	if publisher.topic != queuekafka.TopicAvatarProcess {
		t.Fatalf("topic = %q, want avatar process topic", publisher.topic)
	}
	if avatars.created.Status != avatar.StatusProcessing {
		t.Fatalf("created status = %q, want processing", avatars.created.Status)
	}
}

// TestUploadAvatarDoesNotCreateDBRecordWhenS3Fails проверяет порядок S3 до БД
func TestUploadAvatarDoesNotCreateDBRecordWhenS3Fails(t *testing.T) {
	service := NewAvatarUploadService(
		&fakeUserResolver{item: user.User{ID: "user-id", Email: "user@example.com"}},
		&fakeAvatarStore{},
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

	avatars := service.avatars.(*fakeAvatarStore)
	if avatars.createCalled {
		t.Fatal("CreateAvatar should not be called after S3 failure")
	}
}

// TestUploadAvatarMarksFailedWhenPublishFails проверяет компенсацию после ошибки publish
func TestUploadAvatarMarksFailedWhenPublishFails(t *testing.T) {
	avatars := &fakeAvatarStore{}
	publisher := &fakeEventPublisher{publishErr: errors.New("kafka down")}
	service := NewAvatarUploadService(
		&fakeUserResolver{item: user.User{ID: "user-id", Email: "user@example.com"}},
		avatars,
		&fakeObjectStore{},
		publisher,
	)

	_, err := service.UploadAvatar(context.Background(), AvatarUploadRequest{
		UserEmail:   "user@example.com",
		ContentType: "image/png",
		Reader:      bytes.NewReader([]byte("payload")),
	})
	if err == nil {
		t.Fatal("UploadAvatar should return publish error")
	}
	if avatars.updatedStatus != avatar.StatusFailed {
		t.Fatalf("updatedStatus = %q, want failed", avatars.updatedStatus)
	}
	if publisher.publishCalls != avatarPublishAttempts {
		t.Fatalf("publishCalls = %d, want %d", publisher.publishCalls, avatarPublishAttempts)
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

type fakeAvatarStore struct {
	createCalled  bool
	created       avatar.Avatar
	createErr     error
	updatedStatus avatar.Status
}

// CreateAvatar запоминает fake-запись avatar
func (f *fakeAvatarStore) CreateAvatar(ctx context.Context, item avatar.Avatar) error {
	f.createCalled = true
	f.created = item
	return f.createErr
}

// UpdateAvatarStatus запоминает fake-статус avatar
func (f *fakeAvatarStore) UpdateAvatarStatus(ctx context.Context, id string, status avatar.Status, updatedAt time.Time) error {
	f.updatedStatus = status
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
