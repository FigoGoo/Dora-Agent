package writeprompts

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const (
	previewTestRequestID         = "019f68e8-0201-7000-8000-000000000201"
	previewTestUserID            = "019f68e8-0202-7000-8000-000000000202"
	previewTestProjectID         = "019f68e8-0203-7000-8000-000000000203"
	previewTestSessionID         = "019f68e8-0204-7000-8000-000000000204"
	previewTestInputID           = "019f68e8-0205-7000-8000-000000000205"
	previewTestTurnID            = "019f68e8-0206-7000-8000-000000000206"
	previewTestRunID             = "019f68e8-0207-7000-8000-000000000207"
	previewTestToolCallID        = "019f68e8-0208-7000-8000-000000000208"
	previewTestBusinessCommandID = "019f68e8-0209-7000-8000-000000000209"
	previewTestStoryboardID      = "019f68e8-0210-7000-8000-000000000210"
	previewTestPromptDraftID     = "019f68e8-0211-7000-8000-000000000211"
	previewTestSourceDigest      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// previewTestModel 记录 Graph 对唯一 ChatModel 节点的调用次数。
type previewTestModel struct {
	mu      sync.Mutex
	content string
	calls   int
}

func (fake *previewTestModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.calls++
	return &schema.Message{Role: schema.Assistant, Content: fake.content}, nil
}

func (fake *previewTestModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := fake.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (fake *previewTestModel) callCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.calls
}

// previewTestReader 返回已通过 Business Owner 校验的固定快照。
type previewTestReader struct {
	value GenerationContext
	err   error
}

func (reader *previewTestReader) GetPromptGenerationContext(context.Context, TrustedContext) (GenerationContext, error) {
	if reader.err != nil {
		return GenerationContext{}, reader.err
	}
	return reader.value, nil
}

// previewTestStore 记录保存与 Unknown Outcome 核对调用，并按命令构造权威资源。
type previewTestStore struct {
	mu          sync.Mutex
	saveCalls   int
	queryCalls  int
	saveErr     error
	queryErr    error
	queryStatus string
}

func (store *previewTestStore) SavePromptPreviewDraft(_ context.Context, command DraftCommand) (SaveDisposition, Resource, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.saveCalls++
	if store.saveErr != nil {
		return "", Resource{}, store.saveErr
	}
	resource, err := previewTestResource(command)
	return SaveDispositionCreated, resource, err
}

func (store *previewTestStore) QueryPromptPreviewCommand(_ context.Context, command DraftCommand) (string, *Resource, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.queryCalls++
	if store.queryErr != nil {
		return "", nil, store.queryErr
	}
	status := store.queryStatus
	if status == "" {
		status = "not_found"
	}
	if status != "completed" {
		return status, nil, nil
	}
	resource, err := previewTestResource(command)
	if err != nil {
		return "", nil, err
	}
	return status, &resource, nil
}

func (store *previewTestStore) counts() (int, int) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.saveCalls, store.queryCalls
}

// previewTestJournal 验证副作用前只冻结一次完整命令。
type previewTestJournal struct {
	mu           sync.Mutex
	prepareCalls int
	lastCommand  DraftCommand
}

func (journal *previewTestJournal) PrepareCommand(_ context.Context, command DraftCommand) error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	journal.prepareCalls++
	journal.lastCommand = command
	return nil
}

func (*previewTestJournal) ReserveCommandResend(context.Context, TrustedContext, RecoveryDeferred) (RecoveryDeferred, bool, error) {
	return RecoveryDeferred{}, false, nil
}

func (journal *previewTestJournal) calls() int {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	return journal.prepareCalls
}

func (journal *previewTestJournal) preparedCommand() DraftCommand {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	return journal.lastCommand
}

// previewTestClock 为 completed Card 提供稳定 UTC 时间。
type previewTestClock struct{ now time.Time }

func (clock previewTestClock) Now() time.Time { return clock.now }

