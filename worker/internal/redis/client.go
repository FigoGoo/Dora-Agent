// Package redis 负责 Business Worker 的非权威 Redis 唤醒连接。
package redis

import (
	"context"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/worker/internal/config"
	redisclient "github.com/redis/go-redis/v9"
)

// Client 封装 Worker Redis Client 的生命周期。
type Client struct{ client *redisclient.Client }

// Open 创建 Redis Client 并完成有超时的启动 Ping。
func Open(ctx context.Context, cfg config.RedisConfig) (*Client, error) {
	client := redisclient.NewClient(&redisclient.Options{Addr: cfg.Address, Password: cfg.Password, DB: cfg.DB})
	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping worker redis: %w", err)
	}
	return &Client{client: client}, nil
}

// Close 关闭 Worker Redis 连接。
func (c *Client) Close() error {
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("close worker redis: %w", err)
	}
	return nil
}
