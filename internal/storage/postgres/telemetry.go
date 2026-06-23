package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// postgresInstrumentationName задаёт имя области инструментирования PostgreSQL repositories
const postgresInstrumentationName = "github.com/squaredbusinessman/GophProfile/internal/storage/postgres"

var databaseResultAttribute = attribute.Key("db.operation.result")

// postgresTelemetry содержит метрики операций репозиториев PostgreSQL
type postgresTelemetry struct {
	operations metric.Int64Counter
	duration   metric.Float64Histogram
}

// repositoryOperation связывает span с измерением одной операции репозитория
type repositoryOperation struct {
	trace.Span
	startedAt  time.Time
	operation  string
	collection string
	telemetry  postgresTelemetry
}

// newPostgresTelemetry создаёт инструменты PostgreSQL для текущего провайдера метрик
func newPostgresTelemetry() postgresTelemetry {
	meter := otel.Meter(postgresInstrumentationName)
	operations, _ := meter.Int64Counter(
		"db.client.operation.count",
		metric.WithDescription("Количество завершённых операций PostgreSQL repository"),
		metric.WithUnit("{operation}"),
	)
	duration, _ := meter.Float64Histogram(
		"db.client.operation.duration",
		metric.WithDescription("Продолжительность операций PostgreSQL repository"),
		metric.WithUnit("s"),
	)
	return postgresTelemetry{operations: operations, duration: duration}
}

// startRepositoryOperation создаёт безопасный span и начинает измерение метода репозитория
func (t postgresTelemetry) startRepositoryOperation(ctx context.Context, operation string, collection string) (context.Context, *repositoryOperation) {
	attributes := []attribute.KeyValue{
		semconv.DBSystemNamePostgreSQL,
		semconv.DBOperationName(operation),
	}
	if collection != "" {
		attributes = append(attributes, semconv.DBCollectionName(collection))
	}

	name := operation
	if collection != "" {
		name += " " + collection
	}
	ctx, span := otel.Tracer(postgresInstrumentationName).Start(
		ctx,
		name,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attributes...),
	)
	return ctx, &repositoryOperation{
		Span: span, startedAt: time.Now(), operation: operation, collection: collection, telemetry: t,
	}
}

// finishRepositoryOperation завершает span и записывает безопасный результат операции
func finishRepositoryOperation(operation *repositoryOperation, err error) {
	defer operation.End()
	result := "success"
	if isExpectedRepositoryResult(err) {
		result = "not_found"
	} else if err != nil {
		result = "error"
		operation.SetStatus(codes.Error, "database operation failed")
		operation.SetAttributes(attribute.String("error.type", rootErrorType(err)))
	}

	attributes := []attribute.KeyValue{
		semconv.DBSystemNamePostgreSQL,
		semconv.DBOperationName(operation.operation),
		databaseResultAttribute.String(result),
	}
	if operation.collection != "" {
		attributes = append(attributes, semconv.DBCollectionName(operation.collection))
	}
	options := metric.WithAttributes(attributes...)
	operation.telemetry.operations.Add(context.Background(), 1, options)
	operation.telemetry.duration.Record(context.Background(), time.Since(operation.startedAt).Seconds(), options)
}

// isExpectedRepositoryResult отличает штатное отсутствие записи от ошибки БД
func isExpectedRepositoryResult(err error) bool {
	return errors.Is(err, avatar.ErrNotFound) ||
		errors.Is(err, outbox.ErrNotFound) ||
		errors.Is(err, user.ErrNotFound)
}

// rootErrorType возвращает тип исходной ошибки без раскрытия её текста
func rootErrorType(err error) string {
	for errors.Unwrap(err) != nil {
		err = errors.Unwrap(err)
	}
	return fmt.Sprintf("%T", err)
}

// addTransactionEvent записывает результат изменения состояния SQL-транзакции
func addTransactionEvent(span trace.Span, name string, err error) {
	if err == nil {
		span.AddEvent(name)
		return
	}
	span.AddEvent(name, trace.WithAttributes(attribute.String("error.type", fmt.Sprintf("%T", err))))
}
