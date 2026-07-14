// Package redis 负责 Agent Service 的非权威 Redis 连接。
package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	redisclient "github.com/redis/go-redis/v9"
)

// Client 封装 Agent Redis Client 的生命周期。
type Client struct {
	client *redisclient.Client
}

// ClaimIdentityNonce 使用 Redis SET NX PX 原子占有一次性用户身份断言 Nonce。
// Key 只包含不可逆摘要；Redis 错误原样返回给身份边界并失败关闭，绝不退化为进程内重放表。
func (c *Client) ClaimIdentityNonce(ctx context.Context, kid string, nonce []byte, ttl time.Duration) (bool, error) {
	if c == nil || c.client == nil || kid == "" || len(nonce) != 16 || ttl <= 0 {
		return false, fmt.Errorf("claim Agent HTTP identity nonce: invalid input")
	}
	digest := sha256.Sum256(nonce)
	key := "dora:agent:http-identity:v1:" + kid + ":" + hex.EncodeToString(digest[:])
	claimed, err := c.client.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("claim Agent HTTP identity nonce: %w", err)
	}
	return claimed, nil
}

// Open 创建 Redis Client 并完成有超时的启动 Ping。
func Open(ctx context.Context, cfg config.RedisConfig) (*Client, error) {
	client := redisclient.NewClient(&redisclient.Options{Addr: cfg.Address, Password: cfg.Password, DB: cfg.DB})
	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping agent redis: %w", err)
	}
	return &Client{client: client}, nil
}

// Close 关闭 Agent Redis 连接。
func (c *Client) Close() error {
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("close agent redis: %w", err)
	}
	return nil
}
