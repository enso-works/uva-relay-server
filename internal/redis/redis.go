package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
)

// Client wraps a Redis connection with health check support.
type Client struct {
	rdb *goredis.Client
}

// New creates a Redis client. The url can be a Redis URL (redis://...) or host:port.
func New(url, password string, db int) (*Client, error) {
	if url == "" {
		return nil, fmt.Errorf("redis URL is required")
	}

	opts, err := goredis.ParseURL(url)
	if err != nil {
		// Treat as host:port
		opts = &goredis.Options{
			Addr:     url,
			Password: password,
			DB:       db,
		}
	}

	rdb := goredis.NewClient(opts)
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		rdb.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &Client{rdb: rdb}, nil
}

// RDB returns the underlying go-redis client.
func (c *Client) RDB() *goredis.Client {
	return c.rdb
}

// Ping checks Redis connectivity.
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Close closes the Redis connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}
