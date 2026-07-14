// Package bootstrap 是 Business Worker 的 Composition Root 和生命周期 Owner。
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/FigoGoo/Dora-Agent/worker/internal/config"
	"github.com/FigoGoo/Dora-Agent/worker/internal/etcdcheck"
	"github.com/FigoGoo/Dora-Agent/worker/internal/health"
	"github.com/FigoGoo/Dora-Agent/worker/internal/httpserver"
	"github.com/FigoGoo/Dora-Agent/worker/internal/postgres"
	redisadapter "github.com/FigoGoo/Dora-Agent/worker/internal/redis"
)

// BuildInfo 保存构建阶段注入且允许进入普通日志的版本信息。
type BuildInfo struct {
	// Version 是 Worker 发布版本。
	Version string
	// Commit 是源码 Commit SHA。
	Commit string
	// BuildTime 是 UTC 构建时间。
	BuildTime string
}

// Run 加载配置、验证依赖、启动健康检查，并在退出时按顺序释放资源。
func Run(ctx context.Context, build BuildInfo) error {
	cfg, err := config.Load(build.Version)
	if err != nil {
		return fmt.Errorf("load worker config: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With(
		"service", cfg.Service.Name, "version", cfg.Service.Version, "env", cfg.Service.Environment,
		"instance_id", cfg.Service.InstanceID, "commit", build.Commit, "build_time", build.BuildTime,
	)
	slog.SetDefault(logger)
	postgresClient, err := postgres.Open(ctx, cfg.PostgreSQL)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := postgresClient.Close(); closeErr != nil {
			logger.Error("关闭 PostgreSQL 失败", "error", closeErr)
		}
	}()
	if err := postgresClient.VerifySchema(ctx, cfg.PostgreSQL.PingTimeout); err != nil {
		return err
	}
	redisClient, err := redisadapter.Open(ctx, cfg.Redis)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := redisClient.Close(); closeErr != nil {
			logger.Error("关闭 Redis 失败", "error", closeErr)
		}
	}()
	etcdClient, err := etcdcheck.Open(ctx, cfg.Etcd)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := etcdClient.Close(); closeErr != nil {
			logger.Error("关闭 etcd 失败", "error", closeErr)
		}
	}()
	state := health.NewState()
	server, err := httpserver.New(cfg.HTTP, cfg.Service, state)
	if err != nil {
		return err
	}
	listener, err := server.Listen()
	if err != nil {
		return err
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	state.SetReady(true)
	logger.Info("Business Worker 基础 Runtime 已就绪", "http_addr", cfg.HTTP.Address,
		"concurrency", cfg.Worker.Concurrency, "claim_batch_size", cfg.Worker.ClaimBatchSize)
	var runErr error
	select {
	case <-ctx.Done():
		logger.Info("Business Worker 收到退出信号")
	case err := <-serveErrors:
		runErr = err
	}
	state.SetReady(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = err
	}
	select {
	case err := <-serveErrors:
		if err != nil && runErr == nil {
			runErr = err
		}
	case <-shutdownCtx.Done():
		if runErr == nil {
			runErr = fmt.Errorf("wait worker http shutdown: %w", shutdownCtx.Err())
		}
	}
	return runErr
}
