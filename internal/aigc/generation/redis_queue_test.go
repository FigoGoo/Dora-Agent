package generation

import (
	"context"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestRedisQueueEnqueueDequeue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := aigcstorage.NewGenerationRedisClient(aigcconfig.LoadFromEnv())
	defer client.Close()
	if err := aigcstorage.PingRedis(ctx, client); err != nil {
		t.Skipf("local redis is not available: %v", err)
	}

	listKey := "test:aigc:generation_jobs:" + time.Now().Format("20060102150405.000000000")
	defer client.Del(context.Background(), listKey)
	queue := NewRedisQueue(client, listKey)

	want := QueuePayload{JobID: "job-1", IdempotencyKey: "idem-1"}
	if err := queue.Enqueue(ctx, want); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	got, ok, err := queue.Dequeue(ctx, time.Second)
	if err != nil {
		t.Fatalf("Dequeue() error = %v", err)
	}
	if !ok {
		t.Fatal("queue did not return payload")
	}
	if got.JobID != want.JobID || got.IdempotencyKey != want.IdempotencyKey || got.EnqueuedAt.IsZero() {
		t.Fatalf("payload = %#v", got)
	}
}
