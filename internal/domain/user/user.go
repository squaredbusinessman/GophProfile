package user

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("user not found")
)

type User struct {
	ID        string
	Email     string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}
