package resilience

import (
	"errors"
	"testing"
	"time"
)

// TestCircuitBreakerOpensAfterThreshold проверяет открытие после серии ошибок.
func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	breaker := NewCircuitBreaker("test", CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 2,
		OpenTimeout:      time.Minute,
	})

	testErr := errors.New("dependency down")
	if err := breaker.Execute(func() error { return testErr }); !errors.Is(err, testErr) {
		t.Fatalf("first Execute() error = %v, want dependency error", err)
	}
	if err := breaker.Execute(func() error { return testErr }); !errors.Is(err, testErr) {
		t.Fatalf("second Execute() error = %v, want dependency error", err)
	}
	if err := breaker.Execute(func() error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("open Execute() error = %v, want ErrCircuitOpen", err)
	}
}

// TestCircuitBreakerClosesAfterSuccessfulHalfOpenProbe проверяет восстановление после timeout.
func TestCircuitBreakerClosesAfterSuccessfulHalfOpenProbe(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	breaker := NewCircuitBreaker("test", CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 1,
		OpenTimeout:      time.Minute,
	})
	breaker.now = func() time.Time { return now }

	testErr := errors.New("dependency down")
	if err := breaker.Execute(func() error { return testErr }); !errors.Is(err, testErr) {
		t.Fatalf("Execute() error = %v, want dependency error", err)
	}

	now = now.Add(time.Minute + time.Second)
	if err := breaker.Execute(func() error { return nil }); err != nil {
		t.Fatalf("half-open probe error = %v, want nil", err)
	}
	if err := breaker.Execute(func() error { return nil }); err != nil {
		t.Fatalf("closed Execute() error = %v, want nil", err)
	}
}

// TestCircuitBreakerDisabledAlwaysAllows проверяет отключённый режим.
func TestCircuitBreakerDisabledAlwaysAllows(t *testing.T) {
	breaker := NewCircuitBreaker("test", CircuitBreakerConfig{
		Enabled:          false,
		FailureThreshold: 1,
		OpenTimeout:      time.Minute,
	})

	testErr := errors.New("dependency down")
	for i := 0; i < 3; i++ {
		if err := breaker.Execute(func() error { return testErr }); !errors.Is(err, testErr) {
			t.Fatalf("Execute() error = %v, want dependency error", err)
		}
	}
}
