package postgres

import (
	"errors"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
	"github.com/squaredbusinessman/GophProfile/internal/resilience"
)

// newPostgresBreaker создаёт circuit breaker для PostgreSQL repository.
func newPostgresBreaker(cfg []resilience.CircuitBreakerConfig) *resilience.CircuitBreaker {
	breakerCfg := resilience.CircuitBreakerConfig{}
	if len(cfg) > 0 {
		breakerCfg = cfg[0]
	}
	return resilience.NewCircuitBreaker("postgres", breakerCfg)
}

// finishPostgresBreaker записывает только ошибки зависимости, не бизнес-результаты.
func finishPostgresBreaker(done func(error), err error) {
	if done == nil {
		return
	}
	if isPostgresBusinessError(err) {
		done(nil)
		return
	}
	done(err)
}

// isPostgresBusinessError отличает ожидаемые бизнес-ошибки от отказа PostgreSQL.
func isPostgresBusinessError(err error) bool {
	return errors.Is(err, avatar.ErrNotFound) ||
		errors.Is(err, avatar.ErrInvalidStatus) ||
		errors.Is(err, outbox.ErrNotFound) ||
		errors.Is(err, user.ErrNotFound)
}
