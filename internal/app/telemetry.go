package app

import (
	"context"
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
	uploadDuration  metric.Float64Histogram
	processing      metric.Int64Counter
	processDuration metric.Float64Histogram
	deletes         metric.Int64Counter
	deleteDuration  metric.Float64Histogram
	outboxPublishes metric.Int64Counter
}

// newBusinessTelemetry создаёт инструменты и инициализирует ожидаемые результаты нулями
func newBusinessTelemetry() businessTelemetry {
	meter := otel.Meter(appInstrumentationName)
	uploads, _ := meter.Int64Counter(
		"app.avatar.upload.count",
		metric.WithDescription("Количество завершённых загрузок аватаров по результату"),
		metric.WithUnit("{upload}"),
	)
	uploadDuration, _ := meter.Float64Histogram(
		"app.avatar.upload.duration",
		metric.WithDescription("Продолжительность загрузки аватара"),
		metric.WithUnit("s"),
	)
	processing, _ := meter.Int64Counter(
		"app.avatar.processing.count",
		metric.WithDescription("Количество завершённых попыток обработки аватаров по результату"),
		metric.WithUnit("{attempt}"),
	)
	processDuration, _ := meter.Float64Histogram(
		"app.avatar.processing.duration",
		metric.WithDescription("Продолжительность попытки обработки аватара"),
		metric.WithUnit("s"),
	)
	deletes, _ := meter.Int64Counter(
		"app.avatar.delete.count",
		metric.WithDescription("Количество операций удаления аватаров по этапу и результату"),
		metric.WithUnit("{operation}"),
	)
	deleteDuration, _ := meter.Float64Histogram(
		"app.avatar.delete.duration",
		metric.WithDescription("Продолжительность операции удаления аватара"),
		metric.WithUnit("s"),
	)
	outboxPublishes, _ := meter.Int64Counter(
		"app.outbox.publish.count",
		metric.WithDescription("Количество попыток публикации событий outbox по режиму и результату"),
		metric.WithUnit("{attempt}"),
	)

	telemetry := businessTelemetry{
		uploads: uploads, uploadDuration: uploadDuration,
		processing: processing, processDuration: processDuration,
		deletes: deletes, deleteDuration: deleteDuration,
		outboxPublishes: outboxPublishes,
	}
	telemetry.initialize(context.Background())
	return telemetry
}

// initialize создаёт временные ряды для всех ожидаемых комбинаций меток
func (t businessTelemetry) initialize(ctx context.Context) {
	for _, result := range []string{uploadResultAccepted, uploadResultError} {
		t.uploads.Add(ctx, 0, metric.WithAttributes(resultAttribute.String(result)))
	}
	for _, result := range []string{processResultReady, processResultFailed, processResultRetryScheduled, processResultIdempotentSkip, processResultError} {
		t.processing.Add(ctx, 0, metric.WithAttributes(resultAttribute.String(result)))
	}
	for _, labels := range []struct{ phase, result string }{
		{deletePhaseRequest, deleteResultAccepted},
		{deletePhaseRequest, deleteResultIdempotentSkip},
		{deletePhaseRequest, deleteResultRejected},
		{deletePhaseRequest, deleteResultError},
		{deletePhaseExecute, deleteResultCompleted},
		{deletePhaseExecute, deleteResultIdempotentSkip},
		{deletePhaseExecute, deleteResultError},
	} {
		t.deletes.Add(ctx, 0, metric.WithAttributes(phaseAttribute.String(labels.phase), resultAttribute.String(labels.result)))
	}
	for _, mode := range []string{outboxPublishModeImmediate, outboxPublishModeBackground} {
		for _, result := range []string{outboxPublishResultSuccess, outboxPublishResultError} {
			t.outboxPublishes.Add(ctx, 0, metric.WithAttributes(modeAttribute.String(mode), resultAttribute.String(result)))
		}
	}
}

// recordUpload записывает результат и продолжительность загрузки
func (t businessTelemetry) recordUpload(ctx context.Context, startedAt time.Time, result string) {
	attrs := metric.WithAttributes(resultAttribute.String(result))
	t.uploads.Add(ctx, 1, attrs)
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