func TestGraphTopologyMatchesApprovedExactSet(t *testing.T) {
	t.Parallel()
	wantNodes := []string{
		"validate_intent", "load_storyboard_preview", "resolve_exact_targets", "build_prompt", "call_model",
		"validate_prompt_candidate", "validate_exact_target_set", "save_prompt_preview_draft", "query_save_receipt",
		"build_saved_result", "build_queried_result", "emit_scope_failed", "emit_candidate_failed",
		"emit_exact_set_failed", "defer_recovery",
	}
	wantBranches := []string{
		"route_scope_validation", "route_candidate_validation", "route_exact_set_validation",
		"route_save_outcome", "route_query_outcome",
	}
	if !reflect.DeepEqual(NodeKeys(), wantNodes) {
		t.Fatalf("NodeKeys()=%v want=%v", NodeKeys(), wantNodes)
	}
	if !reflect.DeepEqual(BranchKeys(), wantBranches) {
		t.Fatalf("BranchKeys()=%v want=%v", BranchKeys(), wantBranches)
	}

	terminalOrigins := map[string]bool{}
	for _, edge := range GraphEdges() {
		if edge.To != compose.END {
			continue
		}
		if terminalOrigins[edge.From] {
			t.Fatalf("终点来源重复: %s", edge.From)
		}
		terminalOrigins[edge.From] = true
	}
	wantTerminalOrigins := map[string]bool{
		"build_saved_result": true, "build_queried_result": true, "emit_scope_failed": true,
		"emit_candidate_failed": true, "emit_exact_set_failed": true, "defer_recovery": true,
	}
	if !reflect.DeepEqual(terminalOrigins, wantTerminalOrigins) {
		t.Fatalf("terminal origins=%v want=%v", terminalOrigins, wantTerminalOrigins)
	}
}

func TestGraphHappyPathCallsModelAndSaveOnce(t *testing.T) {
	t.Parallel()
	contextValue := previewTestGenerationContext()
	candidateJSON, err := json.Marshal(previewTestCandidate())
	if err != nil {
		t.Fatal(err)
	}
	modelValue := &previewTestModel{content: string(candidateJSON)}
	store := &previewTestStore{}
	journal := &previewTestJournal{}
	graph := previewTestCompile(t, modelValue, contextValue, store, journal)
	outcome, err := graph.Invoke(context.Background(), previewTestGraphInput(contextValue, 8))
	if err != nil {
		t.Fatalf("Invoke() error=%v", err)
	}
	saveCalls, queryCalls := store.counts()
	if outcome.Terminal == nil || outcome.Recovery != nil || outcome.Terminal.Status != "completed" ||
		outcome.Terminal.ResultCode != ResultCodeCompleted || outcome.Terminal.TargetCount != 3 ||
		modelValue.callCount() != 1 || saveCalls != 1 || queryCalls != 0 || journal.calls() != 1 ||
		journal.preparedCommand().ResendLimit != 1 {
		t.Fatalf("outcome=%+v model=%d save=%d query=%d journal=%d", outcome, modelValue.callCount(), saveCalls, queryCalls, journal.calls())
	}
}

func TestBranchInvalidRoutesFailClosed(t *testing.T) {
	t.Parallel()
	builder := &graphBuilder{}
	checks := []struct {
		name string
		run  func() error
	}{
		{name: "scope", run: func() error {
			_, err := builder.routeScope(context.Background(), targetRoute{Route: "corrupt"})
			return err
		}},
		{name: "candidate", run: func() error {
			_, err := builder.routeCandidate(context.Background(), candidateRoute{Route: "corrupt"})
			return err
		}},
		{name: "exact set", run: func() error {
			_, err := builder.routeExactSet(context.Background(), contentRoute{Route: "corrupt"})
			return err
		}},
		{name: "save", run: func() error {
			_, err := builder.routeSave(context.Background(), SaveOutcome{Status: "corrupt"})
			return err
		}},
		{name: "query", run: func() error {
			_, err := builder.routeQuery(context.Background(), SaveOutcome{Status: "corrupt"})
			return err
		}},
	}
	for _, check := range checks {
		check := check
		t.Run(check.name, func(t *testing.T) {
			t.Parallel()
			if err := check.run(); err == nil {
				t.Fatal("未知 Branch 路由值未失败关闭")
			}
		})
	}
}

