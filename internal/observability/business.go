package observability

import (
	"context"
	"fmt"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const businessInstrumentationName = "github.com/squaredbusinessman/GophProfile/internal/observability/business"

// businessMetricViews задаёт полезные для API временные границы гистограмм
func businessMetricViews() []sdkmetric.View {
	boundaries := []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	views := make([]sdkmetric.View, 0, 3)
	for _, name := range []string{
		"app.avatar.upload.duration",
		"app.avatar.processing.duration",
		"app.avatar.delete.duration",
	} {
		views = append(views, sdkmetric.NewView(
			sdkmetric.Instrument{Name: name, Kind: sdkmetric.InstrumentKindHistogram},
			sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: boundaries}},
		))
	}
	return views
}

// OutboxOperationalStatsReader описывает чтение агрегированного состояния outbox
type OutboxOperationalStatsReader interface {
	ReadOutboxOperationalStats(ctx context.Context) (pendingCount int64, oldestAgeSeconds float64, err error)
}

// AvatarOperationalStatsReader описывает чтение агрегированного состояния аватаров
type AvatarOperationalStatsReader interface {
	ReadAvatarOperationalStats(ctx context.Context) (countByStatus map[avatar.Status]int64, originalStorageBytes int64, err error)
}

// RegisterBusinessMetrics регистрирует восстанавливаемые из PostgreSQL показатели состояния
func (t *Telemetry) RegisterBusinessMetrics(outboxReader OutboxOperationalStatsReader, avatarReader AvatarOperationalStatsReader) error {
	meter := t.MeterProvider.Meter(businessInstrumentationName)
	pending, err := meter.Int64ObservableGauge(
		"app.outbox.pending.count",
		metric.WithDescription("Количество ожидающих публикации событий outbox"),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		return fmt.Errorf("create pending outbox metric: %w", err)
	}
	oldestAge, err := meter.Float64ObservableGauge(
		"app.outbox.oldest.age",
		metric.WithDescription("Возраст старейшего ожидающего события outbox"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("create oldest outbox age metric: %w", err)
	}
	avatarCount, err := meter.Int64ObservableGauge(
		"app.avatar.count",
		metric.WithDescription("Количество аватаров по состояниям"),
		metric.WithUnit("{avatar}"),
	)
	if err != nil {
		return fmt.Errorf("create avatar count metric: %w", err)
	}
	originalStorage, err := meter.Int64ObservableGauge(
		"app.avatar.original.storage",
		metric.WithDescription("Суммарный размер хранимых оригиналов аватаров"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return fmt.Errorf("create original storage metric: %w", err)
	}

	registration, err := meter.RegisterCallback(
		func(ctx context.Context, observer metric.Observer) error {
			pendingCount, oldestAgeSeconds, readErr := outboxReader.ReadOutboxOperationalStats(ctx)
			if readErr != nil {
				otel.Handle(fmt.Errorf("read outbox operational metrics: %w", readErr))
			} else {
				observer.ObserveInt64(pending, pendingCount)
				observer.ObserveFloat64(oldestAge, oldestAgeSeconds)
			}

			countByStatus, originalStorageBytes, readErr := avatarReader.ReadAvatarOperationalStats(ctx)
			if readErr != nil {
				otel.Handle(fmt.Errorf("read avatar operational metrics: %w", readErr))
				return nil
			}
			for _, status := range []avatar.Status{
				avatar.StatusProcessing,
				avatar.StatusReady,
				avatar.StatusFailed,
				avatar.StatusDeleting,
				avatar.StatusDeleted,
			} {
				observer.ObserveInt64(avatarCount, countByStatus[status], metric.WithAttributes(
					attribute.String("status", string(status)),
				))
			}
			observer.ObserveInt64(originalStorage, originalStorageBytes)
			return nil
		},
		pending,
		oldestAge,
		avatarCount,
		originalStorage,
	)
	if err != nil {
		return fmt.Errorf("register business metrics: %w", err)
	}

	t.mu.Lock()
	t.metricRegistrations = append(t.metricRegistrations, registration)
	t.mu.Unlock()
	return nil
}
