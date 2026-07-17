package planstoryboardruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodelagent"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestRuntimeProfileAndToolSchemaExcludeTrustedResources(t *testing.T) {
	if Profile != turncontext.PlanStoryboardRuntimeProfile || SourceType != "plan_storyboard_preview" {
		t.Fatalf("Profile/SourceType 漂移: %s %s", Profile, SourceType)
	}
	info, err := (&schemaOnlyStoryboardTool{}).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	schemaValue, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(schemaValue)
	for _, forbidden := range []string{"creation_spec", "project_id", "user_id", "business_command_id", "fence_token"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("Tool Schema 泄露可信字段 %q: %s", forbidden, encoded)
		}
	}
}

func TestRuntimeRejectsSameNameRelaxedToolSchema(t *testing.T) {
	drift := &schemaDriftStoryboardTool{}
	info, _ := drift.Info(context.Background())
	if _, err := NewFakeRouter().WithTools([]*schema.ToolInfo{info}); err == nil {
		t.Fatal("Fake Router 接受了同名但放宽的 Tool Schema")
	}
	claim := runtimeTestClaim(t)
	if _, err := NewReceiptTool(context.Background(), drift, newMemoryToolStore(claim.FenceToken)); err == nil {
		t.Fatal("Receipt Tool 接受了同名但放宽的 Tool Schema")
	}
}

func TestFakeRouterAndPlanningModelUseOnlyFrozenBoundaries(t *testing.T) {
	claim := runtimeTestClaim(t)
	toolInfo, _ := (&schemaOnlyStoryboardTool{}).Info(context.Background())
	router, err := NewFakeRouter().WithTools([]*schema.ToolInfo{toolInfo})
	if err != nil {
		t.Fatal(err)
	}
	ctx := turncontext.WithPlanStoryboardRuntime(context.Background(), RuntimeContextFromClaim(claim))
	message, err := router.Generate(ctx, []*schema.Message{schema.UserMessage(string(claim.IntentJSON))})
	if err != nil || len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != claim.Context.ToolCallID ||
		message.ToolCalls[0].Function.Name != planstoryboard.ToolKey || message.ToolCalls[0].Function.Arguments != string(claim.IntentJSON) {
		t.Fatalf("Fake Router 未逐字复制冻结 Intent: message=%+v err=%v", message, err)
	}
	if _, err := router.Generate(ctx, []*schema.Message{schema.ToolMessage("{}", claim.Context.ToolCallID)}); err == nil {
		t.Fatal("Fake Router 未阻止 ReturnDirectly 后二次调用")
	}

	creation := testCreationSpecContent()
	creationJSON, _ := json.Marshal(creation)
	user := "prompt_key=x\nintent_json=" + string(claim.IntentJSON) + "\nproject_json={\"title\":\"项目\"}\ncreation_spec_json=" + string(creationJSON) + "\n使用 section_N"
	planned, err := NewFakePlanningModel().Generate(context.Background(), []*schema.Message{
		schema.SystemMessage("system"), schema.UserMessage(user),
	})
	if err != nil {
		t.Fatal(err)
	}
	var candidate planstoryboard.Candidate
	decodeErr := json.Unmarshal([]byte(planned.Content), &candidate)
	content := planstoryboard.Content{
		Title: candidate.Title, Summary: candidate.Summary, Sections: candidate.Sections,
		Elements: candidate.Elements, Slots: candidate.Slots,
	}
	if decodeErr != nil || candidate.SchemaVersion != planstoryboard.CandidateSchemaVersion ||
		planstoryboard.ValidateContent(content) != nil || planstoryboard.ValidateDependencyGraph(content) != nil ||
		len(candidate.Elements) != 1 || candidate.Elements[0].SourcePhaseKey != creation.Phases[0].Key ||
		len(candidate.Slots) != 1 || candidate.Slots[0].Key != "slot_1" ||
		candidate.Slots[0].ElementKey != "element_1" || candidate.Slots[0].SlotType != "image" {
		t.Fatalf("Fake Planning Model 候选无效: %s", planned.Content)
	}
}

func TestRouterAndGraphModelReceiptsAreFirstWriteWins(t *testing.T) {
	claim := runtimeTestClaim(t)
	ctx := turncontext.WithPlanStoryboardRuntime(context.Background(), RuntimeContextFromClaim(claim))
	store := newMemoryModelStore(claim.FenceToken)
	toolInfo, _ := (&schemaOnlyStoryboardTool{}).Info(ctx)
	routerReceipt, _ := NewReceiptModel(NewFakeRouter(), store, ModelCallRouter)
	bound, err := routerReceipt.WithTools([]*schema.ToolInfo{toolInfo})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		response, runErr := bound.Generate(ctx, []*schema.Message{schema.UserMessage(string(claim.IntentJSON))})
		if runErr != nil || len(response.ToolCalls) != 1 {
			t.Fatalf("Router Receipt 重放[%d]失败: %+v %v", index, response, runErr)
		}
	}
	planningBase := &countingModel{content: `{"candidate":true}`}
	planningReceipt, _ := NewReceiptModel(planningBase, store, ModelCallGraphPlanning)
	for index := 0; index < 2; index++ {
		response, runErr := planningReceipt.Generate(ctx, []*schema.Message{schema.UserMessage("prompt")})
		if runErr != nil || response.Content != planningBase.content {
			t.Fatalf("Graph Model Receipt 重放[%d]失败: %+v %v", index, response, runErr)
		}
	}
	if planningBase.callCount() != 1 || store.freezeCount(ModelCallRouter) != 1 || store.freezeCount(ModelCallGraphPlanning) != 1 {
		t.Fatalf("Model Receipt 未 first-write-wins: planning=%d routerFreeze=%d graphFreeze=%d", planningBase.callCount(), store.freezeCount(ModelCallRouter), store.freezeCount(ModelCallGraphPlanning))
	}
}

