package redis

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	redisclient "github.com/redis/go-redis/v9"
)

// TestClaimIdentityNonceRedisContract 使用显式测试 Redis 验证一百并发 SET NX 只有一次成功且 Key 不含原始 Nonce。
func TestClaimIdentityNonceRedisContract(t *testing.T) {
	address := os.Getenv("DORA_REDIS_CONTRACT_ADDR")
	if address == "" {
		t.Skip("未设置 DORA_REDIS_CONTRACT_ADDR，跳过真实 Redis Nonce 契约测试")
	}
	underlying := redisclient.NewClient(&redisclient.Options{Addr: address, DB: 1})
	client := &Client{client: underlying}
	t.Cleanup(func() { _ = underlying.Close() })
	kid := "contract-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	nonce := []byte("0123456789abcdef")
	var successes atomic.Int64
	var waitGroup sync.WaitGroup
	for range 100 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			claimed, err := client.ClaimIdentityNonce(context.Background(), kid, nonce, 2*time.Second)
			if err != nil {
				t.Errorf("Redis Claim 失败: %v", err)
				return
			}
			if claimed {
				successes.Add(1)
			}
		}()
	}
	waitGroup.Wait()
	if successes.Load() != 1 {
		t.Fatalf("Redis 并发成功数=%d，want 1", successes.Load())
	}
	keys, err := underlying.Keys(context.Background(), "dora:agent:http-identity:v1:"+kid+":*").Result()
	if err != nil || len(keys) != 1 {
		t.Fatalf("读取 Redis Contract Key=%v err=%v", keys, err)
	}
	if strings.Contains(keys[0], hex.EncodeToString(nonce)) {
		t.Fatalf("Redis Key 泄漏原始 Nonce: %s", keys[0])
	}
}

// TestClaimIdentityNonceFailsClosedOnRedisError 验证 Redis 不可达时返回依赖错误而不是假装成功或退化到本机状态。
func TestClaimIdentityNonceFailsClosedOnRedisError(t *testing.T) {
	underlying := redisclient.NewClient(&redisclient.Options{
		Addr: "127.0.0.1:1", MaxRetries: 0, DialTimeout: 10 * time.Millisecond,
		ReadTimeout: 10 * time.Millisecond, WriteTimeout: 10 * time.Millisecond,
	})
	defer underlying.Close()
	client := &Client{client: underlying}
	claimed, err := client.ClaimIdentityNonce(context.Background(), "contract-failure", []byte("0123456789abcdef"), time.Second)
	if err == nil || claimed || errors.Is(err, context.Canceled) {
		t.Fatalf("Redis 故障 claimed=%v err=%v", claimed, err)
	}
}
