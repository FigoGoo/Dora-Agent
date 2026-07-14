// Package bootstrap 是 Business Service 的 Composition Root 和生命周期 Owner。
package bootstrap

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"

	"github.com/FigoGoo/Dora-Agent/business/internal/agentidentity"
	"github.com/FigoGoo/Dora-Agent/business/internal/agentsessionrpc"
	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/clock"
	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/etcdregistry"
	"github.com/FigoGoo/Dora-Agent/business/internal/foundationrpc"
	"github.com/FigoGoo/Dora-Agent/business/internal/health"
	"github.com/FigoGoo/Dora-Agent/business/internal/httpserver"
	"github.com/FigoGoo/Dora-Agent/business/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/business/internal/postgres"
	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectdispatch"
	"github.com/FigoGoo/Dora-Agent/business/internal/promptcrypto"
	redisadapter "github.com/FigoGoo/Dora-Agent/business/internal/redis"
	"github.com/FigoGoo/Dora-Agent/business/internal/rpcserver"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1"
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

// Run 加载配置、验证依赖、启动 HTTP、注册 etcd，并在退出时按顺序释放资源。
func Run(ctx context.Context, build BuildInfo) error {
	cfg, err := config.Load(build.Version)
	if err != nil {
		return fmt.Errorf("load business config: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With(
		"service", cfg.Service.Name,
		"version", cfg.Service.Version,
		"env", cfg.Service.Environment,
		"instance_id", cfg.Service.InstanceID,
		"commit", build.Commit,
		"build_time", build.BuildTime,
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

	state := health.NewState()
	userRepository, err := postgres.NewUserRepository(postgresClient)
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create business user repository: %w", err)
	}
	authRepository, err := postgres.NewAuthRepository(postgresClient)
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create business auth repository: %w", err)
	}
	loginRateLimiter, err := redisadapter.NewLoginRateLimiter(
		redisClient,
		cfg.Auth.LoginRateLimitMaxAttempts,
		cfg.Auth.LoginRateLimitWindow,
		cfg.Auth.LoginRateLimitTimeout,
	)
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create business login rate limiter: %w", err)
	}
	authService, err := auth.NewService(
		userRepository,
		authRepository,
		clock.System{},
		idgen.UUIDv7{},
		rand.Reader,
		auth.Argon2idVerifier{},
		loginRateLimiter,
		auth.SessionConfig{
			IdleTTL: cfg.Auth.SessionIdleTTL, AbsoluteTTL: cfg.Auth.SessionAbsoluteTTL,
			CSRFSecret: cfg.Auth.CSRFSecret, MaxConcurrentSessions: cfg.Auth.MaxConcurrentSessions,
		},
	)
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create business auth service: %w", err)
	}
	authHandler, err := httpserver.NewAuthHandler(authService, cfg.Auth, idgen.UUIDv7{})
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create business auth HTTP handler: %w", err)
	}
	projectRepository, err := postgres.NewProjectRepository(postgresClient)
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create business project repository: %w", err)
	}
	promptProtector, err := promptcrypto.NewAESGCMProtectorWithSystemRandom(
		cfg.Project.PromptProtectionKey,
		cfg.Project.PromptProtectionKeyVersion,
	)
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create business project prompt protector: %w", err)
	}
	projectService, err := project.NewService(
		projectRepository,
		clock.System{},
		idgen.UUIDv7{},
		promptProtector,
		cfg.Project.MaxOutboxAttempts,
	)
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create business project service: %w", err)
	}
	projectHandler, err := httpserver.NewProjectHandler(projectService, idgen.UUIDv7{}, cfg.Project.MaxRequestBodyBytes)
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create business project HTTP handler: %w", err)
	}
	agentSessionAccessService, err := project.NewAgentSessionAccessService(projectRepository)
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create Agent Session access service: %w", err)
	}
	agentIdentitySigner, err := agentidentity.NewSigner(clock.System{}, rand.Reader, agentidentity.Config{
		KeyVersion: cfg.AgentHTTP.AssertionKeyVersion,
		Secret:     cfg.AgentHTTP.AssertionSecret,
		TTL:        cfg.AgentHTTP.AssertionTTL,
	})
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create Agent HTTP identity signer: %w", err)
	}
	agentHTTPClient, err := httpserver.NewAgentHTTPClient(cfg.AgentHTTP)
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create Agent HTTP client: %w", err)
	}
	agentProxyHandler, err := httpserver.NewAgentProxyHandler(
		agentSessionAccessService, agentIdentitySigner, idgen.UUIDv7{}, agentHTTPClient, cfg.AgentHTTP,
	)
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create Agent proxy HTTP handler: %w", err)
	}
	agentSessionClient, err := agentsessionrpc.NewClient(ctx, agentsessionrpc.ClientConfig{
		ConnectTimeout: cfg.AgentSessionRPC.ConnectTimeout,
		RequestTimeout: cfg.AgentSessionRPC.RequestTimeout,
		AuthSecret:     cfg.AgentSessionRPC.AuthSecret,
	}, cfg.Etcd)
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create Agent Session RPC client: %w", err)
	}
	defer func() {
		if closeErr := agentSessionClient.Close(); closeErr != nil {
			logger.Error("关闭 Agent Session RPC Client 失败", "error_class", "resolver_close_failed")
		}
	}()
	projectDispatcher, err := project.NewDispatcher(
		projectRepository,
		agentSessionClient,
		promptProtector,
		clock.System{},
		idgen.UUIDv7{},
		project.DispatcherConfig{
			LeaseOwner:    cfg.Service.InstanceID,
			LeaseDuration: cfg.ProjectDispatch.LeaseDuration,
			RetryDelay:    cfg.ProjectDispatch.RetryDelay,
		},
	)
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create project session dispatcher: %w", err)
	}
	dispatchRunner, err := projectdispatch.New(projectDispatcher, logger, projectdispatch.Config{
		PollInterval: cfg.ProjectDispatch.PollInterval,
	})
	if err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("create project session dispatch runner: %w", err)
	}
	server, err := httpserver.New(cfg.HTTP, cfg.Service, state, httpserver.RouteHandlers{
		Auth: authHandler, Project: projectHandler, Agent: agentProxyHandler,
	})
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	listener, err := server.Listen()
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	foundationHandler, err := foundationrpc.NewHandler(cfg.Service, clock.System{}, logger)
	if err != nil {
		_ = listener.Close()
		_ = registry.Close(ctx)
		return fmt.Errorf("create business foundation RPC handler: %w", err)
	}
	rpcServer, err := rpcserver.New(cfg.RPC, cfg.ShutdownTimeout, foundationHandler)
	if err != nil {
		_ = listener.Close()
		_ = registry.Close(ctx)
		return err
	}
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(listener)
	}()
	rpcServeErrors := make(chan error, 1)
	go func() {
		rpcServeErrors <- rpcServer.Serve()
	}()

	// HTTP 与 RPC Listener 都绑定后才用同一租约发布 Endpoint，避免发现半启动实例。
	if err := registry.Register(ctx, []etcdregistry.Endpoint{
		{
			Service: cfg.Service.Name, InstanceID: cfg.Service.InstanceID,
			Address: cfg.Service.AdvertisedAddress, Version: cfg.Service.Version,
		},
		{
			Service: foundationv1.BUSINESS_FOUNDATION_SERVICE_NAME, InstanceID: cfg.Service.InstanceID,
			Address: cfg.RPC.AdvertisedAddress, Version: cfg.Service.Version,
		},
	}, cfg.Etcd.LeaseTTL); err != nil {
		_ = listener.Close()
		_ = rpcServer.Stop()
		_ = registry.Close(ctx)
		return err
	}
	state.SetReady(true)
	dispatchCtx, cancelDispatch := context.WithCancel(ctx)
	dispatchErrors := make(chan error, 1)
	go func() {
		dispatchErrors <- dispatchRunner.Run(dispatchCtx)
	}()
	logger.Info("Business Service 已就绪",
		"http_addr", cfg.HTTP.Address,
		"advertised_address", cfg.Service.AdvertisedAddress,
		"foundation_rpc_addr", cfg.RPC.ListenAddress,
		"foundation_rpc_advertised_address", cfg.RPC.AdvertisedAddress,
	)

	var runErr error
	httpServeStopped := false
	rpcServeStopped := false
	dispatchStopped := false
	select {
	case <-ctx.Done():
		logger.Info("Business Service 收到退出信号")
	case err := <-serveErrors:
		runErr = err
		httpServeStopped = true
	case err := <-rpcServeErrors:
		runErr = err
		rpcServeStopped = true
	case err := <-dispatchErrors:
		dispatchStopped = true
		if err != nil {
			runErr = err
		} else if ctx.Err() == nil {
			runErr = fmt.Errorf("project session dispatch runner stopped unexpectedly")
		}
	case err := <-registry.Errors():
		runErr = err
	}

	// 先从发现和 Readiness 中摘除，再停止 HTTP，避免退出窗口继续接收新流量。
	state.SetReady(false)
	cancelDispatch()
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
				runErr = fmt.Errorf("wait business http shutdown: %w", shutdownCtx.Err())
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
				runErr = fmt.Errorf("wait business RPC shutdown: %w", shutdownCtx.Err())
			}
		}
	}
	if !dispatchStopped {
		select {
		case err := <-dispatchErrors:
			if err != nil && runErr == nil {
				runErr = err
			}
		case <-shutdownCtx.Done():
			if runErr == nil {
				runErr = fmt.Errorf("wait project session dispatch shutdown: %w", shutdownCtx.Err())
			}
		}
	}
	return runErr
}
