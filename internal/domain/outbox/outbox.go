// Package outbox содержит модель надёжной публикации сообщений через базу данных
package outbox

import (
	"errors"
	"time"
)

var (
	// ErrNotFound сообщает об отсутствии события outbox
	ErrNotFound = errors.New("outbox event not found")
)

// Status задаёт состояние публикации outbox события
type Status string

const (
	// StatusPending обозначает событие, ожидающее публикации
	StatusPending Status = "pending"
	// StatusPublished обозначает успешно опубликованное событие
	StatusPublished Status = "published"
)

// Event содержит сообщение Kafka и сохраняемое состояние его публикации
type Event struct {
	// ID содержит уникальный идентификатор события
	ID string
	// Topic содержит целевую тему Kafka
	Topic string
	// Key содержит ключ сообщения Kafka для маршрутизации и идемпотентности
	Key string
	// Payload содержит сериализованное тело сообщения Kafka
	Payload []byte
	// Headers содержит заголовки Kafka вместе с сохранённым контекстом трассировки W3C
	Headers map[string]string
	// Status содержит состояние публикации события
	Status Status
	// Attempts содержит количество неуспешных попыток публикации
	Attempts int
	// LastError содержит последнюю ошибку публикации
	LastError *string
	// CreatedAt содержит время создания события
	CreatedAt time.Time
	// UpdatedAt содержит время последнего изменения события
	UpdatedAt time.Time
	// PublishedAt содержит время успешной публикации
	PublishedAt *time.Time
}
