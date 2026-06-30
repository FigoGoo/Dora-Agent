package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	agenthttp "github.com/FigoGoo/Dora-Agent/services/agent/internal/api/http"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/config"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/queue"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
	agentrpc "github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/rpc"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/observability"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/modeltool"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load agent config: %v", err)
	}

	logger := observability.NewLogger(os.Stdout, "agent", cfg.AppEnv, cfg.LogLevel)
	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{})
	if err != nil {
		logger.Error("open agent database", "error", err)
		os.Exit(1)
	}

	sqlDB, err := db.DB()
	if err != nil {
		logger.Error("get agent sql database", "error", err)
		os.Exit(1)
	}
	defer sqlDB.Close()
	gateway, err := agentrpc.NewBusinessGateway(cfg)
	if err != nil {
		logger.Error("create business rpc gateway", "error", err)
		os.Exit(1)
	}
	app := workbench.New(repository.New(db), gateway, cfg.DefaultConfigVersion)
	if strings.TrimSpace(cfg.DeepSeekAPIKey) == "" {
		if cfg.AppEnv == "local" || cfg.AppEnv == "dev" {
			app.SetModelAdapter(modeltool.LocalAdapter{})
			logger.Info("agent_model_adapter_enabled", "provider", "local", "reason", "DEEPSEEK_API_KEY empty")
		} else {
			logger.Error("deepseek api key is required", "env", "DEEPSEEK_API_KEY")
			os.Exit(1)
		}
	} else {
		app.SetModelAdapter(modeltool.DeepSeekAdapter{
			BaseURL:   cfg.DeepSeekBaseURL,
			APIKey:    cfg.DeepSeekAPIKey,
			Model:     cfg.DeepSeekModel,
			MaxTokens: cfg.DeepSeekMaxTokens,
		})
		logger.Info("agent_model_adapter_enabled", "provider", "deepseek", "model", cfg.DeepSeekModel, "base_url", cfg.DeepSeekBaseURL)
	}
	var generationQueue *queue.RedisGenerationQueue
	if cfg.GenerationQueue == "redis" {
		generationQueue, err = queue.NewRedisGenerationQueue(queue.RedisGenerationQueueConfig{
			Address: cfg.GenerationRedisAddress, Password: cfg.GenerationRedisPassword,
			DB: cfg.GenerationRedisDB, ListKey: cfg.GenerationRedisListKey,
		})
		if err != nil {
			logger.Error("create generation redis queue", "error", err)
			os.Exit(1)
		}
		if err := generationQueue.Ping(context.Background()); err != nil {
			logger.Error("ping generation redis queue", "error", err)
			os.Exit(1)
		}
		defer generationQueue.Close()
		app.SetGenerationQueue(generationQueue)
		if requeued, err := generationQueue.RequeueInflightGenerationJobs(context.Background()); err != nil {
			logger.Error("requeue inflight generation jobs", "error", err)
			os.Exit(1)
		} else if requeued > 0 {
			logger.Info("agent_generation_inflight_requeued", "count", requeued)
		}
		logger.Info("agent_generation_queue_enabled", "queue", cfg.GenerationQueue, "redis_addr", cfg.GenerationRedisAddress, "workers", cfg.GenerationWorkers)
	}
	if result, err := app.RecoverGenerationTasks(context.Background(), cfg.GenerationRecoveryAge, 100, "startup-recovery"); err != nil {
		logger.Error("agent_generation_recovery_failed", "error", err)
	} else if result.Scanned > 0 {
		logger.Info("agent_generation_recovery_complete", "scanned", result.Scanned, "released", result.Released, "reconcile", result.Reconcile, "release_fails", result.ReleaseFails)
	}

	router := agenthttp.NewRouter(agenthttp.RouterOptions{
		Logger: logger,
		Ready: func(ctx context.Context) error {
			return sqlDB.PingContext(ctx)
		},
		App: app,
	})
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("agent_http_starting", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("agent_http_stopped", "error", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if generationQueue != nil {
		for i := 0; i < cfg.GenerationWorkers; i++ {
			workerID := i + 1
			go func() {
				logger.Info("agent_generation_worker_starting", "worker_id", workerID)
				result := app.RunGenerationWorker(ctx, 0)
				if result.Failed > 0 || result.LastError != nil {
					logger.Error("agent_generation_worker_stopped", "worker_id", workerID, "processed", result.Processed, "failed", result.Failed, "error", result.LastError)
					return
				}
				logger.Info("agent_generation_worker_stopped", "worker_id", workerID, "processed", result.Processed, "failed", result.Failed)
			}()
		}
	}
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("agent_http_shutdown_failed", "error", err)
		os.Exit(1)
	}
	logger.Info("agent_http_shutdown_complete")
}
