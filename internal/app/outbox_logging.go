package app

import (
	"context"

	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
)

// logOutboxStateUpdateFailed записывает ошибку обновления состояния outbox без payload и key
func logOutboxStateUpdateFailed(ctx context.Context, event outbox.Event, operation string, err error) {
	LoggerFromContext(ctx).Error().
		Str("event_id", event.ID).
		Str("topic", event.Topic).
		Str("operation", operation).
		Str("error_type", ErrorType(err)).
		Msg("outbox state update failed")
}
