package capabilityruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/artifact"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type scriptedModel struct{}

func (scriptedModel) Generate(_ context.Context, messages []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	system := messages[0].Content
	switch {
	case strings.Contains(system, "多模态素材分析"), strings.Contains(system, "素材清单与元数据分析"):
		return schema.AssistantMessage(`{"summary":"素材可用","facts":{"brand":"Dora"},"reusable_asset_ids":[]}`, nil), nil
	case strings.Contains(system, "创作规范规划"):
		return schema.AssistantMessage(`{"title":"演示短片","video_type":"short_drama","duration_seconds":30,"aspect_ratio":"9:16","narrative_driver":"character","visual_style":"cinematic","sound_style":"dialogue","fields":{}}`, nil), nil
	case strings.Contains(system, "动态故事板规划"):
		return schema.AssistantMessage(`{"scenario":"short_drama","modules":[{"key":"scenes","semantic_type":"scene","title":"场景","planned_count":1,"required":true,"capabilities":{"has_quantity":true,"requires_prompt":false,"requires_asset":true,"output_modality":"image"},"elements":[{"key":"scene-1","semantic_type":"scene","title":"开场","revision":1,"content":{"description":"城市夜景"},"asset_slots":[{"key":"keyframe","media_kind":"image","required":true,"review_required":true,"generation_epoch":0,"status":"missing"}]}]}]}`, nil), nil
	case strings.Contains(system, "提示词生成节点"):
		var input struct {
			Targets []struct {
				TargetID string `json:"target_id"`
				Purpose  string `json:"purpose"`
			} `json:"targets"`
		}
		if err := json.Unmarshal([]byte(messages[1].Content), &input); err != nil {
			return nil, err
		}
		prompts := make([]map[string]string, 0, len(input.Targets))
		for _, target := range input.Targets {
			prompts = append(prompts, map[string]string{"target_id": target.TargetID, "purpose": target.Purpose, "prompt": "generated cinematic " + target.Purpose})
		}
		raw, err := json.Marshal(map[string]any{"prompts": prompts})
		if err != nil {
			return nil, err
		}
		return schema.AssistantMessage(string(raw), nil), nil
	default:
		return nil, fmt.Errorf("unexpected model request: %s", system)
	}
}
func (scriptedModel) Stream(_ context.Context, messages []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := (scriptedModel{}).Generate(context.Background(), messages)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

type promptResponseModel struct {
	response       string
	beforeResponse func(context.Context) error
}

type creationSpecSequenceModel struct {
	responses []string
	systems   []string
	calls     int
}

func (m *creationSpecSequenceModel) Generate(_ context.Context, messages []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	m.systems = append(m.systems, messages[0].Content)
	if m.calls >= len(m.responses) {
		return nil, fmt.Errorf("unexpected creation spec model call %d", m.calls+1)
	}
	response := m.responses[m.calls]
	m.calls++
	return schema.AssistantMessage(response, nil), nil
}

func (m *creationSpecSequenceModel) Stream(ctx context.Context, messages []*schema.Message, options ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func TestGenerateCreationSpecCandidateRetriesSchemaMismatch(t *testing.T) {
	model := &creationSpecSequenceModel{responses: []string{
		`{"goal":"创建城市短片","audience":"短视频用户","duration":8,"aspect_ratio":"16:9"}`,
		`{"title":"雨夜霓虹骑行","video_type":"short_video","target_audience":"短视频用户","output_language":"zh-CN","duration_seconds":8,"aspect_ratio":"16:9","narrative_driver":"骑行者穿过雨夜街道","visual_style":"赛博朋克霓虹","sound_style":"雨声与电子乐","model_preference":"Image2 + Seedance","markdown":"# 雨夜霓虹骑行","fields":{}}`,
	}}
	runtime := &Runtime{modelGraph: mustModelGraph(t, model)}

	candidate, err := runtime.generateCreationSpecCandidate(context.Background(), map[string]any{"goal": "创建8秒16:9城市短片"})
	if err != nil {
		t.Fatalf("generateCreationSpecCandidate() error = %v", err)
	}
	if model.calls != 2 || candidate.Title != "雨夜霓虹骑行" || candidate.VideoType != "short_video" || candidate.DurationSeconds != 8 || candidate.AspectRatio != "16:9" {
		t.Fatalf("calls=%d candidate=%+v", model.calls, candidate)
	}
	if len(model.systems) != 2 || !strings.Contains(model.systems[0], `"video_type"`) || !strings.Contains(model.systems[0], `"duration_seconds"`) || !strings.Contains(model.systems[1], "上一轮输出未通过") {
		t.Fatalf("systems=%#v", model.systems)
	}
}

func TestGenerateStoryboardCandidateRetriesSchemaMismatch(t *testing.T) {
	model := &creationSpecSequenceModel{responses: []string{
		`{"scenario":"雨夜骑行","storyboard":[{"title":"错误别名"}]}`,
		`{"scenario":"雨夜骑行","modules":[{"id":"visuals","key":"visuals","semantic_type":"scene","title":"关键画面","order":1,"planned_count":1,"required":true,"review_required":true,"revision":1,"capabilities":{"has_quantity":true,"requires_prompt":true,"requires_asset":true,"has_timeline":false,"output_modality":"image"},"elements":[{"id":"scene-1","key":"scene-1","semantic_type":"scene","title":"霓虹街道","revision":1,"content":{"description":"雨后霓虹街道"},"prompt_slots":[{"purpose":"keyframe","revision":1,"status":"missing"}],"asset_slots":[{"key":"keyframe","media_kind":"image","required":true,"review_required":true,"generation_epoch":0,"status":"missing"}]}]}],"dependencies":[]}`,
	}}
	runtime := &Runtime{modelGraph: mustModelGraph(t, model)}

	candidate, err := runtime.generateStoryboardCandidate(context.Background(), map[string]any{"goal": "8秒雨夜骑行"}, spec.FinalVideoSpec{Title: "雨夜霓虹骑行", DurationSeconds: 8})
	if err != nil {
		t.Fatal(err)
	}
	if model.calls != 2 || len(candidate.Modules) != 1 || len(candidate.Modules[0].Elements) != 1 {
		t.Fatalf("calls=%d candidate=%+v", model.calls, candidate)
	}
	if len(model.systems) != 2 || !strings.Contains(model.systems[0], `"modules"`) || !strings.Contains(model.systems[1], "上一轮输出未通过") {
		t.Fatalf("systems=%#v", model.systems)
	}
}

func TestLocalDemoStoryboardAndPromptFallbacksAreBounded(t *testing.T) {
	model := &creationSpecSequenceModel{responses: []string{`{"scenario":"bad"}`, `{"modules":[]}`}}
	runtime := &Runtime{cfg: Config{LocalDemoPlanning: true}, modelGraph: mustModelGraph(t, model)}
	creationSpec := spec.FinalVideoSpec{Title: "本地雨夜骑行", DurationSeconds: 8, VisualStyle: "赛博朋克霓虹", SoundStyle: "雨声"}

	candidate, err := runtime.generateStoryboardCandidate(context.Background(), map[string]any{"goal": "local"}, creationSpec)
	if err != nil {
		t.Fatal(err)
	}
	if model.calls != 2 || len(candidate.Modules) != 3 {
		t.Fatalf("calls=%d modules=%d", model.calls, len(candidate.Modules))
	}

	promptModel := &creationSpecSequenceModel{responses: []string{`{"prompts":[]}`, `{"prompts":[]}`}}
	runtime.modelGraph = mustModelGraph(t, promptModel)
	if err := runtime.completeCandidatePrompts(context.Background(), creationSpec, artifact.Revision{}, candidate.Modules); err != nil {
		t.Fatal(err)
	}
	if promptModel.calls != 2 {
		t.Fatalf("prompt model calls=%d, want bounded 2", promptModel.calls)
	}
	for _, module := range candidate.Modules {
		for _, element := range module.Elements {
			if len(element.PromptSlots) == 0 || element.PromptSlots[0].Status != storyboard.PromptStatusReady || !strings.Contains(element.PromptSlots[0].Prompt, "Local demo placeholder") {
				t.Fatalf("fallback prompt=%+v", element.PromptSlots)
			}
		}
	}
}

func TestPlanCreationSpecCreateReusesLatestConfirmedRevision(t *testing.T) {
	specs := &memorySpecs{values: []spec.FinalVideoSpec{{
		ID: "spec-existing", SessionID: "session-existing", Version: 1,
		Status: spec.StatusConfirmed, Title: "已确认规范", VideoType: "short_video",
		DurationSeconds: 8, AspectRatio: "16:9",
	}}}
	runtime := &Runtime{cfg: Config{Specs: specs}}

	result, err := runtime.planCreationSpec(context.Background(), capability.Request[capability.PlanCreationSpecIntent]{
		Command: capability.CommandContext{SessionID: "session-existing", IdempotencyKey: "new-create-call"},
		Intent:  capability.PlanCreationSpecIntent{Mode: "create", Goal: "不得重复创建"},
	})
	if err != nil {
		t.Fatalf("planCreationSpec() error = %v", err)
	}
	if result.Status != capability.StatusCompleted || result.Data.SpecID != "spec-existing" || result.Data.SpecVersion != 1 || len(specs.values) != 1 {
		t.Fatalf("result=%+v specs=%+v", result, specs.values)
	}
}

func (m promptResponseModel) Generate(ctx context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	if m.beforeResponse != nil {
		if err := m.beforeResponse(ctx); err != nil {
			return nil, err
		}
	}
	return schema.AssistantMessage(m.response, nil), nil
}

func (m promptResponseModel) Stream(ctx context.Context, messages []*schema.Message, options ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

type getLatestErrorArtifactStore struct {
	artifact.Store
	err error
}

func (s getLatestErrorArtifactStore) GetLatest(context.Context, string, string) (artifact.Revision, error) {
	return artifact.Revision{}, s.err
}

type memorySpecs struct{ values []spec.FinalVideoSpec }

func (s *memorySpecs) Save(_ context.Context, value spec.FinalVideoSpec) (spec.FinalVideoSpec, error) {
	value.Version = len(s.values) + 1
	s.values = append(s.values, value)
	return value, nil
}
func (s *memorySpecs) GetByIdempotencyKey(_ context.Context, key string) (spec.FinalVideoSpec, error) {
	for _, value := range s.values {
		if value.IdempotencyKey == key {
			return value, nil
		}
	}
	return spec.FinalVideoSpec{}, spec.ErrNotFound
}
func (s *memorySpecs) GetLatestBySession(_ context.Context, sessionID string) (spec.FinalVideoSpec, error) {
	if len(s.values) == 0 {
		return spec.FinalVideoSpec{}, spec.ErrNotFound
	}
	return s.values[len(s.values)-1], nil
}
func (s *memorySpecs) GetConfirmedBySession(_ context.Context, sessionID string) (spec.FinalVideoSpec, error) {
	for index := len(s.values) - 1; index >= 0; index-- {
		if s.values[index].Status == spec.StatusConfirmed {
			return s.values[index], nil
		}
	}
	return spec.FinalVideoSpec{}, spec.ErrNotFound
}

func TestRuntimeExecutesAllFiveCapabilityHandlers(t *testing.T) {
	ctx := context.Background()
	artifacts := artifact.NewMemoryStore()
	specs := &memorySpecs{}
	repository := storyboard.NewMemoryAggregateRepository()
	storyCommands, _ := storyboard.NewCommandService(repository)
	workflowStore := generation.NewMemoryStore()
	genCommands := generation.NewCommandService(generation.CommandServiceConfig{Store: workflowStore})
	id := 0
	approvalCalls := 0
	approvalRequests := make([]ApprovalRequest, 0)
	runtime, err := New(Config{Model: scriptedModel{}, Artifacts: artifacts, Specs: specs, Storyboards: repository, StoryboardCommands: storyCommands, GenerationCommands: genCommands, GenerationJobs: workflowStore, GenerationWorkflow: workflowStore, NewID: func() string { id++; return fmt.Sprintf("id-%d", id) }, CreateApproval: func(_ context.Context, request ApprovalRequest) (string, error) {
		approvalCalls++
		approvalRequests = append(approvalRequests, request)
		return request.ID, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	handlers := runtime.Handlers()
	command := capability.CommandContext{SessionID: "session-1", UserID: "user-1", RequestID: "request-1", IdempotencyKey: "idem-1"}
	analysis, err := handlers.AnalyzeMaterials(ctx, capability.Request[capability.AnalyzeMaterialsIntent]{Command: command, Intent: capability.AnalyzeMaterialsIntent{Goal: "extract references"}})
	if err != nil || analysis.Status != capability.StatusCompleted || analysis.Data.AnalysisVersion != 1 {
		t.Fatalf("analysis=%+v err=%v", analysis, err)
	}
	analysisReplay, err := handlers.AnalyzeMaterials(ctx, capability.Request[capability.AnalyzeMaterialsIntent]{Command: command, Intent: capability.AnalyzeMaterialsIntent{Goal: "extract references"}})
	if err != nil || analysisReplay.Data.AnalysisID != analysis.Data.AnalysisID || analysisReplay.Data.AnalysisVersion != analysis.Data.AnalysisVersion {
		t.Fatalf("analysis replay=%+v err=%v", analysisReplay, err)
	}
	command.RequestID, command.IdempotencyKey = "request-2", "idem-2"
	command.ToolCallID, command.StageRunID = "call-spec", "stage:call-spec"
	plannedSpec, err := handlers.PlanCreationSpec(ctx, capability.Request[capability.PlanCreationSpecIntent]{Command: command, Intent: capability.PlanCreationSpecIntent{Mode: "create", Goal: "make short drama"}})
	if err != nil || plannedSpec.Status != capability.StatusWaitingUser || plannedSpec.Data.ApprovalID == "" {
		t.Fatalf("spec=%+v err=%v", plannedSpec, err)
	}
	plannedSpecReplay, err := handlers.PlanCreationSpec(ctx, capability.Request[capability.PlanCreationSpecIntent]{Command: command, Intent: capability.PlanCreationSpecIntent{Mode: "create", Goal: "make short drama"}})
	if err != nil || plannedSpecReplay.Data.SpecID != plannedSpec.Data.SpecID || plannedSpecReplay.Data.SpecVersion != plannedSpec.Data.SpecVersion {
		t.Fatalf("spec replay=%+v err=%v", plannedSpecReplay, err)
	}
	if request := approvalRequests[len(approvalRequests)-1]; request.Approve.Payload["tool_call_id"] != "call-spec" || request.Approve.Payload["stage_run_id"] != "stage:call-spec" || request.Reject.Payload["tool_call_id"] != "call-spec" {
		t.Fatalf("spec approval lost trusted ToolRun correlation: %+v", request)
	}
	specs.values[len(specs.values)-1].Status = spec.StatusConfirmed
	command.RequestID, command.IdempotencyKey = "request-3", "idem-3"
	command.ToolCallID, command.StageRunID = "call-storyboard", "stage:call-storyboard"
	plannedBoard, err := handlers.PlanStoryboard(ctx, capability.Request[capability.PlanStoryboardIntent]{Command: command, Intent: capability.PlanStoryboardIntent{Mode: "create"}})
	if err != nil || plannedBoard.Data.Revision.Modules[0].PlannedCount != 1 {
		t.Fatalf("board=%+v err=%v", plannedBoard, err)
	}
	plannedElement := plannedBoard.Data.Revision.Modules[0].Elements[0]
	if plannedElement.ID == "" || len(plannedElement.PromptSlots) != 1 || plannedElement.PromptSlots[0].Prompt == "" || !plannedElement.AssetSlots[0].ReviewRequired || plannedElement.AssetSlots[0].Status != storyboard.AssetSlotStatusMissing {
		t.Fatalf("planned element was not normalized before review: %+v", plannedElement)
	}
	plannedBoardReplay, err := handlers.PlanStoryboard(ctx, capability.Request[capability.PlanStoryboardIntent]{Command: command, Intent: capability.PlanStoryboardIntent{Mode: "create"}})
	if err != nil || plannedBoardReplay.Data.Revision.ID != plannedBoard.Data.Revision.ID || plannedBoardReplay.StoryboardVersion != plannedBoard.StoryboardVersion {
		t.Fatalf("board replay=%+v err=%v", plannedBoardReplay, err)
	}
	aggregate, _ := repository.GetAggregateBySession(ctx, "session-1")
	reviewVersion := aggregate.Version
	aggregate, _, err = storyCommands.UpdatePrompt(ctx, storyboard.UpdatePromptCommand{CommandID: "review-edit", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, TargetID: plannedElement.ID, Purpose: plannedElement.PromptSlots[0].Purpose, ExpectedRevision: plannedElement.PromptSlots[0].Revision, Prompt: "reviewed prompt", LockedByUser: true})
	if err != nil {
		t.Fatal(err)
	}
	editedReplay, err := handlers.PlanStoryboard(ctx, capability.Request[capability.PlanStoryboardIntent]{Command: command, Intent: capability.PlanStoryboardIntent{Mode: "create"}})
	if err != nil || editedReplay.Status != capability.StatusWaitingUser || editedReplay.StoryboardVersion != aggregate.Version || editedReplay.Data.ApprovalID != plannedBoard.Data.ApprovalID {
		t.Fatalf("edited pending replay=%+v err=%v", editedReplay, err)
	}
	lastApproval := approvalRequests[len(approvalRequests)-1]
	if lastApproval.StoryboardVersion != reviewVersion || intFromAny(lastApproval.Approve.Payload["base_version"]) != reviewVersion {
		t.Fatalf("approval replay was rebuilt from mutable aggregate: %+v", lastApproval)
	}
	if lastApproval.Approve.Payload["tool_call_id"] != "call-storyboard" || lastApproval.Approve.Payload["stage_run_id"] != "stage:call-storyboard" || lastApproval.Reject.Payload["tool_call_id"] != "call-storyboard" {
		t.Fatalf("storyboard approval lost trusted ToolRun correlation: %+v", lastApproval)
	}
	if !aggregate.HasApplied("idem-3") || aggregate.HasApplied("request-3") {
		t.Fatalf("plan command identity must use per-tool idempotency key: %+v", aggregate.AppliedCommandIDs)
	}
	aggregate, _, err = storyCommands.DecidePending(ctx, storyboard.DecidePendingRevisionCommand{CommandID: "promote", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: aggregate.PendingRevisionID, Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	approvalCallsBeforeTerminalReplay := approvalCalls
	approvedReplay, err := handlers.PlanStoryboard(ctx, capability.Request[capability.PlanStoryboardIntent]{Command: command, Intent: capability.PlanStoryboardIntent{Mode: "create"}})
	if err != nil || approvedReplay.Status != capability.StatusCompleted || approvedReplay.Data.ApprovalID != plannedBoard.Data.ApprovalID {
		t.Fatalf("approved storyboard replay=%+v err=%v", approvedReplay, err)
	}
	if approvalCalls != approvalCallsBeforeTerminalReplay {
		t.Fatalf("terminal storyboard replay recreated approval: before=%d after=%d", approvalCallsBeforeTerminalReplay, approvalCalls)
	}
	command.RequestID, command.IdempotencyKey = "request-4", "idem-4"
	media, err := handlers.GenerateMedia(ctx, capability.Request[capability.GenerateMediaIntent]{Command: command, Intent: capability.GenerateMediaIntent{Phase: "auto_next", Policy: "all_eligible"}})
	if err != nil || media.Status != capability.StatusAccepted || media.Data.JobCount != 1 {
		t.Fatalf("media=%+v err=%v", media, err)
	}
	if _, err := workflowStore.GetOperation(ctx, media.OperationID); err != nil {
		t.Fatal(err)
	}
	mediaBatch, err := workflowStore.GetBatch(ctx, media.BatchID)
	if err != nil || mediaBatch.WakePolicy != generation.WakeOnFailure {
		t.Fatalf("successful media batches must only wake the Agent on failure: batch=%+v err=%v", mediaBatch, err)
	}
	mediaReplay, err := handlers.GenerateMedia(ctx, capability.Request[capability.GenerateMediaIntent]{Command: command, Intent: capability.GenerateMediaIntent{Phase: "auto_next", Policy: "all_eligible"}})
	if err != nil || mediaReplay.OperationID != media.OperationID || mediaReplay.Data.JobCount != media.Data.JobCount || mediaReplay.Data.NoOp {
		t.Fatalf("media replay=%+v err=%v", mediaReplay, err)
	}
	command.RequestID, command.IdempotencyKey = "request-5", "idem-5"
	assembly, err := handlers.AssembleOutput(ctx, capability.Request[capability.AssembleOutputIntent]{Command: command, Intent: capability.AssembleOutputIntent{Mode: "plan", OutputType: "vertical_video"}})
	if err != nil || assembly.Data.AssemblyRevisionID == "" || len(assembly.Data.MissingDependencies) != 1 {
		t.Fatalf("assembly=%+v err=%v", assembly, err)
	}
	assemblyReplay, err := handlers.AssembleOutput(ctx, capability.Request[capability.AssembleOutputIntent]{Command: command, Intent: capability.AssembleOutputIntent{Mode: "plan", OutputType: "vertical_video"}})
	if err != nil || assemblyReplay.Data.AssemblyRevisionID != assembly.Data.AssemblyRevisionID {
		t.Fatalf("assembly replay=%+v err=%v", assemblyReplay, err)
	}
}

func TestPlanStoryboardRejectsWhileNewerCreationSpecIsReviewing(t *testing.T) {
	ctx := context.Background()
	artifacts := artifact.NewMemoryStore()
	specs := &memorySpecs{values: []spec.FinalVideoSpec{
		{ID: "spec-1", SessionID: "session-1", Version: 1, Status: spec.StatusConfirmed, Title: "已确认规范", VideoType: "short_video", DurationSeconds: 8, AspectRatio: "9:16"},
		{ID: "spec-1", SessionID: "session-1", Version: 2, Status: spec.StatusReviewing, Title: "待审新规范", VideoType: "short_video", DurationSeconds: 12, AspectRatio: "9:16"},
	}}
	repository := storyboard.NewMemoryAggregateRepository()
	storyCommands, _ := storyboard.NewCommandService(repository)
	workflowStore := generation.NewMemoryStore()
	approvalCalls := 0
	runtime, err := New(Config{
		Model: scriptedModel{}, Artifacts: artifacts, Specs: specs,
		Storyboards: repository, StoryboardCommands: storyCommands,
		GenerationCommands: generation.NewCommandService(generation.CommandServiceConfig{Store: workflowStore}),
		CreateApproval: func(context.Context, ApprovalRequest) (string, error) {
			approvalCalls++
			return "unexpected", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = runtime.Handlers().PlanStoryboard(ctx, capability.Request[capability.PlanStoryboardIntent]{
		Command: capability.CommandContext{SessionID: "session-1", RequestID: "request-storyboard", IdempotencyKey: "idem-storyboard"},
		Intent:  capability.PlanStoryboardIntent{Mode: "replan"},
	})
	if !errors.Is(err, ErrCreationSpecReviewPending) {
		t.Fatalf("PlanStoryboard() error = %v, want ErrCreationSpecReviewPending", err)
	}
	if approvalCalls != 0 {
		t.Fatalf("storyboard approval created before Spec decision: %d", approvalCalls)
	}
	if _, err := repository.GetAggregateBySession(ctx, "session-1"); !errors.Is(err, storyboard.ErrAggregateNotFound) {
		t.Fatalf("storyboard side effect created before Spec decision: %v", err)
	}
}

func TestPrimaryReviewGateRunsBeforeNewCreationSpecSideEffects(t *testing.T) {
	ctx := context.Background()
	gateErr := errors.New("storyboard review is pending")
	specs := &memorySpecs{values: []spec.FinalVideoSpec{{
		ID: "spec-1", SessionID: "session-1", Version: 1, Status: spec.StatusConfirmed,
		Title: "已确认规范", VideoType: "short_video", DurationSeconds: 8, AspectRatio: "9:16",
	}}}
	repository := storyboard.NewMemoryAggregateRepository()
	storyCommands, _ := storyboard.NewCommandService(repository)
	workflowStore := generation.NewMemoryStore()
	approvalCalls := 0
	runtime, err := New(Config{
		Model: scriptedModel{}, Artifacts: artifact.NewMemoryStore(), Specs: specs,
		Storyboards: repository, StoryboardCommands: storyCommands,
		GenerationCommands: generation.NewCommandService(generation.CommandServiceConfig{Store: workflowStore}),
		PrimaryReviewGate:  func(context.Context, string) error { return gateErr },
		CreateApproval: func(context.Context, ApprovalRequest) (string, error) {
			approvalCalls++
			return "unexpected", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = runtime.Handlers().PlanCreationSpec(ctx, capability.Request[capability.PlanCreationSpecIntent]{
		Command: capability.CommandContext{SessionID: "session-1", RequestID: "request-revise", IdempotencyKey: "idem-revise"},
		Intent:  capability.PlanCreationSpecIntent{Mode: "revise", Goal: "revise while storyboard review is pending"},
	})
	if !errors.Is(err, gateErr) {
		t.Fatalf("PlanCreationSpec() error = %v, want primary review gate error", err)
	}
	if len(specs.values) != 1 || approvalCalls != 0 {
		t.Fatalf("primary review gate ran after side effects: specs=%+v approval_calls=%d", specs.values, approvalCalls)
	}
}

var _ einomodel.BaseChatModel = scriptedModel{}

func TestCompleteCandidatePromptsUsesInternalChatModelNode(t *testing.T) {
	runtime := &Runtime{modelGraph: mustModelGraph(t, scriptedModel{})}
	modules := []storyboard.StoryboardModule{{
		ID: "module-1", Key: "scenes", SemanticType: "scene", Title: "场景", PlannedCount: 1,
		Capabilities: storyboard.ModuleCapabilities{RequiresPrompt: false, RequiresAsset: true},
		Elements: []storyboard.StoryboardElement{{ID: "scene-1", Key: "scene-1", SemanticType: "scene", Title: "开场", Revision: 1,
			PromptSlots: []storyboard.PromptSlot{{Purpose: "keyframe", Prompt: "untrusted planner prompt", PromptRef: "untrusted-ref", LockedByUser: true}},
			AssetSlots:  []storyboard.AssetSlot{{Key: "keyframe", MediaKind: "image", Required: true}},
		}},
	}}
	if err := runtime.completeCandidatePrompts(context.Background(), spec.FinalVideoSpec{Version: 1}, artifact.Revision{}, modules); err != nil {
		t.Fatal(err)
	}
	if got := modules[0].Elements[0].PromptSlots; len(got) != 1 || got[0].Prompt != "generated cinematic keyframe" || got[0].PromptRef != "" || got[0].LockedByUser || got[0].Status != storyboard.PromptStatusReady {
		t.Fatalf("prompt slots = %+v", got)
	}
}

func TestCompleteCandidatePromptsRejectsIncompleteOrUnexpectedModelOutput(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{name: "partial", response: `{"prompts":[{"target_id":"scene","purpose":"image","prompt":"image prompt"}]}`},
		{name: "empty", response: `{"prompts":[]}`},
		{name: "duplicate", response: `{"prompts":[{"target_id":"scene","purpose":"image","prompt":"first"},{"target_id":"scene","purpose":"image","prompt":"second"},{"target_id":"scene","purpose":"motion","prompt":"motion prompt"}]}`},
		{name: "extra", response: `{"prompts":[{"target_id":"scene","purpose":"image","prompt":"image prompt"},{"target_id":"scene","purpose":"motion","prompt":"motion prompt"},{"target_id":"scene","purpose":"other","prompt":"extra prompt"}]}`},
		{name: "blank", response: `{"prompts":[{"target_id":"scene","purpose":"image","prompt":""},{"target_id":"scene","purpose":"motion","prompt":"motion prompt"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := &Runtime{modelGraph: mustModelGraph(t, promptResponseModel{response: test.response})}
			modules := []storyboard.StoryboardModule{{
				ID: "module", Key: "scenes", SemanticType: "scene", Title: "场景", PlannedCount: 1,
				Elements: []storyboard.StoryboardElement{{
					ID: "scene", Key: "scene", SemanticType: "scene", Title: "开场", Revision: 1,
					PromptSlots: []storyboard.PromptSlot{{Purpose: "image"}, {Purpose: "motion"}},
				}},
			}}
			if err := runtime.completeCandidatePrompts(context.Background(), spec.FinalVideoSpec{Version: 1}, artifact.Revision{}, modules); err == nil {
				t.Fatal("invalid prompt response unexpectedly succeeded")
			}
			for _, slot := range modules[0].Elements[0].PromptSlots {
				if slot.Prompt != "" {
					t.Fatalf("invalid response partially updated prompt slots: %+v", modules[0].Elements[0].PromptSlots)
				}
			}
		})
	}
}

func TestFillMissingPromptsRejectsIncompleteOrUnexpectedModelOutput(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{name: "partial", response: `{"prompts":[{"target_id":"scene","purpose":"image","prompt":"image prompt"}]}`},
		{name: "empty", response: `{"prompts":[]}`},
		{name: "duplicate", response: `{"prompts":[{"target_id":"scene","purpose":"image","prompt":"first"},{"target_id":"scene","purpose":"image","prompt":"second"},{"target_id":"scene","purpose":"motion","prompt":"motion prompt"}]}`},
		{name: "extra", response: `{"prompts":[{"target_id":"scene","purpose":"image","prompt":"image prompt"},{"target_id":"scene","purpose":"motion","prompt":"motion prompt"},{"target_id":"scene","purpose":"locked","prompt":"overwrite user prompt"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, aggregate, repository, _ := newPromptFillFixture(t, promptResponseModel{response: test.response})
			if _, err := runtime.fillMissingPrompts(context.Background(), capability.CommandContext{RequestID: "request"}, aggregate); err == nil {
				t.Fatal("invalid prompt response unexpectedly succeeded")
			}
			stored, err := repository.GetAggregate(context.Background(), aggregate.ID)
			if err != nil {
				t.Fatal(err)
			}
			revision, err := stored.ActiveRevision()
			if err != nil {
				t.Fatal(err)
			}
			prompts := revision.Modules[0].Elements[0].PromptSlots
			if prompts[0].Prompt != "" || prompts[0].Revision != 1 || prompts[1].Prompt != "" || prompts[1].Revision != 1 {
				t.Fatalf("invalid response partially updated prompts: %+v", prompts)
			}
			if prompts[2].Prompt != "user prompt" || !prompts[2].LockedByUser {
				t.Fatalf("locked prompt changed: %+v", prompts[2])
			}
		})
	}
}

func TestFillMissingPromptsPreservesPromptLockedWhileModelRuns(t *testing.T) {
	ctx := context.Background()
	var repository storyboard.AggregateRepository
	var commands *storyboard.CommandService
	lockApplied := false
	model := promptResponseModel{
		response: `{"prompts":[{"target_id":"scene","purpose":"image","prompt":"generated image"},{"target_id":"scene","purpose":"motion","prompt":"generated motion"}]}`,
		beforeResponse: func(ctx context.Context) error {
			if lockApplied {
				return nil
			}
			current, err := repository.GetAggregate(ctx, "board")
			if err != nil {
				return err
			}
			_, _, err = commands.UpdatePrompt(ctx, storyboard.UpdatePromptCommand{
				CommandID: "user-lock", StoryboardID: current.ID, BaseVersion: current.Version,
				TargetID: "scene", ExpectedTargetRevision: 1, Purpose: "image", ExpectedRevision: 1,
				Prompt: "user locked during generation", LockedByUser: true,
			})
			lockApplied = err == nil
			return err
		},
	}
	runtime, aggregate, repo, service := newPromptFillFixture(t, model)
	repository, commands = repo, service

	updated, err := runtime.fillMissingPrompts(ctx, capability.CommandContext{RequestID: "request"}, aggregate)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := updated.ActiveRevision()
	if err != nil {
		t.Fatal(err)
	}
	prompts := revision.Modules[0].Elements[0].PromptSlots
	if prompts[0].Prompt != "user locked during generation" || !prompts[0].LockedByUser {
		t.Fatalf("concurrently locked prompt was overwritten: %+v", prompts[0])
	}
	if prompts[1].Prompt != "generated motion" || prompts[1].Status != storyboard.PromptStatusReady {
		t.Fatalf("unlocked prompt was not generated: %+v", prompts[1])
	}
	if prompts[2].Prompt != "user prompt" || !prompts[2].LockedByUser {
		t.Fatalf("pre-existing locked prompt changed: %+v", prompts[2])
	}
}

func TestPlanningPropagatesMaterialAnalysisReadErrors(t *testing.T) {
	readErr := errors.New("artifact database unavailable")
	for _, test := range []struct {
		name string
		run  func(*Runtime) error
	}{
		{
			name: "creation spec",
			run: func(runtime *Runtime) error {
				_, err := runtime.Handlers().PlanCreationSpec(context.Background(), capability.Request[capability.PlanCreationSpecIntent]{
					Command: capability.CommandContext{SessionID: "session", RequestID: "request", IdempotencyKey: "idem"},
					Intent:  capability.PlanCreationSpecIntent{Mode: "create", Goal: "goal"},
				})
				return err
			},
		},
		{
			name: "storyboard",
			run: func(runtime *Runtime) error {
				_, err := runtime.Handlers().PlanStoryboard(context.Background(), capability.Request[capability.PlanStoryboardIntent]{
					Command: capability.CommandContext{SessionID: "session", RequestID: "request", IdempotencyKey: "idem"},
					Intent:  capability.PlanStoryboardIntent{Mode: "create"},
				})
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := storyboard.NewMemoryAggregateRepository()
			commands, err := storyboard.NewCommandService(repository)
			if err != nil {
				t.Fatal(err)
			}
			workflowStore := generation.NewMemoryStore()
			specValues := []spec.FinalVideoSpec{{ID: "spec", SessionID: "session", Version: 1, Status: spec.StatusConfirmed}}
			if test.name == "creation spec" {
				specValues = nil
			}
			runtime, err := New(Config{
				Model:       scriptedModel{},
				Artifacts:   getLatestErrorArtifactStore{Store: artifact.NewMemoryStore(), err: readErr},
				Specs:       &memorySpecs{values: specValues},
				Storyboards: repository, StoryboardCommands: commands,
				GenerationCommands: generation.NewCommandService(generation.CommandServiceConfig{Store: workflowStore}),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := test.run(runtime); !errors.Is(err, readErr) {
				t.Fatalf("planning error = %v, want wrapped material-analysis error", err)
			}
		})
	}
}

func newPromptFillFixture(t *testing.T, model einomodel.BaseChatModel) (*Runtime, storyboard.StoryboardAggregate, storyboard.AggregateRepository, *storyboard.CommandService) {
	t.Helper()
	ctx := context.Background()
	repository := storyboard.NewMemoryAggregateRepository()
	commands, err := storyboard.NewCommandService(repository)
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err := commands.Create(ctx, "board", "session")
	if err != nil {
		t.Fatal(err)
	}
	candidate := storyboard.StoryboardRevision{
		ID: "revision", StoryboardID: aggregate.ID, Scenario: "test",
		Modules: []storyboard.StoryboardModule{{
			ID: "module", Key: "scenes", SemanticType: "scene", Title: "Scenes", PlannedCount: 1,
			Elements: []storyboard.StoryboardElement{{
				ID: "scene", Key: "scene", SemanticType: "scene", Title: "Scene", Revision: 1,
				PromptSlots: []storyboard.PromptSlot{
					{Purpose: "image", Revision: 1, Status: storyboard.PromptStatusMissing},
					{Purpose: "motion", Revision: 1, Status: storyboard.PromptStatusStale},
					{Purpose: "locked", Prompt: "user prompt", Revision: 3, Status: storyboard.PromptStatusReady, LockedByUser: true},
				},
			}},
		}},
	}
	aggregate, _, err = commands.CreatePending(ctx, storyboard.CreatePendingRevisionCommand{
		CommandID: "plan", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate,
	})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = commands.DecidePending(ctx, storyboard.DecidePendingRevisionCommand{
		CommandID: "approve", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version,
		RevisionID: aggregate.PendingRevisionID, Decision: "approved",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Runtime{
		cfg:        Config{Storyboards: repository, StoryboardCommands: commands},
		modelGraph: mustModelGraph(t, model),
	}, aggregate, repository, commands
}

func TestAssemblyRetryUsesFrozenPlanAfterPreflightFailure(t *testing.T) {
	ctx := context.Background()
	artifacts := artifact.NewMemoryStore()
	specs := &memorySpecs{values: []spec.FinalVideoSpec{{ID: "spec-1", SessionID: "session-assembly", Version: 1, Status: spec.StatusConfirmed}}}
	repository := storyboard.NewMemoryAggregateRepository()
	commands, _ := storyboard.NewCommandService(repository)
	aggregate, _ := commands.Create(ctx, "board-assembly", "session-assembly")
	candidate := storyboard.StoryboardRevision{ID: "revision-assembly", DerivedFromSpecVersion: 1, Modules: []storyboard.StoryboardModule{{
		ID: "module", Key: "scenes", SemanticType: "scene", Title: "场景", PlannedCount: 1,
		Elements: []storyboard.StoryboardElement{{ID: "scene", Key: "scene", SemanticType: "scene", Title: "开场", Revision: 1,
			PromptSlots: []storyboard.PromptSlot{{Purpose: "image", Prompt: "original", Revision: 1, Status: storyboard.PromptStatusReady}},
			AssetSlots:  []storyboard.AssetSlot{{Key: "image", MediaKind: "image", Required: true, ReviewRequired: true, Status: storyboard.AssetSlotStatusMissing}},
		}},
	}}}
	aggregate, _, _ = commands.CreatePending(ctx, storyboard.CreatePendingRevisionCommand{CommandID: "plan", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	aggregate, _, _ = commands.DecidePending(ctx, storyboard.DecidePendingRevisionCommand{CommandID: "approve", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: aggregate.PendingRevisionID, Decision: "approved"})
	input, _ := aggregate.ResolveGenerationInput("scene", "image")
	aggregate, _, _ = commands.Bind(ctx, storyboard.BindAssetCommand{CommandID: "bind", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, BindingID: "binding", TargetID: "scene", AssetSlot: "image", AssetID: "asset", TargetRevision: input.TargetRevision, PromptRevision: input.PromptRevision, GenerationEpoch: input.GenerationEpoch, InputFingerprint: input.Fingerprint})
	aggregate, _, _ = commands.Activate(ctx, storyboard.ActivateBindingCommand{CommandID: "activate", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, BindingID: "binding"})
	frozenVersion := aggregate.Version

	workflowStore := generation.NewMemoryStore()
	preflightCalls := 0
	idCounter := 0
	runtime, err := New(Config{
		Model: scriptedModel{}, Artifacts: artifacts, Specs: specs, Storyboards: repository, StoryboardCommands: commands,
		GenerationCommands: generation.NewCommandService(generation.CommandServiceConfig{Store: workflowStore}), GenerationWorkflow: workflowStore,
		GenerationPreflight: func(context.Context, string, []generation.GenerationJob) (int64, error) {
			preflightCalls++
			if preflightCalls == 1 {
				return 0, fmt.Errorf("temporary preflight failure")
			}
			return 0, nil
		},
		NewID: func() string { idCounter++; return fmt.Sprintf("id-%d", idCounter) },
	})
	if err != nil {
		t.Fatal(err)
	}
	command := capability.CommandContext{SessionID: "session-assembly", UserID: "user-assembly", RequestID: "request-assembly", IdempotencyKey: "idem-assembly"}
	request := capability.Request[capability.AssembleOutputIntent]{Command: command, Intent: capability.AssembleOutputIntent{Mode: "preview", OutputType: "vertical_video"}}
	if _, err := runtime.Handlers().AssembleOutput(ctx, request); err == nil {
		t.Fatal("first assembly preflight unexpectedly succeeded")
	}
	conflictRequest := request
	conflictRequest.Intent.OutputType = "horizontal_video"
	conflictRequest.Intent.Instruction = "different request"
	if _, err := runtime.Handlers().AssembleOutput(ctx, conflictRequest); !errors.Is(err, artifact.ErrIdempotencyConflict) {
		t.Fatalf("changed assembly replay error = %v", err)
	}
	aggregate, _, err = commands.UpdatePrompt(ctx, storyboard.UpdatePromptCommand{CommandID: "later-edit", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, TargetID: "scene", Purpose: "image", ExpectedRevision: 1, Prompt: "changed", LockedByUser: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Handlers().AssembleOutput(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != capability.StatusAccepted || result.StoryboardVersion != frozenVersion {
		t.Fatalf("assembly retry=%+v", result)
	}
	operation, err := workflowStore.GetOperation(ctx, result.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := workflowStore.GetBatch(ctx, operation.BatchID)
	if err != nil || batch.WakePolicy != generation.WakeOnFailure {
		t.Fatalf("successful assembly batches must only wake the Agent on failure: batch=%+v err=%v", batch, err)
	}
	jobs, _ := workflowStore.ListJobsByBatch(ctx, operation.BatchID)
	if len(jobs) != 1 || jobs[0].StoryboardVersionAtDispatch != frozenVersion || intFromAny(jobs[0].Payload["manifest"].(map[string]any)["storyboard_version"]) != frozenVersion {
		t.Fatalf("assembly workflow was not frozen: %+v", jobs)
	}
}

func TestNormalizeGeneratedModulesAssignsUniquePlanningIdentity(t *testing.T) {
	modules := []storyboard.StoryboardModule{
		{Key: "scenes", Elements: []storyboard.StoryboardElement{
			{Key: "shot", AssetSlots: []storyboard.AssetSlot{{Key: "image", MediaKind: "image"}, {Key: "image", MediaKind: "image"}, {Key: "image_2", MediaKind: "image"}}},
			{Key: "shot"},
			{ID: "element_scenes_shot", Key: "shot_2"},
		}},
		{Key: "scenes", ID: "module_scenes", Elements: []storyboard.StoryboardElement{{Key: "shot"}}},
	}
	normalizeGeneratedModules(modules)
	ids := map[string]struct{}{}
	for _, module := range modules {
		if module.ID == "" || module.PlannedCount != len(module.Elements) {
			t.Fatalf("module was not normalized: %+v", module)
		}
		if _, duplicate := ids[module.ID]; duplicate {
			t.Fatalf("duplicate module id %q", module.ID)
		}
		ids[module.ID] = struct{}{}
		for _, element := range module.Elements {
			if element.ID == "" {
				t.Fatalf("empty element id: %+v", element)
			}
			if _, duplicate := ids[element.ID]; duplicate {
				t.Fatalf("duplicate element id %q", element.ID)
			}
			ids[element.ID] = struct{}{}
		}
	}
	if modules[0].Key == modules[1].Key {
		t.Fatalf("duplicate module keys were not normalized: %q", modules[0].Key)
	}
	slots := modules[0].Elements[0].AssetSlots
	if slots[0].Key == slots[1].Key || slots[0].Key == slots[2].Key || slots[1].Key == slots[2].Key {
		t.Fatalf("duplicate slot keys were not normalized: %+v", slots)
	}
}

func TestRequiredMissingRejectsStaleActiveBinding(t *testing.T) {
	revision := storyboard.StoryboardRevision{Modules: []storyboard.StoryboardModule{{Elements: []storyboard.StoryboardElement{{ID: "shot-1", AssetSlots: []storyboard.AssetSlot{{Key: "video", Required: true, ActiveBindingID: "binding-old", Status: storyboard.AssetSlotStatusStale}}}}}}}
	missing := requiredMissing(revision)
	if len(missing) != 1 || missing[0] != "shot-1:video" {
		t.Fatalf("missing dependencies = %#v", missing)
	}
}

func TestClassifyGenerateMediaNoOpStates(t *testing.T) {
	readyPrompt := []storyboard.PromptSlot{{Purpose: "image", Prompt: "ready", Revision: 1, Status: storyboard.PromptStatusReady}}
	for _, test := range []struct {
		name      string
		aggregate storyboard.StoryboardAggregate
		jobs      []generation.GenerationJob
		want      string
	}{
		{
			name: "candidate approval pending",
			aggregate: noOpAggregate([]storyboard.StoryboardElement{{
				ID: "image", Revision: 1, PromptSlots: readyPrompt,
				AssetSlots: []storyboard.AssetSlot{{Key: "image", MediaKind: "image", Required: true, Status: storyboard.AssetSlotStatusCandidate, CandidateIDs: []string{"binding-candidate"}}},
			}}, nil),
			want: GenerateMediaNoOpWaitingCandidateApproval,
		},
		{
			name: "generation job in flight",
			aggregate: noOpAggregate([]storyboard.StoryboardElement{{
				ID: "image", Revision: 1, PromptSlots: readyPrompt,
				AssetSlots: []storyboard.AssetSlot{{Key: "image", MediaKind: "image", Required: true, Status: storyboard.AssetSlotStatusMissing}},
			}}, nil),
			jobs: []generation.GenerationJob{{ID: "job", StoryboardID: "board-noop", Provider: generation.ProviderImage2, Status: generation.StatusRunning}},
			want: GenerateMediaNoOpGenerationJobsInFlight,
		},
		{
			name: "dependency blocked",
			aggregate: noOpAggregate([]storyboard.StoryboardElement{
				{ID: "script", Revision: 1, AssetSlots: []storyboard.AssetSlot{{Key: "script", MediaKind: "script", Required: true, Status: storyboard.AssetSlotStatusMissing}}},
				{ID: "video", Revision: 1, PromptSlots: []storyboard.PromptSlot{{Purpose: "video", Prompt: "animate", Revision: 1, Status: storyboard.PromptStatusReady}}, AssetSlots: []storyboard.AssetSlot{{Key: "video", MediaKind: "video", Required: true, Status: storyboard.AssetSlotStatusMissing}}},
			}, []storyboard.DependencyEdge{{FromTargetID: "script", FromSlot: "script", ToTargetID: "video", ToSlot: "video"}}),
			want: GenerateMediaNoOpDependencyBlocked,
		},
		{
			name: "production complete",
			aggregate: noOpAggregate([]storyboard.StoryboardElement{{
				ID: "image", Revision: 1, PromptSlots: readyPrompt,
				AssetSlots: []storyboard.AssetSlot{{Key: "image", MediaKind: "image", Required: true, Status: storyboard.AssetSlotStatusActive, ActiveBindingID: "binding-active"}},
			}}, nil),
			want: GenerateMediaNoOpProductionComplete,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := classifyGenerateMediaNoOp(test.aggregate, test.jobs)
			if err != nil || got != test.want {
				t.Fatalf("classifyGenerateMediaNoOp()=(%q,%v), want %q", got, err, test.want)
			}
		})
	}
}

func TestGenerateMediaNoOpReceiptFreezesReplay(t *testing.T) {
	ctx := context.Background()
	artifacts := artifact.NewMemoryStore()
	specs := &memorySpecs{values: []spec.FinalVideoSpec{{ID: "spec-noop", SessionID: "session-noop", Version: 1, Status: spec.StatusConfirmed}}}
	repository := storyboard.NewMemoryAggregateRepository()
	commands, err := storyboard.NewCommandService(repository)
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err := commands.Create(ctx, "board-noop-replay", "session-noop")
	if err != nil {
		t.Fatal(err)
	}
	candidate := storyboard.StoryboardRevision{ID: "revision-noop", DerivedFromSpecVersion: 1, Modules: []storyboard.StoryboardModule{{
		ID: "module", Key: "visual", SemanticType: "visual", Title: "Visual", PlannedCount: 1,
		Elements: []storyboard.StoryboardElement{{ID: "image", Key: "image", SemanticType: "image", Title: "Image", Revision: 1,
			PromptSlots: []storyboard.PromptSlot{{Purpose: "image", Prompt: "ready", Revision: 1, Status: storyboard.PromptStatusReady}},
			AssetSlots:  []storyboard.AssetSlot{{Key: "image", MediaKind: "image", Required: true, ReviewRequired: true, Status: storyboard.AssetSlotStatusMissing}},
		}},
	}}}
	aggregate, _, err = commands.CreatePending(ctx, storyboard.CreatePendingRevisionCommand{CommandID: "plan-noop", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = commands.DecidePending(ctx, storyboard.DecidePendingRevisionCommand{CommandID: "approve-noop", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: aggregate.PendingRevisionID, Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	input, err := aggregate.ResolveGenerationInput("image", "image")
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = commands.Bind(ctx, storyboard.BindAssetCommand{CommandID: "bind-noop", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, BindingID: "binding-noop", TargetID: "image", AssetSlot: "image", AssetID: "asset-noop", TargetRevision: input.TargetRevision, PromptRevision: input.PromptRevision, GenerationEpoch: input.GenerationEpoch, InputFingerprint: input.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = commands.Activate(ctx, storyboard.ActivateBindingCommand{CommandID: "activate-noop", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, BindingID: "binding-noop"})
	if err != nil {
		t.Fatal(err)
	}
	completedVersion := aggregate.Version
	workflowStore := generation.NewMemoryStore()
	runtime, err := New(Config{
		Model: scriptedModel{}, Artifacts: artifacts, Specs: specs, Storyboards: repository, StoryboardCommands: commands,
		GenerationCommands: generation.NewCommandService(generation.CommandServiceConfig{Store: workflowStore}),
		GenerationJobs:     workflowStore, GenerationWorkflow: workflowStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := capability.Request[capability.GenerateMediaIntent]{
		Command: capability.CommandContext{SessionID: "session-noop", UserID: "user-noop", RequestID: "request-noop", IdempotencyKey: "idem-noop"},
		Intent:  capability.GenerateMediaIntent{Phase: "auto_next", Policy: "all_eligible"},
	}
	first, err := runtime.Handlers().GenerateMedia(ctx, request)
	if err != nil || !first.Data.NoOp || first.Data.Reason != GenerateMediaNoOpProductionComplete || first.StoryboardVersion != completedVersion {
		t.Fatalf("first no-op=%+v err=%v", first, err)
	}
	aggregate, _, err = commands.UpdatePrompt(ctx, storyboard.UpdatePromptCommand{CommandID: "edit-after-noop", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, TargetID: "image", Purpose: "image", ExpectedRevision: 1, Prompt: "changed", LockedByUser: true})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := runtime.Handlers().GenerateMedia(ctx, request)
	if err != nil || replay.Data.Reason != GenerateMediaNoOpProductionComplete || replay.StoryboardVersion != completedVersion {
		t.Fatalf("frozen replay=%+v err=%v", replay, err)
	}
	changed := request
	changed.Intent.Policy = "single_next"
	if _, err := runtime.Handlers().GenerateMedia(ctx, changed); !errors.Is(err, artifact.ErrIdempotencyConflict) {
		t.Fatalf("changed-intent replay error=%v", err)
	}
}

func noOpAggregate(elements []storyboard.StoryboardElement, dependencies []storyboard.DependencyEdge) storyboard.StoryboardAggregate {
	return storyboard.StoryboardAggregate{
		ID: "board-noop", SessionID: "session-noop", Version: 1, ActiveRevisionID: "revision-noop", Status: storyboard.AggregateStatusActive,
		Revisions: []storyboard.StoryboardRevision{{
			ID: "revision-noop", StoryboardID: "board-noop", Status: storyboard.RevisionStatusActive, Dependencies: dependencies,
			Modules: []storyboard.StoryboardModule{{ID: "module-noop", Key: "module", SemanticType: "test", PlannedCount: len(elements), Elements: elements}},
		}},
	}
}

func TestCreationSpecValidationSupportsChineseAudioOnlyScenarios(t *testing.T) {
	for _, videoType := range []string{"音乐创作", "歌曲", "播客节目"} {
		candidate := creationSpecModel{Title: "音频作品", VideoType: videoType, DurationSeconds: 30}
		if err := validateCreationSpecCandidate(candidate); err != nil {
			t.Fatalf("video_type=%q error=%v", videoType, err)
		}
	}
}

func mustModelGraph(t *testing.T, model einomodel.BaseChatModel) compose.Runnable[modelGraphRequest, json.RawMessage] {
	t.Helper()
	graph, err := compileModelGraph(context.Background(), model)
	if err != nil {
		t.Fatal(err)
	}
	return graph
}
