package kafka

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	confluent "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"go.opentelemetry.io/otel"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	// TopicAvatarProcess содержит имя основной темы обработки аватаров
	TopicAvatarProcess = "avatar.process.v1"
	// TopicAvatarProcessRetry1m содержит имя темы повторной обработки через одну минуту
	TopicAvatarProcessRetry1m = "avatar.process.retry.1m.v1"
	// TopicAvatarProcessRetry5m содержит имя темы повторной обработки через пять минут
	TopicAvatarProcessRetry5m = "avatar.process.retry.5m.v1"
	// TopicAvatarProcessRetry30m содержит имя темы повторной обработки через тридцать минут
	TopicAvatarProcessRetry30m = "avatar.process.retry.30m.v1"
	// TopicAvatarProcessDeadLetter содержит имя темы недоставленных сообщений обработки
	TopicAvatarProcessDeadLetter = "avatar.process.dead-letter.v1"
	// TopicAvatarDelete содержит имя темы удаления аватаров
	TopicAvatarDelete = "avatar.delete.v1"
)

// Client объединяет производителя и потребителя Confluent с телеметрией
type Client struct {
	producer      producerAPI
	consumer      consumerAPI
	consumerGroup string
	telemetry     kafkaTelemetry
}

// Message содержит тело, заголовки и метаданные сообщения Kafka
type Message struct {
	// Topic содержит тему полученного сообщения Kafka
	Topic string
	// Key содержит ключ сообщения и не экспортируется в телеметрию
	Key []byte
	// Value содержит тело сообщения и не экспортируется в телеметрию
	Value []byte
	// Headers содержит заголовки Kafka вместе с контекстом трассировки W3C
	Headers map[string]string
	// Partition содержит раздел полученного сообщения
	Partition int32
	// Offset содержит смещение полученного сообщения
	Offset int64
}

// producerAPI описывает используемую часть производителя Confluent
type producerAPI interface {
	Produce(message *confluent.Message, deliveryChan chan confluent.Event) error
	GetMetadata(topic *string, allTopics bool, timeoutMs int) (*confluent.Metadata, error)
	Flush(timeoutMs int) int
	Close()
}

// consumerAPI описывает используемую часть потребителя Confluent
type consumerAPI interface {
	SubscribeTopics(topics []string, rebalanceCb confluent.RebalanceCb) error
	Poll(timeoutMs int) confluent.Event
	CommitMessage(message *confluent.Message) ([]confluent.TopicPartition, error)
	Close() error
}

// NewClient создаёт клиент Kafka на базе производителя и потребителя Confluent
func NewClient(brokers []string, clientID string, consumerGroup string) (*Client, error) {
	producer, err := confluent.NewProducer(&confluent.ConfigMap{
		"bootstrap.servers": strings.Join(brokers, ","),
		"client.id":         clientID,
		"acks":              "all",
	})
	if err != nil {
		return nil, fmt.Errorf("create kafka producer: %w", err)
	}

	consumer, err := confluent.NewConsumer(&confluent.ConfigMap{
		"bootstrap.servers":  strings.Join(brokers, ","),
		"group.id":           consumerGroup,
		"client.id":          clientID + "-consumer",
		"enable.auto.commit": false,
		"auto.offset.reset":  "earliest",
	})
	if err != nil {
		producer.Close()
		return nil, fmt.Errorf("create kafka consumer: %w", err)
	}
	telemetry, err := newKafkaTelemetry()
	if err != nil {
		producer.Close()
		_ = consumer.Close()
		return nil, fmt.Errorf("create kafka telemetry: %w", err)
	}

	return &Client{
		producer:      producer,
		consumer:      consumer,
		consumerGroup: consumerGroup,
		telemetry:     telemetry,
	}, nil
}

