package resilience

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// TestCircuitBreakerOpensAfterThreshold проверяет открытие после серии ошибок
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

// TestCircuitBreakerClosesAfterSuccessfulHalfOpenProbe проверяет восстановление после тайм-аута
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

// TestCircuitBreakerSuccessResetsConsecutiveFailures проверяет сброс последовательной серии ошибок
func TestCircuitBreakerSuccessResetsConsecutiveFailures(t *testing.T) {
	breaker := NewCircuitBreaker("test", CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 2,
		OpenTimeout:      time.Minute,
	})
	testErr := errors.New("dependency down")

	if err := breaker.Execute(func() error { return testErr }); !errors.Is(err, testErr) {
		t.Fatalf("first Execute() error = %v, want dependency error", err)
	}
	if err := breaker.Execute(func() error { return nil }); err != nil {
		t.Fatalf("successful Execute() error = %v, want nil", err)
	}
	if err := breaker.Execute(func() error { return testErr }); !errors.Is(err, testErr) {
		t.Fatalf("second Execute() error = %v, want dependency error", err)
	}
	if err := breaker.Execute(func() error { return nil }); err != nil {
		t.Fatalf("Execute() after reset error = %v, want nil", err)
	}
}

// TestCircuitBreakerHalfOpenFailureRestartsTimeout проверяет новый период ожидания после ошибки пробы
func TestCircuitBreakerHalfOpenFailureRestartsTimeout(t *testing.T) {
	now := time.Date(2026, 7, 14, 11, 0, 0, 0, time.UTC)
	breaker := NewCircuitBreaker("test", CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 1,
		OpenTimeout:      time.Minute,
	})
	breaker.now = func() time.Time { return now }
	testErr := errors.New("dependency down")

	if err := breaker.Execute(func() error { return testErr }); !errors.Is(err, testErr) {
		t.Fatalf("initial Execute() error = %v, want dependency error", err)
	}
	now = now.Add(time.Minute)
	if err := breaker.Execute(func() error { return testErr }); !errors.Is(err, testErr) {
		t.Fatalf("half-open Execute() error = %v, want dependency error", err)
	}
	now = now.Add(30 * time.Second)
	if err := breaker.Execute(func() error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("early Execute() error = %v, want ErrCircuitOpen", err)
	}
	now = now.Add(31 * time.Second)
	if err := breaker.Execute(func() error { return nil }); err != nil {
		t.Fatalf("recovery Execute() error = %v, want nil", err)
	}
}

// TestCircuitBreakerExecuteReportsPanicAndReleasesHalfOpen проверяет освобождение пробной попытки после паники
func TestCircuitBreakerExecuteReportsPanicAndReleasesHalfOpen(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
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
	panicValue := func() (recovered any) {
		defer func() { recovered = recover() }()
		_ = breaker.Execute(func() error { panic("dependency panic") })
		return nil
	}()
	if panicValue != "dependency panic" {
		t.Fatalf("panic value = %v, want dependency panic", panicValue)
	}

	if err := breaker.Execute(func() error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("Execute() after panic error = %v, want ErrCircuitOpen", err)
	}

	now = now.Add(time.Minute + time.Second)
	if err := breaker.Execute(func() error { return nil }); err != nil {
		t.Fatalf("recovery Execute() error = %v, want nil", err)
	}
}

// TestCircuitBreakerIgnoresStaleSuccess проверяет что старый успешный запрос не закрывает новое открытое состояние
func TestCircuitBreakerIgnoresStaleSuccess(t *testing.T) {
	breaker := NewCircuitBreaker("test", CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 1,
		OpenTimeout:      time.Minute,
	})

	failedAttempt, err := breaker.Allow()
	if err != nil {
		t.Fatalf("first Allow() error = %v", err)
	}
	staleSuccessfulAttempt, err := breaker.Allow()
	if err != nil {
		t.Fatalf("second Allow() error = %v", err)
	}

	failedAttempt(errors.New("dependency down"))
	staleSuccessfulAttempt(nil)

	if err := breaker.Execute(func() error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("Execute() error = %v, want ErrCircuitOpen", err)
	}
}

// TestCircuitBreakerCompletionIsIdempotent проверяет однократный учёт результата попытки
func TestCircuitBreakerCompletionIsIdempotent(t *testing.T) {
	breaker := NewCircuitBreaker("test", CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 2,
		OpenTimeout:      time.Minute,
	})
	done, err := breaker.Allow()
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}

	testErr := errors.New("dependency down")
	done(testErr)
	done(testErr)

	if err := breaker.Execute(func() error { return nil }); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
}

// TestCircuitBreakerAllowsOnlyOneHalfOpenProbe проверяет единственную параллельную пробную попытку
func TestCircuitBreakerAllowsOnlyOneHalfOpenProbe(t *testing.T) {
	now := time.Date(2026, 7, 14, 13, 0, 0, 0, time.UTC)
	breaker := NewCircuitBreaker("test", CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 1,
		OpenTimeout:      time.Minute,
	})
	breaker.now = func() time.Time { return now }

	if err := breaker.Execute(func() error { return errors.New("dependency down") }); err == nil {
		t.Fatal("Execute() error = nil, want dependency error")
	}
	now = now.Add(time.Minute + time.Second)

	firstProbeStarted := make(chan struct{})
	releaseFirstProbe := make(chan struct{})
	firstProbeDone := make(chan error, 1)
	go func() {
		firstProbeDone <- breaker.Execute(func() error {
			close(firstProbeStarted)
			<-releaseFirstProbe
			return nil
		})
	}()
	<-firstProbeStarted

	if err := breaker.Execute(func() error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("parallel Execute() error = %v, want ErrCircuitOpen", err)
	}
	close(releaseFirstProbe)
	if err := <-firstProbeDone; err != nil {
		t.Fatalf("first probe error = %v", err)
	}
}

// TestCircuitBreakerAllowIsSafeForConcurrentCompletion проверяет потокобезопасность функции завершения
func TestCircuitBreakerAllowIsSafeForConcurrentCompletion(t *testing.T) {
	breaker := NewCircuitBreaker("test", CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 2,
		OpenTimeout:      time.Minute,
	})
	done, err := breaker.Allow()
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}

	var waitGroup sync.WaitGroup
	for range 10 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			done(errors.New("dependency down"))
		}()
	}
	waitGroup.Wait()

	if err := breaker.Execute(func() error { return nil }); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
}

// TestCircuitBreakerDisabledAlwaysAllows проверяет отключённый режим
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
