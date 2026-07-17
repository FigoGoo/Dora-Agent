package planstoryboard

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	testRequestID           = "019f68e8-0101-7000-8000-000000000101"
	testUserID              = "019f68e8-0001-7000-8000-000000000001"
	testProjectID           = "019f68e8-0002-7000-8000-000000000002"
	testSessionID           = "019f68e8-0102-7000-8000-000000000102"
	testInputID             = "019f68e8-0103-7000-8000-000000000103"
	testTurnID              = "019f68e8-0104-7000-8000-000000000104"
	testRunID               = "019f68e8-0105-7000-8000-000000000105"
	testToolCallID          = "019f68e8-0003-7000-8000-000000000003"
	testBusinessCommandID   = "019f68e8-0106-7000-8000-000000000106"
	testCreationSpecID      = "019f68e8-0010-7000-8000-000000000010"
	testStoryboardPreviewID = "019f68e8-0107-7000-8000-000000000107"
)

type testModel struct {
	mu      sync.Mutex
	content string
	calls   int
}

func (fake *testModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.calls++
	return &schema.Message{Role: schema.Assistant, Content: fake.content}, nil
}

func (fake *testModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := fake.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

type testReader struct {
	value PlanningContext
	err   error
}

func (reader *testReader) GetStoryboardPlanningContext(context.Context, TrustedContext) (PlanningContext, error) {
	if reader.err != nil {
		return PlanningContext{}, reader.err
	}
	return reader.value, nil
}

type testStore struct {
	mu          sync.Mutex
	saveCalls   int
	queryCalls  int
	saveErr     error
	queryStatus string
}

func (store *testStore) SaveStoryboardDraft(_ context.Context, command DraftCommand) (SaveDisposition, Resource, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.saveCalls++
	if store.saveErr != nil {
		return "", Resource{}, store.saveErr
	}
	digest, err := ContentDigest(command.Content)
	if err != nil {
		return "", Resource{}, err
	}
	return SaveDispositionCreated, Resource{
		StoryboardPreviewID: testStoryboardPreviewID, ProjectID: command.TrustedContext.ProjectID,
		CreationSpecRef: command.TrustedContext.CreationSpecRef, Version: 1, Status: "draft",
		ContentDigest: digest, Content: command.Content,
	}, nil
}

func (store *testStore) QueryStoryboardDraftCommand(context.Context, DraftCommand) (string, *Resource, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.queryCalls++
	status := store.queryStatus
	if status == "" {
		status = "not_found"
	}
	return status, nil, nil
}

func (store *testStore) calls() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.saveCalls
}

func (store *testStore) queryCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.queryCalls
}

type testJournal struct{ mu sync.Mutex }

func (journal *testJournal) PrepareCommand(context.Context, DraftCommand) error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	return nil
}

func (*testJournal) ReserveCommandResend(context.Context, TrustedContext, RecoveryDeferred) (RecoveryDeferred, bool, error) {
	return RecoveryDeferred{}, false, nil
}

type testClock struct{ now time.Time }

func (clock testClock) Now() time.Time { return clock.now }