func TestEinoRunnerReturnDirectlyEmitsExactlyOneToolResult(t *testing.T) {
	claim := runtimeTestClaim(t)
	toolStore := newMemoryToolStore(claim.FenceToken)
	receiptTool, err := NewReceiptTool(context.Background(), &failedStoryboardTool{}, toolStore)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := chatmodelagent.NewPlanStoryboard(context.Background(), NewFakeRouter(), []einotool.BaseTool{receiptTool})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewEinoRunner(context.Background(), agent)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), claim)
	if err != nil || result.Status != "failed" || result.InvocationRef.ToolCallID != claim.Context.ToolCallID {
		t.Fatalf("ReturnDirectly 单 Tool 执行失败: result=%+v err=%v", result, err)
	}
	if toolStore.freezeCount() != 1 {
		t.Fatalf("Tool Result freeze=%d want=1", toolStore.freezeCount())
	}
}

func TestReceiptToolRejectsStaleFenceBeforeCallingTool(t *testing.T) {
	claim := runtimeTestClaim(t)
	store := newMemoryToolStore(claim.FenceToken + 1)
	base := &failedStoryboardTool{}
	tool, _ := NewReceiptTool(context.Background(), base, store)
	ctx := turncontext.WithPlanStoryboardRuntime(context.Background(), RuntimeContextFromClaim(claim))
	ctx = turncontext.WithPlanStoryboardPreview(ctx, PreviewContextFromClaim(claim))
	if _, err := tool.InvokableRun(ctx, string(claim.IntentJSON)); !errors.Is(err, ErrFenceLost) {
		t.Fatalf("旧 Fence err=%v want ErrFenceLost", err)
	}
	if base.callCount() != 0 {
		t.Fatalf("旧 Fence 仍调用 Tool: %d", base.callCount())
	}
}

func TestCommandJournalFreezesPreparedCommandBeforeBusinessSave(t *testing.T) {
	claim := runtimeTestClaim(t)
	command := runtimeTestCommand(t, claim)
	store := newMemoryToolStore(claim.FenceToken)
	identity := toolReceiptIdentity(RuntimeContextFromClaim(claim))
	outerDigest := digestToolRequest(claim.Context, claim.Context.IntentDigest)
	if _, execute, err := store.ReplayOrOpenTool(context.Background(), identity, outerDigest); err != nil || !execute {
		t.Fatalf("取得 open Tool Receipt 失败: execute=%v err=%v", execute, err)
	}
	journal, _ := NewCommandJournal(store)
	ctx := turncontext.WithPlanStoryboardRuntime(context.Background(), RuntimeContextFromClaim(claim))
	if err := journal.PrepareCommand(ctx, command); err != nil {
		t.Fatalf("PrepareCommand 失败: %v", err)
	}
	snapshot, _, err := store.ReplayOrOpenTool(context.Background(), identity, outerDigest)
	if err != nil || snapshot.Stage != ToolReceiptBusinessPrepared || snapshot.PreparedCommand == nil ||
		snapshot.PreparedCommand.TrustedContext.BusinessCommandID != claim.Context.BusinessCommandID ||
		snapshot.PreparedCommand.RequestDigest != command.RequestDigest || !canonicalSHA256.MatchString(snapshot.PreparedCommandDigest) ||
		!canonicalSHA256.MatchString(snapshot.ContentDigest) {
		t.Fatalf("prepared Receipt 不完整: %+v err=%v", snapshot, err)
	}
}

func TestRecoveryQueriesThenResendsSamePreparedCommandOnce(t *testing.T) {
	claim := runtimeTestClaim(t)
	command := runtimeTestCommand(t, claim)
	snapshot := runtimePreparedSnapshot(t, claim, command, ToolReceiptBusinessUnknown)
	receipts := newMemoryToolStore(claim.FenceToken)
	receipts.seed(snapshot)
	business := &recoveryBusinessStore{queryStatus: "not_found"}
	coordinator, _ := NewRecoveryCoordinator(business, receipts, fixedClock{})
	_, checkRecovery, checkErr := validateRecoverySnapshot(claim, snapshot)
	if checkErr != nil {
		t.Fatalf("prepared snapshot 预校验失败: %v", checkErr)
	}
	checkRecovery.ResendAttempts, checkRecovery.ResendExhausted = 1, true
	if checkErr = validateReservedRecovery(command, checkRecovery); checkErr != nil {
		t.Fatalf("reserved recovery 预校验失败: %v", checkErr)
	}
	checkResult := completedRecoveryResult(command, runtimeTestResource(t, command), fixedClock{}.Now())
	if checkErr = planstoryboard.ValidateTerminalResult(checkResult, command.TrustedContext); checkErr != nil {
		t.Fatalf("recovery terminal 预校验失败: %v", checkErr)
	}
	if err := coordinator.Recover(context.Background(), claim, snapshot); err != nil {
		t.Fatalf("Recovery 失败: %v", err)
	}
	terminal, _, err := receipts.ReplayOrOpenTool(context.Background(), toolReceiptIdentity(RuntimeContextFromClaim(claim)), snapshot.RequestDigest)
	if err != nil || terminal.Stage != ToolReceiptCompleted || business.queryCount() != 1 || business.saveCount() != 1 {
		t.Fatalf("Recovery 状态异常: stage=%s query=%d save=%d err=%v", terminal.Stage, business.queryCount(), business.saveCount(), err)
	}
	if business.lastCommand().TrustedContext.BusinessCommandID != claim.Context.BusinessCommandID || business.lastCommand().RequestDigest != command.RequestDigest {
		t.Fatal("Recovery 更换了 BusinessCommandID 或 request digest")
	}
	if err := coordinator.Recover(context.Background(), claim, terminal); !errors.Is(err, ErrReceiptConflict) {
		t.Fatalf("终态不应再次 Recovery: %v", err)
	}
	if business.saveCount() != 1 {
		t.Fatalf("Recovery 超预算重发: %d", business.saveCount())
	}
}

