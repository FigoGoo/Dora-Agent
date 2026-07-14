package redis

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	redisclient "github.com/redis/go-redis/v9"
)

const loginRateLimitKeyPrefix = "dora:business:auth:login:v1:"

const incrementLoginWindowScript = `
local current = redis.call('INCR', KEYS[1])
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return current`

var errLoginRateLimiterUnavailable = errors.New("login rate limiter unavailable")

// loginRateLimitStore 是限流器使用的最小 go-redis 命令边界，便于验证键名与失败关闭语义。
type loginRateLimitStore interface {
	// Eval 原子执行计数窗口脚本。
	Eval(ctx context.Context, script string, keys []string, args ...any) *redisclient.Cmd
	// Del 在密码确认成功后清理当前身份摘要的失败窗口。
	Del(ctx context.Context, keys ...string) *redisclient.IntCmd
}

// LoginRateLimiter 使用 Redis 原子窗口计数实现登录防爆破策略；Key 只包含不可逆 SHA-256 摘要。
type LoginRateLimiter struct {
	store       loginRateLimitStore
	maxAttempts int64
	window      time.Duration
	timeout     time.Duration
}

var _ auth.LoginRateLimiter = (*LoginRateLimiter)(nil)

// NewLoginRateLimiter 从 Business Redis Client 创建版本化登录限流器。
func NewLoginRateLimiter(client *Client, maxAttempts int, window time.Duration, timeout time.Duration) (*LoginRateLimiter, error) {
	if client == nil || client.client == nil {
		return nil, fmt.Errorf("create login rate limiter: redis client is nil")
	}
	return newLoginRateLimiter(client.client, maxAttempts, window, timeout)
}

// newLoginRateLimiter 校验策略参数并创建可测试的限流器实现。
func newLoginRateLimiter(store loginRateLimitStore, maxAttempts int, window time.Duration, timeout time.Duration) (*LoginRateLimiter, error) {
	if store == nil || maxAttempts < 1 || maxAttempts > 1000 || window < time.Second || window > 24*time.Hour || timeout <= 0 || timeout > 5*time.Second {
		return nil, fmt.Errorf("create login rate limiter: invalid dependency or config")
	}
	return &LoginRateLimiter{store: store, maxAttempts: int64(maxAttempts), window: window, timeout: timeout}, nil
}

// Allow 原子增加身份摘要当前窗口计数；Redis 异常时失败关闭，避免绕过安全策略。
func (limiter *LoginRateLimiter) Allow(ctx context.Context, subjectDigest auth.Digest) (bool, error) {
	if subjectDigest == (auth.Digest{}) {
		return false, errLoginRateLimiterUnavailable
	}
	operationCtx, cancel := context.WithTimeout(ctx, limiter.timeout)
	defer cancel()
	count, err := limiter.store.Eval(
		operationCtx,
		incrementLoginWindowScript,
		[]string{loginRateLimitKey(subjectDigest)},
		limiter.window.Milliseconds(),
	).Int64()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, err
		}
		return false, errLoginRateLimiterUnavailable
	}
	if count < 1 {
		return false, errLoginRateLimiterUnavailable
	}
	return count <= limiter.maxAttempts, nil
}

// Reset 删除密码已确认身份的当前失败窗口；异常时失败关闭，不继续创建会话。
func (limiter *LoginRateLimiter) Reset(ctx context.Context, subjectDigest auth.Digest) error {
	if subjectDigest == (auth.Digest{}) {
		return errLoginRateLimiterUnavailable
	}
	operationCtx, cancel := context.WithTimeout(ctx, limiter.timeout)
	defer cancel()
	if err := limiter.store.Del(operationCtx, loginRateLimitKey(subjectDigest)).Err(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return errLoginRateLimiterUnavailable
	}
	return nil
}

// loginRateLimitKey 仅编码固定长度 SHA-256 摘要，不允许邮箱或其他用户输入进入 Redis Key。
func loginRateLimitKey(subjectDigest auth.Digest) string {
	return loginRateLimitKeyPrefix + hex.EncodeToString(subjectDigest[:])
}
