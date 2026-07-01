package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
	"github.com/redis/go-redis/v9"
)

const (
	defaultReplayLimit = 100
	defaultMaxLen      = 1000
	dedupeTTL          = 24 * time.Hour
)

var ErrLockNotOwned = errors.New("lock is not owned by caller")

type AGUIEventBus interface {
	PublishAGUI(ctx context.Context, event pr1.AGUIEnvelope) error
	ReplayAGUI(ctx context.Context, runID string, afterSeq int64, limit int) ([]pr1.AGUIEnvelope, error)
}

type SnapshotCache interface {
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Delete(ctx context.Context, key string) error
}

type TurnLock interface {
	Acquire(ctx context.Context, key string, owner string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, key string, owner string) (bool, error)
}

func RunEventsKey(runID string) string {
	return "agent:run:" + strings.TrimSpace(runID) + ":events"
}

func RunEventDedupeKey(runID string, dedupeKey string) string {
	return "agent:run:" + strings.TrimSpace(runID) + ":events:dedupe:" + strings.TrimSpace(dedupeKey)
}

func RunSnapshotKey(runID string) string {
	return "agent:run:" + strings.TrimSpace(runID) + ":snapshot"
}

func BoardSnapshotKey(boardID string, version int) string {
	return "agent:board:" + strings.TrimSpace(boardID) + ":snapshot:" + strconv.Itoa(version)
}

func TurnLockKey(runID string) string {
	return "lock:agent:run:" + strings.TrimSpace(runID) + ":turn"
}

type MemoryAGUIEventBus struct {
	mu     sync.RWMutex
	events map[string][]pr1.AGUIEnvelope
	seen   map[string]map[string]struct{}
}

func NewMemoryAGUIEventBus() *MemoryAGUIEventBus {
	return &MemoryAGUIEventBus{
		events: map[string][]pr1.AGUIEnvelope{},
		seen:   map[string]map[string]struct{}{},
	}
}

func (b *MemoryAGUIEventBus) PublishAGUI(ctx context.Context, event pr1.AGUIEnvelope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := pr1.ValidateAGUIEnvelope(event); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seen[event.RunID] == nil {
		b.seen[event.RunID] = map[string]struct{}{}
	}
	if _, ok := b.seen[event.RunID][event.DedupeKey]; ok {
		return nil
	}
	b.events[event.RunID] = append(b.events[event.RunID], event)
	b.seen[event.RunID][event.DedupeKey] = struct{}{}
	return nil
}

