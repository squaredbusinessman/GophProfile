package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
)

// TestDeleteAvatarByIDSoftDeletesAndPublishes проверяет soft delete и outbox publish
func TestDeleteAvatarByIDSoftDeletesAndPublishes(t *testing.T) {
	avatars := &fakeAvatarDeleteRepository{
		item: avatar.Avatar{
			ID:                "avatar-1",
			UserID:            "user-1",
			Status:            avatar.StatusReady,
			OriginalObjectKey: "avatars/user-1/avatar-1/original",
		},
	}
	events := &fakeAvatarDeleteOutboxStore{}
	publisher := &fakeEventPublisher{}
	service := NewAvatarDeleteService(avatars, events, publisher)

	err := service.DeleteAvatarByID(context.Background(), "avatar-1", "user-1")
	if err != nil {
		t.Fatalf("DeleteAvatarByID returned error: %v", err)
	}

	if !events.softDeleteCalled {
		t.Fatal("SoftDeleteAvatarWithOutbox should be called")
	}
	if events.avatarID != "avatar-1" || events.userID != "user-1" {
		t.Fatalf("soft delete args = %q %q, want avatar-1 user-1", events.avatarID, events.userID)
	}
	if events.event.Topic != queuekafka.TopicAvatarDelete {
		t.Fatalf("topic = %q, want avatar delete", events.event.Topic)
	}
	if !publisher.publishCalled || publisher.topic != queuekafka.TopicAvatarDelete || publisher.key != "avatar-1" {
		t.Fatalf("publisher = %#v, want avatar delete publish", publisher)
	}
	if !events.markPublishedCalled {
		t.Fatal("MarkOutboxPublished should be called")
	}

	var payload AvatarDeleteEvent
	if err := json.Unmarshal(publisher.payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.AvatarID != "avatar-1" || payload.UserID != "user-1" {
		t.Fatalf("payload = %#v, want avatar id and user id", payload)
	}
}

// TestDeleteAvatarByIDRejectsForeignOwner проверяет запрет удаления чужой avatar
func TestDeleteAvatarByIDRejectsForeignOwner(t *testing.T) {
	avatars := &fakeAvatarDeleteRepository{
		item: avatar.Avatar{
			ID:                "avatar-1",
			UserID:            "owner-1",
			Status:            avatar.StatusReady,
			OriginalObjectKey: "avatars/owner-1/avatar-1/original",
		},
	}
	events := &fakeAvatarDeleteOutboxStore{}
	publisher := &fakeEventPublisher{}
	service := NewAvatarDeleteService(avatars, events, publisher)

	err := service.DeleteAvatarByID(context.Background(), "avatar-1", "user-2")
	if !errors.Is(err, ErrAvatarForbidden) {
		t.Fatalf("error = %v, want ErrAvatarForbidden", err)
	}
	if events.softDeleteCalled || publisher.publishCalled {
		t.Fatal("foreign delete should not write outbox or publish")
	}
}

// TestDeleteAvatarByIDIsNoopForAlreadyDeletedAvatar проверяет повторный DELETE по id
func TestDeleteAvatarByIDIsNoopForAlreadyDeletedAvatar(t *testing.T) {
	deletedAt := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	avatars := &fakeAvatarDeleteRepository{
		item: avatar.Avatar{
			ID:                "avatar-1",
			UserID:            "user-1",
			Status:            avatar.StatusDeleting,
			OriginalObjectKey: "avatars/user-1/avatar-1/original",
			DeletedAt:         &deletedAt,
		},
	}
	events := &fakeAvatarDeleteOutboxStore{}
	publisher := &fakeEventPublisher{}
	service := NewAvatarDeleteService(avatars, events, publisher)

	err := service.DeleteAvatarByID(context.Background(), "avatar-1", "user-1")
	if err != nil {
		t.Fatalf("DeleteAvatarByID returned error: %v", err)
	}
	if events.softDeleteCalled || publisher.publishCalled {
		t.Fatal("already deleted avatar should be noop")
	}
}

// TestDeleteLatestAvatarByUserIDIsNoopWithoutActiveAvatar проверяет повторный DELETE по пользователю
func TestDeleteLatestAvatarByUserIDIsNoopWithoutActiveAvatar(t *testing.T) {
	service := NewAvatarDeleteService(
		&fakeAvatarDeleteRepository{},
		&fakeAvatarDeleteOutboxStore{},
		&fakeEventPublisher{},
	)

	err := service.DeleteLatestAvatarByUserID(context.Background(), "user-1", "user-1")
	if err != nil {
		t.Fatalf("DeleteLatestAvatarByUserID returned error: %v", err)
	}
}

// TestHandleDeleteMessageDeletesObjectsAndMarksDeleted проверяет worker удаления S3 objects
func TestHandleDeleteMessageDeletesObjectsAndMarksDeleted(t *testing.T) {
	thumb100 := "avatars/user-1/avatar-1/100x100"
	thumb300 := "avatars/user-1/avatar-1/300x300"
	avatars := &fakeAvatarDeleteRepository{
		item: avatar.Avatar{
			ID:                "avatar-1",
			UserID:            "user-1",
			Status:            avatar.StatusDeleting,
			OriginalObjectKey: "avatars/user-1/avatar-1/original",
			Thumb100ObjectKey: &thumb100,
			Thumb300ObjectKey: &thumb300,
		},
	}
	objects := &fakeAvatarDeleteObjectStore{}
	service := NewAvatarDeleteWorkerService(avatars, objects)

	err := service.HandleDeleteMessage(context.Background(), []byte(`{"avatar_id":"avatar-1","user_id":"user-1"}`))
	if err != nil {
		t.Fatalf("HandleDeleteMessage returned error: %v", err)
	}

	wantKeys := []string{
		"avatars/user-1/avatar-1/original",
		"avatars/user-1/avatar-1/100x100",
		"avatars/user-1/avatar-1/300x300",
	}
	if len(objects.deletedKeys) != len(wantKeys) {
		t.Fatalf("deleted keys = %#v, want %#v", objects.deletedKeys, wantKeys)
	}
	for index, want := range wantKeys {
		if objects.deletedKeys[index] != want {
			t.Fatalf("deletedKeys[%d] = %q, want %q", index, objects.deletedKeys[index], want)
		}
	}
	if !avatars.markDeletedCalled || avatars.markDeletedID != "avatar-1" {
		t.Fatalf("mark deleted = %t %q, want avatar-1", avatars.markDeletedCalled, avatars.markDeletedID)
	}
}

// TestHandleDeleteMessageSkipsDeletedAvatar проверяет идемпотентный skip worker удаления
func TestHandleDeleteMessageSkipsDeletedAvatar(t *testing.T) {
	avatars := &fakeAvatarDeleteRepository{
		item: avatar.Avatar{
			ID:                "avatar-1",
			UserID:            "user-1",
			Status:            avatar.StatusDeleted,
			OriginalObjectKey: "avatars/user-1/avatar-1/original",
		},
	}
	objects := &fakeAvatarDeleteObjectStore{}
	service := NewAvatarDeleteWorkerService(avatars, objects)

	err := service.HandleDeleteMessage(context.Background(), []byte(`{"avatar_id":"avatar-1","user_id":"user-1"}`))
	if err != nil {
		t.Fatalf("HandleDeleteMessage returned error: %v", err)
	}
	if len(objects.deletedKeys) != 0 || avatars.markDeletedCalled {
		t.Fatal("deleted avatar should not delete objects again")
	}
}

type fakeAvatarDeleteRepository struct {
	item              avatar.Avatar
	items             []avatar.Avatar
	getErr            error
	listErr           error
	markDeletedCalled bool
	markDeletedID     string
	markErr           error
}

// GetAvatarIncludingDeleted возвращает fake avatar включая удаленные записи
func (f *fakeAvatarDeleteRepository) GetAvatarIncludingDeleted(ctx context.Context, id string) (avatar.Avatar, error) {
	if f.getErr != nil {
		return avatar.Avatar{}, f.getErr
	}
	if f.item.ID == "" {
		return avatar.Avatar{}, avatar.ErrNotFound
	}
	return f.item, nil
}

// ListAvatarsByUser возвращает fake список активных avatar пользователя
func (f *fakeAvatarDeleteRepository) ListAvatarsByUser(ctx context.Context, userID string, limit int, offset int) ([]avatar.Avatar, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.items, nil
}

// MarkAvatarDeleted запоминает перевод avatar в status deleted
func (f *fakeAvatarDeleteRepository) MarkAvatarDeleted(ctx context.Context, id string, updatedAt time.Time) error {
	f.markDeletedCalled = true
	f.markDeletedID = id
	return f.markErr
}

type fakeAvatarDeleteOutboxStore struct {
	softDeleteCalled        bool
	avatarID                string
	userID                  string
	event                   outbox.Event
	err                     error
	markPublishedCalled     bool
	markFailedAttemptCalled bool
}

// SoftDeleteAvatarWithOutbox запоминает fake soft delete и outbox событие
func (f *fakeAvatarDeleteOutboxStore) SoftDeleteAvatarWithOutbox(ctx context.Context, id string, userID string, deletedAt time.Time, event outbox.Event) error {
	f.softDeleteCalled = true
	f.avatarID = id
	f.userID = userID
	f.event = event
	return f.err
}

// MarkOutboxPublished запоминает fake успешную публикацию outbox
func (f *fakeAvatarDeleteOutboxStore) MarkOutboxPublished(ctx context.Context, id string, publishedAt time.Time) error {
	f.markPublishedCalled = true
	return nil
}

// MarkOutboxPublishAttemptFailed запоминает fake ошибку публикации outbox
func (f *fakeAvatarDeleteOutboxStore) MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) error {
	f.markFailedAttemptCalled = true
	return nil
}

type fakeAvatarDeleteObjectStore struct {
	deletedKeys []string
	err         error
}

// Delete запоминает fake удаление object storage
func (f *fakeAvatarDeleteObjectStore) Delete(ctx context.Context, key string) error {
	f.deletedKeys = append(f.deletedKeys, key)
	return f.err
}
