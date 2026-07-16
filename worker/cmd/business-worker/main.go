// Command business-worker 启动 Dora Business Worker 生产 Runtime。
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/FigoGoo/Dora-Agent/worker/internal/bootstrap"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

// main 只负责进程信号和调用 Composition Root，不包含 Job 执行业务逻辑。
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bootstrap.Run(ctx, bootstrap.BuildInfo{Version: version, Commit: commit, BuildTime: buildTime}); err != nil {
		slog.Error("Business Worker 退出", "error", err)
		os.Exit(1)
	}
}
