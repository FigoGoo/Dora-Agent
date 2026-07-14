// Package bootstrap 是 Agent Service 的 Composition Root 和生命周期 Owner。
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/FigoGoo/Dora-Agent/agent/internal/businessrpc"
	"github.com/FigoGoo/Dora-Agent/agent/internal/clock"
	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/contentcrypto"
	"github.com/FigoGoo/Dora-Agent/agent/internal/etcdregistry"
	"github.com/FigoGoo/Dora-Agent/agent/internal/health"
	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/FigoGoo/Dora-Agent/agent/internal/httpserver"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/postgres"
	redisadapter "github.com/FigoGoo/Dora-Agent/agent/internal/redis"
	"github.com/FigoGoo/Dora-Agent/agent/internal/rpcserver"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/sessionrpc"
	"github.com/FigoGoo/Dora-Agent/agent/internal/workspace"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/sessionv1"
)

// BuildInfo 保存构建阶段注入且允许进入普通日志的版本信息。
type BuildInfo struct {
	// Version 是服务发布版本。
	Version string
	// Commit 是源码 Commit SHA。
	Commit string
	// BuildTime 是 UTC 构建时间。
	BuildTime string
}

// Run 加载配置、验证依赖与 Business 探针，启动 HTTP/Session RPC、注册 etcd，并在退出时先摘除再释放资源。
func Run(ctx context.Context, build BuildInfo) error {
	cfg, err := config.Load(build.Version)
	if err != nil {
		return fmt.Errorf("load agent config: %w", err)
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
	registry, err := etcdregistry.Open(ctx, cfg.Etcd)
	if err != nil {
		return err
	}
	businessClient, err := businessrpc.NewClient(ctx, cfg.BusinessRPC, cfg.Etcd, cfg.Service)
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	defer func() {
		if closeErr := businessClient.Close(); closeErr != nil {
			logger.Error("关闭 Business RPC Resolver 失败", "error", closeErr)
		}
	}()
	startupProbeCtx, cancelStartupProbe := context.WithTimeout(ctx, cfg.BusinessRPC.StartupTimeout)
	probeReceipt, err := businessClient.WaitUntilReady(startupProbeCtx)
	cancelStartupProbe()
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	logger.Info("Business Foundation RPC 探针通过",
		"request_id", probeReceipt.RequestID,
		"business_service", probeReceipt.BusinessService,
		"business_version", probeReceipt.BusinessVersion,
		"business_instance_id", probeReceipt.BusinessInstanceID,
	)

	// 真实内容保护器、Repository、领域 Service 与 RPC Handler 只在 Composition Root 相遇；
	// 密钥不进入日志，生成 DTO 也不会扩散到 Session 领域或 PostgreSQL Adapter。
	contentProtector, err := contentcrypto.NewAES256GCMProtectorWithPrevious(
		cfg.ContentProtection.Key, cfg.ContentProtection.KeyVersion,
		cfg.ContentProtection.PreviousKey, cfg.ContentProtection.PreviousKeyVersion,
	)
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	sessionRepository, err := postgres.NewSessionRepository(postgresClient)
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	sessionService, err := session.NewService(sessionRepository, idgen.UUIDv7{}, clock.System{}, contentProtector)
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	sessionHandler, err := sessionrpc.NewHandler(sessionService)
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	workspaceRepository, err := postgres.NewWorkspaceRepository(postgresClient)
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	workspaceService, err := workspace.NewService(
		workspaceRepository,
		contentProtector,
		workspace.SnapshotLimits{
			MaxMessages: cfg.Workspace.MaxMessages,
			MaxInputs:   cfg.Workspace.MaxInputs,
		},
		cfg.SSE.MaxEventBytes,
	)
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	identityVerifier, err := httpidentity.NewVerifier(cfg.HTTPIdentity, redisClient, clock.System{})
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	streamLimiter, err := workspace.NewStreamLimiter(
		cfg.SSE.MaxConnections,
		cfg.SSE.MaxConnectionsPerUser,
		cfg.SSE.MaxConnectionsPerSession,
	)
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	workspaceHandler, err := httpserver.NewWorkspaceHandler(
		identityVerifier, workspaceService, streamLimiter, cfg.SSE, idgen.UUIDv7{}, clock.System{},
	)
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	state := health.NewState()
	server, err := httpserver.New(cfg.HTTP, cfg.Service, state, workspaceHandler)
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	listener, err := server.Listen()
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	rpcServer, err := rpcserver.New(cfg.RPC, cfg.SessionRPCAuth, cfg.ShutdownTimeout, sessionHandler)
	if err != nil {
		_ = listener.Close()
		_ = registry.Close(ctx)
		return err
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	rpcServeErrors := make(chan error, 1)
	go func() { rpcServeErrors <- rpcServer.Serve() }()

	// HTTP 与 Session RPC Listener 都绑定后再用同一租约注册，发现侧永远不会选到半启动实例。
	if err := registry.Register(ctx, []etcdregistry.Endpoint{
		{
			Service: cfg.Service.Name, InstanceID: cfg.Service.InstanceID,
			Address: cfg.Service.AdvertisedAddress, Version: cfg.Service.Version,
		},
		{
			Service: sessionv1.AGENT_SESSION_SERVICE_NAME, InstanceID: cfg.Service.InstanceID,
			Address: cfg.RPC.AdvertisedAddress, Version: cfg.Service.Version,
		},
	}, cfg.Etcd.LeaseTTL); err != nil {
		_ = listener.Close()
		_ = rpcServer.Stop()
		_ = registry.Close(ctx)
		return err
	}
	state.SetReady(true)
	logger.Info("Agent Service 已就绪",
		"http_addr", cfg.HTTP.Address,
		"advertised_address", cfg.Service.AdvertisedAddress,
		"session_rpc_addr", cfg.RPC.ListenAddress,
		"session_rpc_advertised_address", cfg.RPC.AdvertisedAddress,
	)
	var runErr error
	httpServeStopped := false
	rpcServeStopped := false
	select {
	case <-ctx.Done():
		logger.Info("Agent Service 收到退出信号")
	case err := <-serveErrors:
		runErr = err
		httpServeStopped = true
	case err := <-rpcServeErrors:
		runErr = err
		rpcServeStopped = true
	case err := <-registry.Errors():
		runErr = err
	}
	state.SetReady(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := registry.Close(shutdownCtx); err != nil && runErr == nil {
		runErr = err
	}
	if err := rpcServer.Stop(); err != nil && runErr == nil {
		runErr = err
	}
	if err := server.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = err
	}
	if !httpServeStopped {
		select {
		case err := <-serveErrors:
			if err != nil && runErr == nil {
				runErr = err
			}
		case <-shutdownCtx.Done():
			if runErr == nil {
				runErr = fmt.Errorf("wait agent http shutdown: %w", shutdownCtx.Err())
			}
		}
	}
	if !rpcServeStopped {
		select {
		case err := <-rpcServeErrors:
			if err != nil && runErr == nil {
				runErr = err
			}
		case <-shutdownCtx.Done():
			if runErr == nil {
				runErr = fmt.Errorf("wait Agent Session RPC shutdown: %w", shutdownCtx.Err())
			}
		}
	}
	return runErr
}
