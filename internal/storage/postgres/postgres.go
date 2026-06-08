package postgres

import "context"

type Client struct {
	dsn string
}

// NewClient создает заготовку PostgreSQL client
func NewClient(dsn string) *Client {
	return &Client{dsn: dsn}
}

// HealthCheck проверяет доступность PostgreSQL client
func (c *Client) HealthCheck(ctx context.Context) error {
	return ctx.Err()
}

// DSN возвращает строку подключения PostgreSQL
func (c *Client) DSN() string {
	return c.dsn
}
