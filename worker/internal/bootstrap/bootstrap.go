// Package bootstrap 是 Business Worker 的 Composition Root 和生命周期 Owner。
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/FigoGoo/Dora-Agent/worker/internal/agentconsumer"
	"github.com/FigoGoo/Dora-Agent/worker/internal/businessmedia"
	"github.com/FigoGoo/Dora-Agent/worker/internal/clock"
	"github.com/FigoGoo/Dora-Agent/worker/internal/config"
	"github.com/FigoGoo/Dora-Agent/worker/internal/etcdcheck"
	"github.com/FigoGoo/Dora-Agent/worker/internal/health"
	"github.com/FigoGoo/Dora-Agent/worker/internal/httpserver"
	"github.com/FigoGoo/Dora-Agent/worker/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/worker/internal/mediajob"
	"github.com/FigoGoo/Dora-Agent/worker/internal/mediapreview"
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
	var mediaProcessor *mediajob.Processor
	if cfg.MediaRuntime.Enabled() {
		artifactEngine, err := mediapreview.NewEngine(ctx, mediapreview.Config{
			Profile: mediapreview.RuntimeProfileMediaV3Preview1, ObjectRoot: cfg.MediaRuntime.ObjectRoot,
			GeneratorVersion: mediapreview.GeneratorVersionPNG640x360V1,
			FFMPEGPath:       cfg.MediaRuntime.FFMPEGPath, FFprobePath: cfg.MediaRuntime.FFprobePath,
			StderrLimitBytes: cfg.MediaRuntime.StderrLimitBytes,
		})
		if err != nil {
			return fmt.Errorf("create media preview artifact engine: %w", err)
		}
		defer func() {
			if closeErr := artifactEngine.Close(); closeErr != nil {
				logger.Error("关闭媒体 Preview 产物引擎失败", "error_code", "INTERNAL")
			}
		}()
		mediaRepository, err := postgres.NewMediaJobRepository(postgresClient)
		if err != nil {
			return err
		}
		agentClient, err := agentconsumer.Open(ctx, cfg.MediaRuntime)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := agentClient.Close(); closeErr != nil {
				logger.Error("关闭 Agent Media Consumer 失败", "error_code", "INTERNAL")
			}
		}()
		businessClient, err := businessmedia.New(cfg.MediaRuntime)
		if err != nil {
			return err
		}
		defer businessClient.Close()
		mediaProcessor, err = mediajob.NewProcessor(mediajob.ProcessorConfig{
			WorkerID: cfg.Service.InstanceID, PollInterval: cfg.Worker.PollInterval,
			LeaseTTL: cfg.Worker.LeaseTTL, HeartbeatInterval: cfg.Worker.HeartbeatInterval,
			AttemptTimeout: cfg.Worker.AttemptTimeout, MaxAttempts: cfg.Worker.MaxAttempts,
			AgentCallTimeout:    cfg.MediaRuntime.AgentCallTimeout,
			BusinessCallTimeout: cfg.MediaRuntime.BusinessCallTimeout,
			MaxPNGBytes:         cfg.MediaRuntime.MaxPNGBytes, MaxMP4Bytes: cfg.MediaRuntime.MaxMP4Bytes,
			RetryBaseDelay: cfg.MediaRuntime.RetryBaseDelay, RetryMaxDelay: cfg.MediaRuntime.RetryMaxDelay,
		}, mediaRepository, agentClient, businessClient, artifactEngine, idgen.UUIDv7{}, clock.System{}, nil, logger)
		if err != nil {
			return err
		}
		if err := mediaProcessor.Readiness(ctx); err != nil {
			return err
		}
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
	if mediaProcessor != nil {
		if err := mediaProcessor.Start(); err != nil {
			_ = listener.Close()
			return err
		}
	}
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
	if mediaProcessor != nil {
		if err := mediaProcessor.Stop(shutdownCtx); err != nil && runErr == nil {
			runErr = err
		}
	}
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
