// Command agent-service 启动 Dora Agent Service 生产 Runtime。
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/FigoGoo/Dora-Agent/agent/internal/bootstrap"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

// main 只负责进程信号和调用 Composition Root，不包含 Agent 或 Graph 业务逻辑。
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bootstrap.Run(ctx, bootstrap.BuildInfo{Version: version, Commit: commit, BuildTime: buildTime}); err != nil {
		slog.Error("Agent Service 退出", "error", err)
		os.Exit(1)
	}
}
