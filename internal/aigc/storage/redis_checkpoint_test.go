package storage

import (
	"bytes"
	"context"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
)

func TestRedisCheckpointStorePersistsBytes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := NewRuntimeRedisClient(aigcconfig.LoadFromEnv())
	defer client.Close()
	if err := PingRedis(ctx, client); err != nil {
		t.Skipf("local redis is not available: %v", err)
	}

	store := NewRedisCheckpointStore(client, "test:aigc:checkpoint:")
	checkpointID := "checkpoint-store-test"
	want := []byte(`{"state":"paused"}`)

	if err := store.Set(ctx, checkpointID, want); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	got, ok, err := store.Get(ctx, checkpointID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("checkpoint was not found")
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("checkpoint bytes = %q", got)
	}
}