func TestCompileAndInvokeHappyPath(t *testing.T) {
	t.Parallel()
	contextValue := testPlanningContext(t)
	candidateJSON, err := json.Marshal(testCandidate())
	if err != nil {
		t.Fatal(err)
	}
	modelValue := &testModel{content: string(candidateJSON)}
	store := &testStore{}
	graph, err := Compile(context.Background(), modelValue, &testReader{value: contextValue}, store, &testJournal{}, testClock{now: time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	intentJSON, _ := json.Marshal(Intent{SchemaVersion: IntentSchemaVersion, PlanningInstruction: "规划两段式夏日品牌短片"})
	outcome, err := graph.Invoke(context.Background(), GraphInput{TrustedContext: testTrustedContext(t, contextValue), IntentJSON: intentJSON})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if outcome.Terminal == nil || outcome.Terminal.Status != "completed" || outcome.Recovery != nil || store.calls() != 1 {
		t.Fatalf("Outcome=%+v saveCalls=%d", outcome, store.calls())
	}
	if outcome.Terminal.ResourceRef.StoryboardPreviewID != testStoryboardPreviewID ||
		outcome.Terminal.Card.StoryboardPreviewID != testStoryboardPreviewID {
		t.Fatalf("Preview ID 未使用冻结命名: %+v", outcome.Terminal)
	}
}

func TestGraphValidationBranchesNeverSaveInvalidCandidate(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*Candidate){
		"candidate": func(candidate *Candidate) { candidate.Title = "" },
		"dependency": func(candidate *Candidate) {
			candidate.Elements[0].DependencyKeys = []string{"element_2"}
			candidate.Elements[1].DependencyKeys = []string{"element_1"}
		},
	} {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			contextValue := testPlanningContext(t)
			candidate := testCandidate()
			mutate(&candidate)
			candidateJSON, _ := json.Marshal(candidate)
			store := &testStore{}
			graph, err := Compile(context.Background(), &testModel{content: string(candidateJSON)}, &testReader{value: contextValue}, store, &testJournal{}, testClock{now: time.Now().UTC()})
			if err != nil {
				t.Fatal(err)
			}
			intentJSON, _ := json.Marshal(Intent{SchemaVersion: IntentSchemaVersion, PlanningInstruction: "规划两段式短片"})
			outcome, err := graph.Invoke(context.Background(), GraphInput{TrustedContext: testTrustedContext(t, contextValue), IntentJSON: intentJSON})
			if err != nil {
				t.Fatalf("Invoke() error = %v", err)
			}
			wantCode := ResultCodeCandidateInvalid
			if name == "dependency" {
				wantCode = ResultCodeDependencyInvalid
			}
			if outcome.Terminal == nil || outcome.Terminal.ResultCode != wantCode || store.calls() != 0 {
				t.Fatalf("Outcome=%+v saveCalls=%d wantCode=%s", outcome, store.calls(), wantCode)
			}
		})
	}
}

