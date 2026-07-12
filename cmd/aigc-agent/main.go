package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"

	aigca2ui "github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	aigcagent "github.com/FigoGoo/Dora-Agent/internal/aigc/agent"
	aigcapproval "github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	aigcapprovalruntime "github.com/FigoGoo/Dora-Agent/internal/aigc/approvalruntime"
	aigcartifact "github.com/FigoGoo/Dora-Agent/internal/aigc/artifact"
	aigcasset "github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	aigcbilling "github.com/FigoGoo/Dora-Agent/internal/aigc/billing"
	aigccapability "github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
	aigccapabilityruntime "github.com/FigoGoo/Dora-Agent/internal/aigc/capabilityruntime"
	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcevents "github.com/FigoGoo/Dora-Agent/internal/aigc/events"
	aigcgeneration "github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	aigcgenerationhandlers "github.com/FigoGoo/Dora-Agent/internal/aigc/generation/handlers"
	aigcgenerationruntime "github.com/FigoGoo/Dora-Agent/internal/aigc/generationruntime"
	aigcmodelreceipt "github.com/FigoGoo/Dora-Agent/internal/aigc/modelreceipt"
	aigcserver "github.com/FigoGoo/Dora-Agent/internal/aigc/server"
	aigcsession "github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	aigcsessionruntime "github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
	aigcskill "github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
	aigcspec "github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
	aigcstoryboard "github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
	aigcturncontext "github.com/FigoGoo/Dora-Agent/internal/aigc/turncontext"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := aigcconfig.LoadFromEnv().Normalize()
	instanceID := processInstanceID()
	db, err := aigcstorage.OpenAgentPostgres(ctx, cfg)
	must(err, "open agent postgres")

	sessionStore := aigcsession.NewPostgresStore(db)
	must(sessionStore.AutoMigrate(ctx), "migrate session tables")
	runtimeStore := aigcsessionruntime.NewPostgresStore(db)
	must(runtimeStore.AutoMigrate(ctx), "migrate durable session runtime tables")
	eventStore := aigcevents.NewPostgresStore(db)
	must(eventStore.AutoMigrate(ctx), "migrate session event log")
	modelReceiptStore := aigcmodelreceipt.NewPostgresStore(db)
	must(modelReceiptStore.AutoMigrate(ctx), "migrate durable model output receipts")
	approvalStore := aigcapproval.NewPostgresStore(db)
	must(approvalStore.AutoMigrate(ctx), "migrate approval tables")
	billingStore := aigcbilling.NewPostgresStore(db)
	must(billingStore.Migrate(ctx), "migrate billing tables")
	artifactStore := aigcartifact.NewPostgresStore(db)
	must(artifactStore.AutoMigrate(ctx), "migrate artifact tables")
	skillStore := aigcskill.NewPostgresSkillStore(db)
	must(skillStore.AutoMigrate(ctx), "migrate skill table")
	specStore := aigcspec.NewPostgresStore(db)
	must(specStore.AutoMigrate(ctx), "migrate creation spec revisions")
	storyboardStore := aigcstoryboard.NewPostgresStore(db)
	must(storyboardStore.AutoMigrate(ctx), "migrate storyboard tables")
	storyboardCommands, err := aigcstoryboard.NewCommandService(storyboardStore)
	must(err, "create storyboard command service")
	assetStore := aigcasset.NewPostgresStore(db)
	must(assetStore.AutoMigrate(ctx), "migrate asset table")
	workflowStore := aigcgeneration.NewPostgresWorkflowStore(db)
	must(workflowStore.AutoMigrate(ctx), "migrate generation workflow tables")
	generationCommands := aigcgeneration.NewCommandService(aigcgeneration.CommandServiceConfig{Store: workflowStore})

	localAssetDir := strings.TrimSpace(os.Getenv("DORA_LOCAL_ASSET_DIR"))
	localDemoProviders := strings.EqualFold(strings.TrimSpace(os.Getenv("DORA_LOCAL_DEMO")), "true")
	if localAssetDir == "" && strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "local") && (cfg.Storage.TOS.AccessKeyID == "" || cfg.Storage.TOS.SecretAccessKey == "") {
		localAssetDir = ".local/aigc-assets"
		localDemoProviders = true
	}
	var assetUploader aigcasset.Uploader
	if localAssetDir != "" {
		localUploader, localErr := aigcasset.NewLocalUploader(localAssetDir, strings.TrimSpace(os.Getenv("DORA_LOCAL_ASSET_BASE_URL")))
		must(localErr, "create local asset uploader")
		assetUploader = localUploader
		localAssetDir = localUploader.RootDir()
		slog.Info("local demo asset storage enabled", "root", localAssetDir)
	} else {
		tosUploader, tosErr := aigcasset.NewTOSUploader(cfg.Storage.TOS)
		must(tosErr, "create tos asset uploader")
		assetUploader = tosUploader
	}

	runtimeRedis := aigcstorage.NewRuntimeRedisClient(cfg)
	defer runtimeRedis.Close()
	must(aigcstorage.PingRedis(ctx, runtimeRedis), "ping runtime redis")
	generationRedis := aigcstorage.NewGenerationRedisClient(cfg)
	defer generationRedis.Close()
	must(aigcstorage.PingRedis(ctx, generationRedis), "ping generation redis")
	generationQueue := aigcgeneration.NewRedisQueue(generationRedis, cfg.Storage.GenerationRedis.ListKey)

	wakeBroker := aigca2ui.NewMemoryBroker(128)
	notifications := aigcevents.NewMemoryNotificationHub()
	eventBroker := aigcserver.NewDurableEventBroker(eventStore, wakeBroker, notifications)
	eventRelay, err := aigcevents.NewTailRelay(aigcevents.TailRelayConfig{
		Store: eventStore, Notifications: notifications, PollInterval: time.Second, BatchSize: 100,
	})
	must(err, "create durable session event relay")
	runnerCheckpoints := aigcstorage.NewRedisCheckpointStore(runtimeRedis, "dora:aigc:runner_checkpoint:")

	chatModel, err := aigcagent.NewDeepSeekChatModel(ctx, cfg)
	must(err, "create shared deepseek chat model")

	runtimeBridge := aigcserver.NewRuntimeBridge()
	approvalRuntime, err := aigcapprovalruntime.New(aigcapprovalruntime.Config{
		Approvals: approvalStore, Continuations: runtimeStore, Inputs: runtimeBridge,
		Specs: specStore, Artifacts: artifactStore, Storyboards: storyboardStore,
		StoryboardCommands: storyboardCommands, GenerationJobs: workflowStore,
		OwnerID: "aigc-agent-approval:" + instanceID,
	})
	must(err, "create approval runtime")

	createApproval := func(ctx context.Context, request aigccapabilityruntime.ApprovalRequest) (string, error) {
		if strings.TrimSpace(request.ID) == "" {
			request.ID = stableID("approval", request.SessionID, request.ArtifactKind, request.ArtifactID, fmt.Sprint(request.ArtifactVersion))
		}
		approvePayload, err := json.Marshal(request.Approve.Payload)
		if err != nil {
			return "", err
		}
		rejectPayload, err := json.Marshal(request.Reject.Payload)
		if err != nil {
			return "", err
		}
		reviewMode := aigcapproval.ReviewMode(request.ReviewMode)
		executionMode := aigcapproval.ExecutionMode(request.ExecutionMode)
		created, err := approvalStore.Create(ctx, aigcapproval.Approval{
			ID: request.ID, IdempotencyKey: "artifact:" + request.ArtifactKind + ":" + request.ArtifactID + ":" + fmt.Sprint(request.ArtifactVersion),
			SessionID: request.SessionID, UserID: request.UserID, ArtifactType: request.ArtifactKind,
			Binding:    aigcapproval.VersionBinding{ArtifactID: request.ArtifactID, ArtifactVersion: request.ArtifactVersion, StoryboardID: request.StoryboardID, StoryboardVersion: request.StoryboardVersion},
			ReviewMode: reviewMode, ExecutionMode: executionMode,
			ApproveCommand: aigcapproval.FrozenCommand{Kind: request.Approve.Kind, IdempotencyKey: "approval:" + request.ID + ":approve", Payload: approvePayload},
			RejectCommand:  aigcapproval.FrozenCommand{Kind: request.Reject.Kind, IdempotencyKey: "approval:" + request.ID + ":reject", Payload: rejectPayload},
		})
		if err != nil {
			return "", err
		}
		if created.Approval.Status == aigcapproval.StatusPending {
			if err := publishApprovalCard(ctx, eventBroker, created.Approval, specStore, storyboardStore, assetStore); err != nil {
				return "", err
			}
		}
		return created.Approval.ID, nil
	}
	costCalculator := aigcgenerationruntime.DefaultCostCalculator{Points: map[string]int64{"assembly": 0, "audio": 0, "music": 0, "voice": 0}}
	generationPreflight := func(ctx context.Context, userID string, jobs []aigcgeneration.GenerationJob) (int64, error) {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			return 0, fmt.Errorf("generation requires an authenticated billing user")
		}
		account, err := billingStore.GetAccount(ctx, userID)
		if err != nil {
			return 0, fmt.Errorf("load generation billing account: %w", err)
		}
		var estimate int64
		for _, job := range jobs {
			if err := aigcgeneration.ValidateProviderJob(job); err != nil {
				return 0, fmt.Errorf("invalid %s generation parameters: %w", job.Provider, err)
			}
			available := false
			switch job.Provider {
			case aigcgeneration.ProviderImage2:
				available = cfg.Image2.APIKey != "" || localDemoProviders
			case aigcgeneration.ProviderSeedance:
				available = cfg.Seedance.APIKey != "" || localDemoProviders
			case aigcgeneration.ProviderAudio, aigcgeneration.ProviderAssembly:
				available = true
			}
			if !available {
				return 0, fmt.Errorf("generation provider %s is not configured", job.Provider)
			}
			estimate += costCalculator.Estimate(job)
		}
		if estimate > account.Balance {
			return estimate, fmt.Errorf("generation requires %d points but balance is %d: %w", estimate, account.Balance, aigcbilling.ErrInsufficientPoints)
		}
		return estimate, nil
	}

	capabilityRuntime, err := aigccapabilityruntime.New(aigccapabilityruntime.Config{
		Model: chatModel, Artifacts: artifactStore, Specs: specStore, Assets: assetStore,
		Storyboards: storyboardStore, StoryboardCommands: storyboardCommands,
		GenerationCommands: generationCommands, GenerationJobs: workflowStore, GenerationWorkflow: workflowStore, GenerationPreflight: generationPreflight, CreateApproval: createApproval,
		PrimaryReviewGate: func(ctx context.Context, sessionID string) error {
			pending, err := approvalStore.GetPendingPrimaryReviewBySession(ctx, sessionID)
			if errors.Is(err, aigcapproval.ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("%w: approval_id=%s artifact_type=%s", aigcapproval.ErrPrimaryReviewPending, pending.ID, pending.ArtifactType)
		},
		LocalDemoPlanning: localDemoProviders,
		PublishStoryboard: func(ctx context.Context, aggregate aigcstoryboard.StoryboardAggregate, source string) error {
			envelope := aigca2ui.ActionEnvelope{Version: aigca2ui.Version1, Actions: []aigca2ui.Action{{
				Type: aigca2ui.ActionUpdateCard, Surface: "storyboard",
				Target:  &aigca2ui.ActionTarget{Surface: "storyboard", CardID: "storyboard:" + aggregate.SessionID},
				Payload: map[string]any{"storyboard": aggregate.PublicView(), "source": source},
			}}}
			return eventBroker.Publish(ctx, aigca2ui.SSEEvent{ID: fmt.Sprintf("storyboard:%s:v%d", aggregate.ID, aggregate.Version), SessionID: aggregate.SessionID, Event: aigca2ui.EventAction, Payload: envelope, CreatedAt: time.Now()})
		},
	})
	must(err, "create capability runtime")
	toolSet, err := aigccapability.NewToolSetFromHandlers(ctx, capabilityRuntime.Handlers())
	must(err, "compile capability graphs")
	registry, err := aigccapability.NewAgentRegistry(toolSet)
	must(err, "create five-tool agent registry")

	turnContextBuilder := aigcturncontext.NewBuilder(aigcturncontext.Config{Skills: skillStore, Specs: specStore, DynamicStoryboards: storyboardStore})
	runner, err := aigcagent.NewDeepSeekRunner(ctx, aigcagent.DeepSeekRunnerConfig{
		Runtime: cfg, ChatModel: chatModel, Registry: registry,
		SkillBackend: aigcskill.NewEinoBackend(skillStore), RunnerCheckpoints: runnerCheckpoints,
		ModelReceiptStore: modelReceiptStore,
		ExtraHandlers:     []adk.ChatModelAgentMiddleware{aigcturncontext.NewMiddleware(turnContextBuilder)},
	})
	must(err, "create aigc chatmodel agent")
	runnerInvoker := aigcserver.NewRunnerInvoker(runner, aigcserver.WithRunnerChatModelOptions(einomodel.WithMaxTokens(8192), einomodel.WithTemperature(0.2)))

	baseHTTPConfig := aigcserver.Config{
		Store: sessionStore, Skills: skillStore, Storyboards: storyboardStore,
		DynamicStoryboards: storyboardStore, StoryboardCommands: storyboardCommands,
		Assets: assetStore, GenerationJobs: workflowStore, GenerationWorkflow: workflowStore,
		GenerationCommands: generationCommands, GenerationPreflight: generationPreflight, AssetUploader: assetUploader, LocalAssetDir: localAssetDir,
		Events: eventBroker, EventLog: eventStore, EventRelay: eventRelay, Checkpoints: sessionStore,
		Invoker: runnerInvoker, Approvals: approvalStore,
		ApprovalRuntime: approvalRuntime, RuntimeStore: runtimeStore,
		Billing: billingStore, Specs: specStore, SessionValues: aigcturncontext.SessionValues,
	}
	processor, err := aigcserver.NewDurableAgentProcessor(aigcserver.DurableAgentProcessorConfig{HTTPConfig: baseHTTPConfig, ApprovalRuntime: approvalRuntime})
	must(err, "create durable agent processor")
	runtimeManager, err := aigcsessionruntime.NewManager(aigcsessionruntime.ManagerConfig{
		Store: runtimeStore, Processor: processor, OwnerID: "aigc-agent-runtime:" + instanceID,
		LeaseTTL: 30 * time.Second, PollInterval: time.Second, IdleTimeout: 5 * time.Minute,
		OnError: func(sessionID string, err error) {
			slog.Error("session runtime error", "session_id", sessionID, "error", err)
		},
	})
	must(err, "create durable session runtime")
	runtimeBridge.Set(runtimeManager)
	baseHTTPConfig.Runtime = runtimeManager

	pendingAssets := aigcgenerationruntime.PendingAssetStore{AssetStore: assetStore}
	providerHandlers := map[string]aigcgeneration.JobHandler{}
	if cfg.Image2.APIKey != "" {
		providerHandlers[aigcgeneration.ProviderImage2] = aigcgenerationhandlers.NewImage2JobHandler(aigcgenerationhandlers.Image2JobHandlerConfig{APIKey: cfg.Image2.APIKey, Assets: pendingAssets, AssetUploader: assetUploader})
	} else if localDemoProviders {
		providerHandlers[aigcgeneration.ProviderImage2] = aigcgenerationhandlers.NewDemoVisualJobHandler(aigcgenerationhandlers.DemoVisualJobHandlerConfig{Assets: pendingAssets, Uploader: assetUploader})
	}
	providerHandlers[aigcgeneration.ProviderAudio] = aigcgenerationhandlers.NewDemoAudioJobHandler(aigcgenerationhandlers.DemoAudioJobHandlerConfig{Assets: pendingAssets, Uploader: assetUploader})
	providerHandlers[aigcgeneration.ProviderAssembly] = aigcgenerationhandlers.NewDemoAssemblyJobHandler(aigcgenerationhandlers.DemoAssemblyJobHandlerConfig{Assets: pendingAssets, Uploader: assetUploader})
	providers := make(map[string]aigcgeneration.ProviderAdapter, len(providerHandlers))
	for key, handler := range providerHandlers {
		providers[key] = aigcgeneration.NewJobHandlerProviderAdapter(handler)
	}
	if cfg.Seedance.APIKey != "" {
		providers[aigcgeneration.ProviderSeedance] = aigcgenerationhandlers.NewSeedanceJobHandler(aigcgenerationhandlers.SeedanceJobHandlerConfig{APIKey: cfg.Seedance.APIKey, Assets: pendingAssets, AssetUploader: assetUploader})
	} else if localDemoProviders {
		providers[aigcgeneration.ProviderSeedance] = aigcgeneration.NewJobHandlerProviderAdapter(aigcgenerationhandlers.NewDemoVisualJobHandler(aigcgenerationhandlers.DemoVisualJobHandlerConfig{Assets: pendingAssets, Uploader: assetUploader}))
	}
	bindingAdapter := aigcgenerationruntime.StoryboardBindingAdapter{Repository: storyboardStore, Commands: storyboardCommands, Assets: assetStore, Approvals: approvalStore, Specs: specStore, Events: eventBroker}
	billingAdapter := aigcgenerationruntime.BillingAdapter{Store: billingStore}
	barrier := aigcgeneration.NewBatchBarrier(workflowStore)
	finalizer := aigcgeneration.NewFinalizationEngine(aigcgeneration.FinalizationEngineConfig{
		Store: workflowStore, Bindings: aigcgenerationruntime.BindingGuard{StoryboardBindingAdapter: bindingAdapter},
		Committer: bindingAdapter, Inspector: bindingAdapter, Discarder: bindingAdapter, Billing: billingAdapter, Calculator: costCalculator, Barrier: barrier,
	})
	compensation := aigcgeneration.NewCompensationService(aigcgeneration.CompensationServiceConfig{Store: workflowStore, Billing: billingAdapter, Barrier: barrier})
	baseHTTPConfig.Compensation = compensation
	baseHTTPConfig.AdminToken = strings.TrimSpace(os.Getenv("AIGC_ADMIN_TOKEN"))
	signalPublisher := aigcgenerationruntime.SessionSignalPublisher{Store: workflowStore, Runtime: runtimeManager, Events: eventBroker, Compensation: compensation, Barrier: barrier, Finalized: bindingAdapter}
	outboxDispatcher := aigcgeneration.NewOutboxDispatcher(workflowStore, aigcgeneration.QueueOutboxPublisher{Queue: generationQueue, Next: signalPublisher})
	recoveryScheduler := aigcgeneration.NewRecoveryScheduler(workflowStore, generationQueue)

	httpServer := &http.Server{Addr: cfg.Runtime.HTTPAddr, Handler: aigcserver.NewRouter(baseHTTPConfig), ReadHeaderTimeout: 5 * time.Second}

	go runRuntime(ctx, stop, runtimeManager)
	go runApprovalRelay(ctx, stop, approvalRuntime)
	go runGenerationOutbox(ctx, stop, outboxDispatcher, recoveryScheduler)
	for workerID := 0; workerID < 4; workerID++ {
		worker := aigcgeneration.NewLifecycleWorker(aigcgeneration.LifecycleWorkerConfig{
			Store: workflowStore, Queue: generationQueue, Providers: providers,
			Finalizer: finalizer, Barrier: barrier, ResultRecovery: pendingAssets,
			WorkerID: fmt.Sprintf("aigc-generation-worker:%s:%d", instanceID, workerID), PollTimeout: time.Second,
		})
		go runGenerationWorker(ctx, workerID, worker)
	}
	go func() {
		slog.Info("aigc agent server listening", "addr", cfg.Runtime.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("aigc agent server stopped", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = runtimeManager.Stop(shutdownCtx)
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown aigc agent server", "error", err)
		os.Exit(1)
	}
}

func publishApprovalCard(ctx context.Context, publisher aigca2ui.EventPublisher, record aigcapproval.Approval, specs *aigcspec.PostgresStore, storyboards *aigcstoryboard.PostgresStore, assets *aigcasset.PostgresStore) error {
	// Candidate approvals remain durable and individually rejectable, but their
	// confirmation surface is the storyboard-level batch action rather than one
	// chat card per generated asset.
	if record.ArtifactType == "candidate_asset" {
		return nil
	}
	reviewTitle := "确认创作结果"
	reviewIntro := "请审核当前创作结果。"
	switch record.ArtifactType {
	case "creation_spec_revision":
		reviewTitle = "确认创作规范"
		reviewIntro = "请审核本次创作规范。"
	case "storyboard_revision":
		reviewTitle = "确认故事板方案"
		reviewIntro = "请审核本次故事板方案。"
	}
	children := []string{"message"}
	components := []aigca2ui.Component{
		aigca2ui.Text("message", reviewIntro, "", ""),
	}
	data := map[string]any{"approval_id": record.ID, "review_mode": record.ReviewMode, "artifact_type": record.ArtifactType}
	switch record.ArtifactType {
	case "creation_spec_revision":
		if value, err := specs.GetRevision(ctx, record.Binding.ArtifactID, record.Binding.ArtifactVersion); err == nil {
			details := value.Markdown
			if strings.TrimSpace(details) == "" {
				details = fmt.Sprintf("### %s\n\n- 类型：%s\n- 时长：%d 秒\n- 画幅：%s\n- 视觉：%s\n- 声音：%s", value.Title, value.VideoType, value.DurationSeconds, value.AspectRatio, value.VisualStyle, value.SoundStyle)
			}
			components = append(components, aigca2ui.NewComponent("details", aigca2ui.ComponentMarkdown, aigca2ui.MarkdownComp{Value: details}))
			children = append(children, "details")
		}
	case "storyboard_revision":
		if aggregate, err := storyboards.GetAggregate(ctx, record.Binding.StoryboardID); err == nil {
			if pending, pendingErr := aggregate.PendingRevision(); pendingErr == nil {
				scenario := strings.TrimSpace(pending.Scenario)
				if scenario == "" {
					scenario = "动态故事板"
				}
				lines := []string{fmt.Sprintf("### %s", scenario)}
				for _, module := range pending.Modules {
					lines = append(lines, fmt.Sprintf("- %s：%d 个元素", module.Title, len(module.Elements)))
				}
				components = append(components, aigca2ui.NewComponent("details", aigca2ui.ComponentMarkdown, aigca2ui.MarkdownComp{Value: strings.Join(lines, "\n")}))
				children = append(children, "details")
			}
		}
	case "candidate_asset":
		if aggregate, err := storyboards.GetAggregate(ctx, record.Binding.StoryboardID); err == nil {
			for _, binding := range aggregate.Bindings {
				if binding.ID != record.Binding.ArtifactID {
					continue
				}
				if item, assetErr := assets.Get(ctx, binding.AssetID); assetErr == nil && item.URL != "" {
					preview := aigca2ui.ImagePreview("preview", aigca2ui.MediaPreviewComp{URL: item.URL, Title: item.Filename})
					if item.Kind == aigcasset.KindVideo {
						preview = aigca2ui.VideoPreview("preview", aigca2ui.MediaPreviewComp{URL: item.URL, Title: item.Filename})
					} else if item.Kind == aigcasset.KindAudio {
						preview = aigca2ui.AudioPreview("preview", aigca2ui.MediaPreviewComp{URL: item.URL, Title: item.Filename})
					}
					components = append(components, preview)
					children = append(children, "preview")
					data["asset"] = map[string]any{"id": item.ID, "kind": item.Kind, "url": item.URL, "filename": item.Filename, "mime_type": item.MIMEType}
				}
				break
			}
		}
	}
	components = append(components, aigca2ui.SingleChoice("decision", aigca2ui.ChoiceComp{Key: "decision", Label: "审核决定", Required: true, Options: []aigca2ui.ChoiceOption{{Value: aigcapproval.DecisionApprove, Label: "确认"}, {Value: aigcapproval.DecisionReject, Label: "拒绝"}}}))
	children = append(children, "decision")
	components = append([]aigca2ui.Component{aigca2ui.CardContainer("root", children)}, components...)
	card := aigca2ui.Card{Type: aigca2ui.CardTypeInfoCollection, Title: reviewTitle, Message: "确认后继续下一阶段；拒绝会保留当前已生效版本。", Status: string(record.Status), Root: "root", SubmitLabel: "提交",
		Components: components,
		Data:       data,
	}
	envelope := aigca2ui.ActionEnvelope{Version: aigca2ui.Version1, Actions: []aigca2ui.Action{{Type: aigca2ui.ActionAppendCard, Surface: "chat", CardID: "approval:" + record.ID, Card: &card}}}
	return publisher.Publish(ctx, aigca2ui.SSEEvent{ID: "approval:" + record.ID + ":requested", SessionID: record.SessionID, Event: aigca2ui.EventAction, Payload: envelope, CreatedAt: time.Now()})
}

func runRuntime(ctx context.Context, stop context.CancelFunc, manager *aigcsessionruntime.Manager) {
	if err := manager.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("session runtime stopped", "error", err)
		stop()
	}
}

func runApprovalRelay(ctx context.Context, stop context.CancelFunc, runtime *aigcapprovalruntime.Service) {
	if err := runtime.RunRelay(ctx, time.Second); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("approval relay stopped", "error", err)
		stop()
	}
}

