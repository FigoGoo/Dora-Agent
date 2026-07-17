// Package bootstrap 是 Agent Service 的 Composition Root 和生命周期 Owner。
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/FigoGoo/Dora-Agent/agent/internal/analyzematerialsruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/businessmedia"
	"github.com/FigoGoo/Dora-Agent/agent/internal/businessrpc"
	previewchatmodel "github.com/FigoGoo/Dora-Agent/agent/internal/chatmodel"
	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodelagent"
	"github.com/FigoGoo/Dora-Agent/agent/internal/clock"
	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/contentcrypto"
	"github.com/FigoGoo/Dora-Agent/agent/internal/etcdregistry"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/FigoGoo/Dora-Agent/agent/internal/health"
	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/FigoGoo/Dora-Agent/agent/internal/httpserver"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreviewruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mvpruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/planstoryboardruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/postgres"
	redisadapter "github.com/FigoGoo/Dora-Agent/agent/internal/redis"
	"github.com/FigoGoo/Dora-Agent/agent/internal/rpcserver"
	previewruntime "github.com/FigoGoo/Dora-Agent/agent/internal/runtime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/sessionrpc"
	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
	"github.com/FigoGoo/Dora-Agent/agent/internal/tool"
	"github.com/FigoGoo/Dora-Agent/agent/internal/usermessageruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/workspace"
	"github.com/FigoGoo/Dora-Agent/agent/internal/writepromptsruntime"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/sessionv1"
	einotool "github.com/cloudwego/eino/components/tool"
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
	capabilities := cfg.EffectiveRuntimeCapabilities()
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
	if capabilities.PlanCreationSpec {
		if err := postgresClient.VerifyCreationSpecPreviewSchema(ctx, cfg.PostgreSQL.PingTimeout); err != nil {
			return err
		}
	}
	if capabilities.UserMessage {
		if err := postgresClient.VerifyUserMessageRuntimeSchema(ctx, cfg.PostgreSQL.PingTimeout); err != nil {
			return err
		}
	}
	if capabilities.AnalyzeMaterials {
		if err := postgresClient.VerifyAnalyzeMaterialsRuntimeSchema(ctx, cfg.PostgreSQL.PingTimeout); err != nil {
			return err
		}
	}
	if capabilities.PlanStoryboard {
		if err := postgresClient.VerifyPlanStoryboardRuntimeSchema(ctx, cfg.PostgreSQL.PingTimeout); err != nil {
			return err
		}
	}
	if capabilities.WritePrompts {
		if err := postgresClient.VerifyWritePromptsRuntimeSchema(ctx, cfg.PostgreSQL.PingTimeout); err != nil {
			return err
		}
	}
	if cfg.MediaRuntimeEnabled() {
		if err := postgresClient.VerifyMediaRuntimeSchema(ctx, cfg.PostgreSQL.PingTimeout); err != nil {
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
	registry, err := etcdregistry.Open(ctx, cfg.Etcd)
	if err != nil {
		return err
	}
	businessClient, err := businessrpc.NewClient(
		ctx, cfg.BusinessRPC, cfg.Etcd, cfg.Service, capabilities.PlanCreationSpec, capabilities.PlanStoryboard,
		capabilities.WritePrompts,
	)
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
		"plan_storyboard_runtime_enabled", probeReceipt.PlanStoryboardRuntimeEnabled,
		"plan_storyboard_runtime_profile", probeReceipt.PlanStoryboardRuntimeProfile,
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
	skillSnapshotProtector, err := contentcrypto.NewSkillSnapshotAES256GCMProtectorWithPrevious(
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
	if err := skill.ValidateProducerLimitsV1(cfg.SkillSnapshotLimits, cfg.SkillSnapshotLimits); err != nil {
		_ = registry.Close(ctx)
		return fmt.Errorf("validate Agent Skill Snapshot limits: %w", err)
	}
	var sessionService *session.Service
	if capabilities.UserMessage {
		profile := usermessageruntime.ApprovedSessionProfile()
		if profile.Profile != cfg.UserMessageRuntime.Profile {
			_ = registry.Close(ctx)
			return fmt.Errorf("configure user message runtime: profile does not match approved pins")
		}
		sessionService, err = session.NewServiceWithSkillSnapshotAndUserMessageRuntime(
			sessionRepository, idgen.UUIDv7{}, clock.System{}, contentProtector,
			skillSnapshotProtector, cfg.SkillSnapshotLimits, profile,
		)
	} else {
		sessionService, err = session.NewServiceWithSkillSnapshot(
			sessionRepository, idgen.UUIDv7{}, clock.System{}, contentProtector,
			skillSnapshotProtector, cfg.SkillSnapshotLimits,
		)
	}
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	var previewProcessor *previewruntime.Processor
	var previewService *previewruntime.Service
	var mvpCoordinator *mvpruntime.Coordinator
	var mediaService *mediapreviewruntime.Service
	var mediaBusinessClient *businessmedia.Client
	if cfg.PlanSpecPreviewEnabled {
		previewRepository, createErr := postgres.NewCreationSpecPreviewRepository(
			postgresClient, contentProtector, cfg.PlanSpecPreviewRuntime.MaxBusinessResends,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		proposalModel, createErr := previewchatmodel.NewReceiptModel(
			previewchatmodel.NewFakeProposal(), previewRepository, previewchatmodel.ReceiptCallProposal,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		compiledGraph, createErr := plancreationspec.Compile(
			ctx, proposalModel, businessClient, previewRepository, clock.System{},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		previewTool, createErr := plancreationspec.NewTool(compiledGraph, previewRepository)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		routerModel, createErr := previewchatmodel.NewReceiptModel(
			previewchatmodel.NewFakeRouter(), previewRepository, previewchatmodel.ReceiptCallRouter,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		mainAgent, createErr := chatmodelagent.New(
			ctx, routerModel, []einotool.BaseTool{previewTool}, cfg.PlanSpecPreviewRuntime.MaxIterations,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		einoRunner, createErr := previewruntime.NewEinoRunner(ctx, mainAgent)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		previewProcessor, createErr = previewruntime.NewProcessor(
			previewRepository, einoRunner, clock.System{}, cfg.Service.InstanceID,
			previewruntime.ProcessorConfig{
				Concurrency:       cfg.PlanSpecPreviewRuntime.ProcessorConcurrency,
				PollInterval:      cfg.PlanSpecPreviewRuntime.PollInterval,
				LeaseDuration:     cfg.PlanSpecPreviewRuntime.LeaseDuration,
				HeartbeatInterval: cfg.PlanSpecPreviewRuntime.HeartbeatInterval,
				RetryDelay:        cfg.PlanSpecPreviewRuntime.RetryDelay,
				RecoveryDelay:     cfg.PlanSpecPreviewRuntime.RecoveryDelay,
				MaxAttempts:       cfg.PlanSpecPreviewRuntime.MaxAttempts,
			},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		previewService, createErr = previewruntime.NewService(
			previewRepository, contentProtector, idgen.UUIDv7{}, clock.System{}, previewProcessor.Wake,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
	}
	var userMessageProcessor *usermessageruntime.Processor
	if cfg.UserMessageRuntimeEnabled {
		legacyRepository, createErr := postgres.NewUserMessageLegacyUpgradeRepository(postgresClient)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		legacyService, createErr := usermessageruntime.NewLegacyUpgradeService(
			legacyRepository, contentProtector, sessionService, idgen.UUIDv7{}, cfg.SkillSnapshotLimits,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		const legacyUpgradeBatchSize = 64
		const legacyUpgradeMaxBatches = 10_000
		legacyUpgradeCtx, cancelLegacyUpgrade := context.WithTimeout(ctx, cfg.BusinessRPC.StartupTimeout)
		defer cancelLegacyUpgrade()
		legacyUpgradeReady := false
		for batch := 0; batch < legacyUpgradeMaxBatches; batch++ {
			preview, upgradeErr := legacyService.UpgradeBatch(legacyUpgradeCtx, legacyUpgradeBatchSize)
			if errors.Is(upgradeErr, usermessageruntime.ErrLegacyUpgradeBlocked) {
				logger.Error("User Message Legacy Helper preflight 被阻断", "blocker_count", len(preview.Blockers))
				_ = registry.Close(ctx)
				return fmt.Errorf("user message legacy upgrade blocked by %d safe preflight findings", len(preview.Blockers))
			}
			if upgradeErr != nil {
				_ = registry.Close(ctx)
				return fmt.Errorf("reconcile user message legacy upgrade: %w", upgradeErr)
			}
			if len(preview.Candidates) == 0 {
				legacyUpgradeReady = true
				break
			}
		}
		cancelLegacyUpgrade()
		if !legacyUpgradeReady {
			_ = registry.Close(ctx)
			return fmt.Errorf("user message legacy upgrade exceeded bounded startup batches")
		}
		userMessageRepository, createErr := postgres.NewUserMessageRuntimeRepository(
			postgresClient, contentProtector, idgen.UUIDv7{},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		receiptModel, createErr := usermessageruntime.NewReceiptModel(
			previewchatmodel.NewUserMessageFake(), userMessageRepository,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		directResponseAgent, createErr := chatmodelagent.NewDirectResponse(ctx, receiptModel)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		directResponseRunner, createErr := usermessageruntime.NewEinoRunner(ctx, directResponseAgent)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		userMessageProcessor, createErr = usermessageruntime.NewProcessor(
			userMessageRepository, directResponseRunner, clock.System{}, cfg.Service.InstanceID,
			usermessageruntime.ProcessorConfig{
				Concurrency:       cfg.UserMessageRuntime.ProcessorConcurrency,
				PollInterval:      cfg.UserMessageRuntime.PollInterval,
				LeaseDuration:     cfg.UserMessageRuntime.LeaseDuration,
				HeartbeatInterval: cfg.UserMessageRuntime.HeartbeatInterval,
				RetryDelay:        cfg.UserMessageRuntime.RetryDelay,
				ProjectionDelay:   cfg.UserMessageRuntime.RecoveryDelay,
				MaxAttempts:       cfg.UserMessageRuntime.MaxAttempts,
			},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
	}
	var analyzeMaterialsProcessor *analyzematerialsruntime.Processor
	var analyzeMaterialsService *analyzematerialsruntime.Service
	if cfg.AnalyzeMaterialsRuntimeEnabled {
		if cfg.AnalyzeMaterialsRuntime.Profile != analyzematerialsruntime.Profile {
			_ = registry.Close(ctx)
			return fmt.Errorf("configure analyze materials runtime: profile does not match approved pins")
		}
		analyzeRepository, createErr := postgres.NewAnalyzeMaterialsRuntimeRepository(
			postgresClient, contentProtector, idgen.UUIDv7{},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		analysisModel, createErr := analyzematerialsruntime.NewReceiptModel(
			previewchatmodel.NewAnalyzeMaterialsFakeModel(), analyzeRepository,
			analyzematerialsruntime.ModelCallGraphAnalysis,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		compiledGraph, createErr := analyzematerials.Compile(ctx, analysisModel, businessClient)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		coreTool, createErr := analyzematerials.NewTool(compiledGraph)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		receiptTool, createErr := analyzematerialsruntime.NewReceiptTool(ctx, coreTool, analyzeRepository)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		routerModel, createErr := analyzematerialsruntime.NewReceiptModel(
			previewchatmodel.NewAnalyzeMaterialsFakeRouter(), analyzeRepository,
			analyzematerialsruntime.ModelCallRouter,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		mainAgent, createErr := chatmodelagent.NewAnalyzeMaterials(
			ctx, routerModel, []einotool.BaseTool{receiptTool},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		einoRunner, createErr := analyzematerialsruntime.NewEinoRunner(ctx, mainAgent)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		analyzeMaterialsProcessor, createErr = analyzematerialsruntime.NewProcessor(
			analyzeRepository, einoRunner, clock.System{}, cfg.Service.InstanceID,
			analyzematerialsruntime.ProcessorConfig{
				Concurrency:       cfg.AnalyzeMaterialsRuntime.ProcessorConcurrency,
				PollInterval:      cfg.AnalyzeMaterialsRuntime.PollInterval,
				LeaseDuration:     cfg.AnalyzeMaterialsRuntime.LeaseDuration,
				HeartbeatInterval: cfg.AnalyzeMaterialsRuntime.HeartbeatInterval,
				RetryDelay:        cfg.AnalyzeMaterialsRuntime.RetryDelay,
				ProjectionDelay:   cfg.AnalyzeMaterialsRuntime.RecoveryDelay,
				MaxAttempts:       cfg.AnalyzeMaterialsRuntime.MaxAttempts,
			},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		analyzeMaterialsService, createErr = analyzematerialsruntime.NewService(
			analyzeRepository, clock.System{}, analyzeMaterialsProcessor.Wake,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
	}
	var planStoryboardProcessor *planstoryboardruntime.Processor
	var planStoryboardService *planstoryboardruntime.Service
	if cfg.PlanStoryboardRuntimeEnabled {
		if cfg.PlanStoryboardRuntime.Profile != planstoryboardruntime.Profile {
			_ = registry.Close(ctx)
			return fmt.Errorf("configure plan storyboard runtime: profile does not match approved pins")
		}
		if artifactErr := planstoryboardruntime.ValidateApprovedArtifacts(); artifactErr != nil {
			_ = registry.Close(ctx)
			return artifactErr
		}
		storyboardRepository, createErr := postgres.NewPlanStoryboardRuntimeRepository(
			postgresClient, contentProtector, idgen.UUIDv7{},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		commandJournal, createErr := planstoryboardruntime.NewCommandJournal(storyboardRepository)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		planningModel, createErr := planstoryboardruntime.NewReceiptModel(
			planstoryboardruntime.NewFakePlanningModel(), storyboardRepository,
			planstoryboardruntime.ModelCallGraphPlanning,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		compiledStoryboardGraph, createErr := planstoryboard.Compile(
			ctx, planningModel, businessClient, businessClient, commandJournal, clock.System{},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		storyboardCoreTool, createErr := planstoryboard.NewTool(
			compiledStoryboardGraph, planstoryboardruntime.ResolveCoreContext,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		storyboardReceiptTool, createErr := planstoryboardruntime.NewReceiptTool(
			ctx, storyboardCoreTool, storyboardRepository,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		storyboardRouterModel, createErr := planstoryboardruntime.NewReceiptModel(
			planstoryboardruntime.NewFakeRouter(), storyboardRepository,
			planstoryboardruntime.ModelCallRouter,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		storyboardAgent, createErr := chatmodelagent.NewPlanStoryboard(
			ctx, storyboardRouterModel, []einotool.BaseTool{storyboardReceiptTool},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		storyboardRunner, createErr := planstoryboardruntime.NewEinoRunner(ctx, storyboardAgent)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		storyboardRecovery, createErr := planstoryboardruntime.NewRecoveryCoordinator(
			businessClient, storyboardRepository, clock.System{},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		planStoryboardProcessor, createErr = planstoryboardruntime.NewProcessor(
			storyboardRepository, storyboardRunner, storyboardRecovery, clock.System{}, cfg.Service.InstanceID,
			planstoryboardruntime.ProcessorConfig{
				Concurrency:       cfg.PlanStoryboardRuntime.ProcessorConcurrency,
				PollInterval:      cfg.PlanStoryboardRuntime.PollInterval,
				LeaseDuration:     cfg.PlanStoryboardRuntime.LeaseDuration,
				HeartbeatInterval: cfg.PlanStoryboardRuntime.HeartbeatInterval,
				RetryDelay:        cfg.PlanStoryboardRuntime.RetryDelay,
				RecoveryDelay:     cfg.PlanStoryboardRuntime.RecoveryDelay,
				ProjectionDelay:   cfg.PlanStoryboardRuntime.RecoveryDelay,
				MaxAttempts:       cfg.PlanStoryboardRuntime.MaxAttempts,
			},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		planStoryboardService, createErr = planstoryboardruntime.NewService(
			storyboardRepository, clock.System{}, planStoryboardProcessor.Wake,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
	}
	var writePromptsProcessor *writepromptsruntime.Processor
	var writePromptsService *writepromptsruntime.Service
	if cfg.WritePromptsRuntimeEnabled {
		approvedPolicy := writepromptsruntime.ApprovedPolicy()
		if cfg.WritePromptsRuntime.Profile != writepromptsruntime.Profile ||
			cfg.WritePromptsRuntime.MaxTargets != approvedPolicy.MaxTargets ||
			cfg.WritePromptsRuntime.DefaultOutputLanguage != approvedPolicy.DefaultOutputLanguage ||
			cfg.WritePromptsRuntime.MaxBusinessResends != approvedPolicy.MaxCommandResends {
			_ = registry.Close(ctx)
			return fmt.Errorf("configure write prompts runtime: profile policy does not match approved pins")
		}
		if artifactErr := writepromptsruntime.ValidateApprovedArtifacts(); artifactErr != nil {
			_ = registry.Close(ctx)
			return artifactErr
		}
		promptRepository, createErr := postgres.NewWritePromptsRuntimeRepository(
			postgresClient, contentProtector, idgen.UUIDv7{},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		commandJournal, createErr := writepromptsruntime.NewCommandJournal(promptRepository)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		promptModel, createErr := writepromptsruntime.NewReceiptModel(
			writepromptsruntime.NewFakePromptModel(), promptRepository, writepromptsruntime.ModelCallGraphPrompt,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		compiledPromptGraph, createErr := writeprompts.Compile(
			ctx, promptModel, businessClient, businessClient, commandJournal, clock.System{},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		promptCoreTool, createErr := writeprompts.NewTool(compiledPromptGraph, writepromptsruntime.ResolveCoreContext)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		promptReceiptTool, createErr := writepromptsruntime.NewReceiptTool(ctx, promptCoreTool, promptRepository)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		promptRouterModel, createErr := writepromptsruntime.NewReceiptModel(
			writepromptsruntime.NewFakeRouter(), promptRepository, writepromptsruntime.ModelCallRouter,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		promptAgent, createErr := chatmodelagent.NewWritePrompts(
			ctx, promptRouterModel, []einotool.BaseTool{promptReceiptTool},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		promptRunner, createErr := writepromptsruntime.NewEinoRunner(ctx, promptAgent)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		promptRecovery, createErr := writepromptsruntime.NewRecoveryCoordinator(
			businessClient, promptRepository, clock.System{},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		writePromptsProcessor, createErr = writepromptsruntime.NewProcessor(
			promptRepository, promptRunner, promptRecovery, clock.System{}, cfg.Service.InstanceID,
			writepromptsruntime.ProcessorConfig{
				Concurrency:       cfg.WritePromptsRuntime.ProcessorConcurrency,
				PollInterval:      cfg.WritePromptsRuntime.PollInterval,
				LeaseDuration:     cfg.WritePromptsRuntime.LeaseDuration,
				HeartbeatInterval: cfg.WritePromptsRuntime.HeartbeatInterval,
				RetryDelay:        cfg.WritePromptsRuntime.RetryDelay,
				RecoveryDelay:     cfg.WritePromptsRuntime.RecoveryDelay,
				ProjectionDelay:   cfg.WritePromptsRuntime.RecoveryDelay,
				MaxAttempts:       cfg.WritePromptsRuntime.MaxAttempts,
			},
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		writePromptsService, createErr = writepromptsruntime.NewService(
			promptRepository, clock.System{}, writePromptsProcessor.Wake,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
	}
	if cfg.MVPAllToolsRuntimeEnabled() {
		mvpRuntime, createErr := buildMVPAllToolsRuntime(
			ctx, cfg, logger, postgresClient, businessClient, contentProtector, sessionService,
		)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
		mvpCoordinator = mvpRuntime.coordinator
		previewProcessor = mvpRuntime.previewProcessor
		previewService = mvpRuntime.previewService
		userMessageProcessor = mvpRuntime.userMessageProcessor
		analyzeMaterialsProcessor = mvpRuntime.analyzeMaterialsProcessor
		analyzeMaterialsService = mvpRuntime.analyzeMaterialsService
		planStoryboardProcessor = mvpRuntime.planStoryboardProcessor
		planStoryboardService = mvpRuntime.planStoryboardService
		writePromptsProcessor = mvpRuntime.writePromptsProcessor
		writePromptsService = mvpRuntime.writePromptsService
		mediaService = mvpRuntime.mediaService
		mediaBusinessClient = mvpRuntime.mediaBusinessClient
		if mediaBusinessClient != nil {
			defer mediaBusinessClient.Close()
		}
		sessionService, createErr = sessionService.WithRuntimeWake(mvpCoordinator.Wake)
		if createErr != nil {
			_ = registry.Close(ctx)
			return createErr
		}
	}
	sessionHandler, err := sessionrpc.NewHandlerWithSkillSnapshotLimits(sessionService, cfg.SkillSnapshotLimits)
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
			MaxMessages:      cfg.Workspace.MaxMessages,
			MaxInputs:        cfg.Workspace.MaxInputs,
			MaxMediaPreviews: mediaSnapshotLimit(cfg),
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
	var previewHandler *httpserver.CreationSpecPreviewHandler
	if capabilities.PlanCreationSpec {
		previewHandler, err = httpserver.NewCreationSpecPreviewHandler(
			identityVerifier, previewService, idgen.UUIDv7{},
		)
		if err != nil {
			_ = registry.Close(ctx)
			return err
		}
	}
	var analyzeMaterialsHandler *httpserver.AnalyzeMaterialsPreviewHandler
	if capabilities.AnalyzeMaterials {
		analyzeMaterialsHandler, err = httpserver.NewAnalyzeMaterialsPreviewHandler(
			identityVerifier, analyzeMaterialsService, idgen.UUIDv7{}, cfg.ContentProtection.KeyVersion,
		)
		if err != nil {
			_ = registry.Close(ctx)
			return err
		}
	}
	var planStoryboardHandler *httpserver.PlanStoryboardPreviewHandler
	if capabilities.PlanStoryboard {
		planStoryboardHandler, err = httpserver.NewPlanStoryboardPreviewHandler(
			identityVerifier, planStoryboardService, idgen.UUIDv7{}, cfg.ContentProtection.KeyVersion,
		)
		if err != nil {
			_ = registry.Close(ctx)
			return err
		}
	}
	var writePromptsHandler *httpserver.WritePromptsPreviewHandler
	if capabilities.WritePrompts {
		writePromptsHandler, err = httpserver.NewWritePromptsPreviewHandler(
			identityVerifier, writePromptsService, idgen.UUIDv7{}, cfg.ContentProtection.KeyVersion,
		)
		if err != nil {
			_ = registry.Close(ctx)
			return err
		}
	}
	var generateMediaHandler *httpserver.GenerateMediaPreviewHandler
	if capabilities.GenerateMedia {
		generateMediaHandler, err = httpserver.NewGenerateMediaPreviewHandler(
			identityVerifier, mediaService, idgen.UUIDv7{},
		)
		if err != nil {
			_ = registry.Close(ctx)
			return err
		}
	}
	var assembleOutputHandler *httpserver.AssembleOutputPreviewHandler
	if capabilities.AssembleOutput {
		assembleOutputHandler, err = httpserver.NewAssembleOutputPreviewHandler(
			identityVerifier, mediaService, idgen.UUIDv7{},
		)
		if err != nil {
			_ = registry.Close(ctx)
			return err
		}
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
	toolCatalogHandler, err := httpserver.NewToolCatalogHandler(
		identityVerifier, tool.NewCatalogProvider(), idgen.UUIDv7{},
	)
	if err != nil {
		_ = registry.Close(ctx)
		return err
	}
	state := health.NewState()
	server, err := httpserver.NewWithMedia(
		cfg.HTTP, cfg.Service, state, workspaceHandler, toolCatalogHandler, idgen.UUIDv7{},
		previewHandler, analyzeMaterialsHandler, planStoryboardHandler, writePromptsHandler,
		generateMediaHandler, assembleOutputHandler,
	)
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
	if err := startAgentRuntime(
		mvpCoordinator,
		previewProcessor, userMessageProcessor, analyzeMaterialsProcessor, planStoryboardProcessor, writePromptsProcessor,
	); err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		_ = stopAgentIngressAndProcessors(shutdownCtx, server, rpcServer, mvpCoordinator,
			previewProcessor, userMessageProcessor, analyzeMaterialsProcessor, planStoryboardProcessor, writePromptsProcessor)
		cancel()
		_ = registry.Close(ctx)
		return err
	}

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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		_ = stopAgentIngressAndProcessors(shutdownCtx, server, rpcServer, mvpCoordinator,
			previewProcessor, userMessageProcessor, analyzeMaterialsProcessor, planStoryboardProcessor, writePromptsProcessor)
		cancel()
		_ = registry.Close(ctx)
		return err
	}
	state.SetReady(true)
	logger.Info("Agent Service 已就绪",
		"http_addr", cfg.HTTP.Address,
		"advertised_address", cfg.Service.AdvertisedAddress,
		"session_rpc_addr", cfg.RPC.ListenAddress,
		"session_rpc_advertised_address", cfg.RPC.AdvertisedAddress,
		"runtime_profile", cfg.RuntimeProfile,
		"creation_spec_preview_enabled", capabilities.PlanCreationSpec,
		"user_message_runtime_enabled", capabilities.UserMessage,
		"analyze_materials_runtime_enabled", capabilities.AnalyzeMaterials,
		"plan_storyboard_runtime_enabled", capabilities.PlanStoryboard,
		"write_prompts_runtime_enabled", capabilities.WritePrompts,
		"generate_media_runtime_enabled", capabilities.GenerateMedia,
		"assemble_output_runtime_enabled", capabilities.AssembleOutput,
		"media_runtime_profile", cfg.MediaRuntime.Profile,
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
	registryDone := make(chan error, 1)
	go func() { registryDone <- registry.Close(shutdownCtx) }()
	// HTTP/RPC 立即并发停止接收和排空；Processor 同时停止新 Claim 并等待在途执行，
	// etcd 摘除也并发执行，避免任一外部组件独占整个 shutdown deadline。PostgreSQL 仍由最外层 defer 最后关闭。
	if err := stopAgentIngressAndProcessors(shutdownCtx, server, rpcServer, mvpCoordinator,
		previewProcessor, userMessageProcessor, analyzeMaterialsProcessor, planStoryboardProcessor, writePromptsProcessor); err != nil && runErr == nil {
		runErr = err
	}
	if err := <-registryDone; err != nil && runErr == nil {
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

// mediaSnapshotLimit 保持旧 Profile 的固定三查询，并为媒体 Profile 开启冻结的最近 16 条投影。
func mediaSnapshotLimit(cfg config.Config) int {
	if cfg.MediaRuntimeEnabled() {
		return 16
	}
	return 0
}

type runtimeProcessorStopper interface {
	Stop(context.Context) error
}

type runtimeProcessorStarter interface {
	Start() error
}

// startAgentRuntime 在统一 Profile 下只启动 Coordinator；隔离 Profile 保持原 Processor 生命周期。
func startAgentRuntime(
	coordinator *mvpruntime.Coordinator,
	previewProcessor *previewruntime.Processor,
	userMessageProcessor *usermessageruntime.Processor,
	analyzeMaterialsProcessor *analyzematerialsruntime.Processor,
	planStoryboardProcessor *planstoryboardruntime.Processor,
	writePromptsProcessor *writepromptsruntime.Processor,
) error {
	if coordinator != nil {
		return coordinator.Start()
	}
	processors := make([]runtimeProcessorStarter, 0, 5)
	if previewProcessor != nil {
		processors = append(processors, previewProcessor)
	}
	if userMessageProcessor != nil {
		processors = append(processors, userMessageProcessor)
	}
	if analyzeMaterialsProcessor != nil {
		processors = append(processors, analyzeMaterialsProcessor)
	}
	if planStoryboardProcessor != nil {
		processors = append(processors, planStoryboardProcessor)
	}
	if writePromptsProcessor != nil {
		processors = append(processors, writePromptsProcessor)
	}
	for _, processor := range processors {
		if err := processor.Start(); err != nil {
			return err
		}
	}
	return nil
}

// stopAgentIngressAndProcessors 在同一个总 deadline 内并发排空两个 ingress，
// 并立即停止 Runtime 的新 Claim。统一 Profile 只停止 Coordinator，隔离 Profile 停止自身 Processor。
func stopAgentIngressAndProcessors(
	ctx context.Context,
	httpServer *httpserver.Server,
	rpcServer *rpcserver.Server,
	coordinator *mvpruntime.Coordinator,
	previewProcessor *previewruntime.Processor,
	userMessageProcessor *usermessageruntime.Processor,
	analyzeMaterialsProcessor *analyzematerialsruntime.Processor,
	planStoryboardProcessor *planstoryboardruntime.Processor,
	writePromptsProcessor *writepromptsruntime.Processor,
) error {
	httpDone := make(chan error, 1)
	rpcDone := make(chan error, 1)
	go func() { httpDone <- httpServer.Shutdown(ctx) }()
	go func() { rpcDone <- rpcServer.Stop() }()

	var firstErr error
	processors := make([]runtimeProcessorStopper, 0, 5)
	if coordinator != nil {
		processors = append(processors, coordinator)
	} else {
		if previewProcessor != nil {
			processors = append(processors, previewProcessor)
		}
		if userMessageProcessor != nil {
			processors = append(processors, userMessageProcessor)
		}
		if analyzeMaterialsProcessor != nil {
			processors = append(processors, analyzeMaterialsProcessor)
		}
		if planStoryboardProcessor != nil {
			processors = append(processors, planStoryboardProcessor)
		}
		if writePromptsProcessor != nil {
			processors = append(processors, writePromptsProcessor)
		}
	}
	for _, processor := range processors {
		if err := processor.Stop(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := <-httpDone; err != nil && firstErr == nil {
		firstErr = err
	}
	if err := <-rpcDone; err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
