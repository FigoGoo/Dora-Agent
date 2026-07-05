package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

type RedisCheckpointStore struct {
	client *redis.Client
	prefix string
}

func NewRedisCheckpointStore(client *redis.Client, prefix string) *RedisCheckpointStore {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "dora:aigc:checkpoint:"
	}
	return &RedisCheckpointStore{client: client, prefix: prefix}
}

func (s *RedisCheckpointStore) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	if s == nil || s.client == nil {
		return nil, false, fmt.Errorf("redis checkpoint client is required")
	}
	value, err := s.client.Get(ctx, s.key(checkPointID)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get checkpoint %s: %w", checkPointID, err)
	}
	return value, true, nil
}

func (s *RedisCheckpointStore) Set(ctx context.Context, checkPointID string, checkPoint []byte) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("redis checkpoint client is required")
	}
	if strings.TrimSpace(checkPointID) == "" {
		return fmt.Errorf("checkpoint id is required")
	}
	if err := s.client.Set(ctx, s.key(checkPointID), checkPoint, 0).Err(); err != nil {
		return fmt.Errorf("set checkpoint %s: %w", checkPointID, err)
	}
	return nil
}

func (s *RedisCheckpointStore) key(checkPointID string) string {
	return s.prefix + strings.TrimSpace(checkPointID)
}
