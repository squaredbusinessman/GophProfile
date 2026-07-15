// Package resilience содержит механизмы защиты обращений к внешним зависимостям
package resilience

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrCircuitOpen сообщает, что автоматический выключатель временно не пропускает запросы
	ErrCircuitOpen = errors.New("circuit breaker is open")
	// errOperationPanicked отмечает завершение защищённой операции с паникой
	errOperationPanicked = errors.New("circuit breaker operation panicked")
)

// CircuitBreakerConfig содержит настройки автоматического выключателя
type CircuitBreakerConfig struct {
	// Enabled включает автоматический выключатель
	Enabled bool
	// FailureThreshold задаёт число последовательных ошибок перед открытием
	FailureThreshold int
	// OpenTimeout задаёт время до пробного запроса в полуоткрытом состоянии
	OpenTimeout time.Duration
}

// CircuitBreaker защищает внешнюю зависимость от повторных запросов после серии ошибок
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
	generation       uint64
}

type circuitState string

const (
	stateClosed   circuitState = "closed"
	stateOpen     circuitState = "open"
	stateHalfOpen circuitState = "half-open"
)

// NewCircuitBreaker создаёт автоматический выключатель для именованной внешней зависимости
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

// Allow резервирует попытку обращения к зависимости или возвращает ErrCircuitOpen
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

	attemptGeneration := b.generation
	var once sync.Once
	return func(err error) {
		once.Do(func() {
			b.report(attemptGeneration, err)
		})
	}, nil
}

// Execute выполняет операцию только если circuit breaker разрешает обращение
func (b *CircuitBreaker) Execute(operation func() error) (err error) {
	done, err := b.Allow()
	if err != nil {
		return err
	}
	defer func() {
		panicValue := recover()
		if panicValue != nil {
			done(errOperationPanicked)
			panic(panicValue)
		}
		done(err)
	}()

	err = operation()
	return err
}

// report обновляет состояние выключателя по результату попытки текущего поколения
func (b *CircuitBreaker) report(attemptGeneration uint64, err error) {
	if b == nil || !b.enabled {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if attemptGeneration != b.generation {
		return
	}

	if err == nil {
		if b.state == stateHalfOpen {
			b.generation++
		}
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

// open переводит выключатель в открытое состояние
func (b *CircuitBreaker) open() {
	b.generation++
	b.state = stateOpen
	b.openedAt = b.now()
	b.halfOpenInFlight = false
}