func TestRecoveryUnknownDefersWithoutFreezingFalseTerminal(t *testing.T) {
	claim := runtimeTestClaim(t)
	command := runtimeTestCommand(t, claim)
	snapshot := runtimePreparedSnapshot(t, claim, command, ToolReceiptBusinessPrepared)
	receipts := newMemoryToolStore(claim.FenceToken)
	receipts.seed(snapshot)
	business := &recoveryBusinessStore{queryStatus: "not_found", saveErr: planstoryboard.ErrBusinessUnknownOutcome}
	coordinator, _ := NewRecoveryCoordinator(business, receipts, fixedClock{})
	if err := coordinator.Recover(context.Background(), claim, snapshot); !errors.Is(err, ErrRecoveryDeferred) {
		t.Fatalf("Unknown Outcome err=%v want ErrRecoveryDeferred", err)
	}
	current, _, _ := receipts.ReplayOrOpenTool(context.Background(), toolReceiptIdentity(RuntimeContextFromClaim(claim)), snapshot.RequestDigest)
	if current.Stage != ToolReceiptBusinessUnknown || len(current.ResultJSON) != 0 {
		t.Fatalf("Unknown Outcome 被伪造成终态: %+v", current)
	}
}

func TestRecoveryExhaustedNotFoundBecomesPermanentFailure(t *testing.T) {
	claim := runtimeTestClaim(t)
	command := runtimeTestCommand(t, claim)
	snapshot := runtimePreparedSnapshot(t, claim, command, ToolReceiptBusinessUnknown)
	_, recovery, err := validateRecoverySnapshot(claim, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	recovery.ResendAttempts, recovery.ResendExhausted = recovery.ResendLimit, true
	snapshot.Recovery = &recovery
	receipts := newMemoryToolStore(claim.FenceToken)
	receipts.seed(snapshot)
	business := &recoveryBusinessStore{queryStatus: "not_found"}
	coordinator, _ := NewRecoveryCoordinator(business, receipts, fixedClock{})
	if recoverErr := coordinator.Recover(context.Background(), claim, snapshot); !errors.Is(recoverErr, ErrRecoveryExhausted) {
		t.Fatalf("耗尽预算后的权威 not_found err=%v want ErrRecoveryExhausted", recoverErr)
	}
	if business.queryCount() != 1 || business.saveCount() != 0 {
		t.Fatalf("耗尽预算后仍重发: query=%d save=%d", business.queryCount(), business.saveCount())
	}
}

func TestProcessorReceiptFirstFrozenReplayAndPreparedDeferral(t *testing.T) {
	claim := runtimeTestClaim(t)
	command := runtimeTestCommand(t, claim)
	config := ProcessorConfig{
		Concurrency: 1, PollInterval: time.Millisecond, LeaseDuration: time.Second,
		HeartbeatInterval: 100 * time.Millisecond, RetryDelay: time.Second, RecoveryDelay: time.Second,
		ProjectionDelay: time.Second, MaxAttempts: 2,
	}
	t.Run("frozen completed bypasses runner and recovery", func(t *testing.T) {
		resource := runtimeTestResource(t, command)
		result := completedRecoveryResult(command, resource, fixedClock{}.Now())
		encoded, _ := json.Marshal(result)
		snapshot := runtimePreparedSnapshot(t, claim, command, ToolReceiptCompleted)
		snapshot.ResultJSON, snapshot.ResultDigest = encoded, digestBytes(encoded)
		if _, terminal, receiptErr := validateClaimToolReceipt(claim, snapshot); receiptErr != nil || !terminal {
			t.Fatalf("frozen receipt 预校验失败: terminal=%v err=%v", terminal, receiptErr)
		}
		projected := result
		projected.Card = cardFromPreparedResult(command, result, fixedClock{}.Now())
		if validateErr := planstoryboard.ValidateTerminalResult(projected, command.TrustedContext); validateErr != nil {
			t.Fatalf("projected card 预校验失败: %v", validateErr)
		}
		repository := &processorRepositoryStub{snapshot: snapshot}
		runner, recovery := &processorRunnerStub{}, &processorRecoveryStub{}
		processor := &Processor{repository: repository, runner: runner, recovery: recovery, clock: fixedClock{}, config: config}
		processor.process(context.Background(), claim)
		if runner.callCount() != 0 || recovery.callCount() != 0 || repository.completeCount() != 1 || repository.runtimeFailureCount() != 0 {
			t.Fatalf("frozen replay 分类错误: runner=%d recovery=%d complete=%d dead=%d", runner.callCount(), recovery.callCount(), repository.completeCount(), repository.runtimeFailureCount())
		}
		if repository.completedResult().Card == nil {
			t.Fatal("completed frozen Receipt 未从 prepared command 重建 Card")
		}
	})
	t.Run("permanent projection contract error becomes runtime failure", func(t *testing.T) {
		resource := runtimeTestResource(t, command)
		result := completedRecoveryResult(command, resource, fixedClock{}.Now())
		encoded, _ := json.Marshal(result)
		snapshot := runtimePreparedSnapshot(t, claim, command, ToolReceiptCompleted)
		snapshot.ResultJSON, snapshot.ResultDigest = encoded, digestBytes(encoded)
		repository := &processorRepositoryStub{snapshot: snapshot, completeErr: ErrOutputContract}
		processor := &Processor{
			repository: repository, runner: &processorRunnerStub{}, recovery: &processorRecoveryStub{},
			clock: fixedClock{}, config: config,
		}
		processor.process(context.Background(), claim)
		if repository.completeCount() != 1 || repository.runtimeFailureCount() != 1 ||
			repository.projectionDeferralCount() != 0 {
			t.Fatalf("确定性投影错误分类失败: complete=%d dead=%d defer=%d",
				repository.completeCount(), repository.runtimeFailureCount(), repository.projectionDeferralCount())
		}
	})
	t.Run("transient projection error only defers frozen result", func(t *testing.T) {
		resource := runtimeTestResource(t, command)
		result := completedRecoveryResult(command, resource, fixedClock{}.Now())
		encoded, _ := json.Marshal(result)
		snapshot := runtimePreparedSnapshot(t, claim, command, ToolReceiptCompleted)
		snapshot.ResultJSON, snapshot.ResultDigest = encoded, digestBytes(encoded)
		repository := &processorRepositoryStub{snapshot: snapshot, completeErr: ErrPersistence}
		processor := &Processor{
			repository: repository, runner: &processorRunnerStub{}, recovery: &processorRecoveryStub{},
			clock: fixedClock{}, config: config,
		}
		processor.process(context.Background(), claim)
		if repository.completeCount() != 1 || repository.runtimeFailureCount() != 0 ||
			repository.projectionDeferralCount() != 1 {
			t.Fatalf("暂态投影错误分类失败: complete=%d dead=%d defer=%d",
				repository.completeCount(), repository.runtimeFailureCount(), repository.projectionDeferralCount())
		}
	})
	t.Run("prepared never reruns agent or becomes dead", func(t *testing.T) {
		snapshot := runtimePreparedSnapshot(t, claim, command, ToolReceiptBusinessUnknown)
		repository := &processorRepositoryStub{snapshot: snapshot}
		runner, recovery := &processorRunnerStub{}, &processorRecoveryStub{err: ErrRecoveryDeferred}
		processor := &Processor{repository: repository, runner: runner, recovery: recovery, clock: fixedClock{}, config: config}
		processor.process(context.Background(), claim)
		if runner.callCount() != 0 || recovery.callCount() != 1 || repository.recoveryDeferralCount() != 1 || repository.runtimeFailureCount() != 0 || repository.retryCount() != 0 {
			t.Fatalf("prepared 分类错误: runner=%d recovery=%d defer=%d dead=%d retry=%d", runner.callCount(), recovery.callCount(), repository.recoveryDeferralCount(), repository.runtimeFailureCount(), repository.retryCount())
		}
	})
	t.Run("prepared permanent conflict becomes runtime failure", func(t *testing.T) {
		snapshot := runtimePreparedSnapshot(t, claim, command, ToolReceiptBusinessUnknown)
		repository := &processorRepositoryStub{snapshot: snapshot}
		runner, recovery := &processorRunnerStub{}, &processorRecoveryStub{err: ErrReceiptConflict}
		processor := &Processor{repository: repository, runner: runner, recovery: recovery, clock: fixedClock{}, config: config}
		processor.process(context.Background(), claim)
		if runner.callCount() != 0 || recovery.callCount() != 1 || repository.runtimeFailureCount() != 1 ||
			repository.recoveryDeferralCount() != 0 || repository.retryCount() != 0 {
			t.Fatalf("prepared 永久冲突分类错误: runner=%d recovery=%d dead=%d defer=%d retry=%d",
				runner.callCount(), recovery.callCount(), repository.runtimeFailureCount(),
				repository.recoveryDeferralCount(), repository.retryCount())
		}
	})
}

func TestServiceFreezesTrustedCreationSpecAndStableIDs(t *testing.T) {
	claim := runtimeTestClaim(t)
	store := &enqueueStoreStub{result: EnqueueResult{
		InputID: claim.Context.InputID, TurnID: claim.Context.TurnID, RunID: claim.Context.RunID,
		ToolCallID: claim.Context.ToolCallID, BusinessCommandID: claim.Context.BusinessCommandID,
		RouterModelCallID: claim.Context.RouterModelCallID, GraphModelCallID: claim.Context.GraphModelCallID,
		AcceptedEventID: "019f68e8-1013-7000-8000-000000000013", TerminalEventID: claim.TerminalEventID,
	}}
	wakes := 0
	service, _ := NewService(store, fixedClock{}, func() { wakes++ })
	request := EnqueueRequest{
		RequestID: claim.Context.RequestID, SessionID: claim.Context.SessionID, UserID: claim.Context.UserID,
		ProjectID: claim.Context.ProjectID, IdempotencyKey: "019f68e8-1014-7000-8000-000000000014",
		CreationSpecRef: CoreContextFromRuntime(RuntimeContextFromClaim(claim)).CreationSpecRef,
		IntentJSON:      claim.IntentJSON, AccessScopeRef: claim.Context.AccessScopeRef,
		AccessScopeDigest: claim.Context.AccessScopeDigest, IntentKeyVersion: claim.Context.IntentKeyVersion,
	}
	response, err := service.Enqueue(context.Background(), request)
	if err != nil || wakes != 1 || response.Status != EnqueuePendingStatus || store.command.CreationSpecRef != request.CreationSpecRef {
		t.Fatalf("Enqueue 异常: response=%+v command=%+v wakes=%d err=%v", response, store.command, wakes, err)
	}
	request.CreationSpecRef.ID = ""
	if _, err := service.Enqueue(context.Background(), request); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法可信 CreationSpec err=%v want ErrInvalidInput", err)
	}
}

