// Package mvpruntime 为本地统一 Profile 提供单 Scanner owner，不持有或推断任何业务状态。
package mvpruntime

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Handler 是 Coordinator 消费的 source-specific 单步 Processor 端口。
type Handler interface {
	// ProcessNext 尝试一次全来源 HOL Claim；命中后同步执行既有 source 状态机。
	ProcessNext(context.Context) (bool, error)
}

// Config 冻结统一 Scanner 的共享并发和 PostgreSQL 轮询周期。
type Config struct {
	// Concurrency 是 Coordinator 固定 worker 数。
	Concurrency int
	// PollInterval 是 Wake 丢失后的真源恢复周期。
	PollInterval time.Duration
}

// Coordinator 以轮转方式驱动基础五个或媒体扩展八个 source-specific Processor，并统一生命周期。
type Coordinator struct {
	handlers []Handler
	config   Config
	wake     chan struct{}

	mu           sync.Mutex
	started      bool
	intakeCancel context.CancelFunc
	runCancel    context.CancelFunc
	workers      sync.WaitGroup

	cursorMu sync.Mutex
	cursor   int
}

// NewCoordinator 创建未启动的单 Scanner owner；Handler 必须按设计固定顺序提供五个或八个。
func NewCoordinator(handlers []Handler, config Config) (*Coordinator, error) {
	if (len(handlers) != 5 && len(handlers) != 8) || config.Concurrency < 1 || config.Concurrency > 32 || config.PollInterval <= 0 {
		return nil, fmt.Errorf("create mvp runtime coordinator: invalid handlers or resource budget")
	}
	cloned := make([]Handler, len(handlers))
	for index, handler := range handlers {
		if handler == nil {
			return nil, fmt.Errorf("create mvp runtime coordinator: handler %d is nil", index)
		}
		cloned[index] = handler
	}
	return &Coordinator{handlers: cloned, config: config, wake: make(chan struct{}, 1)}, nil
}

// Start 启动唯一一组共享 Scanner worker；重复启动失败关闭。
func (c *Coordinator) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return fmt.Errorf("start mvp runtime coordinator: already started")
	}
	intakeCtx, intakeCancel := context.WithCancel(context.Background())
	runCtx, runCancel := context.WithCancel(context.Background())
	c.started = true
	c.intakeCancel = intakeCancel
	c.runCancel = runCancel
	for index := 0; index < c.config.Concurrency; index++ {
		c.workers.Add(1)
		go c.worker(intakeCtx, runCtx)
	}
	return nil
}

// Wake 合并重复通知；即使通知丢失，PollInterval 仍会重新扫描 PostgreSQL。
func (c *Coordinator) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Stop 先关闭 intake，等待当前 ProcessNext 完整 Drain；调用方 deadline 到期才取消共享 run context。
func (c *Coordinator) Stop(ctx context.Context) error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
	}
	c.started = false
	intakeCancel, runCancel := c.intakeCancel, c.runCancel
	c.mu.Unlock()
	intakeCancel()
	done := make(chan struct{})
	go func() {
		c.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		runCancel()
		return nil
	case <-ctx.Done():
		runCancel()
		return fmt.Errorf("stop mvp runtime coordinator: %w", ctx.Err())
	}
}

func (c *Coordinator) worker(intakeCtx context.Context, runCtx context.Context) {
	defer c.workers.Done()
	// 启动即扫描一次已有 backlog，不能依赖新 Wake 才恢复崩溃前的 PostgreSQL 输入。
	c.scan(intakeCtx, runCtx)
	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-intakeCtx.Done():
			return
		case <-ticker.C:
		case <-c.wake:
		}
		c.scan(intakeCtx, runCtx)
	}
}

// scan 从共享游标开始轮转；命中工作后继续，直到完整一轮均为空或任一 Handler 返回错误。
func (c *Coordinator) scan(intakeCtx context.Context, runCtx context.Context) {
	emptyHandlers := 0
	for intakeCtx.Err() == nil && emptyHandlers < len(c.handlers) {
		index := c.nextHandlerIndex()
		claimed, err := c.handlers[index].ProcessNext(runCtx)
		if err != nil {
			// Handler 错误只终止当前扫描轮，后续 Poll/Wake 仍从 PostgreSQL 真源恢复。
			return
		}
		if claimed {
			emptyHandlers = 0
			continue
		}
		emptyHandlers++
	}
}

// nextHandlerIndex 只保护低成本轮转游标，不在锁内执行 Claim、Runner 或任何外部调用。
func (c *Coordinator) nextHandlerIndex() int {
	c.cursorMu.Lock()
	defer c.cursorMu.Unlock()
	index := c.cursor
	c.cursor = (c.cursor + 1) % len(c.handlers)
	return index
}