func runGenerationOutbox(ctx context.Context, stop context.CancelFunc, dispatcher *aigcgeneration.OutboxDispatcher, recovery *aigcgeneration.RecoveryScheduler) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	recoveryTicker := time.NewTicker(10 * time.Second)
	defer recoveryTicker.Stop()
	for {
		if _, err := dispatcher.DispatchPending(ctx, 100); err != nil && ctx.Err() == nil {
			slog.Error("generation outbox dispatch", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-recoveryTicker.C:
			if _, err := recovery.EnqueueDue(ctx, 100); err != nil && ctx.Err() == nil {
				slog.Error("generation recovery", "error", err)
			}
		case <-ticker.C:
		}
	}
}

func runGenerationWorker(ctx context.Context, workerID int, worker *aigcgeneration.LifecycleWorker) {
	for ctx.Err() == nil {
		if _, err := worker.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("generation lifecycle iteration", "worker", workerID, "error", err)
		}
	}
}

func must(err error, message string) {
	if err != nil {
		slog.Error(message, "error", err)
		os.Exit(1)
	}
}

func stableID(prefix string, values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return prefix + "_" + hex.EncodeToString(sum[:16])
}

func processInstanceID() string {
	hostname, _ := os.Hostname()
	return stableID("instance", hostname, fmt.Sprint(os.Getpid()), fmt.Sprint(time.Now().UnixNano()))
}
