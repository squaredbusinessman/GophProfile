// Package avatar содержит доменную модель аватара
package avatar

import (
	"errors"
	"time"
)

var (
	// ErrNotFound сообщает об отсутствии аватара
	ErrNotFound = errors.New("avatar not found")
	// ErrInvalidStatus сообщает о недопустимом состоянии аватара
	ErrInvalidStatus = errors.New("invalid avatar status")
)

// Status задаёт состояние жизненного цикла аватара
type Status string

const (
	// StatusProcessing обозначает обработку загруженного оригинала
	StatusProcessing Status = "processing"
	// StatusReady обозначает готовый к выдаче аватар
	StatusReady Status = "ready"
	// StatusFailed обозначает завершившуюся ошибкой обработку
	StatusFailed Status = "failed"
	// StatusDeleting обозначает запланированное удаление объектов
	StatusDeleting Status = "deleting"
	// StatusDeleted обозначает завершённое удаление объектов
	StatusDeleted Status = "deleted"
)

// Avatar содержит метаданные оригинала, миниатюр и состояния обработки
type Avatar struct {
	// ID содержит идентификатор аватара
	ID string
	// UserID содержит идентификатор владельца
	UserID string
	// FileName содержит исходное имя файла
	FileName string
	// MimeType содержит MIME-тип оригинала
	MimeType string
	// SizeBytes содержит размер оригинала в байтах
	SizeBytes int64
	// Width содержит ширину исходного изображения в пикселях
	Width *int
	// Height содержит высоту исходного изображения в пикселях
	Height *int
	// Status содержит состояние жизненного цикла аватара
	Status Status
	// OriginalObjectKey содержит ключ оригинала в объектном хранилище
	OriginalObjectKey string
	// Thumb100ObjectKey содержит ключ миниатюры размером 100 на 100 пикселей
	Thumb100ObjectKey *string
	// Thumb300ObjectKey содержит ключ миниатюры размером 300 на 300 пикселей
	Thumb300ObjectKey *string
	// CreatedAt содержит время создания
	CreatedAt time.Time
	// UpdatedAt содержит время последнего изменения
	UpdatedAt time.Time
	// DeletedAt содержит время логического удаления
	DeletedAt *time.Time
}

// ValidateStatus проверяет допустимость состояния аватара
func ValidateStatus(status Status) error {
	switch status {
	case StatusProcessing, StatusReady, StatusFailed, StatusDeleting, StatusDeleted:
		return nil
	default:
		return ErrInvalidStatus
	}
}

// IsDeleted возвращает признак логического удаления аватара
func (a Avatar) IsDeleted() bool {
	return a.DeletedAt != nil
}

// ObjectKeys возвращает все известные ключи объектов аватара в S3
func (a Avatar) ObjectKeys() []string {
	keys := []string{a.OriginalObjectKey}
	if a.Thumb100ObjectKey != nil && *a.Thumb100ObjectKey != "" {
		keys = append(keys, *a.Thumb100ObjectKey)
	}
	if a.Thumb300ObjectKey != nil && *a.Thumb300ObjectKey != "" {
		keys = append(keys, *a.Thumb300ObjectKey)
	}
	return keys
}
