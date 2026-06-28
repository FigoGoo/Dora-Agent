package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench"
	"github.com/redis/go-redis/v9"
)

type RedisGenerationQueue struct {
	client        *redis.Client
	listKey       string
	processingKey string
}

type RedisGenerationQueueConfig struct {
	Address  string
	Password string
	DB       int
	ListKey  string
}

func NewRedisGenerationQueue(cfg RedisGenerationQueueConfig) (*RedisGenerationQueue, error) {
	if strings.TrimSpace(cfg.Address) == "" {
		return nil, fmt.Errorf("redis address is required")
	}
	if strings.TrimSpace(cfg.ListKey) == "" {
		cfg.ListKey = "dora:agent:generation_jobs"
	}
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Address,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	return &RedisGenerationQueue{client: client, listKey: cfg.ListKey, processingKey: cfg.ListKey + ":processing"}, nil
}

func (q *RedisGenerationQueue) Close() error {
	return q.client.Close()
}

func (q *RedisGenerationQueue) Ping(ctx context.Context) error {
	return q.client.Ping(ctx).Err()
}

func (q *RedisGenerationQueue) EnqueueGenerationJob(ctx context.Context, job workbench.GenerationJob) error {
	if job.EnqueuedAt.IsZero() {
		job.EnqueuedAt = time.Now().UTC()
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return q.client.LPush(ctx, q.listKey, payload).Err()
}

func (q *RedisGenerationQueue) DequeueGenerationJob(ctx context.Context) (workbench.GenerationJob, error) {
	payload, err := q.client.BRPopLPush(ctx, q.listKey, q.processingKey, 0).Result()
	if err != nil {
		return workbench.GenerationJob{}, err
	}
	var job workbench.GenerationJob
	if err := json.Unmarshal([]byte(payload), &job); err != nil {
		return workbench.GenerationJob{}, err
	}
	job.QueueToken = payload
	return job, nil
}

func (q *RedisGenerationQueue) CompleteGenerationJob(ctx context.Context, job workbench.GenerationJob) error {
	token := job.QueueToken
	if strings.TrimSpace(token) == "" {
		payload, err := json.Marshal(job)
		if err != nil {
			return err
		}
		token = string(payload)
	}
	removed, err := q.client.LRem(ctx, q.processingKey, 1, token).Result()
	if err != nil {
		return err
	}
	if removed == 0 {
		return fmt.Errorf("generation job ack token not found")
	}
	return nil
}

func (q *RedisGenerationQueue) RequeueInflightGenerationJobs(ctx context.Context) (int, error) {
	count := 0
	for {
		payload, err := q.client.RPopLPush(ctx, q.processingKey, q.listKey).Result()
		if err == redis.Nil {
			return count, nil
		}
		if err != nil {
			return count, err
		}
		if strings.TrimSpace(payload) != "" {
			count++
		}
	}
}