func TestModelMetadataIsRejectedBeforeCandidateValidation(t *testing.T) {
	t.Parallel()
	builder := &graphBuilder{}
	for _, test := range []struct {
		name    string
		message *schema.Message
	}{
		{name: "tool calls", message: &schema.Message{Role: schema.Assistant, Content: `{}`, ToolCalls: []schema.ToolCall{{ID: "call_1"}}}},
		{name: "reasoning", message: &schema.Message{Role: schema.Assistant, Content: `{}`, ReasoningContent: "internal"}},
		{name: "extra", message: &schema.Message{Role: schema.Assistant, Content: `{}`, Extra: map[string]any{"provider": "hidden"}}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := builder.captureModelMessage(context.Background(), test.message, &State{}); err == nil {
				t.Fatal("含模型 metadata 的响应未失败关闭")
			}
		})
	}
}

func TestGraphScopeFailuresDoNotCallModelOrSave(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		maxTargets int
		mutate     func(*GenerationContext)
		wantCode   string
	}{
		{name: "zero targets", maxTargets: 8, mutate: func(value *GenerationContext) { value.Storyboard.Content.Slots = []StoryboardSlot{} }, wantCode: ResultCodeNoTargets},
		{name: "budget exceeded", maxTargets: 2, mutate: func(*GenerationContext) {}, wantCode: ResultCodeTargetBudgetExceeded},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			contextValue := previewTestGenerationContext()
			test.mutate(&contextValue)
			candidateJSON, _ := json.Marshal(previewTestCandidate())
			modelValue := &previewTestModel{content: string(candidateJSON)}
			store := &previewTestStore{}
			journal := &previewTestJournal{}
			graph := previewTestCompile(t, modelValue, contextValue, store, journal)
			input := previewTestGraphInput(contextValue, test.maxTargets)
			outcome, err := graph.Invoke(context.Background(), input)
			if err != nil {
				t.Fatalf("Invoke() error=%v", err)
			}
			saveCalls, queryCalls := store.counts()
			if outcome.Terminal == nil || outcome.Terminal.ResultCode != test.wantCode || outcome.Terminal.Card == nil ||
				ValidateTerminalResult(*outcome.Terminal, input.TrustedContext) != nil || outcome.Recovery != nil ||
				modelValue.callCount() != 0 || saveCalls != 0 || queryCalls != 0 || journal.calls() != 0 {
				t.Fatalf("outcome=%+v model=%d save=%d query=%d journal=%d", outcome, modelValue.callCount(), saveCalls, queryCalls, journal.calls())
			}
		})
	}
}

func TestGraphCandidateAndExactSetFailuresNeverSave(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		content  string
		wantCode string
	}{
		{name: "candidate schema", content: `{"schema_version":"prompt.preview.candidate.v1","prompts":[]}`, wantCode: ResultCodeCandidateInvalid},
		{name: "exact set reorder", content: previewTestCandidateJSON(t, func(value *Candidate) {
			value.Prompts[0], value.Prompts[1] = value.Prompts[1], value.Prompts[0]
		}), wantCode: ResultCodeExactSetInvalid},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			contextValue := previewTestGenerationContext()
			modelValue := &previewTestModel{content: test.content}
			store := &previewTestStore{}
			journal := &previewTestJournal{}
			graph := previewTestCompile(t, modelValue, contextValue, store, journal)
			input := previewTestGraphInput(contextValue, 8)
			outcome, err := graph.Invoke(context.Background(), input)
			if err != nil {
				t.Fatalf("Invoke() error=%v", err)
			}
			saveCalls, queryCalls := store.counts()
			if outcome.Terminal == nil || outcome.Terminal.ResultCode != test.wantCode || outcome.Terminal.Card == nil ||
				ValidateTerminalResult(*outcome.Terminal, input.TrustedContext) != nil || outcome.Recovery != nil ||
				modelValue.callCount() != 1 || saveCalls != 0 || queryCalls != 0 || journal.calls() != 0 {
				t.Fatalf("outcome=%+v model=%d save=%d query=%d journal=%d", outcome, modelValue.callCount(), saveCalls, queryCalls, journal.calls())
			}
		})
	}
}

