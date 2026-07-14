package redis

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	redisclient "github.com/redis/go-redis/v9"
)

// loginRateLimitTestStore 捕获 Redis 命令，验证实现不暴露身份原文。
type loginRateLimitTestStore struct {
	evalCount int64
	evalErr   error
	delErr    error
	key       string
	windowMS  any
}

// Eval 返回预置窗口计数并捕获单个摘要 Key。
func (store *loginRateLimitTestStore) Eval(_ context.Context, _ string, keys []string, args ...any) *redisclient.Cmd {
	if len(keys) == 1 {
		store.key = keys[0]
	}
	if len(args) == 1 {
		store.windowMS = args[0]
	}
	return redisclient.NewCmdResult(store.evalCount, store.evalErr)
}

// Del 捕获成功登录后的窗口清理 Key。
func (store *loginRateLimitTestStore) Del(_ context.Context, keys ...string) *redisclient.IntCmd {
	if len(keys) == 1 {
		store.key = keys[0]
	}
	command := redisclient.NewIntCmd(context.Background())
	command.SetVal(1)
	command.SetErr(store.delErr)
	return command
}

func TestLoginRateLimiterUsesDigestOnlyKeyAndFixedWindow(t *testing.T) {
	store := &loginRateLimitTestStore{evalCount: 3}
	limiter, err := newLoginRateLimiter(store, 3, 15*time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	digest := auth.Digest{1, 2, 3}
	allowed, err := limiter.Allow(context.Background(), digest)
	if err != nil || !allowed {
		t.Fatalf("Allow() = %v, %v", allowed, err)
	}
	if len(store.key) != len(loginRateLimitKeyPrefix)+64 || !strings.HasPrefix(store.key, loginRateLimitKeyPrefix) || strings.Contains(store.key, "example.com") {
		t.Fatalf("Redis key is not a fixed digest key: %q", store.key)
	}
	if store.windowMS != int64((15 * time.Minute).Milliseconds()) {
		t.Fatalf("unexpected Redis window argument: %#v", store.windowMS)
	}

	store.evalCount = 4
	allowed, err = limiter.Allow(context.Background(), digest)
	if err != nil || allowed {
		t.Fatalf("over-limit Allow() = %v, %v", allowed, err)
	}
	if err := limiter.Reset(context.Background(), digest); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
}

func TestLoginRateLimiterFailsClosedWithoutLeakingRedisError(t *testing.T) {
	store := &loginRateLimitTestStore{evalErr: errors.New("redis password=secret")}
	limiter, err := newLoginRateLimiter(store, 3, time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := limiter.Allow(context.Background(), auth.Digest{1})
	if allowed || !errors.Is(err, errLoginRateLimiterUnavailable) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("Allow() leaked dependency error: allowed=%v err=%v", allowed, err)
	}
}
