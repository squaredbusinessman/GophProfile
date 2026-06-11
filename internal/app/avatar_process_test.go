package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
	storages3 "github.com/squaredbusinessman/GophProfile/internal/storage/s3"
)

// TestHandleProcessMessageCreatesThumbnailsAndMarksReady проверяет успешную обработку avatar
func TestHandleProcessMessageCreatesThumbnailsAndMarksReady(t *testing.T) {
	avatars := &fakeAvatarMetadataStore{
		item: avatar.Avatar{
			ID:                "avatar-1",
			UserID:            "user-1",
			Status:            avatar.StatusProcessing,
			OriginalObjectKey: "avatars/user-1/avatar-1/original",
		},
	}
	objects := &fakeAvatarObjectStore{
		getBody: processTestPNG(t, 8, 6),
	}
	service := NewAvatarProcessService(avatars, objects, &fakeEventPublisher{})

	err := service.HandleProcessMessage(context.Background(), []byte(`{"avatar_id":"avatar-1","user_id":"user-1","original_object_key":"avatars/user-1/avatar-1/original","attempt":1}`))
	if err != nil {
		t.Fatalf("HandleProcessMessage returned error: %v", err)
	}

	if len(objects.putKeys) != 2 {
		t.Fatalf("put count = %d, want 2", len(objects.putKeys))
	}
	if objects.putKeys[0] != "avatars/user-1/avatar-1/100x100" || objects.putKeys[1] != "avatars/user-1/avatar-1/300x300" {
		t.Fatalf("putKeys = %#v, want deterministic thumbnail keys", objects.putKeys)
	}
	if avatars.readyStatus != avatar.StatusReady {
		t.Fatalf("readyStatus = %q, want ready", avatars.readyStatus)
	}
	if avatars.width != 8 || avatars.height != 6 {
		t.Fatalf("dimensions = %dx%d, want 8x6", avatars.width, avatars.height)
	}
}

// TestHandleProcessMessageSkipsReadyAvatar проверяет идемпотентный skip готовой avatar
func TestHandleProcessMessageSkipsReadyAvatar(t *testing.T) {
	avatars := &fakeAvatarMetadataStore{
		item: avatar.Avatar{
			ID:     "avatar-1",
			UserID: "user-1",
			Status: avatar.StatusReady,
		},
	}
	objects := &fakeAvatarObjectStore{}
	service := NewAvatarProcessService(avatars, objects, &fakeEventPublisher{})

	err := service.HandleProcessMessage(context.Background(), []byte(`{"avatar_id":"avatar-1","user_id":"user-1","attempt":1}`))
	if err != nil {
		t.Fatalf("HandleProcessMessage returned error: %v", err)
	}
	if objects.getCalled {
		t.Fatal("S3 Get should not be called for ready avatar")
	}
}

// TestHandleProcessMessageMarksFailedForDecodeError проверяет permanent decode error
func TestHandleProcessMessageMarksFailedForDecodeError(t *testing.T) {
	avatars := &fakeAvatarMetadataStore{
		item: avatar.Avatar{
			ID:                "avatar-1",
			UserID:            "user-1",
			Status:            avatar.StatusProcessing,
			OriginalObjectKey: "avatars/user-1/avatar-1/original",
		},
	}
	objects := &fakeAvatarObjectStore{getBody: []byte("not-image")}
	service := NewAvatarProcessService(avatars, objects, &fakeEventPublisher{})

	err := service.HandleProcessMessage(context.Background(), []byte(`{"avatar_id":"avatar-1","user_id":"user-1","attempt":1}`))
	if err != nil {
		t.Fatalf("HandleProcessMessage returned error: %v", err)
	}
	if avatars.updatedStatus != avatar.StatusFailed {
		t.Fatalf("updatedStatus = %q, want failed", avatars.updatedStatus)
	}
}

