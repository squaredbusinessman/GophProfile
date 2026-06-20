// Package user содержит доменную модель пользователя
package user

import (
	"errors"
	"time"
)

var (
	// ErrNotFound сообщает об отсутствии пользователя
	ErrNotFound = errors.New("user not found")
)

// User содержит идентификатор, электронную почту и временные метки пользователя
type User struct {
	// ID содержит идентификатор пользователя
	ID string
	// Email содержит нормализованный адрес электронной почты
	Email string
	// CreatedAt содержит время создания пользователя
	CreatedAt time.Time
	// UpdatedAt содержит время последнего изменения пользователя
	UpdatedAt time.Time
	// DeletedAt содержит время логического удаления пользователя
	DeletedAt *time.Time
}