func runtimeTestClaim(t *testing.T) Claim {
	t.Helper()
	intent, err := DecodeIntent([]byte(`{"schema_version":"plan_storyboard.preview.intent.v1","planning_instruction":"规划夏日品牌短片","target_duration_seconds":30}`))
	if err != nil {
		t.Fatal(err)
	}
	pins := ApprovedPins()
	ctx := turncontext.PlanStoryboardTurnContext{
		SchemaVersion: turncontext.PlanStoryboardTurnContextSchemaVersion, Profile: Profile,
		RequestID: "019f68e8-1001-7000-8000-000000000001", SessionID: "019f68e8-1002-7000-8000-000000000002",
		InputID: "019f68e8-1003-7000-8000-000000000003", TurnID: "019f68e8-1004-7000-8000-000000000004",
		RunID: "019f68e8-1005-7000-8000-000000000005", ToolCallID: "019f68e8-1006-7000-8000-000000000006",
		BusinessCommandID: "019f68e8-1007-7000-8000-000000000007", RouterModelCallID: "019f68e8-1008-7000-8000-000000000008",
		GraphModelCallID: "019f68e8-1009-7000-8000-000000000009", UserID: "019f68e8-1010-7000-8000-000000000010",
		ProjectID: "019f68e8-1011-7000-8000-000000000011", IntentKeyVersion: "local-test", IntentDigest: intent.Digest,
		CreationSpecID: "019f68e8-1012-7000-8000-000000000012", CreationSpecVersion: 1,
		CreationSpecContentDigest: strings.Repeat("c", 64), AccessScopeRef: "scope:storyboard",
		AccessScopeDigest: strings.Repeat("a", 64), ToolRegistryRef: pins.ToolRegistryRef,
		ToolRegistryDigest: pins.ToolRegistryDigest, ToolDefinitionRef: pins.ToolDefinitionRef,
		ToolDefinitionDigest: pins.ToolDefinitionDigest, IntentSchemaRef: planstoryboard.IntentSchemaVersion,
		CandidateSchemaRef: planstoryboard.CandidateSchemaVersion, ResultSchemaRef: planstoryboard.ResultSchemaVersion,
		PromptRef: pins.PromptRef, PromptDigest: pins.PromptDigest, ValidatorRef: pins.ValidatorRef,
		ValidatorDigest: pins.ValidatorDigest, DAGValidatorRef: pins.DAGValidatorRef,
		DAGValidatorDigest: pins.DAGValidatorDigest, RouterModelRouteRef: pins.RouterModelRouteRef,
		RouterModelRouteDigest: pins.RouterModelRouteDigest, PlanningModelRouteRef: pins.PlanningModelRouteRef,
		PlanningModelRouteDigest: pins.PlanningModelRouteDigest, RuntimePolicyRef: pins.RuntimePolicyRef,
		RuntimePolicyDigest: pins.RuntimePolicyDigest, BudgetRef: pins.BudgetRef, BudgetDigest: pins.BudgetDigest,
	}
	ctx.ContextDigest, _ = DigestTurnContext(ctx)
	claim := Claim{
		Owner: "storyboard-preview/worker-1", FenceToken: 3, Attempts: 1, EnqueueSeq: 1,
		TerminalEventID: "019f68e8-1015-7000-8000-000000000015", IntentJSON: intent.JSON, Context: ctx,
	}
	if err := ValidateClaim(claim); err != nil {
		t.Fatalf("测试 Claim 非法: %v", err)
	}
	return claim
}

