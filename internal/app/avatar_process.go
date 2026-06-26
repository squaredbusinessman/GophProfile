package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/imageproc"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
	storages3 "github.com/squaredbusinessman/GophProfile/internal/storage/s3"
)

const (
	initialProcessAttempt = 1
	maxProcessAttempts    = 4
)

// AvatarMetadataStore описывает доступ обработчика к метаданным аватара
type AvatarMetadataStore interface {
	GetAvatarIncludingDeleted(ctx context.Context, id string) (avatar.Avatar, error)
	MarkAvatarReady(ctx context.Context, id string, width int, height int, thumb100Key string, thumb300Key string, updatedAt time.Time) error
	UpdateAvatarStatus(ctx context.Context, id string, status avatar.Status, updatedAt time.Time) error
}

// AvatarObjectStore описывает чтение и запись объектов аватара
type AvatarObjectStore interface {
	Get(ctx context.Context, key string) (io.ReadCloser, storages3.ObjectMetadata, error)
	Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
}

// AvatarProcessService создаёт миниатюры и управляет повторными попытками обработки
type AvatarProcessService struct {
	avatars   AvatarMetadataStore
	objects   AvatarObjectStore
	producer  EventPublisher
	now       func() time.Time
	telemetry businessTelemetry
}

// AvatarProcessMessage содержит данные сообщения обработки аватара
type AvatarProcessMessage struct {
	// AvatarID содержит идентификатор аватара
	AvatarID string `json:"avatar_id"`
	// UserID содержит идентификатор владельца аватара
	UserID string `json:"user_id"`
	// OriginalObjectKey содержит ключ оригинала в объектном хранилище
	OriginalObjectKey string `json:"original_object_key"`
	// Thumb100ObjectKey содержит ключ миниатюры размером 100 на 100 пикселей
	Thumb100ObjectKey string `json:"thumb_100_object_key,omitempty"`
	// Thumb300ObjectKey содержит ключ миниатюры размером 300 на 300 пикселей
	Thumb300ObjectKey string `json:"thumb_300_object_key,omitempty"`
	// ContentType содержит MIME-тип изображения
	ContentType string `json:"content_type,omitempty"`
	// Attempt содержит номер попытки обработки
	Attempt int `json:"attempt"`
}

// NewAvatarProcessService создаёт сервис обработки сообщений из темы avatar.process
func NewAvatarProcessService(avatars AvatarMetadataStore, objects AvatarObjectStore, producer EventPublisher) (*AvatarProcessService, error) {
	telemetry, err := newBusinessTelemetry()
	if err != nil {
		return nil, fmt.Errorf("create avatar process telemetry: %w", err)
	}
	return &AvatarProcessService{
		avatars:   avatars,
		objects:   objects,
		producer:  producer,
		now:       time.Now,
		telemetry: telemetry,
	}, nil
}

// HandleProcessMessage обрабатывает тело сообщения Kafka из темы avatar.process
func (s *AvatarProcessService) HandleProcessMessage(ctx context.Context, payload []byte) error {
	startedAt := time.Now()
	result := processResultError
	defer func() { s.telemetry.recordProcessing(ctx, startedAt, result) }()

	var message AvatarProcessMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return fmt.Errorf("decode avatar process message: %w", err)
	}
	if message.Attempt <= 0 {
		message.Attempt = initialProcessAttempt
	}

	processResult, err := s.processAvatar(ctx, message)
	if err == nil {
		result = processResult
		return nil
	}
	if isPermanentProcessError(err) {
		if updateErr := s.avatars.UpdateAvatarStatus(ctx, message.AvatarID, avatar.StatusFailed, s.now().UTC()); updateErr != nil {
			return fmt.Errorf("mark avatar failed after permanent error: %w", updateErr)
		}
		result = processResultFailed
		return nil
	}

	if message.Attempt >= maxProcessAttempts {
		if publishErr := s.publishDeadLetter(ctx, message, err); publishErr != nil {
			return publishErr
		}
		if updateErr := s.avatars.UpdateAvatarStatus(ctx, message.AvatarID, avatar.StatusFailed, s.now().UTC()); updateErr != nil {
			return fmt.Errorf("mark avatar failed after dead-letter: %w", updateErr)
		}
		result = processResultDeadLetter
		return nil
	}

	if publishErr := s.publishRetry(ctx, message, err); publishErr != nil {
		return publishErr
	}
	result = processResultRetryScheduled
	return nil
}

