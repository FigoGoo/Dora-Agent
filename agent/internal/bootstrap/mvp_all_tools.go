package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/FigoGoo/Dora-Agent/agent/internal/analyzematerialsruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/businessmedia"
	"github.com/FigoGoo/Dora-Agent/agent/internal/businessrpc"
	previewchatmodel "github.com/FigoGoo/Dora-Agent/agent/internal/chatmodel"
	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodelagent"
	"github.com/FigoGoo/Dora-Agent/agent/internal/clock"
	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/contentcrypto"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreviewruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mvpruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/planstoryboardruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/postgres"
	previewruntime "github.com/FigoGoo/Dora-Agent/agent/internal/runtime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/usermessageruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/writepromptsruntime"
	einotool "github.com/cloudwego/eino/components/tool"
)

// mvpAllToolsRuntime 是统一 Profile 的启动期组合结果，不持有独立生命周期。
// 基础五个或媒体扩展八个 Processor 只由 Coordinator 驱动，禁止调用它们自己的 Start/Stop。
type mvpAllToolsRuntime struct {
	coordinator               *mvpruntime.Coordinator
	previewProcessor          *previewruntime.Processor
	previewService            *previewruntime.Service
	userMessageProcessor      *usermessageruntime.Processor
	analyzeMaterialsProcessor *analyzematerialsruntime.Processor
	analyzeMaterialsService   *analyzematerialsruntime.Service
	planStoryboardProcessor   *planstoryboardruntime.Processor
	planStoryboardService     *planstoryboardruntime.Service
	writePromptsProcessor     *writepromptsruntime.Processor
	writePromptsService       *writepromptsruntime.Service
	mediaRepository           *postgres.MediaRuntimeRepository
	mediaBusinessClient       *businessmedia.Client
	generateMediaProcessor    *mediapreviewruntime.Processor
	assembleOutputProcessor   *mediapreviewruntime.Processor
	mediaTerminalProcessor    *mediapreviewruntime.TerminalProcessor
	mediaService              *mediapreviewruntime.Service
}

