package app

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const appInstrumentationName = "github.com/squaredbusinessman/GophProfile/internal/app"

const (
	uploadResultAccepted = "accepted"
	uploadResultError    = "error"

	processResultReady          = "ready"
	processResultFailed         = "failed"
	processResultDeadLetter     = "dead_letter"
	processResultRetryScheduled = "retry_scheduled"
	processResultIdempotentSkip = "idempotent_skip"
	processResultError          = "error"

	deletePhaseRequest = "request"
	deletePhaseExecute = "execute"

	deleteResultAccepted       = "accepted"
	deleteResultCompleted      = "completed"
	deleteResultIdempotentSkip = "idempotent_skip"
	deleteResultRejected       = "rejected"
	deleteResultError          = "error"

	outboxPublishModeImmediate  = "immediate"
	outboxPublishModeBackground = "background"
	outboxPublishResultSuccess  = "success"
	outboxPublishResultError    = "error"
)

var (
	resultAttribute = attribute.Key("result")
	phaseAttribute  = attribute.Key("phase")
	modeAttribute   = attribute.Key("mode")
)

// businessTelemetry содержит низкокардинальные метрики прикладных операций
type businessTelemetry struct {
	uploads         metric.Int64Counter
	uploadBytes     metric.Int64Counter
	uploadDuration  metric.Float64Histogram
	processing      metric.Int64Counter
	processDuration metric.Float64Histogram
	deletes         metric.Int64Counter
	deleteDuration  metric.Float64Histogram
	outboxPublishes metric.Int64Counter
}

// newBusinessTelemetry создаёт инструменты бизнес-метрик
func newBusinessTelemetry() (businessTelemetry, error) {
	meter := otel.Meter(appInstrumentationName)
	uploads, err := meter.Int64Counter(
		"app.avatar.upload.count",
		metric.WithDescription("Количество завершённых загрузок аватаров по результату"),
		metric.WithUnit("{upload}"),
	)
	if err != nil {
		return businessTelemetry{}, fmt.Errorf("create avatar upload counter: %w", err)
	}
	uploadDuration, err := meter.Float64Histogram(
		"app.avatar.upload.duration",
		metric.WithDescription("Продолжительность загрузки аватара"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return businessTelemetry{}, fmt.Errorf("create avatar upload duration histogram: %w", err)
	}
	uploadBytes, err := meter.Int64Counter(
		"app.avatar.upload.bytes",
		metric.WithDescription("Количество байтов принятых оригиналов аватаров"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return businessTelemetry{}, fmt.Errorf("create avatar upload bytes counter: %w", err)
	}
	processing, err := meter.Int64Counter(
		"app.avatar.processing.count",
		metric.WithDescription("Количество завершённых попыток обработки аватаров по результату"),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		return businessTelemetry{}, fmt.Errorf("create avatar processing counter: %w", err)
	}
	processDuration, err := meter.Float64Histogram(
		"app.avatar.processing.duration",
		metric.WithDescription("Продолжительность попытки обработки аватара"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return businessTelemetry{}, fmt.Errorf("create avatar processing duration histogram: %w", err)
	}
	deletes, err := meter.Int64Counter(
		"app.avatar.delete.count",
		metric.WithDescription("Количество операций удаления аватаров по этапу и результату"),
		metric.WithUnit("{operation}"),
	)
	if err != nil {
		return businessTelemetry{}, fmt.Errorf("create avatar delete counter: %w", err)
	}
	deleteDuration, err := meter.Float64Histogram(
		"app.avatar.delete.duration",
		metric.WithDescription("Продолжительность операции удаления аватара"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return businessTelemetry{}, fmt.Errorf("create avatar delete duration histogram: %w", err)
	}
	outboxPublishes, err := meter.Int64Counter(
		"app.outbox.publish.count",
		metric.WithDescription("Количество попыток публикации событий outbox по режиму и результату"),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		return businessTelemetry{}, fmt.Errorf("create outbox publish counter: %w", err)
	}

	return businessTelemetry{
		uploads: uploads, uploadBytes: uploadBytes, uploadDuration: uploadDuration,
		processing: processing, processDuration: processDuration,
		deletes: deletes, deleteDuration: deleteDuration,
		outboxPublishes: outboxPublishes,
	}, nil
}

// recordUpload записывает результат и продолжительность загрузки
func (t businessTelemetry) recordUpload(ctx context.Context, startedAt time.Time, result string, acceptedBytes int64) {
	attrs := metric.WithAttributes(resultAttribute.String(result))
	t.uploads.Add(ctx, 1, attrs)
	if result == uploadResultAccepted && acceptedBytes > 0 {
		t.uploadBytes.Add(ctx, acceptedBytes)
	}
	t.uploadDuration.Record(ctx, time.Since(startedAt).Seconds(), attrs)
}

// recordProcessing записывает результат и продолжительность обработки
func (t businessTelemetry) recordProcessing(ctx context.Context, startedAt time.Time, result string) {
	attrs := metric.WithAttributes(resultAttribute.String(result))
	t.processing.Add(ctx, 1, attrs)
	t.processDuration.Record(ctx, time.Since(startedAt).Seconds(), attrs)
}

// recordDelete записывает этап, результат и продолжительность удаления
func (t businessTelemetry) recordDelete(ctx context.Context, startedAt time.Time, phase string, result string) {
	attrs := metric.WithAttributes(phaseAttribute.String(phase), resultAttribute.String(result))
	t.deletes.Add(ctx, 1, attrs)
	t.deleteDuration.Record(ctx, time.Since(startedAt).Seconds(), attrs)
}

// recordOutboxPublish записывает режим и результат публикации события outbox
func (t businessTelemetry) recordOutboxPublish(ctx context.Context, mode string, result string) {
	t.outboxPublishes.Add(ctx, 1, metric.WithAttributes(modeAttribute.String(mode), resultAttribute.String(result)))
}
