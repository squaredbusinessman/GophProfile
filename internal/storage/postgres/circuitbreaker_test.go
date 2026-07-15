package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/resilience"
)

// TestExecutePostgresReportsPanic проверяет что паника не закрывает пробную попытку
func TestExecutePostgresReportsPanic(t *testing.T) {
	breaker := resilience.NewCircuitBreaker("postgres", resilience.CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 1,
		OpenTimeout:      5 * time.Millisecond,
	})

	if err := executePostgresCommand(breaker, func() error { return errors.New("database unavailable") }); err == nil {
		t.Fatal("executePostgresCommand() error = nil, want database error")
	}
	time.Sleep(10 * time.Millisecond)

	panicValue := func() (recovered any) {
		defer func() {
			recovered = recover()
		}()
		_ = executePostgresCommand(breaker, func() error {
			panic("database panic")
		})
		return nil
	}()
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

// TestExecutePostgresIgnoresBusinessError проверяет что ожидаемый результат не считается отказом базы
func TestExecutePostgresIgnoresBusinessError(t *testing.T) {
	breaker := resilience.NewCircuitBreaker("postgres", resilience.CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 1,
		OpenTimeout:      time.Minute,
	})
	err := executePostgresCommand(breaker, func() error { return avatar.ErrNotFound })
	if !errors.Is(err, avatar.ErrNotFound) {
		t.Fatalf("executePostgresCommand() error = %v, want avatar.ErrNotFound", err)
	}

	if err := breaker.Execute(func() error { return nil }); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
}