func runtimeTestCommand(t *testing.T, claim Claim) planstoryboard.DraftCommand {
	t.Helper()
	content := planstoryboard.Content{
		Title: "夏日品牌短片", Summary: "单场景开发预览故事板。",
		Sections: []planstoryboard.Section{{Key: "section_1", Title: "主体", Objective: "建立夏日氛围"}},
		Elements: []planstoryboard.Element{{
			Key: "element_1", SectionKey: "section_1", Order: 1, ElementType: "scene", Title: "开场",
			NarrativePurpose: "建立氛围", DurationSeconds: 30, SourcePhaseKey: "phase_1", DependencyKeys: []string{},
		}},
		Slots: []planstoryboard.Slot{},
	}
	command := planstoryboard.DraftCommand{
		TrustedContext: CoreContextFromRuntime(RuntimeContextFromClaim(claim)),
		DomainContext:  planstoryboard.PlanningContext{ProjectID: claim.Context.ProjectID, ProjectVersion: 1},
		Content:        content,
	}
	command.RequestDigest, _ = planstoryboard.SaveRequestDigest(command)
	return command
}

func runtimePreparedSnapshot(t *testing.T, claim Claim, command planstoryboard.DraftCommand, stage ToolReceiptStage) ToolReceiptSnapshot {
	t.Helper()
	commandDigest, err := digestPreparedCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	contentDigest, err := planstoryboard.ContentDigest(command.Content)
	if err != nil {
		t.Fatal(err)
	}
	return ToolReceiptSnapshot{
		Stage: stage, RequestDigest: digestToolRequest(claim.Context, claim.Context.IntentDigest),
		PreparedCommand: &command, PreparedCommandDigest: commandDigest, ContentDigest: contentDigest,
	}
}

