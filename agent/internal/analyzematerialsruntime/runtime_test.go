package analyzematerialsruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestDecodeIntentRequiresExpectedAssetsAndCanonicalizesSets(t *testing.T) {
	without := `{"schema_version":"analyze_materials.preview.intent.v1","asset_ids":["019f68e8-7501-7000-8000-000000000001"],"analysis_goal":"分析","focus_dimensions":["visual"],"output_language":"zh-CN"}`
	if _, err := DecodeIntent([]byte(without)); err == nil {
		t.Fatal("缺失 expected_assets 未失败关闭")
	}
	claim := runtimeTestClaim(t)
	decoded, err := ValidateCanonicalIntent(claim.IntentJSON, claim.Context.IntentDigest)
	if err != nil || len(decoded.Value.ExpectedAssets) != 1 {
		t.Fatalf("canonical Intent 异常: %+v err=%v", decoded, err)
	}
}

func TestModelReceiptSeparatesAndReplaysFirstWrite(t *testing.T) {
	claim := runtimeTestClaim(t)
	base := &countingBaseModel{}
	store := &memoryModelStore{}
	receipt, _ := NewReceiptModel(base, store, ModelCallGraphAnalysis)
	ctx := turncontext.WithMaterialAnalysisRuntime(context.Background(), RuntimeContextFromClaim(claim))
	for index := 0; index < 2; index++ {
		message, err := receipt.Generate(ctx, []*schema.Message{schema.UserMessage("prompt")})
		if err != nil || message.Content != "candidate" {
			t.Fatalf("模型回执重放[%d]失败: %v", index, err)
		}
	}
	if base.calls != 1 || store.freezes != 1 {
		t.Fatalf("模型未 first-write-wins: calls=%d freezes=%d", base.calls, store.freezes)
	}
}

func TestToolReceiptFreezesSemanticFailedAndReplays(t *testing.T) {
	claim := runtimeTestClaim(t)
	base := &failedToolStub{}
	store := &memoryToolStore{}
	tool, err := NewReceiptTool(context.Background(), base, store)
	if err != nil {
		t.Fatalf("创建 Tool Receipt 失败: %v", err)
	}
	ctx := turncontext.WithMaterialAnalysisRuntime(context.Background(), RuntimeContextFromClaim(claim))
	ctx = turncontext.WithMaterialAnalysisPreview(ctx, CoreContextFromClaim(claim))
	for index := 0; index < 2; index++ {
		resultJSON, runErr := tool.InvokableRun(ctx, string(claim.IntentJSON))
		if runErr != nil || !strings.Contains(resultJSON, analyzematerials.ResultCodeDependencyNotReady) {
			t.Fatalf("Tool 回执重放[%d]失败: %s %v", index, resultJSON, runErr)
		}
	}
	if base.calls != 1 || store.freezes != 1 || store.stage != ToolReceiptFailed {
		t.Fatalf("Tool 未 first-write-wins: calls=%d freezes=%d stage=%s", base.calls, store.freezes, store.stage)
	}
}

func TestServiceValidatesAndPassesStableRequestID(t *testing.T) {
	claim := runtimeTestClaim(t)
	store := &enqueueStoreStub{result: EnqueueResult{
		InputID: claim.Context.InputID, TurnID: claim.Context.TurnID, RunID: claim.Context.RunID,
		ToolCallID: claim.Context.ToolCallID, RouterModelCallID: claim.RouterModelCallID,
		GraphModelCallID: claim.GraphModelCallID, AcceptedEventID: "019f68e8-7512-7000-8000-000000000012",
		TerminalEventID: claim.TerminalEventID,
	}}
	wakes := 0
	service, err := NewService(store, fixedClock{}, func() { wakes++ })
	if err != nil {
		t.Fatalf("创建 Enqueue Service 失败: %v", err)
	}
	requestID := "019f68e8-7513-7000-8000-000000000013"
	request := EnqueueRequest{
		RequestID: requestID, SessionID: claim.Context.SessionID, UserID: claim.Context.UserID,
		ProjectID: claim.Context.ProjectID, IdempotencyKey: "019f68e8-7514-7000-8000-000000000014",
		IntentJSON: claim.IntentJSON, AccessScopeRef: claim.Context.AccessScopeRef,
		AccessScopeDigest: claim.Context.AccessScopeDigest, IntentKeyVersion: claim.Context.IntentKeyVersion,
	}
	response, err := service.Enqueue(context.Background(), request)
	if err != nil || store.command.RequestID != requestID || wakes != 1 || response.SchemaVersion != EnqueueResponseSchemaVersion {
		t.Fatalf("RequestID 入队异常: command=%+v response=%+v wakes=%d err=%v", store.command, response, wakes, err)
	}
	request.RequestID = "not-uuidv7"
	if _, err := service.Enqueue(context.Background(), request); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法 RequestID err=%v want ErrInvalidInput", err)
	}
	request.RequestID = requestID
	request.IntentJSON = []byte(`{}`)
	if _, err := service.Enqueue(context.Background(), request); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法 Intent err=%v want ErrInvalidInput", err)
	}
}

