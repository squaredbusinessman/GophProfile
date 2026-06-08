package postgres

import (
	"context"
	"database/sql"
)

type Client struct {
	db *sql.DB
}

// NewClient создает PostgreSQL client поверх открытого DB connection pool
func NewClient(db *sql.DB) *Client {
	return &Client{db: db}
}

// HealthCheck проверяет доступность PostgreSQL client
func (c *Client) HealthCheck(ctx context.Context) error {
	return c.db.PingContext(ctx)
}

// DB возвращает открытый PostgreSQL connection pool
func (c *Client) DB() *sql.DB {
	return c.db
}