func runtimeTestResource(t *testing.T, command planstoryboard.DraftCommand) planstoryboard.Resource {
	t.Helper()
	digest, err := planstoryboard.ContentDigest(command.Content)
	if err != nil {
		t.Fatal(err)
	}
	return planstoryboard.Resource{
		StoryboardPreviewID: "019f68e8-1016-7000-8000-000000000016", ProjectID: command.TrustedContext.ProjectID,
		CreationSpecRef: command.TrustedContext.CreationSpecRef, Version: 1, Status: "draft",
		ContentDigest: digest, Content: command.Content,
	}
}

func testCreationSpecContent() planstoryboard.CreationSpecContent {
	return planstoryboard.CreationSpecContent{
		Title: "夏日品牌短片", Goal: "建立轻快夏日氛围", DeliverableType: "video", Audience: "年轻用户", Locale: "zh-CN",
		Phases:      []planstoryboard.CreationSpecPhase{{Key: "phase_1", Title: "开场", Objective: "建立氛围", Output: "开场段落"}},
		Constraints: []string{}, AcceptanceCriteria: []string{"叙事完整"},
	}
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC) }

type schemaOnlyStoryboardTool struct{}

func (*schemaOnlyStoryboardTool) Info(context.Context) (*schema.ToolInfo, error) {
	return (&planstoryboard.Tool{}).Info(context.Background())
}
func (*schemaOnlyStoryboardTool) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return "", errors.New("unexpected")
}

type schemaDriftStoryboardTool struct{}

func (*schemaDriftStoryboardTool) Info(context.Context) (*schema.ToolInfo, error) {
	canonical, _ := planstoryboard.CanonicalToolInfo(context.Background())
	return &schema.ToolInfo{
		Name: planstoryboard.ToolKey, Desc: canonical.Desc,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"schema_version": {Type: schema.String, Required: true},
		}),
	}, nil
}
func (*schemaDriftStoryboardTool) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return "", errors.New("unexpected")
}

type failedStoryboardTool struct {
	mu    sync.Mutex
	calls int
}

func (*failedStoryboardTool) Info(context.Context) (*schema.ToolInfo, error) {
	return (&planstoryboard.Tool{}).Info(context.Background())
}
func (tool *failedStoryboardTool) InvokableRun(ctx context.Context, _ string, _ ...einotool.Option) (string, error) {
	tool.mu.Lock()
	tool.calls++
	tool.mu.Unlock()
	trusted, _ := turncontext.PlanStoryboardRuntimeFrom(ctx)
	retryable := false
	result := planstoryboard.Result{
		SchemaVersion: planstoryboard.ResultSchemaVersion, Status: "failed",
		ResultCode:    planstoryboard.ResultCodeCandidateInvalid,
		InvocationRef: planstoryboard.InvocationRef{ToolCallID: trusted.Context.ToolCallID, BusinessCommandID: trusted.Context.BusinessCommandID},
		Summary:       "模型候选不符合 Storyboard 预览协议。", Retryable: &retryable,
	}
	encoded, _ := json.Marshal(result)
	return string(encoded), nil
}
func (tool *failedStoryboardTool) callCount() int {
	tool.mu.Lock()
	defer tool.mu.Unlock()
	return tool.calls
}

type countingModel struct {
	mu      sync.Mutex
	content string
	calls   int
}

func (m *countingModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return schema.AssistantMessage(m.content, nil), nil
}
func (m *countingModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}
func (m *countingModel) callCount() int { m.mu.Lock(); defer m.mu.Unlock(); return m.calls }

type modelReceiptRecord struct {
	digest   string
	stage    ModelReceiptStage
	response *schema.Message
	freezes  int
}
type memoryModelStore struct {
	mu            sync.Mutex
	expectedFence int64
	records       map[ModelCallKind]*modelReceiptRecord
}

