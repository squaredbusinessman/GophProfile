package postgres

import (
	"errors"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
	"github.com/squaredbusinessman/GophProfile/internal/resilience"
)

var errPostgresOperationPanicked = errors.New("postgres operation panicked")

// newPostgresBreaker создаёт автоматический выключатель для хранилища PostgreSQL
func newPostgresBreaker(cfg []resilience.CircuitBreakerConfig) *resilience.CircuitBreaker {
	breakerCfg := resilience.CircuitBreakerConfig{}
	if len(cfg) > 0 {
		breakerCfg = cfg[0]
	}
	return resilience.NewCircuitBreaker("postgres", breakerCfg)
}

// executePostgres выполняет операцию репозитория через автоматический выключатель
func executePostgres[T any](breaker *resilience.CircuitBreaker, operation func() (T, error)) (result T, err error) {
	done, err := breaker.Allow()
	if err != nil {
		return result, err
	}
	defer finishPostgresBreaker(done, &err)

	return operation()
}

// executePostgresCommand выполняет команду репозитория через автоматический выключатель
func executePostgresCommand(breaker *resilience.CircuitBreaker, operation func() error) error {
	_, err := executePostgres(breaker, func() (struct{}, error) {
		return struct{}{}, operation()
	})
	return err
}

// finishPostgresBreaker завершает попытку и сохраняет исходное значение паники
func finishPostgresBreaker(done func(error), operationErr *error) {
	panicValue := recover()
	if panicValue != nil {
		if done != nil {
			done(errPostgresOperationPanicked)
		}
		panic(panicValue)
	}
	if done == nil {
		return
	}
	if operationErr == nil {
		done(nil)
		return
	}
	err := *operationErr
	if isPostgresBusinessError(err) {
		done(nil)
		return
	}
	done(err)
}

// isPostgresBusinessError отличает ожидаемые бизнес-ошибки от отказа PostgreSQL
func isPostgresBusinessError(err error) bool {
	return errors.Is(err, avatar.ErrNotFound) ||
		errors.Is(err, avatar.ErrInvalidStatus) ||
		errors.Is(err, outbox.ErrNotFound) ||
		errors.Is(err, user.ErrNotFound)
}