// buildMVPAllToolsRuntime 只组合既有 Repository、Receipt、Graph、Runner 与 Processor。
// 唯一新增执行所有者是一个共享 ChatModelAgent 和一个 Coordinator。
func buildMVPAllToolsRuntime(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	postgresClient *postgres.Client,
	businessClient *businessrpc.Client,
	contentProtector *contentcrypto.AES256GCMProtector,
	sessionService *session.Service,
) (*mvpAllToolsRuntime, error) {
	if !cfg.MVPAllToolsRuntimeEnabled() || logger == nil || postgresClient == nil || businessClient == nil ||
		contentProtector == nil || sessionService == nil {
		return nil, fmt.Errorf("build mvp all-tools runtime: invalid composition dependencies")
	}
	if cfg.AnalyzeMaterialsRuntime.Profile != analyzematerialsruntime.Profile {
		return nil, fmt.Errorf("configure analyze materials runtime: profile does not match approved pins")
	}
	if cfg.PlanStoryboardRuntime.Profile != planstoryboardruntime.Profile {
		return nil, fmt.Errorf("configure plan storyboard runtime: profile does not match approved pins")
	}
	if err := planstoryboardruntime.ValidateApprovedArtifacts(); err != nil {
		return nil, err
	}
	approvedPromptPolicy := writepromptsruntime.ApprovedPolicy()
	if cfg.WritePromptsRuntime.Profile != writepromptsruntime.Profile ||
		cfg.WritePromptsRuntime.MaxTargets != approvedPromptPolicy.MaxTargets ||
		cfg.WritePromptsRuntime.DefaultOutputLanguage != approvedPromptPolicy.DefaultOutputLanguage ||
		cfg.WritePromptsRuntime.MaxBusinessResends != approvedPromptPolicy.MaxCommandResends {
		return nil, fmt.Errorf("configure write prompts runtime: profile policy does not match approved pins")
	}
	if err := writepromptsruntime.ValidateApprovedArtifacts(); err != nil {
		return nil, err
	}
	if err := reconcileUserMessageLegacyUpgrade(
		ctx, cfg, logger, postgresClient, contentProtector, sessionService,
	); err != nil {
		return nil, err
	}

	previewRepository, err := postgres.NewCreationSpecPreviewRepository(
		postgresClient, contentProtector, cfg.PlanSpecPreviewRuntime.MaxBusinessResends,
	)
	if err != nil {
		return nil, err
	}
	proposalModel, err := previewchatmodel.NewReceiptModel(
		previewchatmodel.NewFakeProposal(), previewRepository, previewchatmodel.ReceiptCallProposal,
	)
	if err != nil {
		return nil, err
	}
	compiledCreationSpec, err := plancreationspec.Compile(
		ctx, proposalModel, businessClient, previewRepository, clock.System{},
	)
	if err != nil {
		return nil, err
	}
	creationSpecTool, err := plancreationspec.NewTool(compiledCreationSpec, previewRepository)
	if err != nil {
		return nil, err
	}
	creationSpecRouter, err := previewchatmodel.NewReceiptModel(
		previewchatmodel.NewFakeRouter(), previewRepository, previewchatmodel.ReceiptCallRouter,
	)
	if err != nil {
		return nil, err
	}

	userMessageRepository, err := postgres.NewUserMessageRuntimeRepository(
		postgresClient, contentProtector, idgen.UUIDv7{},
	)
	if err != nil {
		return nil, err
	}
	userMessageModel, err := usermessageruntime.NewReceiptModel(
		previewchatmodel.NewUserMessageFake(), userMessageRepository,
	)
	if err != nil {
		return nil, err
	}

	analyzeRepository, err := postgres.NewAnalyzeMaterialsRuntimeRepository(
		postgresClient, contentProtector, idgen.UUIDv7{},
	)
	if err != nil {
		return nil, err
	}
	analysisModel, err := analyzematerialsruntime.NewReceiptModel(
		previewchatmodel.NewAnalyzeMaterialsFakeModel(), analyzeRepository,
		analyzematerialsruntime.ModelCallGraphAnalysis,
	)
	if err != nil {
		return nil, err
	}
	compiledAnalysis, err := analyzematerials.Compile(ctx, analysisModel, businessClient)
	if err != nil {
		return nil, err
	}
	analyzeCoreTool, err := analyzematerials.NewTool(compiledAnalysis)
	if err != nil {
		return nil, err
	}
	analyzeTool, err := analyzematerialsruntime.NewReceiptTool(ctx, analyzeCoreTool, analyzeRepository)
	if err != nil {
		return nil, err
	}
	analyzeRouter, err := analyzematerialsruntime.NewReceiptModel(
		previewchatmodel.NewAnalyzeMaterialsFakeRouter(), analyzeRepository,
		analyzematerialsruntime.ModelCallRouter,
	)
	if err != nil {
		return nil, err
	}

	storyboardRepository, err := postgres.NewPlanStoryboardRuntimeRepository(
		postgresClient, contentProtector, idgen.UUIDv7{},
	)
	if err != nil {
		return nil, err
	}
	storyboardJournal, err := planstoryboardruntime.NewCommandJournal(storyboardRepository)
	if err != nil {
		return nil, err
	}
	storyboardPlanningModel, err := planstoryboardruntime.NewReceiptModel(
		planstoryboardruntime.NewFakePlanningModel(), storyboardRepository,
		planstoryboardruntime.ModelCallGraphPlanning,
	)
	if err != nil {
		return nil, err
	}
	compiledStoryboard, err := planstoryboard.Compile(
		ctx, storyboardPlanningModel, businessClient, businessClient, storyboardJournal, clock.System{},
	)
	if err != nil {
		return nil, err
	}
	storyboardCoreTool, err := planstoryboard.NewTool(
		compiledStoryboard, planstoryboardruntime.ResolveCoreContext,
	)
	if err != nil {
		return nil, err
	}
	storyboardTool, err := planstoryboardruntime.NewReceiptTool(
		ctx, storyboardCoreTool, storyboardRepository,
	)
	if err != nil {
		return nil, err
	}
	storyboardRouter, err := planstoryboardruntime.NewReceiptModel(
		planstoryboardruntime.NewFakeRouter(), storyboardRepository,
		planstoryboardruntime.ModelCallRouter,
	)
	if err != nil {
		return nil, err
	}
	storyboardRecovery, err := planstoryboardruntime.NewRecoveryCoordinator(
		businessClient, storyboardRepository, clock.System{},
	)
	if err != nil {
		return nil, err
	}

	promptRepository, err := postgres.NewWritePromptsRuntimeRepository(
		postgresClient, contentProtector, idgen.UUIDv7{},
	)
	if err != nil {
		return nil, err
	}
	promptJournal, err := writepromptsruntime.NewCommandJournal(promptRepository)
	if err != nil {
		return nil, err
	}
	promptModel, err := writepromptsruntime.NewReceiptModel(
		writepromptsruntime.NewFakePromptModel(), promptRepository, writepromptsruntime.ModelCallGraphPrompt,
	)
	if err != nil {
		return nil, err
	}
	compiledPrompt, err := writeprompts.Compile(
		ctx, promptModel, businessClient, businessClient, promptJournal, clock.System{},
	)
	if err != nil {
		return nil, err
	}
	promptCoreTool, err := writeprompts.NewTool(compiledPrompt, writepromptsruntime.ResolveCoreContext)
	if err != nil {
		return nil, err
	}
	promptTool, err := writepromptsruntime.NewReceiptTool(ctx, promptCoreTool, promptRepository)
	if err != nil {
		return nil, err
	}
	promptRouter, err := writepromptsruntime.NewReceiptModel(
		writepromptsruntime.NewFakeRouter(), promptRepository, writepromptsruntime.ModelCallRouter,
	)
	if err != nil {
		return nil, err
	}
	promptRecovery, err := writepromptsruntime.NewRecoveryCoordinator(
		businessClient, promptRepository, clock.System{},
	)
	if err != nil {
		return nil, err
	}

	var mediaRepository *postgres.MediaRuntimeRepository
	var mediaClient *businessmedia.Client
	var generateMediaTool *mediapreview.GenerateMediaTool
	var assembleOutputTool *mediapreview.AssembleOutputTool
	var generateMediaRouter *mediapreviewruntime.FakeRouter
	var assembleOutputRouter *mediapreviewruntime.FakeRouter
	keepMediaClient := false
	defer func() {
		if mediaClient != nil && !keepMediaClient {
			mediaClient.Close()
		}
	}()
	if cfg.MediaRuntimeEnabled() {
		mediaClient, err = businessmedia.New(cfg.MediaRuntime)
		if err != nil {
			return nil, err
		}
		readinessCtx, cancel := context.WithTimeout(ctx, cfg.BusinessRPC.StartupTimeout)
		err = mediaClient.Readiness(readinessCtx)
		cancel()
		if err != nil {
			mediaClient.Close()
			return nil, err
		}
		mediaRepository, err = postgres.NewMediaRuntimeRepository(postgresClient, idgen.UUIDv7{})
		if err != nil {
			mediaClient.Close()
			return nil, err
		}
		generateGraph, compileErr := mediapreview.CompileGenerateMediaGraph(ctx, mediaClient, mediaRepository, clock.System{})
		if compileErr != nil {
			mediaClient.Close()
			return nil, compileErr
		}
		assembleGraph, compileErr := mediapreview.CompileAssembleOutputGraph(ctx, mediaClient, mediaRepository, clock.System{})
		if compileErr != nil {
			mediaClient.Close()
			return nil, compileErr
		}
		generateMediaTool, err = mediapreview.NewGenerateMediaTool(generateGraph)
		if err != nil {
			mediaClient.Close()
			return nil, err
		}
		assembleOutputTool, err = mediapreview.NewAssembleOutputTool(assembleGraph)
		if err != nil {
			mediaClient.Close()
			return nil, err
		}
		generateMediaRouter, err = mediapreviewruntime.NewFakeRouter(mediapreview.GenerateMediaToolKey)
		if err != nil {
			mediaClient.Close()
			return nil, err
		}
		assembleOutputRouter, err = mediapreviewruntime.NewFakeRouter(mediapreview.AssembleOutputToolKey)
		if err != nil {
			mediaClient.Close()
			return nil, err
		}
	}

	dispatcher, err := previewchatmodel.NewMVPDispatcher(previewchatmodel.MVPDispatcherModels{
		CreationSpec:     creationSpecRouter,
		UserMessage:      userMessageModel,
		AnalyzeMaterials: analyzeRouter,
		PlanStoryboard:   storyboardRouter,
		WritePrompts:     promptRouter,
		GenerateMedia:    generateMediaRouter,
		AssembleOutput:   assembleOutputRouter,
	})
	if err != nil {
		return nil, err
	}
	tools := []einotool.BaseTool{creationSpecTool, analyzeTool, storyboardTool, promptTool}
	if cfg.MediaRuntimeEnabled() {
		tools = append(tools, generateMediaTool, assembleOutputTool)
	}
	mainAgent, err := chatmodelagent.NewMVPAllTools(
		ctx,
		dispatcher,
		tools,
		cfg.PlanSpecPreviewRuntime.MaxIterations,
	)
	if err != nil {
		return nil, err
	}

	previewRunner, err := previewruntime.NewEinoRunner(ctx, mainAgent)
	if err != nil {
		return nil, err
	}
	userMessageRunner, err := usermessageruntime.NewEinoRunner(ctx, mainAgent)
	if err != nil {
		return nil, err
	}
	analyzeRunner, err := analyzematerialsruntime.NewEinoRunner(ctx, mainAgent)
	if err != nil {
		return nil, err
	}
	storyboardRunner, err := planstoryboardruntime.NewEinoRunner(ctx, mainAgent)
	if err != nil {
		return nil, err
	}
	promptRunner, err := writepromptsruntime.NewEinoRunner(ctx, mainAgent)
	if err != nil {
		return nil, err
	}
	var mediaRunner *mediapreviewruntime.EinoRunner
	if cfg.MediaRuntimeEnabled() {
		mediaRunner, err = mediapreviewruntime.NewEinoRunner(ctx, mainAgent)
		if err != nil {
			return nil, err
		}
	}

	previewProcessor, err := previewruntime.NewProcessor(
		previewRepository, previewRunner, clock.System{}, cfg.Service.InstanceID,
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
	if err != nil {
		return nil, err
	}
	userMessageProcessor, err := usermessageruntime.NewProcessor(
		userMessageRepository, userMessageRunner, clock.System{}, cfg.Service.InstanceID,
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
	if err != nil {
		return nil, err
	}
	analyzeProcessor, err := analyzematerialsruntime.NewProcessor(
		analyzeRepository, analyzeRunner, clock.System{}, cfg.Service.InstanceID,
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
	if err != nil {
		return nil, err
	}
	storyboardProcessor, err := planstoryboardruntime.NewProcessor(
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
	if err != nil {
		return nil, err
	}
	promptProcessor, err := writepromptsruntime.NewProcessor(
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
	if err != nil {
		return nil, err
	}
	var generateMediaProcessor *mediapreviewruntime.Processor
	var assembleOutputProcessor *mediapreviewruntime.Processor
	var mediaTerminalProcessor *mediapreviewruntime.TerminalProcessor
	if cfg.MediaRuntimeEnabled() {
		mediaConfig := mediapreviewruntime.ProcessorConfig{LeaseDuration: cfg.PlanSpecPreviewRuntime.LeaseDuration,
			HeartbeatInterval: cfg.PlanSpecPreviewRuntime.HeartbeatInterval,
			RecoveryDelay:     cfg.PlanSpecPreviewRuntime.RecoveryDelay, MaxAttempts: cfg.PlanSpecPreviewRuntime.MaxAttempts}
		generateMediaProcessor, err = mediapreviewruntime.NewProcessor(mediaRepository, mediaRunner, clock.System{},
			cfg.Service.InstanceID, mediapreviewruntime.GenerateSourceType, mediaConfig)
		if err != nil {
			return nil, err
		}
		assembleOutputProcessor, err = mediapreviewruntime.NewProcessor(mediaRepository, mediaRunner, clock.System{},
			cfg.Service.InstanceID, mediapreviewruntime.AssembleSourceType, mediaConfig)
		if err != nil {
			return nil, err
		}
		mediaTerminalProcessor, err = mediapreviewruntime.NewTerminalProcessor(mediaRepository,
			cfg.Service.InstanceID, cfg.PlanSpecPreviewRuntime.LeaseDuration)
		if err != nil {
			return nil, err
		}
	}

	handlers := []mvpruntime.Handler{
		userMessageProcessor,
		previewProcessor,
		analyzeProcessor,
		storyboardProcessor,
		promptProcessor,
	}
	if cfg.MediaRuntimeEnabled() {
		handlers = append(handlers, generateMediaProcessor, assembleOutputProcessor, mediaTerminalProcessor)
	}
	coordinator, err := mvpruntime.NewCoordinator(
		handlers,
		mvpruntime.Config{
			Concurrency:  cfg.PlanSpecPreviewRuntime.ProcessorConcurrency,
			PollInterval: cfg.PlanSpecPreviewRuntime.PollInterval,
		},
	)
	if err != nil {
		return nil, err
	}
	previewService, err := previewruntime.NewService(
		previewRepository, contentProtector, idgen.UUIDv7{}, clock.System{}, coordinator.Wake,
	)
	if err != nil {
		return nil, err
	}
	analyzeService, err := analyzematerialsruntime.NewService(analyzeRepository, clock.System{}, coordinator.Wake)
	if err != nil {
		return nil, err
	}
	storyboardService, err := planstoryboardruntime.NewService(storyboardRepository, clock.System{}, coordinator.Wake)
	if err != nil {
		return nil, err
	}
	promptService, err := writepromptsruntime.NewService(promptRepository, clock.System{}, coordinator.Wake)
	if err != nil {
		return nil, err
	}
	var mediaService *mediapreviewruntime.Service
	if cfg.MediaRuntimeEnabled() {
		mediaService, err = mediapreviewruntime.NewService(mediaRepository, clock.System{}, coordinator.Wake)
		if err != nil {
			return nil, err
		}
	}
	keepMediaClient = true

	return &mvpAllToolsRuntime{
		coordinator:               coordinator,
		previewProcessor:          previewProcessor,
		previewService:            previewService,
		userMessageProcessor:      userMessageProcessor,
		analyzeMaterialsProcessor: analyzeProcessor,
		analyzeMaterialsService:   analyzeService,
		planStoryboardProcessor:   storyboardProcessor,
		planStoryboardService:     storyboardService,
		writePromptsProcessor:     promptProcessor,
		writePromptsService:       promptService,
		mediaRepository:           mediaRepository,
		mediaBusinessClient:       mediaClient,
		generateMediaProcessor:    generateMediaProcessor,
		assembleOutputProcessor:   assembleOutputProcessor,
		mediaTerminalProcessor:    mediaTerminalProcessor,
		mediaService:              mediaService,
	}, nil
}

func reconcileUserMessageLegacyUpgrade(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	postgresClient *postgres.Client,
	contentProtector *contentcrypto.AES256GCMProtector,
	sessionService *session.Service,
) error {
	legacyRepository, err := postgres.NewUserMessageLegacyUpgradeRepository(postgresClient)
	if err != nil {
		return err
	}
	legacyService, err := usermessageruntime.NewLegacyUpgradeService(
		legacyRepository, contentProtector, sessionService, idgen.UUIDv7{}, cfg.SkillSnapshotLimits,
	)
	if err != nil {
		return err
	}
	const legacyUpgradeBatchSize = 64
	const legacyUpgradeMaxBatches = 10_000
	legacyUpgradeCtx, cancelLegacyUpgrade := context.WithTimeout(ctx, cfg.BusinessRPC.StartupTimeout)
	defer cancelLegacyUpgrade()
	for batch := 0; batch < legacyUpgradeMaxBatches; batch++ {
		preview, upgradeErr := legacyService.UpgradeBatch(legacyUpgradeCtx, legacyUpgradeBatchSize)
		if errors.Is(upgradeErr, usermessageruntime.ErrLegacyUpgradeBlocked) {
			logger.Error("User Message Legacy Helper preflight 被阻断", "blocker_count", len(preview.Blockers))
			return fmt.Errorf("user message legacy upgrade blocked by %d safe preflight findings", len(preview.Blockers))
		}
		if upgradeErr != nil {
			return fmt.Errorf("reconcile user message legacy upgrade: %w", upgradeErr)
		}
		if len(preview.Candidates) == 0 {
			return nil
		}
	}
	return fmt.Errorf("user message legacy upgrade exceeded bounded startup batches")
}