func TestGraphUnknownOutcomeQueriesOriginalCommandOnce(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		queryStatus  string
		wantTerminal bool
	}{
		{name: "receipt found", queryStatus: "completed", wantTerminal: true},
		{name: "receipt unresolved", queryStatus: "not_found"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			contextValue := previewTestGenerationContext()
			candidateJSON, _ := json.Marshal(previewTestCandidate())
			modelValue := &previewTestModel{content: string(candidateJSON)}
			store := &previewTestStore{saveErr: ErrBusinessUnknownOutcome, queryStatus: test.queryStatus}
			journal := &previewTestJournal{}
			graph := previewTestCompile(t, modelValue, contextValue, store, journal)
			outcome, err := graph.Invoke(context.Background(), previewTestGraphInput(contextValue, 8))
			if err != nil {
				t.Fatalf("Invoke() error=%v", err)
			}
			saveCalls, queryCalls := store.counts()
			if saveCalls != 1 || queryCalls != 1 || journal.calls() != 1 || modelValue.callCount() != 1 {
				t.Fatalf("model=%d save=%d query=%d journal=%d", modelValue.callCount(), saveCalls, queryCalls, journal.calls())
			}
			if test.wantTerminal {
				if outcome.Terminal == nil || outcome.Recovery != nil || outcome.Terminal.Status != "completed" {
					t.Fatalf("outcome=%+v", outcome)
				}
				return
			}
			if outcome.Terminal != nil || outcome.Recovery == nil || outcome.Recovery.ResendLimit != 1 ||
				outcome.Recovery.BusinessCommandID != previewTestBusinessCommandID || outcome.Recovery.RequestDigest == "" {
				t.Fatalf("outcome=%+v", outcome)
			}
		})
	}
}

func TestGraphReceiptConflictFailsWithoutNewCommand(t *testing.T) {
	t.Parallel()
	contextValue := previewTestGenerationContext()
	modelValue := &previewTestModel{content: previewTestCandidateJSON(t, func(*Candidate) {})}
	store := &previewTestStore{saveErr: ErrBusinessUnknownOutcome, queryStatus: "conflict"}
	journal := &previewTestJournal{}
	graph := previewTestCompile(t, modelValue, contextValue, store, journal)
	_, err := graph.Invoke(context.Background(), previewTestGraphInput(contextValue, 8))
	if !errors.Is(err, ErrBusinessConflict) {
		t.Fatalf("Invoke() error=%v want ErrBusinessConflict", err)
	}
	saveCalls, queryCalls := store.counts()
	if modelValue.callCount() != 1 || saveCalls != 1 || queryCalls != 1 || journal.calls() != 1 {
		t.Fatalf("model=%d save=%d query=%d journal=%d", modelValue.callCount(), saveCalls, queryCalls, journal.calls())
	}
}

func TestGraphZeroResendBudgetProducesExhaustedRecovery(t *testing.T) {
	t.Parallel()
	contextValue := previewTestGenerationContext()
	modelValue := &previewTestModel{content: previewTestCandidateJSON(t, func(*Candidate) {})}
	store := &previewTestStore{saveErr: ErrBusinessUnknownOutcome, queryStatus: "not_found"}
	journal := &previewTestJournal{}
	graph := previewTestCompile(t, modelValue, contextValue, store, journal)
	input := previewTestGraphInput(contextValue, 8)
	input.TrustedContext.Policy.MaxCommandResends = 0
	outcome, err := graph.Invoke(context.Background(), input)
	if err != nil {
		t.Fatalf("Invoke() error=%v", err)
	}
	prepared := journal.preparedCommand()
	if outcome.Terminal != nil || outcome.Recovery == nil || outcome.Recovery.ResendLimit != 0 ||
		!outcome.Recovery.ResendExhausted || outcome.Recovery.ResendAttempts != 0 || prepared.ResendLimit != 0 {
		t.Fatalf("outcome=%+v prepared_resend_limit=%d", outcome, prepared.ResendLimit)
	}
}

func previewTestCompile(t *testing.T, modelValue model.BaseChatModel, contextValue GenerationContext, store *previewTestStore, journal *previewTestJournal) *CompiledGraph {
	t.Helper()
	graph, err := Compile(
		context.Background(), modelValue, &previewTestReader{value: contextValue}, store, journal,
		previewTestClock{now: time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)},
	)
	if err != nil {
		t.Fatalf("Compile() error=%v", err)
	}
	return graph
}

func previewTestGraphInput(contextValue GenerationContext, maxTargets int) GraphInput {
	intentJSON, _ := json.Marshal(Intent{SchemaVersion: IntentSchemaVersion, WritingInstruction: "为全部故事板目标编写清晰的生成提示词"})
	return GraphInput{TrustedContext: previewTestTrustedContext(contextValue, maxTargets), IntentJSON: intentJSON}
}

