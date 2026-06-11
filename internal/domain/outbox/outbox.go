package outbox

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("outbox event not found")
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusPublished Status = "published"
)

type Event struct {
	ID          string
	Topic       string
	Key         string
	Payload     []byte
	Status      Status
	Attempts    int
	LastError   *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	PublishedAt *time.Time
}
