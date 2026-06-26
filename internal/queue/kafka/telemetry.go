package kafka

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	// kafkaInstrumentationName задаёт имя области инструментирования Kafka
	kafkaInstrumentationName = "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
	// kafkaResultSuccess обозначает успешную операцию Kafka
	kafkaResultSuccess = "success"
	// kafkaResultError обозначает ошибочную операцию Kafka
	kafkaResultError = "error"
)

var kafkaResultKey = attribute.Key("messaging.operation.result")

// kafkaTelemetry содержит метрики операций Kafka
type kafkaTelemetry struct {
	operations      metric.Int64Counter
	clientDuration  metric.Float64Histogram
	processDuration metric.Float64Histogram
}

// kafkaOperation хранит состояние одного span производителя или потребителя
type kafkaOperation struct {
	ctx       context.Context
	span      trace.Span
	telemetry kafkaTelemetry
	topic     string
	name      string
	typeName  string
	startedAt time.Time
}

// newKafkaTelemetry создаёт инструменты метрик операций Kafka
func newKafkaTelemetry() (kafkaTelemetry, error) {
	meter := otel.Meter(kafkaInstrumentationName)
	operations, err := meter.Int64Counter("messaging.client.operation.count", metric.WithDescription("Количество операций Kafka по результату"), metric.WithUnit("{operation}"))
	if err != nil {
		return kafkaTelemetry{}, fmt.Errorf("create Kafka operation counter: %w", err)
	}
	clientDuration, err := meter.Float64Histogram("messaging.client.operation.duration", metric.WithDescription("Продолжительность клиентских операций Kafka"), metric.WithUnit("s"))
	if err != nil {
		return kafkaTelemetry{}, fmt.Errorf("create Kafka client duration histogram: %w", err)
	}
	processDuration, err := meter.Float64Histogram("messaging.process.duration", metric.WithDescription("Продолжительность обработки сообщений Kafka"), metric.WithUnit("s"))
	if err != nil {
		return kafkaTelemetry{}, fmt.Errorf("create Kafka process duration histogram: %w", err)
	}
	return kafkaTelemetry{operations: operations, clientDuration: clientDuration, processDuration: processDuration}, nil
}

// startOperation создаёт span Kafka с низкокардинальными атрибутами
func (t kafkaTelemetry) startOperation(ctx context.Context, topic string, name string, typeName string, kind trace.SpanKind, attrs ...attribute.KeyValue) (context.Context, *kafkaOperation) {
	spanAttrs := []attribute.KeyValue{
		semconv.MessagingSystemKafka,
		semconv.MessagingDestinationName(topic),
		semconv.MessagingOperationName(name),
		semconv.MessagingOperationTypeKey.String(typeName),
	}
	spanAttrs = append(spanAttrs, attrs...)
	ctx, span := otel.Tracer(kafkaInstrumentationName).Start(ctx, name+" "+topic, trace.WithSpanKind(kind), trace.WithAttributes(spanAttrs...))
	return ctx, &kafkaOperation{ctx: ctx, span: span, telemetry: t, topic: topic, name: name, typeName: typeName, startedAt: time.Now()}
}

// finish завершает span Kafka и записывает метрики операции
func (o *kafkaOperation) finish(result string, err error, attrs ...attribute.KeyValue) {
	metricAttrs := metric.WithAttributes(
		semconv.MessagingSystemKafka,
		semconv.MessagingDestinationName(o.topic),
		semconv.MessagingOperationName(o.name),
		semconv.MessagingOperationTypeKey.String(o.typeName),
		kafkaResultKey.String(result),
	)
	duration := time.Since(o.startedAt).Seconds()
	o.telemetry.operations.Add(o.ctx, 1, metricAttrs)
	if o.typeName == "process" {
		o.telemetry.processDuration.Record(o.ctx, duration, metricAttrs)
	} else {
		o.telemetry.clientDuration.Record(o.ctx, duration, metricAttrs)
	}
	o.span.SetAttributes(append([]attribute.KeyValue{kafkaResultKey.String(result)}, attrs...)...)
	if err != nil {
		o.span.SetStatus(codes.Error, "Kafka operation failed")
		o.span.SetAttributes(attribute.String("error.type", rootKafkaErrorType(err)))
	}
	o.span.End()
}

// kafkaMessageAttributes создаёт атрибуты метаданных без тела и ключа сообщения
func kafkaMessageAttributes(group string, partition int32, offset int64) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.MessagingDestinationPartitionID(strconv.FormatInt(int64(partition), 10)),
		semconv.MessagingKafkaOffsetKey.Int64(offset),
	}
	if group != "" {
		attrs = append(attrs, semconv.MessagingConsumerGroupName(group))
	}
	return attrs
}

// rootKafkaErrorType возвращает тип исходной ошибки без раскрытия текста
func rootKafkaErrorType(err error) string {
	for errors.Unwrap(err) != nil {
		err = errors.Unwrap(err)
	}
	return fmt.Sprintf("%T", err)
}
