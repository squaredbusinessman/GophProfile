package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
	storages3 "github.com/squaredbusinessman/GophProfile/internal/storage/s3"
)

// ErrUserNotFound сообщает об отсутствии пользователя для загрузки аватара
var ErrUserNotFound = errors.New("user not found")

// UserLookup описывает получение пользователя для операций с аватаром
type UserLookup interface {
	// GetUser возвращает активного пользователя по внутреннему идентификатору.
	GetUser(ctx context.Context, id string) (user.User, error)
}

// AvatarOutboxStore описывает атомарное сохранение аватара и события outbox
type AvatarOutboxStore interface {
	// CreateAvatarWithOutbox сохраняет аватар и событие outbox в одной транзакции.
	CreateAvatarWithOutbox(ctx context.Context, item avatar.Avatar, event outbox.Event) error
	// MarkOutboxPublished отмечает событие outbox успешно опубликованным.
	MarkOutboxPublished(ctx context.Context, id string, publishedAt time.Time) error
	// MarkOutboxPublishAttemptFailed сохраняет ошибку публикации события outbox.
	MarkOutboxPublishAttemptFailed(ctx context.Context, id string, publishErr error, updatedAt time.Time) error
}

// ObjectStore описывает загрузку объекта в хранилище
type ObjectStore interface {
	// Put сохраняет объект по ключу с известным размером и MIME-типом.
	Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
}

// EventPublisher описывает публикацию события в брокер сообщений
type EventPublisher interface {
	// Publish отправляет сообщение в указанную тему с ключом и заголовками трассировки.
	Publish(ctx context.Context, topic string, key string, payload []byte, headers map[string]string) error
}

// AvatarUploadService выполняет загрузку оригинала аватара и создаёт событие обработки
type AvatarUploadService struct {
	users        UserLookup
	avatarOutbox AvatarOutboxStore
	objects      ObjectStore
	publisher    EventPublisher
	now          func() time.Time
	telemetry    businessTelemetry
}

// AvatarUploadRequest содержит данные запроса на загрузку аватара
type AvatarUploadRequest struct {
	// UserID содержит идентификатор владельца аватара
	UserID string
	// FileName содержит исходное имя файла
	FileName string
	// ContentType содержит MIME-тип файла
	ContentType string
	// Size содержит заявленный размер файла в байтах
	Size int64
	// Width содержит заявленную ширину изображения в пикселях
	Width int
	// Height содержит заявленную высоту изображения в пикселях
	Height int
	// Reader предоставляет содержимое загружаемого файла
	Reader io.Reader
}

// AvatarUploadResult содержит сведения о созданном аватаре
type AvatarUploadResult struct {
	// ID содержит идентификатор аватара
	ID string
	// UserID содержит идентификатор владельца аватара
	UserID string
	// FileName содержит исходное имя файла
	FileName string
	// ContentType содержит MIME-тип файла
	ContentType string
	// Size содержит фактический размер файла в байтах
	Size int64
	// Width содержит заявленную ширину изображения в пикселях
	Width int
	// Height содержит заявленную высоту изображения в пикселях
	Height int
	// Status содержит состояние обработки аватара
	Status avatar.Status
	// OriginalObjectKey содержит ключ оригинала в объектном хранилище
	OriginalObjectKey string
	// CreatedAt содержит время создания аватара
	CreatedAt time.Time
}

// AvatarProcessEvent содержит данные события обработки аватара
type AvatarProcessEvent struct {
	// AvatarID содержит идентификатор аватара
	AvatarID string `json:"avatar_id"`
	// UserID содержит идентификатор владельца аватара
	UserID string `json:"user_id"`
	// OriginalObjectKey содержит ключ оригинала в объектном хранилище
	OriginalObjectKey string `json:"original_object_key"`
	// Thumb100ObjectKey содержит ключ миниатюры размером 100 на 100 пикселей
	Thumb100ObjectKey string `json:"thumb_100_object_key"`
	// Thumb300ObjectKey содержит ключ миниатюры размером 300 на 300 пикселей
	Thumb300ObjectKey string `json:"thumb_300_object_key"`
	// ContentType содержит MIME-тип изображения
	ContentType string `json:"content_type"`
	// Attempt содержит номер попытки обработки
	Attempt int `json:"attempt"`
}

// NewAvatarUploadService создаёт сервис загрузки аватара
func NewAvatarUploadService(users UserLookup, avatarOutbox AvatarOutboxStore, objects ObjectStore, publisher EventPublisher) (*AvatarUploadService, error) {
	telemetry, err := newBusinessTelemetry()
	if err != nil {
		return nil, fmt.Errorf("create avatar upload telemetry: %w", err)
	}
	return &AvatarUploadService{
		users:        users,
		avatarOutbox: avatarOutbox,
		objects:      objects,
		publisher:    publisher,
		now:          time.Now,
		telemetry:    telemetry,
	}, nil
}

