// Command business-service 启动 Dora Business Service 生产 Runtime。
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/FigoGoo/Dora-Agent/business/internal/bootstrap"
)

var (
	// version 由构建参数注入，标识发布版本。
	version = "dev"
	// commit 由构建参数注入，标识源码 Commit。
	commit = "unknown"
	// buildTime 由构建参数注入，标识 UTC 构建时间。
	buildTime = "unknown"
)

// main 只负责进程信号和调用 Composition Root，不包含业务逻辑。
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := bootstrap.Run(ctx, bootstrap.BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildTime: buildTime,
	}); err != nil {
		slog.Error("Business Service 退出", "error", err)
		os.Exit(1)
	}
}
