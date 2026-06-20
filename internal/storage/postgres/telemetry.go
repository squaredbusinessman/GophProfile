package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/squaredbusinessman/GophProfile/internal/domain/avatar"
	"github.com/squaredbusinessman/GophProfile/internal/domain/outbox"
	"github.com/squaredbusinessman/GophProfile/internal/domain/user"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// postgresInstrumentationName задаёт имя области инструментирования PostgreSQL repositories
const postgresInstrumentationName = "github.com/squaredbusinessman/GophProfile/internal/storage/postgres"

// startRepositorySpan создаёт безопасный span на границе repository method
func startRepositorySpan(ctx context.Context, operation string, collection string) (context.Context, trace.Span) {
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
	return otel.Tracer(postgresInstrumentationName).Start(
		ctx,
		name,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attributes...),
	)
}

// finishRepositorySpan завершает span и отмечает неожиданные ошибки
func finishRepositorySpan(span trace.Span, err error) {
	defer span.End()
	if err == nil || isExpectedRepositoryResult(err) {
		return
	}

	span.SetStatus(codes.Error, "database operation failed")
	span.SetAttributes(attribute.String("error.type", rootErrorType(err)))
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