func TestGraphUnknownOutcomeQueriesOnceThenDefers(t *testing.T) {
	t.Parallel()
	contextValue := testPlanningContext(t)
	candidateJSON, _ := json.Marshal(testCandidate())
	store := &testStore{saveErr: ErrBusinessUnknownOutcome, queryStatus: "not_found"}
	graph, err := Compile(context.Background(), &testModel{content: string(candidateJSON)}, &testReader{value: contextValue}, store, &testJournal{}, testClock{now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	intentJSON, _ := json.Marshal(Intent{SchemaVersion: IntentSchemaVersion, PlanningInstruction: "规划两段式短片"})
	outcome, err := graph.Invoke(context.Background(), GraphInput{TrustedContext: testTrustedContext(t, contextValue), IntentJSON: intentJSON})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if outcome.Terminal != nil || outcome.Recovery == nil || store.calls() != 1 || store.queryCount() != 1 {
		t.Fatalf("Outcome=%+v saveCalls=%d queryCalls=%d", outcome, store.calls(), store.queryCount())
	}
}

func TestToolInfoStrictSchemaExcludesTrustedAndPromptFields(t *testing.T) {
	t.Parallel()
	info, err := (&Tool{}).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	jsonSchema, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(jsonSchema)
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if document["additionalProperties"] != false {
		t.Fatalf("Tool Schema 非 strict: %s", encoded)
	}
	properties := document["properties"].(map[string]any)
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"planning_instruction", "schema_version", "target_duration_seconds"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("Tool Schema properties=%v want=%v", keys, want)
	}
	for _, forbidden := range []string{
		"creation_spec_id", "creation_spec_ref", "user_id", "project_id", "business_command_id",
		"fence_token", "prompt", "negative_prompt", "asset_id", "provider",
	} {
		if strings.Contains(string(encoded), `"`+forbidden+`"`) {
			t.Fatalf("Tool Schema 暴露禁止字段 %q: %s", forbidden, encoded)
		}
	}
}

func TestToolReportsCreationSpecConflictSeparately(t *testing.T) {
	t.Parallel()
	contextValue := testPlanningContext(t)
	graph, err := Compile(
		context.Background(),
		&testModel{content: `{}`},
		&testReader{err: ErrBusinessCreationSpecConflict},
		&testStore{},
		&testJournal{},
		testClock{now: time.Now().UTC()},
	)
	if err != nil {
		t.Fatal(err)
	}
	trusted := testTrustedContext(t, contextValue)
	toolValue, err := NewTool(graph, func(context.Context) (TrustedContext, bool) { return trusted, true })
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := toolValue.InvokableRun(context.Background(), `{"schema_version":"plan_storyboard.preview.intent.v1","planning_instruction":"规划两段式短片"}`)
	if err != nil {
		t.Fatalf("InvokableRun() error=%v", err)
	}
	var result Result
	if err := json.Unmarshal([]byte(encoded), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" || result.ResultCode != ResultCodeCreationSpecConflict {
		t.Fatalf("result=%+v", result)
	}
}

func TestGraphTopologyHasNoMutuallyExclusiveFanIn(t *testing.T) {
	t.Parallel()
	want := []string{
		"validate_intent", "load_creation_spec", "build_prompt", "call_model", "validate_candidate",
		"validate_dependency_graph", "save_storyboard_draft", "query_save_receipt", "build_saved_result",
		"build_queried_result", "emit_candidate_failed", "emit_dependency_failed", "defer_recovery",
	}
	if !reflect.DeepEqual(NodeKeys(), want) {
		t.Fatalf("NodeKeys()=%v want=%v", NodeKeys(), want)
	}
}

func testTrustedContext(t *testing.T, contextValue PlanningContext) TrustedContext {
	t.Helper()
	return TrustedContext{
		Owner: "storyboard-preview-lease/worker-1", RequestID: testRequestID, UserID: testUserID, ProjectID: testProjectID,
		SessionID: testSessionID, InputID: testInputID, TurnID: testTurnID, RunID: testRunID,
		ToolCallID: testToolCallID, BusinessCommandID: testBusinessCommandID, FenceToken: 7,
		CreationSpecRef: CreationSpecRef{ID: testCreationSpecID, Version: 1, ContentDigest: contextValue.CreationSpec.ContentDigest},
		PromptVersion:   PromptVersion, ValidatorVersion: ValidatorVersion, DAGValidatorVersion: DAGValidatorVersion,
	}
}

func testPlanningContext(t *testing.T) PlanningContext {
	t.Helper()
	content := CreationSpecContent{
		Title: "夏日品牌短片", Goal: "制作一支突出轻快体验的品牌短片", DeliverableType: "video",
		Audience: "年轻消费者", Locale: "zh-CN",
		Phases: []CreationSpecPhase{
			{Key: "phase_1", Title: "开场", Objective: "建立夏日氛围", Output: "开场段落"},
			{Key: "phase_2", Title: "体验", Objective: "展示产品体验", Output: "产品段落"},
		},
		Constraints: []string{}, AcceptanceCriteria: []string{"叙事完整"},
	}
	digest, err := digestJSON(content, "test CreationSpec")
	if err != nil {
		t.Fatal(err)
	}
	return PlanningContext{
		ProjectID: testProjectID, ProjectVersion: 1, ProjectTitle: "夏日活动",
		CreationSpec: CreationSpecResource{
			ID: testCreationSpecID, ProjectID: testProjectID, Version: 1, Status: "draft",
			ContentDigest: digest, Content: content,
		},
	}
}

func testCandidate() Candidate {
	return Candidate{
		SchemaVersion: CandidateSchemaVersion, Title: "夏日品牌短片故事板", Summary: "以氛围建立和产品体验串联两段叙事。",
		Sections: []Section{
			{Key: "section_1", Title: "开场", Objective: "建立轻快夏日氛围"},
			{Key: "section_2", Title: "体验", Objective: "展示产品核心体验"},
		},
		Elements: []Element{
			{Key: "element_1", SectionKey: "section_1", Order: 1, ElementType: "scene", Title: "海边开场", NarrativePurpose: "建立情绪", DurationSeconds: 10, SourcePhaseKey: "phase_1", DependencyKeys: []string{}},
			{Key: "element_2", SectionKey: "section_2", Order: 2, ElementType: "shot", Title: "产品特写", NarrativePurpose: "突出卖点", DurationSeconds: 20, SourcePhaseKey: "phase_2", DependencyKeys: []string{"element_1"}},
		},
		Slots: []Slot{
			{Key: "slot_1", ElementKey: "element_1", SlotType: "video", Purpose: "海边环境画面", Required: true},
			{Key: "slot_2", ElementKey: "element_2", SlotType: "image", Purpose: "产品包装特写", Required: true},
		},
	}
}

var _ model.BaseChatModel = (*testModel)(nil)
var _ BusinessContextReader = (*testReader)(nil)
var _ BusinessDraftStore = (*testStore)(nil)
var _ CommandJournal = (*testJournal)(nil)
