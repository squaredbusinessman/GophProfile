package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	// s3InstrumentationName задаёт имя области инструментирования S3 client
	s3InstrumentationName = "github.com/squaredbusinessman/GophProfile/internal/storage/s3"
	// s3ResultSuccess обозначает успешную S3 operation
	s3ResultSuccess = "success"
	// s3ResultNotFound обозначает ожидаемое отсутствие объекта
	s3ResultNotFound = "not_found"
	// s3ResultError обозначает неожиданную ошибку S3
	s3ResultError = "error"
)

var (
	// s3OperationNameKey содержит низкокардинальное имя S3 operation
	s3OperationNameKey = attribute.Key("s3.operation.name")
	// s3OperationResultKey содержит нормализованный результат S3 operation
	s3OperationResultKey = attribute.Key("s3.operation.result")
	// s3ObjectSizeKey содержит известный размер объекта без object key
	s3ObjectSizeKey = attribute.Key("s3.object.size")
	// s3ObjectContentTypeKey содержит MIME-тип объекта без object key
	s3ObjectContentTypeKey = attribute.Key("s3.object.content_type")
)

// s3Telemetry содержит инструменты S3 operation metrics
type s3Telemetry struct {
	operations metric.Int64Counter
	duration   metric.Float64Histogram
}

// s3Operation хранит состояние одной измеряемой S3 operation
type s3Operation struct {
	ctx       context.Context
	span      trace.Span
	telemetry s3Telemetry
	name      string
	startedAt time.Time
}

// observedReadCloser завершает Get operation по фактическому чтению или закрытию body
type observedReadCloser struct {
	body      io.ReadCloser
	operation *s3Operation
	once      sync.Once
}

// newS3Telemetry создаёт инструменты operation result и duration
func newS3Telemetry() s3Telemetry {
	meter := otel.Meter(s3InstrumentationName)
	operations, _ := meter.Int64Counter(
		"s3.client.operation.count",
		metric.WithDescription("Количество завершённых S3 operations по результату"),
		metric.WithUnit("{operation}"),
	)
	duration, _ := meter.Float64Histogram(
		"s3.client.operation.duration",
		metric.WithDescription("Продолжительность S3 operations"),
		metric.WithUnit("s"),
	)
	return s3Telemetry{operations: operations, duration: duration}
}

// startS3Operation создаёт client span и начинает измерение продолжительности
func (t s3Telemetry) startS3Operation(ctx context.Context, operation string, rpcMethod string, attrs ...attribute.KeyValue) (context.Context, *s3Operation) {
	spanAttributes := []attribute.KeyValue{
		attribute.String("rpc.system.name", "aws-api"),
		attribute.String("rpc.method", rpcMethod),
		s3OperationNameKey.String(operation),
	}
	spanAttributes = append(spanAttributes, attrs...)
	ctx, span := otel.Tracer(s3InstrumentationName).Start(
		ctx,
		"S3 "+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(spanAttributes...),
	)
	return ctx, &s3Operation{
		ctx:       ctx,
		span:      span,
		telemetry: t,
		name:      operation,
		startedAt: time.Now(),
	}
}

// finish завершает S3 span и записывает метрики без object key
func (o *s3Operation) finish(result string, err error, attrs ...attribute.KeyValue) {
	metricAttrs := metric.WithAttributes(
		s3OperationNameKey.String(o.name),
		s3OperationResultKey.String(result),
	)
	o.telemetry.operations.Add(o.ctx, 1, metricAttrs)
	o.telemetry.duration.Record(o.ctx, time.Since(o.startedAt).Seconds(), metricAttrs)

	spanAttrs := append([]attribute.KeyValue{s3OperationResultKey.String(result)}, attrs...)
	o.span.SetAttributes(spanAttrs...)
	if err != nil && result == s3ResultError {
		o.span.SetStatus(codes.Error, "S3 operation failed")
		o.span.SetAttributes(attribute.String("error.type", rootS3ErrorType(err)))
	}
	o.span.End()
}

// Read делегирует чтение без дополнительных обращений к body
func (r *observedReadCloser) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if err != nil {
		if errors.Is(err, io.EOF) {
			r.finish(s3ResultSuccess, nil)
		} else {
			r.finish(s3ResultError, err)
		}
	}
	return n, err
}

// Close закрывает исходный body и завершает Get operation
func (r *observedReadCloser) Close() error {
	err := r.body.Close()
	if err != nil {
		r.finish(s3ResultError, err)
	} else {
		r.finish(s3ResultSuccess, nil)
	}
	return err
}

// finish гарантирует однократное завершение span и запись метрик
func (r *observedReadCloser) finish(result string, err error) {
	r.once.Do(func() {
		r.operation.finish(result, err)
	})
}

// objectAttributes создаёт безопасные object attributes из известных metadata
func objectAttributes(size int64, contentType string) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 2)
	if size >= 0 {
		attrs = append(attrs, s3ObjectSizeKey.Int64(size))
	}
	if contentType != "" {
		attrs = append(attrs, s3ObjectContentTypeKey.String(contentType))
	}
	return attrs
}

// rootS3ErrorType возвращает тип исходной ошибки без раскрытия текста
func rootS3ErrorType(err error) string {
	for errors.Unwrap(err) != nil {
		err = errors.Unwrap(err)
	}
	return fmt.Sprintf("%T", err)
}