// TestHandleProcessMessagePublishesRetryForTemporaryError проверяет retry topic для временной ошибки
func TestHandleProcessMessagePublishesRetryForTemporaryError(t *testing.T) {
	avatars := &fakeAvatarMetadataStore{
		item: avatar.Avatar{
			ID:                "avatar-1",
			UserID:            "user-1",
			Status:            avatar.StatusProcessing,
			OriginalObjectKey: "avatars/user-1/avatar-1/original",
		},
	}
	objects := &fakeAvatarObjectStore{getErr: errors.New("s3 timeout")}
	publisher := &fakeEventPublisher{}
	service := NewAvatarProcessService(avatars, objects, publisher)

	err := service.HandleProcessMessage(context.Background(), []byte(`{"avatar_id":"avatar-1","user_id":"user-1","attempt":1}`))
	if err != nil {
		t.Fatalf("HandleProcessMessage returned error: %v", err)
	}
	if publisher.topic != queuekafka.TopicAvatarProcessRetry1m {
		t.Fatalf("topic = %q, want retry 1m", publisher.topic)
	}
}

// TestHandleProcessMessageDeadLettersAfterAttemptLimit проверяет dead-letter после лимита попыток
func TestHandleProcessMessageDeadLettersAfterAttemptLimit(t *testing.T) {
	avatars := &fakeAvatarMetadataStore{
		item: avatar.Avatar{
			ID:                "avatar-1",
			UserID:            "user-1",
			Status:            avatar.StatusProcessing,
			OriginalObjectKey: "avatars/user-1/avatar-1/original",
		},
	}
	objects := &fakeAvatarObjectStore{getErr: errors.New("s3 timeout")}
	publisher := &fakeEventPublisher{}
	service := NewAvatarProcessService(avatars, objects, publisher)

	err := service.HandleProcessMessage(context.Background(), []byte(`{"avatar_id":"avatar-1","user_id":"user-1","attempt":4}`))
	if err != nil {
		t.Fatalf("HandleProcessMessage returned error: %v", err)
	}
	if publisher.topic != queuekafka.TopicAvatarProcessDeadLetter {
		t.Fatalf("topic = %q, want dead-letter", publisher.topic)
	}
	if avatars.updatedStatus != avatar.StatusFailed {
		t.Fatalf("updatedStatus = %q, want failed", avatars.updatedStatus)
	}
}

type fakeAvatarMetadataStore struct {
	item          avatar.Avatar
	getErr        error
	readyStatus   avatar.Status
	updatedStatus avatar.Status
	width         int
	height        int
}

// GetAvatarIncludingDeleted возвращает fake metadata avatar
func (f *fakeAvatarMetadataStore) GetAvatarIncludingDeleted(ctx context.Context, id string) (avatar.Avatar, error) {
	if f.getErr != nil {
		return avatar.Avatar{}, f.getErr
	}
	return f.item, nil
}

// MarkAvatarReady запоминает fake ready update
func (f *fakeAvatarMetadataStore) MarkAvatarReady(ctx context.Context, id string, width int, height int, thumb100Key string, thumb300Key string, updatedAt time.Time) error {
	f.readyStatus = avatar.StatusReady
	f.width = width
	f.height = height
	return nil
}

// UpdateAvatarStatus запоминает fake status update
func (f *fakeAvatarMetadataStore) UpdateAvatarStatus(ctx context.Context, id string, status avatar.Status, updatedAt time.Time) error {
	f.updatedStatus = status
	return nil
}

type fakeAvatarObjectStore struct {
	getCalled bool
	getBody   []byte
	getErr    error
	putKeys   []string
	putErr    error
}

// Get возвращает fake original object
func (f *fakeAvatarObjectStore) Get(ctx context.Context, key string) (io.ReadCloser, storages3.ObjectMetadata, error) {
	f.getCalled = true
	if f.getErr != nil {
		return nil, storages3.ObjectMetadata{}, f.getErr
	}
	return io.NopCloser(bytes.NewReader(f.getBody)), storages3.ObjectMetadata{}, nil
}

// Put запоминает fake thumbnail upload
func (f *fakeAvatarObjectStore) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	f.putKeys = append(f.putKeys, key)
	return f.putErr
}

// processTestPNG создает PNG fixture для worker tests
func processTestPNG(t *testing.T, width int, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}

	var body bytes.Buffer
	if err := png.Encode(&body, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return body.Bytes()
}