// UploadAvatar сохраняет оригинал в S3 и атомарно создаёт аватар с событием outbox
func (s *AvatarUploadService) UploadAvatar(ctx context.Context, req AvatarUploadRequest) (AvatarUploadResult, error) {
	startedAt := time.Now()
	result := uploadResultError
	var acceptedBytes int64
	defer func() { s.telemetry.recordUpload(ctx, startedAt, result, acceptedBytes) }()

	if s.users == nil || s.avatarOutbox == nil || s.objects == nil || s.publisher == nil {
		return AvatarUploadResult{}, fmt.Errorf("avatar upload service is not configured")
	}

	now := s.now().UTC()
	owner, err := s.users.GetUser(ctx, req.UserID)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return AvatarUploadResult{}, ErrUserNotFound
		}
		return AvatarUploadResult{}, fmt.Errorf("get upload user: %w", err)
	}

	avatarID := uuid.NewString()
	originalKey := storages3.OriginalObjectKey(owner.ID, avatarID)
	thumb100Key := storages3.Thumb100ObjectKey(owner.ID, avatarID)
	thumb300Key := storages3.Thumb300ObjectKey(owner.ID, avatarID)

	body, err := io.ReadAll(req.Reader)
	if err != nil {
		return AvatarUploadResult{}, fmt.Errorf("read avatar body: %w", err)
	}
	size := int64(len(body))
	if req.Size > 0 {
		size = req.Size
	}

	if err := s.objects.Put(ctx, originalKey, bytes.NewReader(body), size, req.ContentType); err != nil {
		return AvatarUploadResult{}, fmt.Errorf("put original object: %w", err)
	}

	item := avatar.Avatar{
		ID:                avatarID,
		UserID:            owner.ID,
		FileName:          req.FileName,
		MimeType:          req.ContentType,
		SizeBytes:         size,
		Width:             intPtr(req.Width),
		Height:            intPtr(req.Height),
		Status:            avatar.StatusProcessing,
		OriginalObjectKey: originalKey,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	event := AvatarProcessEvent{
		AvatarID:          avatarID,
		UserID:            owner.ID,
		OriginalObjectKey: originalKey,
		Thumb100ObjectKey: thumb100Key,
		Thumb300ObjectKey: thumb300Key,
		ContentType:       req.ContentType,
		Attempt:           initialProcessAttempt,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return AvatarUploadResult{}, fmt.Errorf("marshal avatar process event: %w", err)
	}

	outboxEvent := outbox.Event{
		ID:        uuid.NewString(),
		Topic:     queuekafka.TopicAvatarProcess,
		Key:       avatarID,
		Payload:   payload,
		Headers:   queuekafka.InjectTraceContext(ctx, nil),
		Status:    outbox.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.avatarOutbox.CreateAvatarWithOutbox(ctx, item, outboxEvent); err != nil {
		return AvatarUploadResult{}, fmt.Errorf("create avatar metadata and outbox event: %w", err)
	}

	s.publishOutboxEvent(ctx, outboxEvent)
	result = uploadResultAccepted
	acceptedBytes = int64(len(body))

	return AvatarUploadResult{
		ID:                avatarID,
		UserID:            owner.ID,
		FileName:          req.FileName,
		ContentType:       req.ContentType,
		Size:              size,
		Width:             req.Width,
		Height:            req.Height,
		Status:            avatar.StatusProcessing,
		OriginalObjectKey: originalKey,
		CreatedAt:         now,
	}, nil
}

// publishOutboxEvent пытается опубликовать событие outbox сразу после фиксации транзакции
func (s *AvatarUploadService) publishOutboxEvent(ctx context.Context, event outbox.Event) {
	if err := s.publisher.Publish(ctx, event.Topic, event.Key, event.Payload, event.Headers); err != nil {
		s.telemetry.recordOutboxPublish(ctx, outboxPublishModeImmediate, outboxPublishResultError)
		if markErr := s.avatarOutbox.MarkOutboxPublishAttemptFailed(ctx, event.ID, err, s.now().UTC()); markErr != nil {
			logOutboxStateUpdateFailed(ctx, event, "mark_publish_attempt_failed", markErr)
		}
		return
	}

	if err := s.avatarOutbox.MarkOutboxPublished(ctx, event.ID, s.now().UTC()); err != nil {
		s.telemetry.recordOutboxPublish(ctx, outboxPublishModeImmediate, outboxPublishResultError)
		logOutboxStateUpdateFailed(ctx, event, "mark_published", err)
		return
	}
	s.telemetry.recordOutboxPublish(ctx, outboxPublishModeImmediate, outboxPublishResultSuccess)
}

// intPtr возвращает указатель на положительный int или nil
func intPtr(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}
