package kafka

import "context"

const (
	TopicAvatarProcess = "avatar.process.v1"
	TopicAvatarDelete  = "avatar.delete.v1"
)

type Client struct {
	brokers       []string
	clientID      string
	consumerGroup string
}

// NewClient создает заготовку Kafka client
func NewClient(brokers []string, clientID string, consumerGroup string) *Client {
	return &Client{
		brokers:       append([]string(nil), brokers...),
		clientID:      clientID,
		consumerGroup: consumerGroup,
	}
}

// HealthCheck проверяет доступность Kafka client
func (c *Client) HealthCheck(ctx context.Context) error {
	return ctx.Err()
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
