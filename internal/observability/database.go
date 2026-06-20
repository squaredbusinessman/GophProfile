package observability

import (
	"context"
	"database/sql"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// databaseInstrumentationName задаёт имя области метрик database/sql
const databaseInstrumentationName = "github.com/squaredbusinessman/GophProfile/internal/observability/database"

// RegisterDBPool регистрирует observable metrics состояния connection pool
func (t *Telemetry) RegisterDBPool(db *sql.DB, poolName string) error {
	meter := t.MeterProvider.Meter(databaseInstrumentationName)
	connections, err := meter.Int64ObservableUpDownCounter(
		"db.client.connection.count",
		metric.WithDescription("Количество соединений PostgreSQL по состоянию"),
		metric.WithUnit("{connection}"),
	)
	if err != nil {
		return fmt.Errorf("create database connection count metric: %w", err)
	}
	maximum, err := meter.Int64ObservableUpDownCounter(
		"db.client.connection.max",
		metric.WithDescription("Максимальное количество открытых соединений PostgreSQL"),
		metric.WithUnit("{connection}"),
	)
	if err != nil {
		return fmt.Errorf("create database connection max metric: %w", err)
	}
	waits, err := meter.Int64ObservableCounter(
		"db.client.connection.wait.count",
		metric.WithDescription("Количество ожиданий свободного соединения PostgreSQL"),
		metric.WithUnit("{wait}"),
	)
	if err != nil {
		return fmt.Errorf("create database connection wait count metric: %w", err)
	}
	waitDuration, err := meter.Float64ObservableCounter(
		"db.client.connection.wait.duration",
		metric.WithDescription("Суммарное время ожидания свободного соединения PostgreSQL"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("create database connection wait duration metric: %w", err)
	}

	poolAttribute := attribute.String("db.client.connection.pool.name", poolName)
	registration, err := meter.RegisterCallback(
		func(_ context.Context, observer metric.Observer) error {
			stats := db.Stats()
			observer.ObserveInt64(connections, int64(stats.InUse), metric.WithAttributes(
				poolAttribute,
				attribute.String("db.client.connection.state", "used"),
			))
			observer.ObserveInt64(connections, int64(stats.Idle), metric.WithAttributes(
				poolAttribute,
				attribute.String("db.client.connection.state", "idle"),
			))
			observer.ObserveInt64(maximum, int64(stats.MaxOpenConnections), metric.WithAttributes(poolAttribute))
			observer.ObserveInt64(waits, stats.WaitCount, metric.WithAttributes(poolAttribute))
			observer.ObserveFloat64(waitDuration, stats.WaitDuration.Seconds(), metric.WithAttributes(poolAttribute))
			return nil
		},
		connections,
		maximum,
		waits,
		waitDuration,
	)
	if err != nil {
		return fmt.Errorf("register database pool metrics: %w", err)
	}

	t.mu.Lock()
	t.metricRegistrations = append(t.metricRegistrations, registration)
	t.mu.Unlock()
	return nil
}