// processAvatar создаёт миниатюры и обновляет метаданные аватара
func (s *AvatarProcessService) processAvatar(ctx context.Context, message AvatarProcessMessage) (string, error) {
	if s.avatars == nil || s.objects == nil || s.producer == nil {
		return "", retryableProcessError{err: fmt.Errorf("avatar process service is not configured")}
	}

	item, err := s.avatars.GetAvatarIncludingDeleted(ctx, message.AvatarID)
	if err != nil {
		return "", retryableProcessError{err: err}
	}
	if item.Status == avatar.StatusReady {
		return processResultIdempotentSkip, nil
	}
	if item.DeletedAt != nil {
		return processResultIdempotentSkip, nil
	}

	body, _, err := s.objects.Get(ctx, item.OriginalObjectKey)
	if err != nil {
		return "", retryableProcessError{err: err}
	}
	defer func() {
		_ = body.Close()
	}()

	decoded, err := imageproc.Decode(body)
	if err != nil {
		return "", permanentProcessError{err: err}
	}

	thumbnails, err := imageproc.BuildThumbnails(decoded.Image, imageproc.DefaultThumbnailSizes)
	if err != nil {
		return "", permanentProcessError{err: err}
	}

	thumb100Key := storages3.Thumb100ObjectKey(item.UserID, item.ID)
	thumb300Key := storages3.Thumb300ObjectKey(item.UserID, item.ID)

	for _, thumbnail := range thumbnails {
		key := thumb100Key
		if thumbnail.Size.Name == "300x300" {
			key = thumb300Key
		}
		if err := s.objects.Put(ctx, key, bytes.NewReader(thumbnail.Data), int64(len(thumbnail.Data)), thumbnail.ContentType); err != nil {
			return "", retryableProcessError{err: err}
		}
	}

	if err := s.avatars.MarkAvatarReady(ctx, item.ID, decoded.Width, decoded.Height, thumb100Key, thumb300Key, s.now().UTC()); err != nil {
		return "", retryableProcessError{err: err}
	}

	return processResultReady, nil
}

// publishRetry публикует сообщение в следующую тему повторной обработки
func (s *AvatarProcessService) publishRetry(ctx context.Context, message AvatarProcessMessage, cause error) error {
	next := message
	next.Attempt++
	payload, err := json.Marshal(next)
	if err != nil {
		return err
	}

	topic := processRetryTopic(message.Attempt)
	if err := s.producer.Publish(ctx, topic, message.AvatarID, payload, nil); err != nil {
		return fmt.Errorf("publish avatar retry after %v: %w", cause, err)
	}
	return nil
}

// publishDeadLetter публикует сообщение в тему недоставленных сообщений
func (s *AvatarProcessService) publishDeadLetter(ctx context.Context, message AvatarProcessMessage, cause error) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if err := s.producer.Publish(ctx, queuekafka.TopicAvatarProcessDeadLetter, message.AvatarID, payload, nil); err != nil {
		return fmt.Errorf("publish avatar dead-letter after %v: %w", cause, err)
	}
	return nil
}

// processRetryTopic возвращает тему повторной обработки для текущей попытки
func processRetryTopic(attempt int) string {
	switch attempt {
	case 1:
		return queuekafka.TopicAvatarProcessRetry1m
	case 2:
		return queuekafka.TopicAvatarProcessRetry5m
	default:
		return queuekafka.TopicAvatarProcessRetry30m
	}
}

type retryableProcessError struct {
	err error
}

// Error возвращает текст временной ошибки
func (e retryableProcessError) Error() string {
	return e.err.Error()
}

type permanentProcessError struct {
	err error
}

// Error возвращает текст постоянной ошибки
func (e permanentProcessError) Error() string {
	return e.err.Error()
}

// isPermanentProcessError проверяет тип ошибки обработки
func isPermanentProcessError(err error) bool {
	var permanent permanentProcessError
	return errors.As(err, &permanent)
}
