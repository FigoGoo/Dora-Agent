package storage

import (
	"context"
	"testing"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
)

func TestOpenPostgresRequiresDSN(t *testing.T) {
	if _, err := OpenPostgres(context.Background(), " "); err == nil {
		t.Fatalf("expected missing dsn error")
	}
}

func TestNewRedisClientUsesOptions(t *testing.T) {
	client := NewRedisClient(aigcconfig.RedisConfig{
		Addr:     "127.0.0.1:6379",
		Password: "local-password",
		DB:       2,
	})
	defer client.Close()

	opts := client.Options()
	if opts.Addr != "127.0.0.1:6379" || opts.Password != "local-password" || opts.DB != 2 {
		t.Fatalf("unexpected redis options: %#v", opts)
	}
}

func TestRuntimeRedisClientUsesNormalizedDefaults(t *testing.T) {
	client := NewRuntimeRedisClient(aigcconfig.Config{})
	defer client.Close()

	opts := client.Options()
	if opts.Addr != aigcconfig.DefaultAgentRuntimeRedisAddr || opts.DB != aigcconfig.DefaultAgentRuntimeRedisDB {
		t.Fatalf("unexpected runtime redis defaults: %#v", opts)
	}
}