func newMemoryModelStore(fence int64) *memoryModelStore {
	return &memoryModelStore{expectedFence: fence, records: make(map[ModelCallKind]*modelReceiptRecord)}
}
func (store *memoryModelStore) ReplayOrReserveModel(_ context.Context, identity ModelReceiptIdentity, digest string) (ModelReceiptSnapshot, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if identity.FenceToken != store.expectedFence {
		return ModelReceiptSnapshot{}, false, ErrFenceLost
	}
	record := store.records[identity.CallKind]
	if record == nil {
		store.records[identity.CallKind] = &modelReceiptRecord{digest: digest, stage: ModelReceiptReserved}
		return ModelReceiptSnapshot{Stage: ModelReceiptReserved}, true, nil
	}
	if record.digest != digest {
		return ModelReceiptSnapshot{}, false, ErrReceiptConflict
	}
	return ModelReceiptSnapshot{Stage: record.stage, Response: cloneMessage(record.response)}, false, nil
}
func (store *memoryModelStore) FreezeModelCompleted(_ context.Context, identity ModelReceiptIdentity, digest string, response *schema.Message) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	record := store.records[identity.CallKind]
	if identity.FenceToken != store.expectedFence {
		return ErrFenceLost
	}
	if record == nil || record.digest != digest {
		return ErrReceiptConflict
	}
	if record.stage == ModelReceiptReserved {
		record.stage, record.response = ModelReceiptCompleted, cloneMessage(response)
		record.freezes++
	}
	return nil
}
func (*memoryModelStore) FreezeModelFailed(context.Context, ModelReceiptIdentity, string, string) error {
	return errors.New("unexpected")
}
func (store *memoryModelStore) freezeCount(kind ModelCallKind) int {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.records[kind] == nil {
		return 0
	}
	return store.records[kind].freezes
}

type memoryToolStore struct {
	mu            sync.Mutex
	expectedFence int64
	snapshot      ToolReceiptSnapshot
	freezes       int
}

func newMemoryToolStore(fence int64) *memoryToolStore { return &memoryToolStore{expectedFence: fence} }
func (store *memoryToolStore) seed(snapshot ToolReceiptSnapshot) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.snapshot = cloneToolSnapshot(snapshot)
}
func (store *memoryToolStore) ReplayOrOpenTool(_ context.Context, identity ToolReceiptIdentity, digest string) (ToolReceiptSnapshot, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if identity.FenceToken != store.expectedFence {
		return ToolReceiptSnapshot{}, false, ErrFenceLost
	}
	if store.snapshot.Stage == "" {
		store.snapshot = ToolReceiptSnapshot{Stage: ToolReceiptOpen, RequestDigest: digest}
		return cloneToolSnapshot(store.snapshot), true, nil
	}
	if store.snapshot.RequestDigest != digest {
		return ToolReceiptSnapshot{}, false, ErrReceiptConflict
	}
	return cloneToolSnapshot(store.snapshot), false, nil
}
func (store *memoryToolStore) PrepareToolCommand(_ context.Context, identity ToolReceiptIdentity, outer string, command planstoryboard.DraftCommand, commandDigest, contentDigest string, resendLimit int) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if identity.FenceToken != store.expectedFence {
		return ErrFenceLost
	}
	store.snapshot = ToolReceiptSnapshot{Stage: ToolReceiptBusinessPrepared, RequestDigest: outer, PreparedCommand: &command, PreparedCommandDigest: commandDigest, ContentDigest: contentDigest}
	return nil
}
func (store *memoryToolStore) MarkToolBusinessUnknown(_ context.Context, identity ToolReceiptIdentity, digest string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if identity.FenceToken != store.expectedFence {
		return ErrFenceLost
	}
	if store.snapshot.RequestDigest != digest {
		return ErrReceiptConflict
	}
	store.snapshot.Stage = ToolReceiptBusinessUnknown
	return nil
}
func (store *memoryToolStore) ReserveToolCommandResend(_ context.Context, identity ToolReceiptIdentity, digest string, recovery planstoryboard.RecoveryDeferred) (planstoryboard.RecoveryDeferred, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if identity.FenceToken != store.expectedFence {
		return planstoryboard.RecoveryDeferred{}, false, ErrFenceLost
	}
	if store.snapshot.RequestDigest != digest {
		return planstoryboard.RecoveryDeferred{}, false, ErrReceiptConflict
	}
	if store.snapshot.Recovery != nil {
		recovery = *store.snapshot.Recovery
	}
	if recovery.ResendAttempts >= recovery.ResendLimit {
		return recovery, false, nil
	}
	recovery.ResendAttempts++
	recovery.ResendExhausted = recovery.ResendAttempts >= recovery.ResendLimit
	store.snapshot.Recovery = &recovery
	return recovery, true, nil
}
func (store *memoryToolStore) FreezeToolResult(_ context.Context, identity ToolReceiptIdentity, digest string, stage ToolReceiptStage, encoded []byte, resultDigest string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if identity.FenceToken != store.expectedFence {
		return ErrFenceLost
	}
	if store.snapshot.RequestDigest != digest {
		return ErrReceiptConflict
	}
	if store.snapshot.Stage != ToolReceiptCompleted && store.snapshot.Stage != ToolReceiptFailed {
		store.snapshot.Stage, store.snapshot.ResultJSON, store.snapshot.ResultDigest = stage, append([]byte(nil), encoded...), resultDigest
		store.freezes++
	}
	return nil
}
func (store *memoryToolStore) freezeCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.freezes
}

func cloneToolSnapshot(snapshot ToolReceiptSnapshot) ToolReceiptSnapshot {
	clone := snapshot
	clone.ResultJSON = append([]byte(nil), snapshot.ResultJSON...)
	if snapshot.PreparedCommand != nil {
		command := *snapshot.PreparedCommand
		clone.PreparedCommand = &command
	}
	if snapshot.Recovery != nil {
		recovery := *snapshot.Recovery
		clone.Recovery = &recovery
	}
	return clone
}

