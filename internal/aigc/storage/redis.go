package storage

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
)

func NewRedisClient(cfg aigcconfig.RedisConfig) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
}

func NewGenerationRedisClient(cfg aigcconfig.Config) *redis.Client {
	return NewRedisClient(cfg.Normalize().Storage.GenerationRedis.RedisConfig)
}

func NewRuntimeRedisClient(cfg aigcconfig.Config) *redis.Client {
	return NewRedisClient(cfg.Normalize().Storage.RuntimeRedis.RedisConfig)
}

func PingRedis(ctx context.Context, client *redis.Client) error {
	if client == nil {
		return fmt.Errorf("redis client is required")
	}
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	return nil
}
