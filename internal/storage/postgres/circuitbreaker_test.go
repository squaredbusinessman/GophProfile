package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/resilience"
)

// TestFinishPostgresBreakerReportsPanic проверяет что паника не закрывает пробную попытку
func TestFinishPostgresBreakerReportsPanic(t *testing.T) {
	breaker := resilience.NewCircuitBreaker("postgres", resilience.CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 1,
		OpenTimeout:      5 * time.Millisecond,
	})

	if err := breaker.Execute(func() error { return errors.New("database unavailable") }); err == nil {
		t.Fatal("Execute() error = nil, want database error")
	}
	time.Sleep(10 * time.Millisecond)

	done, err := breaker.Allow()
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	panicValue := callPanickingPostgresOperation(done)
	if panicValue != "database panic" {
		t.Fatalf("panic value = %v, want database panic", panicValue)
	}

	if err := breaker.Execute(func() error { return nil }); !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Fatalf("Execute() after panic error = %v, want ErrCircuitOpen", err)
	}

	time.Sleep(10 * time.Millisecond)
	if err := breaker.Execute(func() error { return nil }); err != nil {
		t.Fatalf("recovery Execute() error = %v, want nil", err)
	}
}

// TestFinishPostgresBreakerIgnoresBusinessError проверяет что ожидаемый результат не считается отказом базы
func TestFinishPostgresBreakerIgnoresBusinessError(t *testing.T) {
	breaker := resilience.NewCircuitBreaker("postgres", resilience.CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 1,
		OpenTimeout:      time.Minute,
	})
	done, err := breaker.Allow()
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	operationErr := error(avatar.ErrNotFound)
	finishPostgresBreaker(done, &operationErr)

	if err := breaker.Execute(func() error { return nil }); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
}

// callPanickingPostgresOperation запускает панику через штатный отложенный обработчик
func callPanickingPostgresOperation(done func(error)) (recovered any) {
	defer func() {
		recovered = recover()
	}()
	func() {
		var operationErr error
		defer finishPostgresBreaker(done, &operationErr)
		panic("database panic")
	}()
	return nil
}