func previewTestTrustedContext(contextValue GenerationContext, maxTargets int) TrustedContext {
	return TrustedContext{
		Owner: "prompt-preview-lease/worker-1", RequestID: previewTestRequestID, UserID: previewTestUserID,
		ProjectID: previewTestProjectID, SessionID: previewTestSessionID, InputID: previewTestInputID,
		TurnID: previewTestTurnID, RunID: previewTestRunID, ToolCallID: previewTestToolCallID,
		BusinessCommandID: previewTestBusinessCommandID, FenceToken: 9,
		StoryboardPreviewRef: StoryboardPreviewRef{ID: previewTestStoryboardID, Version: 1, ContentDigest: contextValue.Storyboard.ContentDigest},
		PromptVersion:        PromptVersion, ValidatorVersion: ValidatorVersion, ExactSetValidatorVersion: ExactSetValidatorVersion,
		Policy: Policy{Version: RuntimePolicyVersion, MaxTargets: maxTargets, DefaultOutputLanguage: "zh-CN", MaxCommandResends: 1},
	}
}

func previewTestGenerationContext() GenerationContext {
	return GenerationContext{
		ProjectID: previewTestProjectID, ProjectVersion: 3, ProjectTitle: "夏日新品项目",
		Storyboard: StoryboardResource{
			ID: previewTestStoryboardID, ProjectID: previewTestProjectID, Version: 1, Status: "draft",
			ContentDigest: previewTestSourceDigest,
			Content: StoryboardContent{
				SchemaVersion: storyboardDraftSchemaVersion, Title: "夏日新品故事板", Summary: "由氛围建立过渡到核心产品展示。",
				Elements: []StoryboardElement{
					{Key: "element_1", Order: 1, Title: "海边开场", NarrativePurpose: "建立轻快明亮的夏日氛围"},
					{Key: "element_2", Order: 2, Title: "产品展示", NarrativePurpose: "突出产品设计和关键卖点"},
				},
				Slots: []StoryboardSlot{
					{Key: "slot_3", ElementKey: "element_2", SlotType: "video", Purpose: "产品动态展示", Required: true},
					{Key: "slot_1", ElementKey: "element_1", SlotType: "image", Purpose: "海边环境主视觉", Required: true},
					{Key: "slot_2", ElementKey: "element_1", SlotType: "voiceover", Purpose: "开场品牌旁白", Required: false},
				},
			},
		},
	}
}

func previewTestCandidate() Candidate {
	return Candidate{
		SchemaVersion: CandidateSchemaVersion,
		Prompts: []CandidatePrompt{
			{TargetLocalKey: "slot_1", PositivePrompt: "明亮夏日海边的品牌主视觉，构图清爽。", NegativeConstraints: []string{"避免阴暗色调"}},
			{TargetLocalKey: "slot_2", PositivePrompt: "温暖自然的中文品牌旁白，语速舒缓。", NegativeConstraints: []string{"避免机械语气"}},
			{TargetLocalKey: "slot_3", PositivePrompt: "产品沿海岸线运动的动态镜头，突出核心卖点。", NegativeConstraints: []string{"避免画面抖动"}},
		},
	}
}

func previewTestCandidateJSON(t *testing.T, mutate func(*Candidate)) string {
	t.Helper()
	candidate := previewTestCandidate()
	mutate(&candidate)
	encoded, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func previewTestResource(command DraftCommand) (Resource, error) {
	digest, err := ContentDigest(command.Content)
	if err != nil {
		return Resource{}, err
	}
	return Resource{
		PromptPreviewID: previewTestPromptDraftID, ProjectID: command.TrustedContext.ProjectID,
		StoryboardPreviewRef: command.TrustedContext.StoryboardPreviewRef, Version: 1, Status: "draft",
		ContentDigest: digest, ExactTargetSetDigest: command.ExactTargetSetDigest, Content: command.Content,
	}, nil
}

var _ model.BaseChatModel = (*previewTestModel)(nil)
var _ BusinessContextReader = (*previewTestReader)(nil)
var _ BusinessDraftStore = (*previewTestStore)(nil)
var _ CommandJournal = (*previewTestJournal)(nil)