type recoveryBusinessStore struct {
	mu          sync.Mutex
	queryStatus string
	saveErr     error
	queries     int
	saves       int
	last        planstoryboard.DraftCommand
}

func (store *recoveryBusinessStore) QueryStoryboardDraftCommand(context.Context, planstoryboard.DraftCommand) (string, *planstoryboard.Resource, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.queries++
	return store.queryStatus, nil, nil
}
func (store *recoveryBusinessStore) SaveStoryboardDraft(_ context.Context, command planstoryboard.DraftCommand) (planstoryboard.SaveDisposition, planstoryboard.Resource, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.saves++
	store.last = command
	if store.saveErr != nil {
		return "", planstoryboard.Resource{}, store.saveErr
	}
	digest, _ := planstoryboard.ContentDigest(command.Content)
	return planstoryboard.SaveDispositionCreated, planstoryboard.Resource{
		StoryboardPreviewID: "019f68e8-1016-7000-8000-000000000016", ProjectID: command.TrustedContext.ProjectID,
		CreationSpecRef: command.TrustedContext.CreationSpecRef, Version: 1, Status: "draft",
		ContentDigest: digest, Content: command.Content,
	}, nil
}
func (store *recoveryBusinessStore) queryCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.queries
}
func (store *recoveryBusinessStore) saveCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.saves
}
func (store *recoveryBusinessStore) lastCommand() planstoryboard.DraftCommand {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.last
}

type processorRunnerStub struct {
	mu    sync.Mutex
	calls int
}

func (runner *processorRunnerStub) Run(context.Context, Claim) (planstoryboard.Result, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.calls++
	return planstoryboard.Result{}, errors.New("unexpected")
}
func (runner *processorRunnerStub) callCount() int {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.calls
}

type processorRecoveryStub struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (recovery *processorRecoveryStub) Recover(context.Context, Claim, ToolReceiptSnapshot) error {
	recovery.mu.Lock()
	defer recovery.mu.Unlock()
	recovery.calls++
	return recovery.err
}
func (recovery *processorRecoveryStub) callCount() int {
	recovery.mu.Lock()
	defer recovery.mu.Unlock()
	return recovery.calls
}

type processorRepositoryStub struct {
	mu               sync.Mutex
	snapshot         ToolReceiptSnapshot
	completed        planstoryboard.Result
	completes        int
	completeErr      error
	runtimeFailures  int
	retries          int
	recoveryDefers   int
	projectionDefers int
}

func (*processorRepositoryStub) ClaimNext(context.Context, string, time.Time, time.Duration) (*Claim, error) {
	return nil, nil
}
func (*processorRepositoryStub) MarkRunning(context.Context, Claim, time.Time) error { return nil }
func (*processorRepositoryStub) RenewLease(context.Context, Claim, time.Time, time.Duration) error {
	return nil
}
func (repository *processorRepositoryStub) LoadToolReceipt(context.Context, Claim) (ToolReceiptSnapshot, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return cloneToolSnapshot(repository.snapshot), nil
}
func (repository *processorRepositoryStub) CompleteToolResult(_ context.Context, _ Claim, result planstoryboard.Result, _ time.Time) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.completed, repository.completes = result, repository.completes+1
	return repository.completeErr
}
func (repository *processorRepositoryStub) CompleteRuntimeFailure(context.Context, Claim, RuntimeFailure, time.Time) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.runtimeFailures++
	return nil
}
func (repository *processorRepositoryStub) RetryExecution(context.Context, Claim, time.Time) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.retries++
	return nil
}
func (repository *processorRepositoryStub) DeferRecovery(context.Context, Claim, time.Time) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.recoveryDefers++
	return nil
}
func (repository *processorRepositoryStub) DeferProjection(context.Context, Claim, time.Time) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.projectionDefers++
	return nil
}
func (repository *processorRepositoryStub) completeCount() int {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.completes
}
func (repository *processorRepositoryStub) completedResult() planstoryboard.Result {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.completed
}
func (repository *processorRepositoryStub) runtimeFailureCount() int {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.runtimeFailures
}
func (repository *processorRepositoryStub) retryCount() int {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.retries
}
func (repository *processorRepositoryStub) recoveryDeferralCount() int {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.recoveryDefers
}
func (repository *processorRepositoryStub) projectionDeferralCount() int {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.projectionDefers
}

type enqueueStoreStub struct {
	command EnqueueCommand
	result  EnqueueResult
}

func (store *enqueueStoreStub) Enqueue(_ context.Context, command EnqueueCommand, _ time.Time) (EnqueueResult, error) {
	store.command = command
	return store.result, nil
}

var _ model.BaseChatModel = (*countingModel)(nil)
var _ ModelReceiptStore = (*memoryModelStore)(nil)
var _ ToolReceiptStore = (*memoryToolStore)(nil)
var _ planstoryboard.BusinessDraftStore = (*recoveryBusinessStore)(nil)
var _ Runner = (*processorRunnerStub)(nil)
var _ Recovery = (*processorRecoveryStub)(nil)
var _ Repository = (*processorRepositoryStub)(nil)
var _ EnqueueStore = (*enqueueStoreStub)(nil)
