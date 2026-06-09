package avatar

import (
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("avatar not found")
	ErrInvalidStatus = errors.New("invalid avatar status")
)

type Status string

const (
	StatusProcessing Status = "processing"
	StatusReady      Status = "ready"
	StatusFailed     Status = "failed"
	StatusDeleting   Status = "deleting"
	StatusDeleted    Status = "deleted"
)

type Avatar struct {
	ID                string
	UserID            string
	FileName          string
	MimeType          string
	SizeBytes         int64
	Width             *int
	Height            *int
	Status            Status
	OriginalObjectKey string
	Thumb100ObjectKey *string
	Thumb300ObjectKey *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

// ValidateStatus проверяет допустимость статуса avatar
func ValidateStatus(status Status) error {
	switch status {
	case StatusProcessing, StatusReady, StatusFailed, StatusDeleting, StatusDeleted:
		return nil
	default:
		return ErrInvalidStatus
	}
}

// IsDeleted возвращает признак мягкого удаления avatar
func (a Avatar) IsDeleted() bool {
	return a.DeletedAt != nil
}

// ObjectKeys возвращает все известные S3 object keys avatar
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