func TestProcessorOnlyDefersProjectionForKnownFrozenResult(t *testing.T) {
	claim := runtimeTestClaim(t)
	config := ProcessorConfig{RetryDelay: time.Second, ProjectionDelay: time.Second, MaxAttempts: 2}
	t.Run("pre-execution load error retries execution", func(t *testing.T) {
		repository := &processorRepositoryStub{loadErr: ErrPersistence}
		processor := &Processor{repository: repository, runner: &processorRunnerStub{}, clock: fixedClock{}, config: config}
		processor.process(context.Background(), claim)
		if repository.retries != 1 || repository.defers != 0 || repository.runtimeFailures != 0 {
			t.Fatalf("未冻结读取失败分类错误: %+v", repository)
		}
	})
	t.Run("receipt conflict becomes runtime failure", func(t *testing.T) {
		repository := &processorRepositoryStub{snapshot: ToolReceiptSnapshot{Stage: ToolReceiptCompleted, ResultJSON: []byte(`{}`), ResultDigest: strings.Repeat("a", 64)}}
		processor := &Processor{repository: repository, runner: &processorRunnerStub{}, clock: fixedClock{}, config: config}
		processor.process(context.Background(), claim)
		if repository.runtimeFailures != 1 || repository.defers != 0 || repository.retries != 0 {
			t.Fatalf("回执冲突分类错误: %+v", repository)
		}
	})
	t.Run("known frozen completion failure defers projection", func(t *testing.T) {
		retryable := false
		result := analyzematerials.Result{SchemaVersion: analyzematerials.ResultSchemaVersion, Status: "failed", ResultCode: analyzematerials.ResultCodeDependencyNotReady, InvocationRef: analyzematerials.InvocationRef{ToolCallID: claim.Context.ToolCallID}, Summary: "素材证据尚不足以生成可信分析", Retryable: &retryable}
		encoded, _ := json.Marshal(result)
		repository := &processorRepositoryStub{snapshot: ToolReceiptSnapshot{Stage: ToolReceiptFailed, ResultJSON: encoded, ResultDigest: digestBytes(encoded)}, completeErr: ErrPersistence}
		processor := &Processor{repository: repository, runner: &processorRunnerStub{}, clock: fixedClock{}, config: config}
		processor.process(context.Background(), claim)
		if repository.defers != 1 || repository.retries != 0 || repository.runtimeFailures != 0 {
			t.Fatalf("冻结投影失败分类错误: %+v", repository)
		}
	})
}

