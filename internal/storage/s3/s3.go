package s3

import "context"

type Client struct {
	endpoint string
	bucket   string
}

// NewClient создает заготовку S3 client
func NewClient(endpoint string, bucket string) *Client {
	return &Client{
		endpoint: endpoint,
		bucket:   bucket,
	}
}

// HealthCheck проверяет доступность S3 client
func (c *Client) HealthCheck(ctx context.Context) error {
	return ctx.Err()
}

// Endpoint возвращает endpoint S3-хранилища
func (c *Client) Endpoint() string {
	return c.endpoint
}

// Bucket возвращает bucket S3-хранилища
func (c *Client) Bucket() string {
	return c.bucket
}