func (b *MemoryAGUIEventBus) ReplayAGUI(ctx context.Context, runID string, afterSeq int64, limit int) ([]pr1.AGUIEnvelope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit = normalizeReplayLimit(limit)
	b.mu.RLock()
	defer b.mu.RUnlock()
	events := append([]pr1.AGUIEnvelope(nil), b.events[strings.TrimSpace(runID)]...)
	sort.Slice(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
	out := make([]pr1.AGUIEnvelope, 0, min(limit, len(events)))
	for _, event := range events {
		if event.Seq <= afterSeq {
			continue
		}
		out = append(out, event)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

type RedisAGUIEventBus struct {
	client redis.Cmdable
	maxLen int64
}

func NewRedisAGUIEventBus(client redis.Cmdable, maxLen int64) (*RedisAGUIEventBus, error) {
	if client == nil {
		return nil, errors.New("redis client is required")
	}
	if maxLen <= 0 {
		maxLen = defaultMaxLen
	}
	return &RedisAGUIEventBus{client: client, maxLen: maxLen}, nil
}

func (b *RedisAGUIEventBus) PublishAGUI(ctx context.Context, event pr1.AGUIEnvelope) error {
	if err := pr1.ValidateAGUIEnvelope(event); err != nil {
		return err
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return b.client.Eval(ctx, `
if redis.call("EXISTS", KEYS[2]) == 1 then
  return 0
end
redis.call("XADD", KEYS[1], "MAXLEN", "~", ARGV[1], "*",
  "seq", ARGV[2],
  "event_id", ARGV[3],
  "event_type", ARGV[4],
  "dedupe_key", ARGV[5],
  "event", ARGV[6])
redis.call("SET", KEYS[2], "1", "EX", ARGV[7])
return 1
`,
		[]string{RunEventsKey(event.RunID), RunEventDedupeKey(event.RunID, event.DedupeKey)},
		strconv.FormatInt(b.maxLen, 10),
		strconv.FormatInt(event.Seq, 10),
		event.EventID,
		event.EventType,
		event.DedupeKey,
		string(body),
		strconv.FormatInt(int64(dedupeTTL/time.Second), 10),
	).Err()
}

func (b *RedisAGUIEventBus) ReplayAGUI(ctx context.Context, runID string, afterSeq int64, limit int) ([]pr1.AGUIEnvelope, error) {
	limit = normalizeReplayLimit(limit)
	rows, err := b.client.XRange(ctx, RunEventsKey(runID), "-", "+").Result()
	if err != nil {
		return nil, err
	}
	events := make([]pr1.AGUIEnvelope, 0, min(limit, len(rows)))
	for _, row := range rows {
		raw, ok := row.Values["event"].(string)
		if !ok {
			return nil, fmt.Errorf("redis event %s missing event payload", row.ID)
		}
		var event pr1.AGUIEnvelope
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		if err := pr1.ValidateAGUIEnvelope(event); err != nil {
			return nil, err
		}
		if event.Seq <= afterSeq {
			continue
		}
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

type MemorySnapshotCache struct {
	mu    sync.RWMutex
	items map[string]cacheItem
	now   func() time.Time
}

type cacheItem struct {
	value     []byte
	expiresAt time.Time
}

func NewMemorySnapshotCache() *MemorySnapshotCache {
	return &MemorySnapshotCache{items: map[string]cacheItem{}, now: func() time.Time { return time.Now().UTC() }}
}

func (c *MemorySnapshotCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("cache key is required")
	}
	if ttl <= 0 {
		return errors.New("cache ttl must be positive")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = cacheItem{value: append([]byte(nil), value...), expiresAt: c.now().Add(ttl)}
	return nil
}

func (c *MemorySnapshotCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	c.mu.RLock()
	item, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	if !item.expiresAt.After(c.now()) {
		_ = c.Delete(ctx, key)
		return nil, false, nil
	}
	return append([]byte(nil), item.value...), true, nil
}

func (c *MemorySnapshotCache) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
	return nil
}

type RedisSnapshotCache struct {
	client redis.Cmdable
}

func NewRedisSnapshotCache(client redis.Cmdable) (*RedisSnapshotCache, error) {
	if client == nil {
		return nil, errors.New("redis client is required")
	}
	return &RedisSnapshotCache{client: client}, nil
}

func (c *RedisSnapshotCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("cache ttl must be positive")
	}
	return c.client.Set(ctx, key, value, ttl).Err()
}

func (c *RedisSnapshotCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

func (c *RedisSnapshotCache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

type MemoryTurnLock struct {
	mu    sync.Mutex
	locks map[string]lockItem
	now   func() time.Time
}

type lockItem struct {
	owner     string
	expiresAt time.Time
}

func NewMemoryTurnLock() *MemoryTurnLock {
	return &MemoryTurnLock{locks: map[string]lockItem{}, now: func() time.Time { return time.Now().UTC() }}
}

func (l *MemoryTurnLock) Acquire(ctx context.Context, key string, owner string, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if strings.TrimSpace(key) == "" || strings.TrimSpace(owner) == "" {
		return false, errors.New("lock key and owner are required")
	}
	if ttl <= 0 {
		return false, errors.New("lock ttl must be positive")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if existing, ok := l.locks[key]; ok && existing.expiresAt.After(now) {
		return false, nil
	}
	l.locks[key] = lockItem{owner: owner, expiresAt: now.Add(ttl)}
	return true, nil
}

func (l *MemoryTurnLock) Release(ctx context.Context, key string, owner string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	existing, ok := l.locks[key]
	if !ok || !existing.expiresAt.After(l.now()) {
		delete(l.locks, key)
		return false, nil
	}
	if existing.owner != owner {
		return false, ErrLockNotOwned
	}
	delete(l.locks, key)
	return true, nil
}

type RedisTurnLock struct {
	client redis.Cmdable
}

func NewRedisTurnLock(client redis.Cmdable) (*RedisTurnLock, error) {
	if client == nil {
		return nil, errors.New("redis client is required")
	}
	return &RedisTurnLock{client: client}, nil
}

func (l *RedisTurnLock) Acquire(ctx context.Context, key string, owner string, ttl time.Duration) (bool, error) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(owner) == "" {
		return false, errors.New("lock key and owner are required")
	}
	if ttl <= 0 {
		return false, errors.New("lock ttl must be positive")
	}
	return l.client.SetNX(ctx, key, owner, ttl).Result()
}

func (l *RedisTurnLock) Release(ctx context.Context, key string, owner string) (bool, error) {
	result, err := l.client.Eval(ctx, `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) else return -1 end`, []string{key}, owner).Int()
	if err != nil {
		return false, err
	}
	if result == -1 {
		return false, ErrLockNotOwned
	}
	return result == 1, nil
}

func normalizeReplayLimit(limit int) int {
	if limit <= 0 {
		return defaultReplayLimit
	}
	if limit > defaultMaxLen {
		return defaultMaxLen
	}
	return limit
}