func runtimeTestClaim(t *testing.T) Claim {
	t.Helper()
	intent, err := DecodeIntent([]byte(`{"schema_version":"analyze_materials.preview.intent.v1","asset_ids":["019f68e8-7501-7000-8000-000000000001"],"analysis_goal":"分析素材","focus_dimensions":["visual"],"output_language":"zh-CN","expected_assets":[{"asset_id":"019f68e8-7501-7000-8000-000000000001","asset_version":1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	pins := ApprovedPins()
	ctx := turncontext.MaterialAnalysisTurnContext{SchemaVersion: turncontext.MaterialAnalysisTurnContextSchemaVersion, Profile: Profile, SessionID: "019f68e8-7502-7000-8000-000000000002", InputID: "019f68e8-7503-7000-8000-000000000003", TurnID: "019f68e8-7504-7000-8000-000000000004", RunID: "019f68e8-7505-7000-8000-000000000005", ToolCallID: "019f68e8-7506-7000-8000-000000000006", UserID: "019f68e8-7507-7000-8000-000000000007", ProjectID: "019f68e8-7508-7000-8000-000000000008", IntentKeyVersion: "local-test", IntentDigest: intent.Digest, AccessScopeRef: "scope:test", AccessScopeDigest: strings.Repeat("a", 64), ToolRegistryRef: pins.ToolRegistryRef, ToolRegistryDigest: pins.ToolRegistryDigest, ToolDefinitionRef: pins.ToolDefinitionRef, ToolDefinitionDigest: pins.ToolDefinitionDigest, IntentSchemaRef: analyzematerials.IntentSchemaVersion, ResultSchemaRef: analyzematerials.ResultSchemaVersion, PromptRef: pins.PromptRef, PromptDigest: pins.PromptDigest, ValidatorRef: pins.ValidatorRef, ValidatorDigest: pins.ValidatorDigest, EvidencePolicyRef: pins.EvidencePolicyRef, EvidencePolicyDigest: pins.EvidencePolicyDigest, RouterModelRouteRef: pins.RouterModelRouteRef, RouterModelRouteDigest: pins.RouterModelRouteDigest, AnalysisModelRouteRef: pins.AnalysisModelRouteRef, AnalysisModelRouteDigest: pins.AnalysisModelRouteDigest, RuntimePolicyRef: pins.RuntimePolicyRef, RuntimePolicyDigest: pins.RuntimePolicyDigest, BudgetRef: pins.BudgetRef, BudgetDigest: pins.BudgetDigest}
	ctx.ContextDigest, _ = DigestTurnContext(ctx)
	claim := Claim{Owner: "test-owner", FenceToken: 2, Attempts: 1, EnqueueSeq: 1, RouterModelCallID: "019f68e8-7509-7000-8000-000000000009", GraphModelCallID: "019f68e8-7510-7000-8000-000000000010", TerminalEventID: "019f68e8-7511-7000-8000-000000000011", IntentJSON: intent.JSON, Context: ctx}
	if err := ValidateClaim(claim); err != nil {
		t.Fatalf("测试 Claim 非法: %v", err)
	}
	return claim
}

type countingBaseModel struct{ calls int }

func (m *countingBaseModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.calls++
	return schema.AssistantMessage("candidate", nil), nil
}
func (m *countingBaseModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	v, e := m.Generate(ctx, in, opts...)
	if e != nil {
		return nil, e
	}
	return schema.StreamReaderFromArray([]*schema.Message{v}), nil
}

type memoryModelStore struct {
	sync.Mutex
	digest   string
	response *schema.Message
	freezes  int
}

func (s *memoryModelStore) ReplayOrReserveModel(_ context.Context, _ ModelReceiptIdentity, d string) (ModelReceiptSnapshot, bool, error) {
	s.Lock()
	defer s.Unlock()
	if s.digest == "" {
		s.digest = d
		return ModelReceiptSnapshot{Stage: ModelReceiptReserved}, true, nil
	}
	if s.digest != d {
		return ModelReceiptSnapshot{}, false, errors.New("conflict")
	}
	if s.response == nil {
		return ModelReceiptSnapshot{Stage: ModelReceiptReserved}, false, nil
	}
	return ModelReceiptSnapshot{Stage: ModelReceiptCompleted, Response: cloneMessage(s.response)}, false, nil
}
func (s *memoryModelStore) FreezeModelCompleted(_ context.Context, _ ModelReceiptIdentity, d string, m *schema.Message) error {
	s.Lock()
	defer s.Unlock()
	if d != s.digest {
		return errors.New("conflict")
	}
	if s.response == nil {
		s.response = cloneMessage(m)
		s.freezes++
	}
	return nil
}
func (*memoryModelStore) FreezeModelFailed(context.Context, ModelReceiptIdentity, string, string) error {
	return errors.New("unexpected")
}

type failedToolStub struct{ calls int }

func (*failedToolStub) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: analyzematerials.ToolKey, ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"schema_version": {Type: schema.String}})}, nil
}
func (t *failedToolStub) InvokableRun(ctx context.Context, _ string, _ ...einotool.Option) (string, error) {
	t.calls++
	trusted, _ := turncontext.MaterialAnalysisPreviewFrom(ctx)
	retry := false
	value := analyzematerials.Result{SchemaVersion: analyzematerials.ResultSchemaVersion, Status: "failed", ResultCode: analyzematerials.ResultCodeDependencyNotReady, InvocationRef: analyzematerials.InvocationRef{ToolCallID: trusted.ToolCallID}, Summary: "素材证据尚不足以生成可信分析", Retryable: &retry}
	encoded, _ := json.Marshal(value)
	return string(encoded), nil
}

type memoryToolStore struct {
	digest       string
	stage        ToolReceiptStage
	result       []byte
	resultDigest string
	freezes      int
}

func (s *memoryToolStore) ReplayOrOpenTool(_ context.Context, _ ToolReceiptIdentity, d string) (ToolReceiptSnapshot, bool, error) {
	if s.digest == "" {
		s.digest = d
		s.stage = ToolReceiptOpen
		return ToolReceiptSnapshot{Stage: ToolReceiptOpen, RequestDigest: d}, true, nil
	}
	if d != s.digest {
		return ToolReceiptSnapshot{}, false, errors.New("conflict")
	}
	return ToolReceiptSnapshot{Stage: s.stage, RequestDigest: d, ResultJSON: append([]byte(nil), s.result...), ResultDigest: s.resultDigest}, false, nil
}
func (s *memoryToolStore) FreezeToolResult(_ context.Context, _ ToolReceiptIdentity, d string, stage ToolReceiptStage, result []byte, resultDigest string) error {
	if d != s.digest {
		return errors.New("conflict")
	}
	if s.stage == ToolReceiptOpen {
		s.stage = stage
		s.result = append([]byte(nil), result...)
		s.resultDigest = resultDigest
		s.freezes++
	}
	return nil
}

var _ model.BaseChatModel = (*countingBaseModel)(nil)
var _ ModelReceiptStore = (*memoryModelStore)(nil)
var _ einotool.InvokableTool = (*failedToolStub)(nil)
var _ ToolReceiptStore = (*memoryToolStore)(nil)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Unix(1, 0) }

type enqueueStoreStub struct {
	command EnqueueCommand
	result  EnqueueResult
}

func (s *enqueueStoreStub) Enqueue(_ context.Context, command EnqueueCommand, _ time.Time) (EnqueueResult, error) {
	s.command = command
	return s.result, nil
}

var _ EnqueueStore = (*enqueueStoreStub)(nil)

type processorRunnerStub struct{ calls int }

func (r *processorRunnerStub) Run(context.Context, Claim) (analyzematerials.Result, error) {
	r.calls++
	return analyzematerials.Result{}, errors.New("unexpected runner call")
}

type processorRepositoryStub struct {
	snapshot        ToolReceiptSnapshot
	loadErr         error
	completeErr     error
	retries         int
	defers          int
	runtimeFailures int
}

func (*processorRepositoryStub) ClaimNext(context.Context, string, time.Time, time.Duration) (*Claim, error) {
	return nil, nil
}
func (*processorRepositoryStub) MarkRunning(context.Context, Claim, time.Time) error { return nil }
func (*processorRepositoryStub) RenewLease(context.Context, Claim, time.Time, time.Duration) error {
	return nil
}
func (s *processorRepositoryStub) LoadToolReceipt(context.Context, Claim) (ToolReceiptSnapshot, error) {
	return s.snapshot, s.loadErr
}
func (s *processorRepositoryStub) CompleteToolResult(context.Context, Claim, analyzematerials.Result, time.Time) error {
	return s.completeErr
}
func (s *processorRepositoryStub) CompleteRuntimeFailure(context.Context, Claim, RuntimeFailure, time.Time) error {
	s.runtimeFailures++
	return nil
}
func (s *processorRepositoryStub) RetryExecution(context.Context, Claim, time.Time) error {
	s.retries++
	return nil
}
func (s *processorRepositoryStub) DeferProjection(context.Context, Claim, time.Time) error {
	s.defers++
	return nil
}

var _ Runner = (*processorRunnerStub)(nil)
var _ Repository = (*processorRepositoryStub)(nil)
