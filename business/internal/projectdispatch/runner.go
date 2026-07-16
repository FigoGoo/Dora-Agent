// Package projectdispatch 管理 Project Session Outbox 派发循环及其进程生命周期。
package projectdispatch

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
)

// Dispatcher 是 Runner 消费的单命令派发边界。
type Dispatcher interface {
	// DispatchNext 领取并处理一个到期命令；没有工作时返回 project.ErrOutboxEmpty。
	DispatchNext(ctx context.Context) error
}

// Config 描述空闲或失败后的有界轮询间隔。
type Config struct {
	// PollInterval 是无工作或一次派发失败后再次查询的最短间隔。
	PollInterval time.Duration
}

// Runner 串行驱动单命令 Dispatcher；成功后立即继续排空，到空闲或错误时有界等待。
type Runner struct {
	dispatcher Dispatcher
	logger     *slog.Logger
	config     Config
}

// New 校验依赖和轮询间隔，避免无界忙循环。
func New(dispatcher Dispatcher, logger *slog.Logger, config Config) (*Runner, error) {
	if dispatcher == nil || logger == nil || config.PollInterval <= 0 {
		return nil, errors.New("create project dispatch runner: required dependency is missing")
	}
	return &Runner{dispatcher: dispatcher, logger: logger, config: config}, nil
}

// Run 阻塞运行直到 Context 取消；取消是正常生命周期结束，不作为运行错误返回。
func (runner *Runner) Run(ctx context.Context) error {
	for {
		err := runner.dispatcher.DispatchNext(ctx)
		if err == nil {
			continue
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if ctx.Err() != nil {
				return nil
			}
		}
		if !errors.Is(err, project.ErrOutboxEmpty) {
			// 不记录底层错误原文，避免数据库、RPC 地址或内容意外进入普通日志。
			runner.logger.Warn("Project Session Outbox 单次派发未完成", "error_class", dispatchErrorClass(err))
		}
		if !waitForNextPoll(ctx, runner.config.PollInterval) {
			return nil
		}
	}
}

// dispatchErrorClass 只投影稳定错误类别，不暴露底层错误链。
func dispatchErrorClass(err error) string {
	switch {
	case errors.Is(err, project.ErrOutboxLeaseLost):
		return "lease_lost"
	case errors.Is(err, project.ErrAgentSessionConflict):
		return "agent_conflict"
	case errors.Is(err, project.ErrAgentSessionInvalid), errors.Is(err, project.ErrInvalidAgentReceipt):
		return "agent_invalid"
	case errors.Is(err, project.ErrAgentSessionUnavailable):
		return "agent_unavailable"
	default:
		return "internal"
	}
}

// waitForNextPoll 使用可取消 Timer，避免退出时遗留轮询等待。
func waitForNextPoll(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