// Publish публикует сообщение в тему Kafka через производителя Confluent
func (c *Client) Publish(ctx context.Context, topic string, key string, payload []byte, headers map[string]string) error {
	messageHeaders := messageHeadersFromContext(ctx)
	for name, value := range headers {
		messageHeaders[name] = value
	}
	parentCtx := ctx
	if headers != nil {
		extractedCtx := ExtractTraceContext(ctx, headers)
		extractedSpan := trace.SpanContextFromContext(extractedCtx)
		currentSpan := trace.SpanContextFromContext(ctx)
		if !currentSpan.IsValid() || currentSpan.TraceID() != extractedSpan.TraceID() {
			parentCtx = extractedCtx
		}
	}
	spanCtx, operation := c.telemetry.startOperation(parentCtx, topic, "send", "send", trace.SpanKindProducer)
	carrier := headerCarrierFromMap(messageHeaders)
	otel.GetTextMapPropagator().Inject(spanCtx, &carrier)
	delivery := make(chan confluent.Event, 1)

	err := c.producer.Produce(&confluent.Message{
		TopicPartition: confluent.TopicPartition{
			Topic:     &topic,
			Partition: confluent.PartitionAny,
		},
		Key:     []byte(key),
		Value:   payload,
		Headers: carrier,
	}, delivery)
	if err != nil {
		operation.finish(kafkaResultError, err)
		return fmt.Errorf("produce kafka message: %w", err)
	}

	select {
	case event := <-delivery:
		message, ok := event.(*confluent.Message)
		if !ok {
			err := fmt.Errorf("unexpected kafka delivery event %T", event)
			operation.finish(kafkaResultError, err)
			return err
		}
		if message.TopicPartition.Error != nil {
			operation.finish(kafkaResultError, message.TopicPartition.Error)
			return fmt.Errorf("deliver kafka message: %w", message.TopicPartition.Error)
		}
		operation.finish(kafkaResultSuccess, nil,
			semconv.MessagingDestinationPartitionID(strconv.FormatInt(int64(message.TopicPartition.Partition), 10)),
		)
		return nil
	case <-ctx.Done():
		operation.finish(kafkaResultError, ctx.Err())
		return ctx.Err()
	}
}

// Consume читает сообщения Kafka и фиксирует смещение только после успешной обработки
func (c *Client) Consume(ctx context.Context, topics []string, handler func(context.Context, Message) error) error {
	if err := c.consumer.SubscribeTopics(topics, nil); err != nil {
		return fmt.Errorf("subscribe kafka topics: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		event := c.consumer.Poll(250)
		if event == nil {
			continue
		}

		switch item := event.(type) {
		case *confluent.Message:
			topic := ""
			if item.TopicPartition.Topic != nil {
				topic = *item.TopicPartition.Topic
			}
			message := Message{
				Topic:     topic,
				Key:       item.Key,
				Value:     item.Value,
				Headers:   HeaderCarrier(item.Headers).Map(),
				Partition: item.TopicPartition.Partition,
				Offset:    int64(item.TopicPartition.Offset),
			}
			parentCtx := ExtractTraceContext(ctx, message.Headers)
			parentCtx = contextWithMessageHeaders(parentCtx, message.Headers)
			processCtx, processOperation := c.telemetry.startOperation(
				parentCtx,
				topic,
				"process",
				"process",
				trace.SpanKindConsumer,
				kafkaMessageAttributes(c.consumerGroup, message.Partition, message.Offset)...,
			)
			if err := handler(processCtx, message); err != nil {
				processOperation.finish(kafkaResultError, err)
				continue
			}
			_, commitOperation := c.telemetry.startOperation(
				processCtx,
				topic,
				"commit",
				"settle",
				trace.SpanKindClient,
				kafkaMessageAttributes(c.consumerGroup, message.Partition, message.Offset)...,
			)
			_, err := c.consumer.CommitMessage(item)
			if err != nil {
				commitOperation.finish(kafkaResultError, err)
				processOperation.finish(kafkaResultError, err)
				return fmt.Errorf("commit kafka message: %w", err)
			}
			commitOperation.finish(kafkaResultSuccess, nil)
			processOperation.span.AddEvent(
				"messaging.kafka.offset.commit",
				trace.WithAttributes(kafkaMessageAttributes(c.consumerGroup, message.Partition, message.Offset)...),
			)
			processOperation.finish(kafkaResultSuccess, nil)
		case confluent.Error:
			if item.IsFatal() {
				return fmt.Errorf("fatal kafka consumer error: %w", item)
			}
		}
	}
}

// HealthCheck проверяет доступность клиента Kafka
func (c *Client) HealthCheck(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		_, err := c.producer.GetMetadata(nil, false, 1000)
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("check kafka metadata: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close закрывает клиент Kafka и дожидается отправки буфера производителя
func (c *Client) Close() {
	_ = c.consumer.Close()
	c.producer.Flush(int((5 * time.Second).Milliseconds()))
	c.producer.Close()
}
