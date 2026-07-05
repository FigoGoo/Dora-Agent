package generation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type QueuePayload struct {
	JobID          string    `json:"job_id"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	EnqueuedAt     time.Time `json:"enqueued_at"`
}

type RedisQueue struct {
	client  *redis.Client
	listKey string
}

func NewRedisQueue(client *redis.Client, listKey string) *RedisQueue {
	return &RedisQueue{
		client:  client,
		listKey: strings.TrimSpace(listKey),
	}
}

func (q *RedisQueue) Enqueue(ctx context.Context, payload QueuePayload) error {
	if q == nil || q.client == nil {
		return fmt.Errorf("redis generation queue client is required")
	}
	if q.listKey == "" {
		return fmt.Errorf("redis generation queue list key is required")
	}
	payload.JobID = strings.TrimSpace(payload.JobID)
	payload.IdempotencyKey = strings.TrimSpace(payload.IdempotencyKey)
	if payload.JobID == "" {
		return fmt.Errorf("generation job id is required")
	}
	if payload.EnqueuedAt.IsZero() {
		payload.EnqueuedAt = time.Now()
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal generation queue payload: %w", err)
	}
	if err := q.client.RPush(ctx, q.listKey, raw).Err(); err != nil {
		return fmt.Errorf("enqueue generation job %s: %w", payload.JobID, err)
	}
	return nil
}

func (q *RedisQueue) Dequeue(ctx context.Context, timeout time.Duration) (QueuePayload, bool, error) {
	if q == nil || q.client == nil {
		return QueuePayload{}, false, fmt.Errorf("redis generation queue client is required")
	}
	if q.listKey == "" {
		return QueuePayload{}, false, fmt.Errorf("redis generation queue list key is required")
	}
	values, err := q.client.BLPop(ctx, timeout, q.listKey).Result()
	if err == redis.Nil {
		return QueuePayload{}, false, nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return QueuePayload{}, false, ctx.Err()
		}
		return QueuePayload{}, false, fmt.Errorf("dequeue generation job: %w", err)
	}
	if len(values) != 2 {
		return QueuePayload{}, false, fmt.Errorf("unexpected generation queue payload shape: %#v", values)
	}
	var payload QueuePayload
	if err := json.Unmarshal([]byte(values[1]), &payload); err != nil {
		return QueuePayload{}, false, fmt.Errorf("decode generation queue payload: %w", err)
	}
	return payload, true, nil
}
