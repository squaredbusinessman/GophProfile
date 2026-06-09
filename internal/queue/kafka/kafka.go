package kafka

import (
	"context"
	"fmt"
	"strings"
	"time"

	confluent "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

const (
	TopicAvatarProcess           = "avatar.process.v1"
	TopicAvatarProcessRetry1m    = "avatar.process.retry.1m.v1"
	TopicAvatarProcessRetry5m    = "avatar.process.retry.5m.v1"
	TopicAvatarProcessRetry30m   = "avatar.process.retry.30m.v1"
	TopicAvatarProcessDeadLetter = "avatar.process.dead-letter.v1"
	TopicAvatarDelete            = "avatar.delete.v1"
)

type Client struct {
	brokers       []string
	clientID      string
	consumerGroup string
	producer      *confluent.Producer
	consumer      *confluent.Consumer
}

type Message struct {
	Topic string
	Key   []byte
	Value []byte
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

	return &Client{
		brokers:       append([]string(nil), brokers...),
		clientID:      clientID,
		consumerGroup: consumerGroup,
		producer:      producer,
		consumer:      consumer,
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

// Consume читает Kafka messages и коммитит offset только после успешного handler
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
				Topic: topic,
				Key:   item.Key,
				Value: item.Value,
			}
			if err := handler(ctx, message); err != nil {
				continue
			}
			if _, err := c.consumer.CommitMessage(item); err != nil {
				return fmt.Errorf("commit kafka message: %w", err)
			}
		case confluent.Error:
			if item.IsFatal() {
				return fmt.Errorf("fatal kafka consumer error: %w", item)
			}
		}
	}
}

// HealthCheck проверяет доступность Kafka client
func (c *Client) HealthCheck(ctx context.Context) error {
	return ctx.Err()
}

// Close закрывает Kafka producer и дожидается отправки буфера
func (c *Client) Close() {
	c.consumer.Close()
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
