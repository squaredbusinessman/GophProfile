// Package resilience содержит защитные runtime-механизмы для внешних зависимостей.
package resilience

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrCircuitOpen сообщает, что circuit breaker временно не пропускает запросы.
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

// CircuitBreakerConfig содержит настройки circuit breaker.
type CircuitBreakerConfig struct {
	// Enabled включает circuit breaker.
	Enabled bool
	// FailureThreshold задаёт число последовательных ошибок перед открытием.
	FailureThreshold int
	// OpenTimeout задаёт время до пробного запроса в half-open состоянии.
	OpenTimeout time.Duration
}

// CircuitBreaker защищает внешнюю зависимость от повторных запросов после серии ошибок.
type CircuitBreaker struct {
	name             string
	enabled          bool
	failureThreshold int
	openTimeout      time.Duration
	now              func() time.Time

	mu               sync.Mutex
	state            circuitState
	failures         int
	openedAt         time.Time
	halfOpenInFlight bool
}

type circuitState string

const (
	stateClosed   circuitState = "closed"
	stateOpen     circuitState = "open"
	stateHalfOpen circuitState = "half-open"
)

// NewCircuitBreaker создаёт circuit breaker для именованной внешней зависимости.
func NewCircuitBreaker(name string, cfg CircuitBreakerConfig) *CircuitBreaker {
	threshold := cfg.FailureThreshold
	if threshold <= 0 {
		threshold = 5
	}
	timeout := cfg.OpenTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return &CircuitBreaker{
		name:             name,
		enabled:          cfg.Enabled,
		failureThreshold: threshold,
		openTimeout:      timeout,
		now:              time.Now,
		state:            stateClosed,
	}
}

// Allow резервирует попытку обращения к зависимости или возвращает ErrCircuitOpen.
func (b *CircuitBreaker) Allow() (func(error), error) {
	if b == nil || !b.enabled {
		return func(error) {}, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	if b.state == stateOpen {
		if now.Sub(b.openedAt) < b.openTimeout {
			return nil, fmt.Errorf("%w: %s", ErrCircuitOpen, b.name)
		}
		b.state = stateHalfOpen
		b.halfOpenInFlight = false
	}

	if b.state == stateHalfOpen {
		if b.halfOpenInFlight {
			return nil, fmt.Errorf("%w: %s", ErrCircuitOpen, b.name)
		}
		b.halfOpenInFlight = true
	}

	return b.report, nil
}

// Execute выполняет операцию только если circuit breaker разрешает обращение.
func (b *CircuitBreaker) Execute(operation func() error) error {
	done, err := b.Allow()
	if err != nil {
		return err
	}
	err = operation()
	done(err)
	return err
}

// report обновляет состояние breaker по результату попытки.
func (b *CircuitBreaker) report(err error) {
	if b == nil || !b.enabled {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if err == nil {
		b.state = stateClosed
		b.failures = 0
		b.halfOpenInFlight = false
		return
	}

	if b.state == stateHalfOpen {
		b.open()
		return
	}

	b.failures++
	if b.failures >= b.failureThreshold {
		b.open()
	}
}

// open переводит breaker в open состояние.
func (b *CircuitBreaker) open() {
	b.state = stateOpen
	b.openedAt = b.now()
	b.halfOpenInFlight = false
}
