package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/joho/godotenv"

	aigca2ui "github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	aigcagent "github.com/FigoGoo/Dora-Agent/internal/aigc/agent"
	aigcasset "github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcgeneration "github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	aigcgenerationhandlers "github.com/FigoGoo/Dora-Agent/internal/aigc/generation/handlers"
	aigcmediagraph "github.com/FigoGoo/Dora-Agent/internal/aigc/mediagraph"
	aigcserver "github.com/FigoGoo/Dora-Agent/internal/aigc/server"
	aigcsession "github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	aigcskill "github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
	aigcspec "github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
	aigcstoryboard "github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
	aigcturncontext "github.com/FigoGoo/Dora-Agent/internal/aigc/turncontext"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	loadDotEnv()

	cfg := aigcconfig.LoadFromEnv().Normalize()
	db, err := aigcstorage.OpenAgentPostgres(ctx, cfg)
	if err != nil {
		slog.Error("open agent postgres", "error", err)
		os.Exit(1)
	}

	sessionStore := aigcsession.NewPostgresStore(db)
	if err := sessionStore.AutoMigrate(ctx); err != nil {
		slog.Error("migrate session tables", "error", err)
		os.Exit(1)
	}
	skillStore := aigcskill.NewPostgresSkillStore(db)
	if err := skillStore.AutoMigrate(ctx); err != nil {
		slog.Error("migrate skill table", "error", err)
		os.Exit(1)
	}
	specStore := aigcspec.NewPostgresStore(db)
	if err := specStore.AutoMigrate(ctx); err != nil {
		slog.Error("migrate final video spec table", "error", err)
		os.Exit(1)
	}
	storyboardStore := aigcstoryboard.NewPostgresStore(db)
	if err := storyboardStore.AutoMigrate(ctx); err != nil {
		slog.Error("migrate storyboard tables", "error", err)
		os.Exit(1)
	}
	assetStore := aigcasset.NewPostgresStore(db)
	if err := assetStore.AutoMigrate(ctx); err != nil {
		slog.Error("migrate asset table", "error", err)
		os.Exit(1)
	}
	generationStore := aigcgeneration.NewPostgresStore(db)
	if err := generationStore.AutoMigrate(ctx); err != nil {
		slog.Error("migrate generation job table", "error", err)
		os.Exit(1)
	}
	assetUploader, err := aigcasset.NewTOSUploader(cfg.Storage.TOS)
	if err != nil {
		slog.Error("create tos asset uploader", "error", err)
		os.Exit(1)
	}

	runtimeRedis := aigcstorage.NewRuntimeRedisClient(cfg)
	defer runtimeRedis.Close()
	if err := aigcstorage.PingRedis(ctx, runtimeRedis); err != nil {
		slog.Error("ping runtime redis", "error", err)
		os.Exit(1)
	}
	generationRedis := aigcstorage.NewGenerationRedisClient(cfg)
	defer generationRedis.Close()
	if err := aigcstorage.PingRedis(ctx, generationRedis); err != nil {
		slog.Error("ping generation redis", "error", err)
		os.Exit(1)
	}
	generationQueue := aigcgeneration.NewRedisQueue(generationRedis, cfg.Storage.GenerationRedis.ListKey)
	generationDispatcher := aigcgeneration.NewDispatcher(aigcgeneration.DispatcherConfig{
		Store: generationStore,
		Queue: generationQueue,
	})
	generationHandlers := map[string]aigcgeneration.JobHandler{}
	if cfg.Image2.APIKey != "" {
		generationHandlers[aigcgeneration.ProviderImage2] = aigcgenerationhandlers.NewImage2JobHandler(aigcgenerationhandlers.Image2JobHandlerConfig{
			APIKey:        cfg.Image2.APIKey,
			Assets:        assetStore,
			AssetUploader: assetUploader,
		})
	} else {
		// 无 image2 key → demo 模式：关键元素参考图用固定图片，让 job 状态机走通到 succeeded。
		generationHandlers[aigcgeneration.ProviderImage2] = aigcgenerationhandlers.NewDemoMediaJobHandler(aigcgenerationhandlers.DemoMediaJobHandlerConfig{
			Assets:   assetStore,
			Provider: aigcgeneration.ProviderImage2,
			Kind:     aigcasset.KindImage,
			MIMEType: "image/png",
			URLs: []string{
				"/works/doraigc-aigc-cultural-tourism.png",
				"/works/doraigc-aigc-product-hero.png",
				"/works/doraigc-aigc-ecommerce-ad.png",
				"/works/doraigc-aigc-mv-shotboard.png",
				"/works/mv-city-generated.png",
				"/works/product-band-generated.png",
			},
		})
	}
	if cfg.Seedance.APIKey != "" {
		generationHandlers[aigcgeneration.ProviderSeedance] = aigcgenerationhandlers.NewSeedanceJobHandler(aigcgenerationhandlers.SeedanceJobHandlerConfig{
			APIKey:        cfg.Seedance.APIKey,
			Assets:        assetStore,
			AssetUploader: assetUploader,
		})
	} else {
		// 无 seedance key → demo 模式：镜头视频用固定 mp4。
		generationHandlers[aigcgeneration.ProviderSeedance] = aigcgenerationhandlers.NewDemoMediaJobHandler(aigcgenerationhandlers.DemoMediaJobHandlerConfig{
			Assets:   assetStore,
			Provider: aigcgeneration.ProviderSeedance,
			Kind:     aigcasset.KindVideo,
			MIMEType: "video/mp4",
			URLs:     []string{"/demo/demo-shot.mp4"},
		})
	}
	// 旁白/音乐：demo 固定音频（旁白 + BGM 两条），让音轨也绑上真资产。
	generationHandlers[aigcgeneration.ProviderAudio] = aigcgenerationhandlers.NewDemoMediaJobHandler(aigcgenerationhandlers.DemoMediaJobHandlerConfig{
		Assets:   assetStore,
		Provider: aigcgeneration.ProviderAudio,
		Kind:     aigcasset.KindAudio,
		MIMEType: "audio/mpeg",
		URLs:     []string{"/demo/demo-narration.mp3", "/demo/demo-music.mp3"},
	})
	eventBroker := aigca2ui.NewMemoryBroker(64)
	mediaCheckpoints := aigcstorage.NewRedisCheckpointStore(runtimeRedis, "dora:aigc:media_graph_checkpoint:")
	runnerCheckpoints := aigcstorage.NewRedisCheckpointStore(runtimeRedis, "dora:aigc:runner_checkpoint:")
	mediaGraph, err := aigcmediagraph.NewGenerator(ctx, aigcmediagraph.Config{
		Checkpoints: mediaCheckpoints,
		Dispatcher:  generationDispatcher,
	})
	if err != nil {
		slog.Error("create media generator graph", "error", err)
		os.Exit(1)
	}
	turnContextBuilder := aigcturncontext.NewBuilder(aigcturncontext.Config{
		Skills:      skillStore,
		Specs:       specStore,
		Storyboards: storyboardStore,
	})

	runner, err := aigcagent.NewDeepSeekRunner(ctx, aigcagent.DeepSeekRunnerConfig{
		Runtime:           cfg,
		SkillBackend:      aigcskill.NewEinoBackend(skillStore),
		RunnerCheckpoints: runnerCheckpoints,
		MediaCheckpoints:  mediaCheckpoints,
		MediaDispatcher:   generationDispatcher,
		SpecStore:         specStore,
		StoryboardStore:   storyboardStore,
		AssetStore:        assetStore,
		AssetUploader:     assetUploader,
		ExtraHandlers:     []adk.ChatModelAgentMiddleware{aigcturncontext.NewMiddleware(turnContextBuilder)},
	})
	if err != nil {
		slog.Error("create aigc chatmodel agent", "error", err)
		os.Exit(1)
	}
	runnerInvoker := aigcserver.NewRunnerInvoker(runner)
	selectorModel, err := aigcagent.NewDeepSeekChatModel(ctx, cfg)
	if err != nil {
		slog.Error("build skill selector model", "error", err)
		os.Exit(1)
	}
	wakeupRunner := aigcserver.NewJobWakeupRunner(aigcserver.JobWakeupRunnerConfig{
		Store:         sessionStore,
		Invoker:       runnerInvoker,
		Events:        eventBroker,
		SessionValues: aigcturncontext.SessionValues,
	})
	generationWorker := aigcgeneration.NewWorker(aigcgeneration.WorkerConfig{
		Store:       generationStore,
		Queue:       generationQueue,
		Assets:      assetStore,
		Storyboards: storyboardStore,
		Events:      aigcserver.GenerationEventPublisher{Broker: eventBroker, Wakeup: wakeupRunner},
		Handlers:    generationHandlers,
		Concurrency: 4,
	})

	httpServer := &http.Server{
		Addr: cfg.Runtime.HTTPAddr,
		Handler: aigcserver.NewRouter(aigcserver.Config{
			Store:          sessionStore,
			Skills:         skillStore,
			Storyboards:    storyboardStore,
			Assets:         assetStore,
			GenerationJobs: generationStore,
			AssetUploader:  assetUploader,
			Events:         eventBroker,
			Checkpoints:    sessionStore,
			Invoker:        runnerInvoker,
			MediaGraph:     mediaGraph,
			SessionValues:  aigcturncontext.SessionValues,
			SkillSelector:  aigcskill.NewLLMSkillSelector(selectorModel),
			DefaultSkillID: "",
			Publisher:      eventBroker,
			Specs:          specStore,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := generationWorker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("generation worker stopped", "error", err)
			stop()
		}
	}()

	go func() {
		slog.Info("aigc agent server listening", "addr", cfg.Runtime.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("aigc agent server stopped", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown aigc agent server", "error", err)
		os.Exit(1)
	}
}

// loadDotEnv loads .env.local/.env into the process environment without
// overriding variables already set by the caller, then maps the legacy
// DEEPSEEK_API_KEY name onto DORA_DEEPSEEK_API_KEY when the latter is unset.
func loadDotEnv() {
	_ = godotenv.Load(".env.local", ".env")
	if strings.TrimSpace(os.Getenv(aigcconfig.EnvDeepSeekAPIKey)) == "" {
		if v := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")); v != "" {
			_ = os.Setenv(aigcconfig.EnvDeepSeekAPIKey, v)
		}
	}
}
