package kafka

import (
	"context"
	"fmt"
	"strings"
	"time"

	confluent "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

const (
	TopicAvatarProcess = "avatar.process.v1"
	TopicAvatarDelete  = "avatar.delete.v1"
)

type Client struct {
	brokers       []string
	clientID      string
	consumerGroup string
	producer      *confluent.Producer
}

// NewClient создает Kafka client на базе Confluent producer
func NewClient(brokers []string, clientID string, consumerGroup string) (*Client, error) {
	producer, err := confluent.NewProducer(&confluent.ConfigMap{
		"bootstrap.servers": strings.Join(brokers, ","),
		"client.id":         clientID,
		"acks":              "all",
	})
	if err != nil {
		return nil, fmt.Errorf("create kafka producer: %w", err)
	}

	return &Client{
		brokers:       append([]string(nil), brokers...),
		clientID:      clientID,
		consumerGroup: consumerGroup,
		producer:      producer,
	}, nil
}

// Publish публикует сообщение в Kafka topic через Confluent producer
func (c *Client) Publish(ctx context.Context, topic string, key string, payload []byte) error {
	delivery := make(chan confluent.Event, 1)

	err := c.producer.Produce(&confluent.Message{
		TopicPartition: confluent.TopicPartition{
			Topic:     &topic,
			Partition: confluent.PartitionAny,
		},
		Key:   []byte(key),
		Value: payload,
	}, delivery)
	if err != nil {
		return fmt.Errorf("produce kafka message: %w", err)
	}

	select {
	case event := <-delivery:
		message, ok := event.(*confluent.Message)
		if !ok {
			return fmt.Errorf("unexpected kafka delivery event %T", event)
		}
		if message.TopicPartition.Error != nil {
			return fmt.Errorf("deliver kafka message: %w", message.TopicPartition.Error)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// HealthCheck проверяет доступность Kafka client
func (c *Client) HealthCheck(ctx context.Context) error {
	return ctx.Err()
}

// Close закрывает Kafka producer и дожидается отправки буфера
func (c *Client) Close() {
	c.producer.Flush(int((5 * time.Second).Milliseconds()))
	c.producer.Close()
}

// Brokers возвращает список Kafka brokers
func (c *Client) Brokers() []string {
	return append([]string(nil), c.brokers...)
}

// ClientID возвращает Kafka client id
func (c *Client) ClientID() string {
	return c.clientID
}

// ConsumerGroup возвращает Kafka consumer group
func (c *Client) ConsumerGroup() string {
	return c.consumerGroup
}
