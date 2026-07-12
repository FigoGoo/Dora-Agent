// Package capabilityruntime implements the production handlers behind the five
// Agent-facing capability graphs. Provider calls, persistence and business
// commands remain internal nodes and are never registered as Agent tools.
package capabilityruntime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/artifact"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type SpecStore interface {
	Save(context.Context, spec.FinalVideoSpec) (spec.FinalVideoSpec, error)
	GetByIdempotencyKey(context.Context, string) (spec.FinalVideoSpec, error)
	GetLatestBySession(context.Context, string) (spec.FinalVideoSpec, error)
	GetConfirmedBySession(context.Context, string) (spec.FinalVideoSpec, error)
}

type AssetStore interface {
	Get(context.Context, string) (asset.Asset, error)
}

type GenerationJobSource interface {
	ListBySession(context.Context, string) ([]generation.GenerationJob, error)
}

type GenerationWorkflowSource interface {
	GetOperationByIdempotencyKey(context.Context, string) (generation.GenerationOperation, error)
	GetBatch(context.Context, string) (generation.GenerationBatch, error)
	ListJobsByBatch(context.Context, string) ([]generation.GenerationJob, error)
}

type GenerationPreflight func(context.Context, string, []generation.GenerationJob) (int64, error)

type ApprovalCommand struct {
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload,omitempty"`
}

// ApprovalRequest is deliberately package-neutral. main wires it to the
// approval domain without exposing approval persistence as an Agent tool.
type ApprovalRequest struct {
	ID                    string
	SessionID             string
	UserID                string
	ArtifactKind          string
	ArtifactID            string
	ArtifactVersion       int
	StoryboardID          string
	StoryboardVersion     int
	ReviewMode            string
	ExecutionMode         string
	Approve               ApprovalCommand
	Reject                ApprovalCommand
	VersionMismatchPolicy string
}

type ApprovalCreator func(context.Context, ApprovalRequest) (string, error)
type StoryboardPublisher func(context.Context, storyboard.StoryboardAggregate, string) error
type PrimaryReviewGate func(context.Context, string) error

// approvalPayload freezes the trusted Tool correlation beside the semantic
// command arguments. The command executor ignores these correlation fields;
// the system Decision publisher uses them to close the exact waiting ToolRun
// that produced this durable Approval.
func approvalPayload(command capability.CommandContext, values map[string]any) map[string]any {
	out := make(map[string]any, len(values)+2)
	for key, value := range values {
		out[key] = value
	}
	if toolCallID := strings.TrimSpace(command.ToolCallID); toolCallID != "" {
		out["tool_call_id"] = toolCallID
	}
	if stageRunID := strings.TrimSpace(command.StageRunID); stageRunID != "" {
		out["stage_run_id"] = stageRunID
	}
	return out
}

type Config struct {
	Model               einomodel.BaseChatModel
	Artifacts           artifact.Store
	Specs               SpecStore
	Assets              AssetStore
	Storyboards         storyboard.AggregateRepository
	StoryboardCommands  *storyboard.CommandService
	GenerationCommands  *generation.CommandService
	GenerationJobs      GenerationJobSource
	GenerationWorkflow  GenerationWorkflowSource
	GenerationPreflight GenerationPreflight
	CreateApproval      ApprovalCreator
	PrimaryReviewGate   PrimaryReviewGate
	PublishStoryboard   StoryboardPublisher
	// LocalDemoPlanning allows deterministic storyboard/prompt fallbacks only
	// for the local acceptance environment. Production remains fail closed.
	LocalDemoPlanning bool
	NewID             func() string
	Now               func() time.Time
}

type Runtime struct {
	cfg        Config
	modelGraph compose.Runnable[modelGraphRequest, json.RawMessage]
}

const (
	generateMediaNoOpArtifactKind               = "generate_media_noop_receipt"
	GenerateMediaNoOpWaitingCandidateApproval   = "waiting_candidate_approval"
	GenerateMediaNoOpGenerationJobsInFlight     = "generation_jobs_in_flight"
	GenerateMediaNoOpDependencyBlocked          = "dependency_blocked"
	GenerateMediaNoOpProductionComplete         = "production_complete"
	GenerateMediaNoOpNoTargetsForRequestedPhase = "no_targets_for_requested_phase"
)

var ErrCreationSpecReviewPending = errors.New("creation spec review must be decided before storyboard planning")

func New(config Config) (*Runtime, error) {
	if config.Artifacts == nil || config.Specs == nil || config.Storyboards == nil || config.StoryboardCommands == nil || config.GenerationCommands == nil {
		return nil, fmt.Errorf("artifact, spec, storyboard and generation dependencies are required")
	}
	if config.NewID == nil {
		config.NewID = newID
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	modelGraph, err := compileModelGraph(context.Background(), config.Model)
	if err != nil {
		return nil, fmt.Errorf("compile internal ChatModel graph: %w", err)
	}
	return &Runtime{cfg: config, modelGraph: modelGraph}, nil
}

func (r *Runtime) Handlers() capability.Handlers {
	return capability.Handlers{
		AnalyzeMaterials: r.analyzeMaterials,
		PlanCreationSpec: r.planCreationSpec,
		PlanStoryboard:   r.planStoryboard,
		GenerateMedia:    r.generateMedia,
		AssembleOutput:   r.assembleOutput,
	}
}

type materialAnalysisModel struct {
	Summary          string         `json:"summary"`
	Facts            map[string]any `json:"facts,omitempty"`
	Constraints      []string       `json:"constraints,omitempty"`
	ReusableAssetIDs []string       `json:"reusable_asset_ids,omitempty"`
	MissingInputs    []string       `json:"missing_inputs,omitempty"`
}

func (r *Runtime) analyzeMaterials(ctx context.Context, request capability.Request[capability.AnalyzeMaterialsIntent]) (capability.CapabilityResult[capability.AnalyzeMaterialsData], error) {
	if existing, err := r.cfg.Artifacts.GetByIdempotencyKey(ctx, request.Command.IdempotencyKey); err == nil {
		if existing.SessionID != request.Command.SessionID || existing.Kind != artifact.KindMaterialAnalysis {
			return capability.CapabilityResult[capability.AnalyzeMaterialsData]{}, artifact.ErrIdempotencyConflict
		}
		return materialAnalysisCapabilityResult(existing)
	} else if !errors.Is(err, artifact.ErrNotFound) {
		return capability.CapabilityResult[capability.AnalyzeMaterialsData]{}, err
	}
	if len(compact(request.Intent.AssetIDs)) > 0 && r.cfg.Assets == nil {
		return capability.CapabilityResult[capability.AnalyzeMaterialsData]{}, fmt.Errorf("asset store is required to analyze asset_ids")
	}
	assets := make([]asset.Asset, 0, len(request.Intent.AssetIDs))
	for _, id := range compact(request.Intent.AssetIDs) {
		if r.cfg.Assets == nil {
			break
		}
		item, err := r.cfg.Assets.Get(ctx, id)
		if err != nil {
			return capability.CapabilityResult[capability.AnalyzeMaterialsData]{}, fmt.Errorf("load analysis asset %s: %w", id, err)
		}
		if item.SessionID != "" && item.SessionID != request.Command.SessionID {
			return capability.CapabilityResult[capability.AnalyzeMaterialsData]{}, fmt.Errorf("asset %s does not belong to session", id)
		}
		if item.Availability != "" && item.Availability != asset.AvailabilityAvailable {
			return capability.CapabilityResult[capability.AnalyzeMaterialsData]{}, fmt.Errorf("asset %s is not available", id)
		}
		assets = append(assets, item)
	}
	input := map[string]any{"goal": request.Intent.Goal, "instruction": request.Intent.Instruction, "assets": assets}
	analysis := materialAnalysisModel{}
	if err := r.generateJSON(ctx, "你是素材清单与元数据分析节点。只能根据已提供的文字、文件名、MIME、URL 和可信 metadata 提取事实；不得臆测图片、音视频或 PDF 的实际内容。无法读取的内容必须写入 missing_inputs。只返回合法 JSON。", input, &analysis); err != nil {
		return capability.CapabilityResult[capability.AnalyzeMaterialsData]{}, err
	}
	if strings.TrimSpace(analysis.Summary) == "" {
		return capability.CapabilityResult[capability.AnalyzeMaterialsData]{}, fmt.Errorf("material analysis summary is empty")
	}
	allowedAssets := map[string]struct{}{}
	for _, item := range assets {
		allowedAssets[item.ID] = struct{}{}
	}
	if len(analysis.ReusableAssetIDs) == 0 {
		analysis.ReusableAssetIDs = compact(request.Intent.AssetIDs)
	} else {
		filtered := make([]string, 0, len(analysis.ReusableAssetIDs))
		for _, assetID := range compact(analysis.ReusableAssetIDs) {
			if _, allowed := allowedAssets[assetID]; allowed {
				filtered = append(filtered, assetID)
			}
		}
		analysis.ReusableAssetIDs = filtered
	}
	for _, item := range assets {
		if item.Kind == asset.KindImage || item.Kind == asset.KindVideo || item.Kind == asset.KindAudio || item.Kind == asset.KindPDF || item.Kind == asset.KindReference {
			analysis.MissingInputs = append(analysis.MissingInputs, fmt.Sprintf("asset %s requires a configured multimodal/content extraction node", item.ID))
		}
	}
	analysis.MissingInputs = uniqueStrings(analysis.MissingInputs)
	id := stableArtifactID(request.Command.SessionID, artifact.KindMaterialAnalysis, request.Command.IdempotencyKey)
	created, err := r.cfg.Artifacts.CreateRevision(ctx, artifact.Revision{
		ID: id, SessionID: request.Command.SessionID, Kind: artifact.KindMaterialAnalysis,
		Status: artifact.StatusActive, IdempotencyKey: request.Command.IdempotencyKey,
		CreatedBy: request.Command.UserID,
		Content:   map[string]any{"summary": analysis.Summary, "facts": analysis.Facts, "constraints": analysis.Constraints, "reusable_asset_ids": analysis.ReusableAssetIDs, "missing_inputs": analysis.MissingInputs},
	})
	if err != nil {
		return capability.CapabilityResult[capability.AnalyzeMaterialsData]{}, err
	}
	return materialAnalysisCapabilityResult(created.Revision)
}

func materialAnalysisCapabilityResult(revision artifact.Revision) (capability.CapabilityResult[capability.AnalyzeMaterialsData], error) {
	var analysis materialAnalysisModel
	raw, err := json.Marshal(revision.Content)
	if err != nil {
		return capability.CapabilityResult[capability.AnalyzeMaterialsData]{}, err
	}
	if err := json.Unmarshal(raw, &analysis); err != nil {
		return capability.CapabilityResult[capability.AnalyzeMaterialsData]{}, err
	}
	if strings.TrimSpace(analysis.Summary) == "" {
		return capability.CapabilityResult[capability.AnalyzeMaterialsData]{}, fmt.Errorf("persisted material analysis summary is empty")
	}
	status := capability.StatusCompleted
	if len(analysis.MissingInputs) > 0 {
		status = capability.StatusPartial
	}
	return capability.CapabilityResult[capability.AnalyzeMaterialsData]{Status: status, Data: capability.AnalyzeMaterialsData{
		AnalysisID: revision.ID, AnalysisVersion: revision.Version, Summary: analysis.Summary,
		ReusableAssetIDs: analysis.ReusableAssetIDs, MissingInputs: analysis.MissingInputs,
	}}, nil
}

type creationSpecModel struct {
	Title           string         `json:"title"`
	VideoType       string         `json:"video_type"`
	TargetAudience  string         `json:"target_audience"`
	OutputLanguage  string         `json:"output_language"`
	DurationSeconds int            `json:"duration_seconds"`
	AspectRatio     string         `json:"aspect_ratio"`
	NarrativeDriver string         `json:"narrative_driver"`
	VisualStyle     string         `json:"visual_style"`
	SoundStyle      string         `json:"sound_style"`
	ModelPreference string         `json:"model_preference"`
	Markdown        string         `json:"markdown"`
	Fields          map[string]any `json:"fields"`
}

const creationSpecPlannerInstruction = `你是创作规范规划节点。必须只返回一个合法 JSON object，字段名严格使用以下 schema，不得改名、缩写或输出额外说明：
{"title":"非空标题","video_type":"short_video","target_audience":"目标受众","output_language":"zh-CN","duration_seconds":8,"aspect_ratio":"16:9","narrative_driver":"叙事驱动","visual_style":"视觉风格","sound_style":"声音风格","model_preference":"模型偏好","markdown":"供用户审核的完整 Markdown 规范","fields":{}}
title、video_type 必须是非空字符串；duration_seconds 必须是正整数；非纯音频内容的 aspect_ratio 必须是非空字符串。严格采用用户明确给出的时长和画幅，不要因为投放平台惯例擅自覆盖。fields 必须是 JSON object。`

func (r *Runtime) planCreationSpec(ctx context.Context, request capability.Request[capability.PlanCreationSpecIntent]) (capability.CapabilityResult[capability.PlanCreationSpecData], error) {
	if existing, err := r.cfg.Specs.GetByIdempotencyKey(ctx, request.Command.IdempotencyKey); err == nil {
		if existing.SessionID != request.Command.SessionID {
			return capability.CapabilityResult[capability.PlanCreationSpecData]{}, fmt.Errorf("creation spec idempotency key belongs to another session")
		}
		return r.creationSpecCapabilityResult(ctx, request, existing)
	} else if !errors.Is(err, spec.ErrNotFound) {
		return capability.CapabilityResult[capability.PlanCreationSpecData]{}, err
	}
	if request.Intent.Mode == "create" {
		existing, err := r.cfg.Specs.GetLatestBySession(ctx, request.Command.SessionID)
		if err == nil {
			return r.creationSpecCapabilityResult(ctx, request, existing)
		}
		if !errors.Is(err, spec.ErrNotFound) {
			return capability.CapabilityResult[capability.PlanCreationSpecData]{}, err
		}
	}
	if r.cfg.PrimaryReviewGate != nil {
		if err := r.cfg.PrimaryReviewGate(ctx, request.Command.SessionID); err != nil {
			return capability.CapabilityResult[capability.PlanCreationSpecData]{}, err
		}
	}
	var previous spec.FinalVideoSpec
	if request.Intent.Mode == "revise" {
		loaded, err := r.cfg.Specs.GetConfirmedBySession(ctx, request.Command.SessionID)
		if err != nil && !errors.Is(err, spec.ErrNotFound) {
			return capability.CapabilityResult[capability.PlanCreationSpecData]{}, err
		}
		previous = loaded
	}
	var analysis artifact.Revision
	analysis, err := r.cfg.Artifacts.GetLatest(ctx, request.Command.SessionID, artifact.KindMaterialAnalysis)
	if err != nil && !errors.Is(err, artifact.ErrNotFound) {
		return capability.CapabilityResult[capability.PlanCreationSpecData]{}, fmt.Errorf("load material analysis: %w", err)
	}
	input := map[string]any{"mode": request.Intent.Mode, "background": request.Intent.Background, "goal": request.Intent.Goal, "instruction": request.Intent.Instruction, "previous_spec": previous, "material_analysis": analysis.Content}
	candidate, err := r.generateCreationSpecCandidate(ctx, input)
	if err != nil {
		return capability.CapabilityResult[capability.PlanCreationSpecData]{}, err
	}
	specID := previous.ID
	if specID == "" {
		specID = "spec:" + request.Command.SessionID
	}
	saved, err := r.cfg.Specs.Save(ctx, spec.FinalVideoSpec{
		ID: specID, SessionID: request.Command.SessionID, Status: spec.StatusReviewing,
		IdempotencyKey: request.Command.IdempotencyKey,
		Title:          candidate.Title, VideoType: candidate.VideoType, TargetAudience: candidate.TargetAudience,
		OutputLanguage: candidate.OutputLanguage, DurationSeconds: candidate.DurationSeconds,
		AspectRatio: candidate.AspectRatio, NarrativeDriver: candidate.NarrativeDriver,
		VisualStyle: candidate.VisualStyle, SoundStyle: candidate.SoundStyle,
		ModelPreference: candidate.ModelPreference, Markdown: candidate.Markdown, Fields: candidate.Fields,
	})
	if err != nil {
		return capability.CapabilityResult[capability.PlanCreationSpecData]{}, err
	}
	return r.creationSpecCapabilityResult(ctx, request, saved)
}

// generateCreationSpecCandidate retries one pre-persistence model/schema
// failure. Internal Capability inference is not covered by the outer Agent
// receipt, so no durable side effect may occur before this function succeeds.
func (r *Runtime) generateCreationSpecCandidate(ctx context.Context, input any) (creationSpecModel, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		candidate := creationSpecModel{}
		system := creationSpecPlannerInstruction
		if attempt > 0 {
			system += "\n上一轮输出未通过上述 schema 校验。重新生成完整 JSON；不要解释错误，也不要复述上一轮输出。"
		}
		if err := r.generateJSON(ctx, system, input, &candidate); err != nil {
			lastErr = err
			continue
		}
		if err := validateCreationSpecCandidate(candidate); err != nil {
			lastErr = err
			continue
		}
		return candidate, nil
	}
	return creationSpecModel{}, lastErr
}

func (r *Runtime) creationSpecCapabilityResult(ctx context.Context, request capability.Request[capability.PlanCreationSpecIntent], saved spec.FinalVideoSpec) (capability.CapabilityResult[capability.PlanCreationSpecData], error) {
	approvalID := stableApprovalRequestID(request.Command.SessionID, "creation_spec_revision", saved.ID, saved.Version)
	if saved.Status == spec.StatusReviewing {
		var err error
		approvalID, err = r.createApproval(ctx, ApprovalRequest{
			ID: approvalID, SessionID: request.Command.SessionID, UserID: request.Command.UserID,
			ArtifactKind: "creation_spec_revision", ArtifactID: saved.ID, ArtifactVersion: saved.Version,
			ReviewMode: "durable", ExecutionMode: "durable", VersionMismatchPolicy: "mark_stale",
			Approve: ApprovalCommand{Kind: "ActivateCreationSpecRevision", Payload: approvalPayload(request.Command, map[string]any{"spec_id": saved.ID, "spec_version": saved.Version})},
			Reject:  ApprovalCommand{Kind: "RejectCreationSpecRevision", Payload: approvalPayload(request.Command, map[string]any{"spec_id": saved.ID, "spec_version": saved.Version})},
		})
		if err != nil {
			return capability.CapabilityResult[capability.PlanCreationSpecData]{}, err
		}
	}
	candidateMap := structMap(creationSpecModel{
		Title: saved.Title, VideoType: saved.VideoType, TargetAudience: saved.TargetAudience,
		OutputLanguage: saved.OutputLanguage, DurationSeconds: saved.DurationSeconds,
		AspectRatio: saved.AspectRatio, NarrativeDriver: saved.NarrativeDriver,
		VisualStyle: saved.VisualStyle, SoundStyle: saved.SoundStyle,
		ModelPreference: saved.ModelPreference, Markdown: saved.Markdown, Fields: saved.Fields,
	})
	status := capability.StatusWaitingUser
	switch saved.Status {
	case spec.StatusConfirmed:
		status = capability.StatusCompleted
	case spec.StatusRejected, spec.StatusSuperseded:
		status = capability.StatusCancelled
	}
	return capability.CapabilityResult[capability.PlanCreationSpecData]{
		Status: status,
		Data:   capability.PlanCreationSpecData{SpecID: saved.ID, SpecVersion: saved.Version, Status: saved.Status, Candidate: candidateMap, ApprovalID: approvalID, StoryboardReplanRequired: request.Intent.Mode == "revise" || saved.Version > 1},
	}, nil
}

type storyboardModelResponse struct {
	Scenario     string                        `json:"scenario"`
	Modules      []storyboard.StoryboardModule `json:"modules"`
	Dependencies []storyboard.DependencyEdge   `json:"dependencies,omitempty"`
}

const storyboardPlannerInstruction = `你是动态故事板规划节点（元素规划阶段）。根据输入场景动态推理模块和元素数量；只声明 PromptSlot 的 purpose，不得填写 prompt 文本。只返回一个严格 JSON object，禁止 Markdown、解释或其他顶层字段。JSON 必须严格使用以下字段结构：
{"scenario":"场景摘要","modules":[{"id":"稳定模块ID","key":"稳定模块key","semantic_type":"scene|shot|audio_layer 等语义类型","title":"模块标题","description":"可选说明","order":1,"planned_count":1,"required":true,"review_required":true,"revision":1,"capabilities":{"has_quantity":true,"requires_prompt":true,"requires_asset":true,"has_timeline":false,"output_modality":"image|video|audio"},"elements":[{"id":"稳定元素ID","key":"稳定元素key","semantic_type":"元素语义类型","title":"元素标题","revision":1,"content":{"description":"内容描述"},"prompt_slots":[{"purpose":"与素材用途一致的稳定 purpose","revision":1,"status":"missing"}],"asset_slots":[{"key":"稳定素材槽 key","role":"用途","media_kind":"image|illustration|keyframe|video|audio|music|voice|text|script|lyrics","required":true,"review_required":true,"generation_epoch":0,"status":"missing"}]}]}],"dependencies":[]}
modules 必须至少一个，每个 module.elements 必须至少一个，planned_count 必须等于 elements 数量。不要使用 storyboard、shots、scenes 等别名替代顶层 modules。`

func (r *Runtime) planStoryboard(ctx context.Context, request capability.Request[capability.PlanStoryboardIntent]) (capability.CapabilityResult[capability.PlanStoryboardData], error) {
	latestReviewState, err := r.cfg.Specs.GetLatestBySession(ctx, request.Command.SessionID)
	if err != nil && !errors.Is(err, spec.ErrNotFound) {
		return capability.CapabilityResult[capability.PlanStoryboardData]{}, fmt.Errorf("load latest creation spec: %w", err)
	}
	if err == nil && latestReviewState.Status == spec.StatusReviewing {
		return capability.CapabilityResult[capability.PlanStoryboardData]{}, fmt.Errorf("%w: spec_id=%s spec_version=%d", ErrCreationSpecReviewPending, latestReviewState.ID, latestReviewState.Version)
	}
	latestSpec, err := r.cfg.Specs.GetConfirmedBySession(ctx, request.Command.SessionID)
	if err != nil {
		return capability.CapabilityResult[capability.PlanStoryboardData]{}, fmt.Errorf("load creation spec: %w", err)
	}
	analysis, err := r.cfg.Artifacts.GetLatest(ctx, request.Command.SessionID, artifact.KindMaterialAnalysis)
	if err != nil && !errors.Is(err, artifact.ErrNotFound) {
		return capability.CapabilityResult[capability.PlanStoryboardData]{}, fmt.Errorf("load material analysis: %w", err)
	}
	aggregate, err := r.cfg.Storyboards.GetAggregateBySession(ctx, request.Command.SessionID)
	if errors.Is(err, storyboard.ErrAggregateNotFound) {
		aggregate, err = r.cfg.StoryboardCommands.Create(ctx, "storyboard:"+request.Command.SessionID, request.Command.SessionID)
	}
	if err != nil {
		return capability.CapabilityResult[capability.PlanStoryboardData]{}, err
	}
	// A turn may invoke plan_storyboard more than once. RequestID identifies the
	// whole turn, while the trusted tool wrapper derives IdempotencyKey from the
	// canonical intent. Use the latter as the storyboard command identity so a
	// second, different plan in the same turn cannot be mistaken for a replay.
	planCommandID := request.Command.IdempotencyKey
	if aggregate.HasApplied(planCommandID) {
		revision, diff, planVersion, reviewStoryboardVersion, replayErr := r.storyboardPlanReplay(ctx, aggregate, planCommandID)
		if replayErr != nil {
			return capability.CapabilityResult[capability.PlanStoryboardData]{}, replayErr
		}
		return r.storyboardPlanCapabilityResult(ctx, request, aggregate, revision, diff, planVersion, reviewStoryboardVersion)
	}
	if r.cfg.PrimaryReviewGate != nil {
		if err := r.cfg.PrimaryReviewGate(ctx, request.Command.SessionID); err != nil {
			return capability.CapabilityResult[capability.PlanStoryboardData]{}, err
		}
	}
	if request.Intent.Mode == "create" && aggregate.ActiveRevisionID != "" {
		return capability.CapabilityResult[capability.PlanStoryboardData]{}, fmt.Errorf("storyboard already exists; use mode=replan for a whole-plan revision")
	}
	input := map[string]any{
		"mode": request.Intent.Mode, "instruction": request.Intent.Instruction,
		"creation_spec": latestSpec, "material_analysis": analysis.Content,
		"active_storyboard": activeRevisionOrNil(aggregate),
		"requirements":      fmt.Sprintf("根据场景动态创建模块；每个模块给出 planned_count；每个元素包含内容、PromptSlot purpose（禁止填写 prompt 文本）、所需 AssetSlot 和稳定语义 key。AssetSlot.media_kind 只能使用 %s；其中 text、script、lyrics 由用户素材满足，不会自动生成。音乐、短剧、广告等只创建真正需要的模块，不使用固定元素枚举。提示词由后继独立 ChatModel 节点统一生成。", strings.Join(storyboard.SupportedAssetSlotMediaKinds(), "、")),
	}
	generated, err := r.generateStoryboardCandidate(ctx, input, latestSpec)
	if err != nil {
		return capability.CapabilityResult[capability.PlanStoryboardData]{}, err
	}
	if err := r.completeCandidatePrompts(ctx, latestSpec, analysis, generated.Modules); err != nil {
		return capability.CapabilityResult[capability.PlanStoryboardData]{}, err
	}
	candidate := storyboard.StoryboardRevision{
		ID: r.cfg.NewID(), StoryboardID: aggregate.ID, Scenario: generated.Scenario,
		DerivedFromSpecVersion: latestSpec.Version, DerivedFromAnalysisVersion: analysis.Version,
		Modules: generated.Modules, Dependencies: generated.Dependencies,
	}
	updated, diff, err := r.cfg.StoryboardCommands.CreatePending(ctx, storyboard.CreatePendingRevisionCommand{
		CommandID: planCommandID, IdempotencyKey: request.Command.IdempotencyKey,
		StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Actor: request.Command.UserID,
		Source: capability.PlanStoryboardToolKey, Candidate: candidate,
		PreserveApprovedAssets: request.Intent.PreserveApprovedAssets,
	})
	if err != nil {
		return capability.CapabilityResult[capability.PlanStoryboardData]{}, err
	}
	pending, err := updated.PendingRevision()
	if err != nil {
		return capability.CapabilityResult[capability.PlanStoryboardData]{}, err
	}
	return r.storyboardPlanCapabilityResult(ctx, request, updated, *pending, diff, updated.PlanRevision+1, updated.Version)
}

func (r *Runtime) generateStoryboardCandidate(ctx context.Context, input any, creationSpec spec.FinalVideoSpec) (storyboardModelResponse, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		system := storyboardPlannerInstruction
		if attempt > 0 {
			system += "\n上一轮输出未通过 Storyboard schema 校验。请重新生成完整对象，严格使用顶层 modules，并确保每个 module 至少包含一个 element。"
		}
		generated := storyboardModelResponse{}
		if err := r.generateJSON(ctx, system, input, &generated); err != nil {
			lastErr = err
			continue
		}
		normalizeGeneratedModules(generated.Modules)
		if err := validateStoryboardCandidate(generated); err != nil {
			lastErr = err
			continue
		}
		return generated, nil
	}
	if r.cfg.LocalDemoPlanning {
		generated := localDemoStoryboardCandidate(creationSpec)
		normalizeGeneratedModules(generated.Modules)
		if err := validateStoryboardCandidate(generated); err != nil {
			return storyboardModelResponse{}, fmt.Errorf("validate local demo storyboard fallback: %w", err)
		}
		return generated, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("storyboard planner returned no valid candidate")
	}
	return storyboardModelResponse{}, lastErr
}

func validateStoryboardCandidate(candidate storyboardModelResponse) error {
	if len(candidate.Modules) == 0 {
		return fmt.Errorf("storyboard planner returned no modules")
	}
	for index, module := range candidate.Modules {
		if len(module.Elements) == 0 {
			return fmt.Errorf("storyboard planner module %d returned no elements", index)
		}
	}
	return storyboard.ValidateRevision(storyboard.StoryboardRevision{
		ID: "candidate-validation", StoryboardID: "candidate-validation", Modules: candidate.Modules,
	})
}

func localDemoStoryboardCandidate(creationSpec spec.FinalVideoSpec) storyboardModelResponse {
	title := valueOr(creationSpec.Title, "本地占位创作")
	visual := valueOr(creationSpec.VisualStyle, title)
	sound := valueOr(creationSpec.SoundStyle, "本地占位环境音")
	return storyboardModelResponse{
		Scenario: "local_demo:" + title,
		Modules: []storyboard.StoryboardModule{
			{
				ID: "visuals", Key: "visuals", SemanticType: "scene", Title: "关键画面", Required: true,
				Capabilities: storyboard.ModuleCapabilities{HasQuantity: true, RequiresPrompt: true, RequiresAsset: true, OutputModality: "image"},
				Elements: []storyboard.StoryboardElement{{
					ID: "scene-1", Key: "scene-1", SemanticType: "scene", Title: title + "关键画面", Revision: 1,
					Content:     map[string]any{"description": visual},
					PromptSlots: []storyboard.PromptSlot{{Purpose: "keyframe", Revision: 1, Status: storyboard.PromptStatusMissing}},
					AssetSlots:  []storyboard.AssetSlot{{Key: "keyframe", MediaKind: "image", Required: true, ReviewRequired: true, Status: storyboard.AssetSlotStatusMissing}},
				}},
			},
			{
				ID: "videos", Key: "videos", SemanticType: "shot", Title: "动态镜头", Required: true,
				Capabilities: storyboard.ModuleCapabilities{HasQuantity: true, RequiresPrompt: true, RequiresAsset: true, HasTimeline: true, OutputModality: "video"},
				Elements: []storyboard.StoryboardElement{{
					ID: "shot-1", Key: "shot-1", SemanticType: "shot", Title: title + "动态镜头", Revision: 1,
					Content:     map[string]any{"description": visual, "duration_seconds": creationSpec.DurationSeconds},
					PromptSlots: []storyboard.PromptSlot{{Purpose: "clip", Revision: 1, Status: storyboard.PromptStatusMissing}},
					AssetSlots:  []storyboard.AssetSlot{{Key: "clip", MediaKind: "video", Required: true, ReviewRequired: true, Status: storyboard.AssetSlotStatusMissing}},
				}},
			},
			{
				ID: "audio", Key: "audio", SemanticType: "audio_layer", Title: "声音", Required: true,
				Capabilities: storyboard.ModuleCapabilities{HasQuantity: true, RequiresPrompt: true, RequiresAsset: true, HasTimeline: true, OutputModality: "audio"},
				Elements: []storyboard.StoryboardElement{{
					ID: "audio-1", Key: "audio-1", SemanticType: "audio_layer", Title: title + "声音", Revision: 1,
					Content:     map[string]any{"description": sound, "duration_seconds": creationSpec.DurationSeconds},
					PromptSlots: []storyboard.PromptSlot{{Purpose: "track", Revision: 1, Status: storyboard.PromptStatusMissing}},
					AssetSlots:  []storyboard.AssetSlot{{Key: "track", MediaKind: "audio", Required: true, ReviewRequired: true, Status: storyboard.AssetSlotStatusMissing}},
				}},
			},
		},
	}
}

func (r *Runtime) storyboardPlanCapabilityResult(ctx context.Context, request capability.Request[capability.PlanStoryboardIntent], updated storyboard.StoryboardAggregate, revision storyboard.StoryboardRevision, diff storyboard.RevisionDiff, planVersion, reviewStoryboardVersion int) (capability.CapabilityResult[capability.PlanStoryboardData], error) {
	approvalID := stableApprovalRequestID(request.Command.SessionID, "storyboard_revision", revision.ID, planVersion)
	status := capability.StatusWaitingUser
	switch revision.Status {
	case storyboard.RevisionStatusActive:
		status = capability.StatusCompleted
	case storyboard.RevisionStatusRejected, storyboard.RevisionStatusSuperseded, storyboard.RevisionStatusArchived:
		status = capability.StatusCancelled
	}
	if revision.Status == storyboard.RevisionStatusReviewing {
		var err error
		approvalID, err = r.createApproval(ctx, ApprovalRequest{
			ID: approvalID, SessionID: request.Command.SessionID, UserID: request.Command.UserID,
			ArtifactKind: "storyboard_revision", ArtifactID: revision.ID, ArtifactVersion: planVersion,
			StoryboardID: updated.ID, StoryboardVersion: reviewStoryboardVersion,
			ReviewMode: "durable", ExecutionMode: "durable", VersionMismatchPolicy: "mark_stale",
			Approve: ApprovalCommand{Kind: "PromoteStoryboardRevision", Payload: approvalPayload(request.Command, map[string]any{"storyboard_id": updated.ID, "base_version": reviewStoryboardVersion, "revision_id": revision.ID})},
			Reject:  ApprovalCommand{Kind: "RejectAndArchivePendingRevision", Payload: approvalPayload(request.Command, map[string]any{"storyboard_id": updated.ID, "base_version": reviewStoryboardVersion, "revision_id": revision.ID})},
		})
		if err != nil {
			return capability.CapabilityResult[capability.PlanStoryboardData]{}, err
		}
	}
	if revision.Status == storyboard.RevisionStatusReviewing && r.cfg.PublishStoryboard != nil {
		if err := r.cfg.PublishStoryboard(ctx, updated, "plan_storyboard"); err != nil {
			return capability.CapabilityResult[capability.PlanStoryboardData]{}, err
		}
	}
	return capability.CapabilityResult[capability.PlanStoryboardData]{
		Status: status, StoryboardID: updated.ID, StoryboardVersion: updated.Version,
		Data: capability.PlanStoryboardData{Revision: revision, Diff: diff, ApprovalID: approvalID},
	}, nil
}

func (r *Runtime) storyboardPlanReplay(ctx context.Context, aggregate storyboard.StoryboardAggregate, commandID string) (storyboard.StoryboardRevision, storyboard.RevisionDiff, int, int, error) {
	events, err := r.cfg.Storyboards.ListDomainEvents(ctx, aggregate.ID, -1)
	if err != nil {
		return storyboard.StoryboardRevision{}, storyboard.RevisionDiff{}, 0, 0, err
	}
	revisionID := ""
	planVersion := 0
	reviewStoryboardVersion := 0
	var persistedDiff storyboard.RevisionDiff
	for _, event := range events {
		if event.CommandID != commandID || event.Type != "storyboard.revision_requested" {
			continue
		}
		revisionID = strings.TrimSpace(fmt.Sprint(event.Payload["revision_id"]))
		planVersion = intFromAny(event.Payload["plan_revision"])
		reviewStoryboardVersion = event.AggregateVersion
		if raw, marshalErr := json.Marshal(event.Payload["diff"]); marshalErr == nil {
			_ = json.Unmarshal(raw, &persistedDiff)
		}
		break
	}
	if revisionID == "" {
		return storyboard.StoryboardRevision{}, storyboard.RevisionDiff{}, 0, 0, fmt.Errorf("persisted storyboard plan command %s has no revision event", commandID)
	}
	for _, revision := range aggregate.Revisions {
		if revision.ID == revisionID {
			if planVersion <= 0 {
				planVersion = max(1, aggregate.PlanRevision)
			}
			return revision, persistedDiff, planVersion, reviewStoryboardVersion, nil
		}
	}
	return storyboard.StoryboardRevision{}, storyboard.RevisionDiff{}, 0, 0, fmt.Errorf("persisted storyboard revision %s was not found", revisionID)
}

func (r *Runtime) completeCandidatePrompts(ctx context.Context, creationSpec spec.FinalVideoSpec, analysis artifact.Revision, modules []storyboard.StoryboardModule) error {
	type promptRequest struct {
		TargetID string         `json:"target_id"`
		Purpose  string         `json:"purpose"`
		Title    string         `json:"title"`
		Kind     string         `json:"media_kind"`
		Content  map[string]any `json:"content,omitempty"`
	}
	requests := make([]promptRequest, 0)
	requested := map[string]int{}
	appendRequest := func(element *storyboard.StoryboardElement, purpose, mediaKind string) {
		purpose = strings.TrimSpace(purpose)
		if purpose == "" {
			purpose = "content"
		}
		key := element.ID + "\x00" + purpose
		promptIndex := -1
		for index := range element.PromptSlots {
			if strings.TrimSpace(element.PromptSlots[index].Purpose) == purpose {
				promptIndex = index
				break
			}
		}
		if promptIndex < 0 {
			element.PromptSlots = append(element.PromptSlots, storyboard.PromptSlot{Purpose: purpose, Status: storyboard.PromptStatusMissing})
			promptIndex = len(element.PromptSlots) - 1
		}
		// Planner output is untrusted structure only. Prompt text, refs and lock
		// state are always owned by this dedicated Graph ChatModel node.
		element.PromptSlots[promptIndex].Prompt = ""
		element.PromptSlots[promptIndex].PromptRef = ""
		element.PromptSlots[promptIndex].LockedByUser = false
		element.PromptSlots[promptIndex].Status = storyboard.PromptStatusGenerating
		if index, exists := requested[key]; exists {
			if requests[index].Kind == "" && strings.TrimSpace(mediaKind) != "" {
				requests[index].Kind = strings.TrimSpace(mediaKind)
			}
			return
		}
		requested[key] = len(requests)
		requests = append(requests, promptRequest{TargetID: element.ID, Purpose: purpose, Title: element.Title, Kind: mediaKind, Content: element.Content})
	}
	for moduleIndex := range modules {
		module := &modules[moduleIndex]
		for elementIndex := range module.Elements {
			element := &module.Elements[elementIndex]
			for promptIndex := range element.PromptSlots {
				appendRequest(element, element.PromptSlots[promptIndex].Purpose, "")
			}
			for slotIndex := range element.AssetSlots {
				slot := element.AssetSlots[slotIndex]
				if providerFor(slot.MediaKind) == "" {
					continue
				}
				promptIndex := candidatePromptIndex(*element, slot)
				purpose := slot.Key
				if promptIndex >= 0 {
					purpose = element.PromptSlots[promptIndex].Purpose
				}
				appendRequest(element, purpose, slot.MediaKind)
			}
			if module.Capabilities.RequiresPrompt && len(element.PromptSlots) == 0 {
				appendRequest(element, "content", "")
			}
		}
	}
	if len(requests) == 0 {
		return nil
	}
	modelInput := map[string]any{"creation_spec": creationSpec, "material_analysis": analysis.Content, "modules": modules, "targets": requests}
	var generated map[string]string
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		var response struct {
			Prompts []map[string]string `json:"prompts"`
		}
		system := "你是故事板规划 Graph 内部的提示词生成节点。为每个 target_id/purpose 生成可直接交给对应 media_kind Provider 的完整提示词；不得省略任何输入项。只返回严格 JSON {\"prompts\":[{\"target_id\":\"与输入完全一致\",\"purpose\":\"与输入完全一致\",\"prompt\":\"非空完整提示词\"}]}，禁止解释、Markdown 和额外顶层字段。"
		if attempt > 0 {
			system += "上一轮输出未通过完整性校验，请逐项覆盖 targets，不能新增、遗漏或重复 target_id/purpose。"
		}
		if err := r.generateJSON(ctx, system, modelInput, &response); err != nil {
			lastErr = err
			continue
		}
		candidate := make(map[string]string, len(response.Prompts))
		validationErr := error(nil)
		for index, item := range response.Prompts {
			targetID := strings.TrimSpace(item["target_id"])
			purpose := strings.TrimSpace(item["purpose"])
			prompt := strings.TrimSpace(item["prompt"])
			if targetID == "" || purpose == "" || prompt == "" {
				validationErr = fmt.Errorf("internal prompt node returned an invalid prompt at index %d", index)
				break
			}
			key := targetID + "\x00" + purpose
			if _, ok := requested[key]; !ok {
				validationErr = fmt.Errorf("internal prompt node returned unexpected target %s purpose %s", targetID, purpose)
				break
			}
			if _, duplicate := candidate[key]; duplicate {
				validationErr = fmt.Errorf("internal prompt node returned duplicate target %s purpose %s", targetID, purpose)
				break
			}
			candidate[key] = prompt
		}
		if validationErr == nil && len(candidate) != len(requests) {
			missing := make([]string, 0, len(requests)-len(candidate))
			for _, request := range requests {
				key := request.TargetID + "\x00" + request.Purpose
				if _, ok := candidate[key]; !ok {
					missing = append(missing, request.TargetID+":"+request.Purpose)
				}
			}
			sort.Strings(missing)
			validationErr = fmt.Errorf("internal prompt node omitted requested prompts: %s", strings.Join(missing, ", "))
		}
		if validationErr != nil {
			lastErr = validationErr
			continue
		}
		generated = candidate
		break
	}
	if generated == nil {
		if !r.cfg.LocalDemoPlanning {
			if lastErr == nil {
				lastErr = fmt.Errorf("internal prompt node returned no valid prompts")
			}
			return lastErr
		}
		generated = make(map[string]string, len(requests))
		for _, request := range requests {
			description := strings.TrimSpace(fmt.Sprint(request.Content["description"]))
			generated[request.TargetID+"\x00"+request.Purpose] = fmt.Sprintf("Local demo placeholder for %s; purpose=%s; media_kind=%s; description=%s", request.Title, request.Purpose, request.Kind, description)
		}
	}
	for _, request := range requests {
		prompt := generated[request.TargetID+"\x00"+request.Purpose]
		for moduleIndex := range modules {
			for elementIndex := range modules[moduleIndex].Elements {
				element := &modules[moduleIndex].Elements[elementIndex]
				if element.ID != request.TargetID {
					continue
				}
				index := -1
				for promptIndex := range element.PromptSlots {
					if element.PromptSlots[promptIndex].Purpose == request.Purpose {
						index = promptIndex
						break
					}
				}
				if index < 0 {
					element.PromptSlots = append(element.PromptSlots, storyboard.PromptSlot{Purpose: request.Purpose})
					index = len(element.PromptSlots) - 1
				}
				element.PromptSlots[index].Prompt = prompt
				element.PromptSlots[index] = normalizeCandidatePrompt(element.PromptSlots[index])
			}
		}
	}
	return nil
}

func candidatePromptIndex(element storyboard.StoryboardElement, slot storyboard.AssetSlot) int {
	for index, prompt := range element.PromptSlots {
		purpose := strings.TrimSpace(prompt.Purpose)
		if purpose != "" && (purpose == slot.Key || purpose == slot.Role || purpose == slot.MediaKind || strings.Contains(slot.Key, purpose)) {
			return index
		}
	}
	if len(element.PromptSlots) == 1 {
		return 0
	}
	return -1
}

func normalizeCandidatePrompt(prompt storyboard.PromptSlot) storyboard.PromptSlot {
	if prompt.Revision <= 0 {
		prompt.Revision = 1
	}
	prompt.Status = storyboard.PromptStatusReady
	return prompt
}

func (r *Runtime) generateMedia(ctx context.Context, request capability.Request[capability.GenerateMediaIntent]) (capability.CapabilityResult[capability.GenerateMediaData], error) {
	if replay, found, err := r.replayGenerateMedia(ctx, request.Command.SessionID, request.Command.IdempotencyKey); err != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, err
	} else if found {
		return replay, nil
	}
	if replay, found, err := r.replayGenerateMediaNoOp(ctx, request); err != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, err
	} else if found {
		return replay, nil
	}
	aggregate, err := r.cfg.Storyboards.GetAggregateBySession(ctx, request.Command.SessionID)
	if err != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, err
	}
	if aggregate.PendingRevisionID != "" {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, fmt.Errorf("pending storyboard revision must be approved or rejected before media generation")
	}
	confirmedSpec, err := r.cfg.Specs.GetConfirmedBySession(ctx, request.Command.SessionID)
	if err != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, fmt.Errorf("load confirmed creation spec: %w", err)
	}
	activeRevision, err := aggregate.ActiveRevision()
	if err != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, err
	}
	if activeRevision.DerivedFromSpecVersion > 0 && activeRevision.DerivedFromSpecVersion != confirmedSpec.Version {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, fmt.Errorf("active storyboard is based on creation spec v%d but confirmed spec is v%d; replan storyboard first", activeRevision.DerivedFromSpecVersion, confirmedSpec.Version)
	}
	aggregate, err = r.fillMissingPrompts(ctx, request.Command, aggregate)
	if err != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, err
	}
	if r.cfg.PublishStoryboard != nil {
		if err := r.cfg.PublishStoryboard(ctx, aggregate, "generate_media_preparation"); err != nil {
			return capability.CapabilityResult[capability.GenerateMediaData]{}, err
		}
	}
	inFlight := map[string]struct{}{}
	generationJobs := make([]generation.GenerationJob, 0)
	if r.cfg.GenerationJobs != nil {
		jobs, err := r.cfg.GenerationJobs.ListBySession(ctx, request.Command.SessionID)
		if err != nil {
			return capability.CapabilityResult[capability.GenerateMediaData]{}, err
		}
		generationJobs = jobs
		for _, job := range jobs {
			if !generation.IsTerminalJobStatus(job.Status) {
				inFlight[job.TargetID+"\x00"+job.AssetSlot] = struct{}{}
			}
		}
	}
	targets, err := selectGenerationTargets(aggregate, request.Intent.Phase, inFlight)
	if err != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, err
	}
	if request.Intent.Policy == "single_next" && len(targets) > 1 {
		targets = targets[:1]
	}
	if len(targets) == 0 {
		reason, classifyErr := classifyGenerateMediaNoOp(aggregate, generationJobs)
		if classifyErr != nil {
			return capability.CapabilityResult[capability.GenerateMediaData]{}, classifyErr
		}
		if reason == GenerateMediaNoOpNoTargetsForRequestedPhase && request.Intent.Phase == "auto_next" {
			return capability.CapabilityResult[capability.GenerateMediaData]{}, fmt.Errorf("generate_media auto_next found a ready target that selection did not return")
		}
		return r.freezeGenerateMediaNoOp(ctx, request, aggregate, reason)
	}
	operationID, batchID := r.cfg.NewID(), r.cfg.NewID()
	jobs := make([]generation.GenerationJob, 0, len(targets))
	selected := make([]string, 0, len(targets))
	for _, target := range targets {
		policy := generation.DeliveryPolicy{BindingMode: generation.BindingModeCandidate, ApprovalPolicy: generation.ApprovalReviewRequired, ChargePolicy: generation.ChargePostpaidNoReservation}
		jobID := r.cfg.NewID()
		payload := generationPayload(target, confirmedSpec, request.Command.UserID)
		jobs = append(jobs, generation.GenerationJob{
			ID: jobID, SessionID: request.Command.SessionID, UserID: request.Command.UserID,
			StoryboardID: aggregate.ID, ToolCallID: request.Command.ToolCallID,
			IdempotencyKey: request.Command.IdempotencyKey + ":" + target.Element.ID + ":" + target.Slot.Key,
			Provider:       providerFor(target.Slot.MediaKind), MediaKind: target.Slot.MediaKind,
			TargetType: target.Module.SemanticType, TargetID: target.Element.ID, AssetSlot: target.Slot.Key,
			Required: target.Slot.Required, StoryboardVersionAtDispatch: aggregate.Version,
			BindingToken:   generation.BindingToken{StoryboardID: aggregate.ID, TargetID: target.Element.ID, AssetSlot: target.Slot.Key, TargetRevision: target.Element.Revision, PromptRevision: target.PromptRevision, GenerationEpoch: target.Slot.GenerationEpoch, SpecVersion: confirmedSpec.Version, InputFingerprint: target.Fingerprint},
			DeliveryPolicy: policy, MaxAttempts: 4,
			Payload: payload,
		})
		selected = append(selected, target.Element.ID+":"+target.Slot.Key)
	}
	estimatedPoints, err := r.runGenerationPreflight(ctx, request.Command.UserID, jobs)
	if err != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, err
	}
	workflow, _, err := r.cfg.GenerationCommands.Create(ctx, generation.CreateWorkflowCommand{
		Operation: generation.GenerationOperation{ID: operationID, SessionID: request.Command.SessionID, UserID: request.Command.UserID, WorkflowRunID: valueOr(request.Command.WorkflowID, request.Command.RunID), StageRunID: request.Command.StageRunID, ToolCallID: request.Command.ToolCallID, IdempotencyKey: request.Command.IdempotencyKey, Kind: "generate_media", Status: generation.OperationStatusAccepted, BatchID: batchID, Result: map[string]any{"estimated_points": estimatedPoints}},
		Batch:     generation.GenerationBatch{ID: batchID, SessionID: request.Command.SessionID, UserID: request.Command.UserID, WorkflowRunID: valueOr(request.Command.WorkflowID, request.Command.RunID), StageRunID: request.Command.StageRunID, ToolCallID: request.Command.ToolCallID, OperationID: operationID, Kind: request.Intent.Phase, CompletionPolicy: generation.CompletionAllowPartial, WakePolicy: generation.WakeOnFailure, DeliveryPolicy: generation.DeliveryPolicy{BindingMode: generation.BindingModeCandidate, ApprovalPolicy: generation.ApprovalReviewRequired, ChargePolicy: generation.ChargePostpaidNoReservation}, ExpectedSpecVersion: confirmedSpec.Version, ExpectedStoryboardVersion: aggregate.Version},
		Jobs:      jobs,
	})
	if err != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, err
	}
	result := capability.CapabilityResult[capability.GenerateMediaData]{
		Status: capability.StatusAccepted, OperationID: workflow.Operation.ID, BatchID: workflow.Batch.ID,
		StoryboardID: aggregate.ID, StoryboardVersion: aggregate.Version,
		Data: capability.GenerateMediaData{SelectedTargets: selected, JobCount: len(workflow.Jobs)},
	}
	if estimatedPoints > 0 {
		result.Cost = &capability.CostSummary{Currency: "points", EstimatedMinor: estimatedPoints}
	}
	return result, nil
}

func generationPayload(target generationTarget, creationSpec spec.FinalVideoSpec, userID string) map[string]any {
	payload := map[string]any{
		"prompt": target.Prompt, "target": target.Element.Content,
		"media_kind": target.Slot.MediaKind, "bind_asset_ids": target.InputAssetIDs,
		"user_id": userID,
	}
	if ratio := strings.TrimSpace(creationSpec.AspectRatio); ratio != "" {
		payload["ratio"] = ratio
	}
	if model := modelPreferenceFor(creationSpec, target.Slot.MediaKind); model != "" {
		payload["model"] = model
	}
	for _, source := range []map[string]any{target.Element.Content, target.Element.Metadata} {
		for _, key := range []string{"model", "size", "n", "ratio", "resolution", "duration_seconds", "fps", "filename_prefix"} {
			if value, exists := source[key]; exists && fmt.Sprint(value) != "" {
				payload[key] = value
			}
		}
	}
	return payload
}

func modelPreferenceFor(creationSpec spec.FinalVideoSpec, mediaKind string) string {
	kind := strings.ToLower(strings.TrimSpace(mediaKind))
	fieldKeys := []string{kind + "_model"}
	if kind == "illustration" || kind == "keyframe" {
		fieldKeys = append(fieldKeys, "image_model")
	}
	if kind == "music" || kind == "voice" {
		fieldKeys = append(fieldKeys, "audio_model")
	}
	for _, key := range fieldKeys {
		if value := strings.TrimSpace(fmt.Sprint(creationSpec.Fields[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	preference := strings.TrimSpace(creationSpec.ModelPreference)
	lower := strings.ToLower(preference)
	switch {
	case kind == "video" && strings.Contains(lower, "seedance"):
		return preference
	case (kind == "image" || kind == "illustration" || kind == "keyframe") && strings.Contains(lower, "image"):
		return preference
	}
	return ""
}

func (r *Runtime) assembleOutput(ctx context.Context, request capability.Request[capability.AssembleOutputIntent]) (capability.CapabilityResult[capability.AssembleOutputData], error) {
	mode := strings.TrimSpace(request.Intent.Mode)
	intentFingerprint := assemblyIntentFingerprint(request.Intent)
	if mode == "preview" || mode == "export" {
		if replay, found, err := r.replayAssembly(ctx, request.Command.SessionID, "assemble_"+request.Intent.Mode, request.Command.IdempotencyKey, intentFingerprint); err != nil {
			return capability.CapabilityResult[capability.AssembleOutputData]{}, err
		} else if found {
			return replay, nil
		}
	}
	if mode != "validate" {
		frozen, err := r.cfg.Artifacts.GetByIdempotencyKey(ctx, request.Command.IdempotencyKey)
		if err == nil {
			if frozen.SessionID != request.Command.SessionID || frozen.Kind != artifact.KindAssemblyPlan || frozen.CreatedBy != strings.TrimSpace(request.Command.UserID) || strings.TrimSpace(fmt.Sprint(frozen.Content["intent_fingerprint"])) != intentFingerprint {
				return capability.CapabilityResult[capability.AssembleOutputData]{}, artifact.ErrIdempotencyConflict
			}
			return r.continueAssembly(ctx, request, frozen)
		}
		if !errors.Is(err, artifact.ErrNotFound) {
			return capability.CapabilityResult[capability.AssembleOutputData]{}, err
		}
	}
	aggregate, err := r.cfg.Storyboards.GetAggregateBySession(ctx, request.Command.SessionID)
	if err != nil {
		return capability.CapabilityResult[capability.AssembleOutputData]{}, err
	}
	revision, err := aggregate.ActiveRevision()
	if err != nil {
		return capability.CapabilityResult[capability.AssembleOutputData]{}, err
	}
	confirmedSpec, err := r.cfg.Specs.GetConfirmedBySession(ctx, request.Command.SessionID)
	if err != nil {
		return capability.CapabilityResult[capability.AssembleOutputData]{}, fmt.Errorf("load confirmed creation spec: %w", err)
	}
	if revision.DerivedFromSpecVersion > 0 && revision.DerivedFromSpecVersion != confirmedSpec.Version {
		return capability.CapabilityResult[capability.AssembleOutputData]{}, fmt.Errorf("active storyboard is based on creation spec v%d but confirmed spec is v%d; replan storyboard first", revision.DerivedFromSpecVersion, confirmedSpec.Version)
	}
	missing := requiredMissing(*revision)
	manifest := map[string]any{"storyboard_id": aggregate.ID, "storyboard_version": aggregate.Version, "revision_id": revision.ID, "scenario": revision.Scenario, "modules": revision.Modules, "bindings": activeBindings(aggregate.Bindings), "output_type": strings.TrimSpace(request.Intent.OutputType), "instruction": strings.TrimSpace(request.Intent.Instruction), "mode": mode, "intent_fingerprint": intentFingerprint, "missing_dependencies": missing}
	if mode == "validate" {
		status := capability.StatusCompleted
		if len(missing) > 0 {
			status = capability.StatusPartial
		}
		return capability.CapabilityResult[capability.AssembleOutputData]{Status: status, StoryboardID: aggregate.ID, StoryboardVersion: aggregate.Version, Data: capability.AssembleOutputData{Manifest: manifest, MissingDependencies: missing}}, nil
	}
	created, err := r.cfg.Artifacts.CreateRevision(ctx, artifact.Revision{
		ID: stableArtifactID(request.Command.SessionID, artifact.KindAssemblyPlan, request.Command.IdempotencyKey), SessionID: request.Command.SessionID, Kind: artifact.KindAssemblyPlan,
		Status: artifact.StatusActive, IdempotencyKey: request.Command.IdempotencyKey,
		DerivedFrom: map[string]int{"storyboard": aggregate.Version, "spec": confirmedSpec.Version}, Content: manifest, CreatedBy: request.Command.UserID,
	})
	if err != nil {
		return capability.CapabilityResult[capability.AssembleOutputData]{}, err
	}
	return r.continueAssembly(ctx, request, created.Revision)
}

func (r *Runtime) continueAssembly(ctx context.Context, request capability.Request[capability.AssembleOutputIntent], plan artifact.Revision) (capability.CapabilityResult[capability.AssembleOutputData], error) {
	manifest := plan.Content
	storyboardID := strings.TrimSpace(fmt.Sprint(manifest["storyboard_id"]))
	storyboardVersion := plan.DerivedFrom["storyboard"]
	specVersion := plan.DerivedFrom["spec"]
	mode := strings.TrimSpace(fmt.Sprint(manifest["mode"]))
	missing := stringSliceFromAny(manifest["missing_dependencies"])
	if storyboardID == "" || storyboardVersion <= 0 || strings.TrimSpace(fmt.Sprint(manifest["intent_fingerprint"])) != assemblyIntentFingerprint(request.Intent) {
		return capability.CapabilityResult[capability.AssembleOutputData]{}, fmt.Errorf("%w: invalid frozen assembly plan", artifact.ErrIdempotencyConflict)
	}
	status := capability.StatusCompleted
	if len(missing) > 0 {
		status = capability.StatusPartial
	}
	if mode == "preview" || mode == "export" {
		if len(missing) > 0 {
			return capability.CapabilityResult[capability.AssembleOutputData]{
				Status: capability.StatusPartial, StoryboardID: storyboardID, StoryboardVersion: storyboardVersion,
				Data: capability.AssembleOutputData{AssemblyRevisionID: plan.ID, Manifest: manifest, MissingDependencies: missing},
			}, nil
		}
		operationID, batchID, jobID := r.cfg.NewID(), r.cfg.NewID(), r.cfg.NewID()
		outputSlot := strings.TrimSpace(fmt.Sprint(manifest["output_type"]))
		if outputSlot == "" {
			outputSlot = "default"
		}
		fingerprintRaw, _ := json.Marshal(manifest)
		fingerprint := sha256.Sum256(fingerprintRaw)
		policy := generation.DeliveryPolicy{BindingMode: generation.BindingModeActive, ApprovalPolicy: generation.ApprovalAutoApprove, ChargePolicy: generation.ChargePostpaidNoReservation}
		assemblyJob := generation.GenerationJob{ID: jobID, SessionID: request.Command.SessionID, UserID: request.Command.UserID, StoryboardID: storyboardID, ToolCallID: request.Command.ToolCallID, IdempotencyKey: request.Command.IdempotencyKey + ":assembly", Provider: generation.ProviderAssembly, MediaKind: "assembly", TargetType: "assembly", TargetID: "assembly:" + plan.ID, AssetSlot: outputSlot, Required: true, StoryboardVersionAtDispatch: storyboardVersion, BindingToken: generation.BindingToken{StoryboardID: storyboardID, TargetID: "assembly:" + plan.ID, AssetSlot: outputSlot, TargetRevision: 1, SpecVersion: specVersion, AggregateVersion: storyboardVersion, InputFingerprint: hex.EncodeToString(fingerprint[:])}, DeliveryPolicy: policy, MaxAttempts: 3, Payload: map[string]any{"assembly_revision_id": plan.ID, "output_type": outputSlot, "mode": mode, "manifest": manifest}}
		estimatedPoints, preflightErr := r.runGenerationPreflight(ctx, request.Command.UserID, []generation.GenerationJob{assemblyJob})
		if preflightErr != nil {
			return capability.CapabilityResult[capability.AssembleOutputData]{}, preflightErr
		}
		workflow, _, err := r.cfg.GenerationCommands.Create(ctx, generation.CreateWorkflowCommand{
			Operation: generation.GenerationOperation{ID: operationID, SessionID: request.Command.SessionID, UserID: request.Command.UserID, WorkflowRunID: valueOr(request.Command.WorkflowID, request.Command.RunID), StageRunID: request.Command.StageRunID, ToolCallID: request.Command.ToolCallID, IdempotencyKey: request.Command.IdempotencyKey, Kind: "assemble_" + mode, BatchID: batchID, Result: map[string]any{"assembly_revision_id": plan.ID, "estimated_points": estimatedPoints}},
			Batch:     generation.GenerationBatch{ID: batchID, SessionID: request.Command.SessionID, UserID: request.Command.UserID, WorkflowRunID: valueOr(request.Command.WorkflowID, request.Command.RunID), StageRunID: request.Command.StageRunID, OperationID: operationID, ToolCallID: request.Command.ToolCallID, Kind: "assemble_" + mode, CompletionPolicy: generation.CompletionAllRequired, WakePolicy: generation.WakeOnFailure, DeliveryPolicy: policy, ExpectedSpecVersion: specVersion, ExpectedStoryboardVersion: storyboardVersion},
			Jobs:      []generation.GenerationJob{assemblyJob},
		})
		if err != nil {
			return capability.CapabilityResult[capability.AssembleOutputData]{}, err
		}
		result := capability.CapabilityResult[capability.AssembleOutputData]{
			Status: capability.StatusAccepted, OperationID: workflow.Operation.ID, BatchID: workflow.Batch.ID,
			StoryboardID: storyboardID, StoryboardVersion: storyboardVersion,
			Data: capability.AssembleOutputData{AssemblyRevisionID: plan.ID, Manifest: manifest},
		}
		if estimatedPoints > 0 {
			result.Cost = &capability.CostSummary{Currency: "points", EstimatedMinor: estimatedPoints}
		}
		return result, nil
	}
	return capability.CapabilityResult[capability.AssembleOutputData]{
		Status: status, StoryboardID: storyboardID, StoryboardVersion: storyboardVersion,
		Data: capability.AssembleOutputData{AssemblyRevisionID: plan.ID, Manifest: manifest, MissingDependencies: missing},
	}, nil
}

func activeBindings(bindings []storyboard.ArtifactBinding) []storyboard.ArtifactBinding {
	out := make([]storyboard.ArtifactBinding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.State == storyboard.BindingStateActive {
			out = append(out, binding)
		}
	}
	return out
}

func (r *Runtime) loadExistingWorkflow(ctx context.Context, sessionID, expectedKind, idempotencyKey string) (generation.WorkflowAggregate, bool, error) {
	if r.cfg.GenerationWorkflow == nil {
		return generation.WorkflowAggregate{}, false, nil
	}
	operation, err := r.cfg.GenerationWorkflow.GetOperationByIdempotencyKey(ctx, strings.TrimSpace(idempotencyKey))
	if errors.Is(err, generation.ErrNotFound) {
		return generation.WorkflowAggregate{}, false, nil
	}
	if err != nil {
		return generation.WorkflowAggregate{}, false, err
	}
	if operation.SessionID != strings.TrimSpace(sessionID) || operation.Kind != strings.TrimSpace(expectedKind) {
		return generation.WorkflowAggregate{}, false, fmt.Errorf("idempotency key is already bound to a different generation request")
	}
	batch, err := r.cfg.GenerationWorkflow.GetBatch(ctx, operation.BatchID)
	if err != nil {
		return generation.WorkflowAggregate{}, false, err
	}
	jobs, err := r.cfg.GenerationWorkflow.ListJobsByBatch(ctx, batch.ID)
	if err != nil {
		return generation.WorkflowAggregate{}, false, err
	}
	return generation.WorkflowAggregate{Operation: operation, Batch: batch, Jobs: jobs}, true, nil
}

func (r *Runtime) replayGenerateMedia(ctx context.Context, sessionID, idempotencyKey string) (capability.CapabilityResult[capability.GenerateMediaData], bool, error) {
	workflow, found, err := r.loadExistingWorkflow(ctx, sessionID, "generate_media", idempotencyKey)
	if err != nil || !found {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, found, err
	}
	selected := make([]string, 0, len(workflow.Jobs))
	storyboardID, storyboardVersion := "", 0
	for _, job := range workflow.Jobs {
		selected = append(selected, job.TargetID+":"+job.AssetSlot)
		if storyboardID == "" {
			storyboardID, storyboardVersion = job.StoryboardID, job.StoryboardVersionAtDispatch
		}
	}
	result := capability.CapabilityResult[capability.GenerateMediaData]{
		Status: capabilityStatusForOperation(workflow.Operation.Status), OperationID: workflow.Operation.ID, BatchID: workflow.Batch.ID,
		StoryboardID: storyboardID, StoryboardVersion: storyboardVersion,
		Data: capability.GenerateMediaData{SelectedTargets: selected, JobCount: len(workflow.Jobs)},
	}
	result.Cost = replayCostSummary(workflow.Operation)
	result.ArtifactRefs = terminalArtifactRefs(workflow.Jobs)
	return result, true, nil
}

func (r *Runtime) replayGenerateMediaNoOp(ctx context.Context, request capability.Request[capability.GenerateMediaIntent]) (capability.CapabilityResult[capability.GenerateMediaData], bool, error) {
	receipt, err := r.cfg.Artifacts.GetByIdempotencyKey(ctx, strings.TrimSpace(request.Command.IdempotencyKey))
	if errors.Is(err, artifact.ErrNotFound) {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, false, nil
	}
	if err != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, false, err
	}
	result, err := generateMediaNoOpResultFromReceipt(receipt, request)
	return result, true, err
}

func (r *Runtime) freezeGenerateMediaNoOp(ctx context.Context, request capability.Request[capability.GenerateMediaIntent], aggregate storyboard.StoryboardAggregate, reason string) (capability.CapabilityResult[capability.GenerateMediaData], error) {
	receipt, err := r.cfg.Artifacts.CreateRevision(ctx, artifact.Revision{
		ID:             stableArtifactID(request.Command.SessionID, generateMediaNoOpArtifactKind, request.Command.IdempotencyKey),
		SessionID:      strings.TrimSpace(request.Command.SessionID),
		Kind:           generateMediaNoOpArtifactKind,
		Status:         artifact.StatusActive,
		IdempotencyKey: strings.TrimSpace(request.Command.IdempotencyKey),
		DerivedFrom:    map[string]int{"storyboard": aggregate.Version},
		Content: map[string]any{
			"intent_fingerprint": generateMediaIntentFingerprint(request.Intent),
			"status":             capability.StatusCompleted,
			"storyboard_id":      aggregate.ID,
			"storyboard_version": aggregate.Version,
			"no_op":              true,
			"reason":             reason,
		},
		CreatedBy: strings.TrimSpace(request.Command.UserID),
	})
	if err != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, err
	}
	return generateMediaNoOpResultFromReceipt(receipt.Revision, request)
}

func generateMediaNoOpResultFromReceipt(receipt artifact.Revision, request capability.Request[capability.GenerateMediaIntent]) (capability.CapabilityResult[capability.GenerateMediaData], error) {
	if receipt.SessionID != strings.TrimSpace(request.Command.SessionID) || receipt.Kind != generateMediaNoOpArtifactKind || receipt.Status != artifact.StatusActive || receipt.CreatedBy != strings.TrimSpace(request.Command.UserID) {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, artifact.ErrIdempotencyConflict
	}
	var frozen struct {
		IntentFingerprint string `json:"intent_fingerprint"`
		Status            string `json:"status"`
		StoryboardID      string `json:"storyboard_id"`
		StoryboardVersion int    `json:"storyboard_version"`
		NoOp              bool   `json:"no_op"`
		Reason            string `json:"reason"`
	}
	raw, err := json.Marshal(receipt.Content)
	if err != nil || json.Unmarshal(raw, &frozen) != nil {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, fmt.Errorf("%w: invalid generate_media no-op receipt", artifact.ErrIdempotencyConflict)
	}
	if frozen.IntentFingerprint != generateMediaIntentFingerprint(request.Intent) || frozen.Status != capability.StatusCompleted || strings.TrimSpace(frozen.StoryboardID) == "" || frozen.StoryboardVersion <= 0 || !frozen.NoOp || !validGenerateMediaNoOpReason(frozen.Reason) {
		return capability.CapabilityResult[capability.GenerateMediaData]{}, fmt.Errorf("%w: generate_media no-op receipt changed", artifact.ErrIdempotencyConflict)
	}
	return capability.CapabilityResult[capability.GenerateMediaData]{
		Status: frozen.Status, StoryboardID: frozen.StoryboardID, StoryboardVersion: frozen.StoryboardVersion,
		Data: capability.GenerateMediaData{NoOp: true, Reason: frozen.Reason},
	}, nil
}

func generateMediaIntentFingerprint(intent capability.GenerateMediaIntent) string {
	raw, _ := json.Marshal(struct {
		Phase  string `json:"phase"`
		Policy string `json:"policy"`
	}{Phase: strings.TrimSpace(intent.Phase), Policy: strings.TrimSpace(intent.Policy)})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func validGenerateMediaNoOpReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case GenerateMediaNoOpWaitingCandidateApproval,
		GenerateMediaNoOpGenerationJobsInFlight,
		GenerateMediaNoOpDependencyBlocked,
		GenerateMediaNoOpProductionComplete,
		GenerateMediaNoOpNoTargetsForRequestedPhase:
		return true
	default:
		return false
	}
}

func (r *Runtime) replayAssembly(ctx context.Context, sessionID, expectedKind, idempotencyKey, intentFingerprint string) (capability.CapabilityResult[capability.AssembleOutputData], bool, error) {
	workflow, found, err := r.loadExistingWorkflow(ctx, sessionID, expectedKind, idempotencyKey)
	if err != nil || !found {
		return capability.CapabilityResult[capability.AssembleOutputData]{}, found, err
	}
	data := capability.AssembleOutputData{}
	storyboardID, storyboardVersion := "", 0
	if len(workflow.Jobs) > 0 {
		job := workflow.Jobs[0]
		storyboardID, storyboardVersion = job.StoryboardID, job.StoryboardVersionAtDispatch
		data.AssemblyRevisionID = strings.TrimSpace(fmt.Sprint(job.Payload["assembly_revision_id"]))
		if manifest, ok := job.Payload["manifest"].(map[string]any); ok {
			data.Manifest = manifest
		} else if raw, marshalErr := json.Marshal(job.Payload["manifest"]); marshalErr == nil {
			_ = json.Unmarshal(raw, &data.Manifest)
		}
		if strings.TrimSpace(fmt.Sprint(data.Manifest["intent_fingerprint"])) != strings.TrimSpace(intentFingerprint) {
			return capability.CapabilityResult[capability.AssembleOutputData]{}, true, artifact.ErrIdempotencyConflict
		}
	}
	result := capability.CapabilityResult[capability.AssembleOutputData]{
		Status: capabilityStatusForOperation(workflow.Operation.Status), OperationID: workflow.Operation.ID, BatchID: workflow.Batch.ID,
		StoryboardID: storyboardID, StoryboardVersion: storyboardVersion, Data: data,
	}
	result.Cost = replayCostSummary(workflow.Operation)
	result.ArtifactRefs = terminalArtifactRefs(workflow.Jobs)
	return result, true, nil
}

func assemblyIntentFingerprint(intent capability.AssembleOutputIntent) string {
	raw, _ := json.Marshal(struct {
		Mode        string `json:"mode"`
		OutputType  string `json:"output_type"`
		Instruction string `json:"instruction"`
	}{strings.TrimSpace(intent.Mode), strings.TrimSpace(intent.OutputType), strings.TrimSpace(intent.Instruction)})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func capabilityStatusForOperation(status string) string {
	switch strings.TrimSpace(status) {
	case generation.OperationStatusCompleted:
		return capability.StatusCompleted
	case generation.OperationStatusPartialFailed:
		return capability.StatusPartial
	case generation.OperationStatusFailed:
		return capability.StatusFailed
	case generation.OperationStatusCancelled:
		return capability.StatusCancelled
	default:
		return capability.StatusAccepted
	}
}

func replayCostSummary(operation generation.GenerationOperation) *capability.CostSummary {
	estimate := int64FromAny(operation.Result["estimated_points"])
	var settled generation.CostSummary
	if raw, ok := operation.Result["cost"]; ok {
		if encoded, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(encoded, &settled)
		}
	}
	if estimate == 0 && settled.GrossChargedPoints == 0 && settled.RefundedPoints == 0 && settled.NetChargedPoints == 0 {
		return nil
	}
	return &capability.CostSummary{
		Currency: "points", EstimatedMinor: estimate,
		GrossMinor: settled.GrossChargedPoints, RefundMinor: settled.RefundedPoints, NetMinor: settled.NetChargedPoints,
	}
}

func terminalArtifactRefs(jobs []generation.GenerationJob) []capability.ArtifactRef {
	refs := make([]capability.ArtifactRef, 0)
	seen := map[string]struct{}{}
	for _, job := range jobs {
		if job.Status != generation.StatusSucceeded {
			continue
		}
		for _, assetID := range job.ResultAssetIDs {
			assetID = strings.TrimSpace(assetID)
			if assetID == "" {
				continue
			}
			if _, exists := seen[assetID]; exists {
				continue
			}
			seen[assetID] = struct{}{}
			refs = append(refs, capability.ArtifactRef{AssetID: assetID, Kind: job.MediaKind})
		}
	}
	return refs
}

func (r *Runtime) runGenerationPreflight(ctx context.Context, userID string, jobs []generation.GenerationJob) (int64, error) {
	for _, job := range jobs {
		if err := generation.ValidateProviderJob(job); err != nil {
			return 0, fmt.Errorf("invalid %s generation parameters: %w", job.Provider, err)
		}
	}
	if r.cfg.GenerationPreflight == nil {
		return 0, nil
	}
	return r.cfg.GenerationPreflight(ctx, strings.TrimSpace(userID), jobs)
}

func (r *Runtime) createApproval(ctx context.Context, request ApprovalRequest) (string, error) {
	if r.cfg.CreateApproval == nil {
		return "", fmt.Errorf("approval creator is required for reviewed artifacts")
	}
	return r.cfg.CreateApproval(ctx, request)
}

func stableApprovalRequestID(sessionID, artifactKind, artifactID string, artifactVersion int) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{sessionID, artifactKind, artifactID, fmt.Sprint(artifactVersion)}, "\x00")))
	return "approval_" + hex.EncodeToString(sum[:16])
}

func stableArtifactID(sessionID, kind, idempotencyKey string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{sessionID, kind, idempotencyKey}, "\x00")))
	return "artifact_" + hex.EncodeToString(sum[:16])
}

func (r *Runtime) generateJSON(ctx context.Context, system string, input any, output any) error {
	if r.modelGraph == nil {
		return fmt.Errorf("internal capability ChatModel is required")
	}
	raw, err := r.modelGraph.Invoke(ctx, modelGraphRequest{System: system, Input: input})
	if err != nil {
		return fmt.Errorf("capability ChatModel graph: %w", err)
	}
	if err := json.Unmarshal(raw, output); err != nil {
		return fmt.Errorf("decode capability ChatModel JSON: %w", err)
	}
	return nil
}

type generationTarget struct {
	Module         storyboard.StoryboardModule
	Element        storyboard.StoryboardElement
	Slot           storyboard.AssetSlot
	Prompt         string
	PromptRevision int
	InputAssetIDs  []string
	Fingerprint    string
}

func (r *Runtime) fillMissingPrompts(ctx context.Context, command capability.CommandContext, aggregate storyboard.StoryboardAggregate) (storyboard.StoryboardAggregate, error) {
	revision, err := aggregate.ActiveRevision()
	if err != nil {
		return aggregate, err
	}
	type promptKey struct {
		TargetID string
		Purpose  string
	}
	type promptRequest struct {
		TargetID       string         `json:"target_id"`
		Purpose        string         `json:"purpose"`
		Title          string         `json:"title"`
		Content        map[string]any `json:"content,omitempty"`
		TargetRevision int            `json:"target_revision"`
		PromptRevision int            `json:"prompt_revision"`
	}
	requests := make([]promptRequest, 0)
	expected := make(map[promptKey]struct{})
	for _, module := range revision.Modules {
		for _, element := range module.Elements {
			for _, slot := range element.PromptSlots {
				if !slot.LockedByUser && (slot.Status == storyboard.PromptStatusMissing || slot.Status == storyboard.PromptStatusStale || strings.TrimSpace(slot.Prompt) == "") {
					request := promptRequest{
						TargetID: strings.TrimSpace(element.ID), Purpose: strings.TrimSpace(slot.Purpose),
						Title: element.Title, Content: element.Content,
						TargetRevision: element.Revision, PromptRevision: slot.Revision,
					}
					if request.TargetID == "" || request.Purpose == "" {
						return aggregate, fmt.Errorf("storyboard contains a prompt request with an empty target_id or purpose")
					}
					key := promptKey{TargetID: request.TargetID, Purpose: request.Purpose}
					if _, duplicate := expected[key]; duplicate {
						return aggregate, fmt.Errorf("storyboard contains duplicate prompt request target_id=%q purpose=%q", request.TargetID, request.Purpose)
					}
					expected[key] = struct{}{}
					requests = append(requests, request)
				}
			}
		}
	}
	if len(requests) == 0 {
		return aggregate, nil
	}
	// A custom decoder-friendly shape avoids exposing prompt generation as a Tool.
	var raw struct {
		Prompts []map[string]string `json:"prompts"`
	}
	if err := r.generateJSON(ctx, "你是故事板内部提示词生成节点。为每个 target_id/purpose 生成可直接用于对应媒体模型的提示词。返回 {\"prompts\":[{\"target_id\":...,\"purpose\":...,\"prompt\":...}]}。", map[string]any{"storyboard": revision, "targets": requests}, &raw); err != nil {
		return aggregate, err
	}
	generatedPrompts := make(map[promptKey]string, len(raw.Prompts))
	for index, generated := range raw.Prompts {
		targetID := strings.TrimSpace(generated["target_id"])
		purpose := strings.TrimSpace(generated["purpose"])
		prompt := strings.TrimSpace(generated["prompt"])
		if targetID == "" || purpose == "" || prompt == "" {
			return aggregate, fmt.Errorf("prompt generator returned an invalid prompt at index %d", index)
		}
		key := promptKey{TargetID: targetID, Purpose: purpose}
		if _, ok := expected[key]; !ok {
			return aggregate, fmt.Errorf("prompt generator returned unexpected target_id=%q purpose=%q", targetID, purpose)
		}
		if _, duplicate := generatedPrompts[key]; duplicate {
			return aggregate, fmt.Errorf("prompt generator returned duplicate target_id=%q purpose=%q", targetID, purpose)
		}
		generatedPrompts[key] = prompt
	}
	if len(generatedPrompts) != len(expected) {
		missing := make([]string, 0, len(expected)-len(generatedPrompts))
		for key := range expected {
			if _, ok := generatedPrompts[key]; !ok {
				missing = append(missing, key.TargetID+":"+key.Purpose)
			}
		}
		sort.Strings(missing)
		return aggregate, fmt.Errorf("prompt generator omitted requested prompts: %s", strings.Join(missing, ", "))
	}

	current, err := r.cfg.Storyboards.GetAggregate(ctx, aggregate.ID)
	if err != nil {
		return aggregate, err
	}
	locked := make(map[promptKey]struct{})
	for _, request := range requests {
		key := promptKey{TargetID: request.TargetID, Purpose: request.Purpose}
		slot, targetRevision, ok := findPromptSlot(current, request.TargetID, request.Purpose)
		if !ok {
			return aggregate, fmt.Errorf("requested prompt target_id=%q purpose=%q no longer exists", request.TargetID, request.Purpose)
		}
		if slot.LockedByUser {
			locked[key] = struct{}{}
			continue
		}
		if targetRevision != request.TargetRevision || slot.Revision != request.PromptRevision {
			return aggregate, fmt.Errorf("requested prompt target_id=%q purpose=%q changed while prompts were generated", request.TargetID, request.Purpose)
		}
	}

	aggregate = current
	for _, request := range requests {
		key := promptKey{TargetID: request.TargetID, Purpose: request.Purpose}
		if _, isLocked := locked[key]; isLocked {
			continue
		}
		updated, _, err := r.cfg.StoryboardCommands.UpdatePrompt(ctx, storyboard.UpdatePromptCommand{
			CommandID:    command.RequestID + ":prompt:" + request.TargetID + ":" + request.Purpose,
			StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, TargetID: request.TargetID,
			ExpectedTargetRevision: request.TargetRevision, Purpose: request.Purpose,
			ExpectedRevision: request.PromptRevision, Prompt: generatedPrompts[key],
		})
		if err != nil {
			return aggregate, err
		}
		aggregate = updated
	}
	return aggregate, nil
}

func selectGenerationTargets(aggregate storyboard.StoryboardAggregate, phase string, inFlight map[string]struct{}) ([]generationTarget, error) {
	revision, err := aggregate.ActiveRevision()
	if err != nil {
		return nil, err
	}
	result := make([]generationTarget, 0)
	for _, module := range revision.Modules {
		for _, element := range module.Elements {
			for _, slot := range element.AssetSlots {
				if !phaseMatches(phase, slot) || (slot.Status != storyboard.AssetSlotStatusMissing && slot.Status != storyboard.AssetSlotStatusStale) || len(slot.CandidateIDs) > 0 {
					continue
				}
				if providerFor(slot.MediaKind) == "" {
					continue
				}
				if _, busy := inFlight[element.ID+"\x00"+slot.Key]; busy {
					continue
				}
				input, resolveErr := aggregate.ResolveGenerationInput(element.ID, slot.Key)
				if errors.Is(resolveErr, storyboard.ErrDependencyNotReady) {
					continue
				}
				if resolveErr != nil {
					return nil, resolveErr
				}
				if strings.TrimSpace(input.Prompt) == "" {
					continue
				}
				result = append(result, generationTarget{Module: module, Element: element, Slot: slot, Prompt: input.Prompt, PromptRevision: input.PromptRevision, InputAssetIDs: input.InputAssetIDs, Fingerprint: input.Fingerprint})
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if phase == "auto_next" && mediaStageRank(result[i].Slot) != mediaStageRank(result[j].Slot) {
			return mediaStageRank(result[i].Slot) < mediaStageRank(result[j].Slot)
		}
		if result[i].Module.Order != result[j].Module.Order {
			return result[i].Module.Order < result[j].Module.Order
		}
		return result[i].Element.ID < result[j].Element.ID
	})
	if phase == "auto_next" && len(result) > 0 {
		rank := mediaStageRank(result[0].Slot)
		end := 1
		for end < len(result) && mediaStageRank(result[end].Slot) == rank {
			end++
		}
		result = result[:end]
	}
	return result, nil
}

// classifyGenerateMediaNoOp explains why target selection returned an empty
// set. The reason is a stable protocol code persisted in the no-op receipt;
// callers must not infer production completion from an undifferentiated empty
// selection.
func classifyGenerateMediaNoOp(aggregate storyboard.StoryboardAggregate, jobs []generation.GenerationJob) (string, error) {
	revision, err := aggregate.ActiveRevision()
	if err != nil {
		return "", err
	}
	hasCandidate := false
	hasInFlight := false
	hasBlockedDependency := false
	hasReadyProductionTarget := false
	productionComplete := true
	requiredReady := true

	for _, job := range jobs {
		if generation.IsTerminalJobStatus(job.Status) || job.Provider == generation.ProviderAssembly {
			continue
		}
		if storyboardID := strings.TrimSpace(job.StoryboardID); storyboardID != "" && storyboardID != aggregate.ID {
			continue
		}
		hasInFlight = true
	}
	for _, module := range revision.Modules {
		for _, element := range module.Elements {
			for _, slot := range element.AssetSlots {
				active := slot.Status == storyboard.AssetSlotStatusActive && strings.TrimSpace(slot.ActiveBindingID) != ""
				if slot.Required && !active {
					requiredReady = false
				}
				if slot.Status == storyboard.AssetSlotStatusCandidate || len(slot.CandidateIDs) > 0 {
					hasCandidate = true
					productionComplete = false
				}
				if providerFor(slot.MediaKind) == "" {
					if slot.Required && !active {
						hasBlockedDependency = true
					}
					continue
				}
				if active && len(slot.CandidateIDs) == 0 {
					continue
				}
				productionComplete = false
				switch slot.Status {
				case storyboard.AssetSlotStatusMissing, storyboard.AssetSlotStatusStale:
					input, resolveErr := aggregate.ResolveGenerationInput(element.ID, slot.Key)
					if errors.Is(resolveErr, storyboard.ErrDependencyNotReady) {
						hasBlockedDependency = true
						continue
					}
					if resolveErr != nil {
						return "", resolveErr
					}
					if strings.TrimSpace(input.Prompt) == "" {
						hasBlockedDependency = true
						continue
					}
					hasReadyProductionTarget = true
				case storyboard.AssetSlotStatusCandidate:
					hasCandidate = true
				default:
					hasBlockedDependency = true
				}
			}
		}
	}

	switch {
	case hasCandidate:
		return GenerateMediaNoOpWaitingCandidateApproval, nil
	case hasInFlight:
		return GenerateMediaNoOpGenerationJobsInFlight, nil
	case productionComplete && requiredReady:
		return GenerateMediaNoOpProductionComplete, nil
	case hasBlockedDependency:
		return GenerateMediaNoOpDependencyBlocked, nil
	case hasReadyProductionTarget:
		return GenerateMediaNoOpNoTargetsForRequestedPhase, nil
	default:
		// Unknown or internally inconsistent slot states must never be promoted
		// to production_complete.
		return GenerateMediaNoOpDependencyBlocked, nil
	}
}

func findPromptSlot(aggregate storyboard.StoryboardAggregate, targetID, purpose string) (storyboard.PromptSlot, int, bool) {
	revision, err := aggregate.ActiveRevision()
	if err != nil {
		return storyboard.PromptSlot{}, 0, false
	}
	for _, module := range revision.Modules {
		for _, element := range module.Elements {
			if element.ID == targetID {
				for _, prompt := range element.PromptSlots {
					if prompt.Purpose == purpose {
						return prompt, element.Revision, true
					}
				}
			}
		}
	}
	return storyboard.PromptSlot{}, 0, false
}

func phaseMatches(phase string, slot storyboard.AssetSlot) bool {
	kind := strings.ToLower(slot.MediaKind)
	key := strings.ToLower(slot.Key)
	switch phase {
	case "auto_next":
		return true
	case "element_images":
		return kind == "image" || kind == "illustration"
	case "keyframes":
		return kind == "keyframe" || (kind == "image" && strings.Contains(key, "keyframe"))
	case "videos":
		return kind == "video"
	case "audio":
		return kind == "audio" || kind == "music" || kind == "voice"
	default:
		return false
	}
}

func mediaStageRank(slot storyboard.AssetSlot) int {
	kind, key := strings.ToLower(slot.MediaKind), strings.ToLower(slot.Key)
	switch {
	case kind == "image" || kind == "illustration" || kind == "keyframe" || strings.Contains(key, "keyframe"):
		return 10
	case kind == "audio" || kind == "music" || kind == "voice":
		return 20
	case kind == "video":
		return 30
	default:
		return 40
	}
}

func providerFor(kind string) string {
	switch strings.ToLower(kind) {
	case "video":
		return generation.ProviderSeedance
	case "audio", "music", "voice":
		return generation.ProviderAudio
	case "image", "illustration", "keyframe":
		return generation.ProviderImage2
	default:
		return ""
	}
}

func normalizeGeneratedModules(modules []storyboard.StoryboardModule) {
	usedIDs := map[string]struct{}{}
	moduleKeys := map[string]int{}
	for i := range modules {
		module := &modules[i]
		module.Key = uniquePlanningKey(module.Key, fmt.Sprintf("module_%d", i+1), moduleKeys)
		module.ID = uniquePlanningID(module.ID, "module_"+module.Key, usedIDs)
		if module.SemanticType == "" {
			module.SemanticType = module.Key
		}
		if module.Title == "" {
			module.Title = module.SemanticType
		}
		module.Order = i + 1
		module.PlannedCount = len(module.Elements)
		if module.Capabilities == (storyboard.ModuleCapabilities{}) {
			module.Capabilities = storyboard.ModuleCapabilities{HasQuantity: true, RequiresPrompt: true, RequiresAsset: true}
		}
		elementKeys := map[string]int{}
		for j := range module.Elements {
			element := &module.Elements[j]
			element.Key = uniquePlanningKey(element.Key, fmt.Sprintf("element_%d", j+1), elementKeys)
			element.ID = uniquePlanningID(element.ID, "element_"+module.Key+"_"+element.Key, usedIDs)
			if element.SemanticType == "" {
				element.SemanticType = module.SemanticType
			}
			if element.Title == "" {
				element.Title = element.Key
			}
			if element.Revision <= 0 {
				element.Revision = 1
			}
			slotKeys := map[string]int{}
			for slotIndex := range element.AssetSlots {
				slot := &element.AssetSlots[slotIndex]
				fallback := strings.TrimSpace(slot.MediaKind)
				if fallback == "" {
					fallback = fmt.Sprintf("asset_%d", slotIndex+1)
				}
				slot.Key = uniquePlanningKey(slot.Key, fallback, slotKeys)
			}
			promptPurposes := map[string]int{}
			for promptIndex := range element.PromptSlots {
				prompt := &element.PromptSlots[promptIndex]
				fallback := "content"
				if len(element.AssetSlots) == 1 {
					fallback = element.AssetSlots[0].Key
				}
				prompt.Purpose = uniquePlanningKey(prompt.Purpose, fallback, promptPurposes)
			}
		}
	}
}

func uniquePlanningID(preferred, fallback string, used map[string]struct{}) string {
	base := strings.TrimSpace(preferred)
	if base == "" {
		base = strings.TrimSpace(fallback)
	}
	if base == "" {
		base = "target"
	}
	value := base
	for suffix := 2; ; suffix++ {
		if _, exists := used[value]; !exists {
			used[value] = struct{}{}
			return value
		}
		value = fmt.Sprintf("%s_%d", base, suffix)
	}
}

func uniquePlanningKey(preferred, fallback string, counts map[string]int) string {
	base := strings.TrimSpace(preferred)
	if base == "" {
		base = strings.TrimSpace(fallback)
	}
	if base == "" {
		base = "item"
	}
	if counts[base] == 0 {
		counts[base] = 1
		return base
	}
	for suffix := counts[base] + 1; ; suffix++ {
		value := fmt.Sprintf("%s_%d", base, suffix)
		if counts[value] == 0 {
			counts[base] = suffix
			counts[value] = 1
			return value
		}
	}
}

func activeRevisionOrNil(aggregate storyboard.StoryboardAggregate) any {
	revision, err := aggregate.ActiveRevision()
	if err != nil {
		return nil
	}
	return revision
}

func requiredMissing(revision storyboard.StoryboardRevision) []string {
	missing := make([]string, 0)
	for _, module := range revision.Modules {
		for _, element := range module.Elements {
			for _, slot := range element.AssetSlots {
				if slot.Required && (slot.ActiveBindingID == "" || slot.Status != storyboard.AssetSlotStatusActive) {
					missing = append(missing, element.ID+":"+slot.Key)
				}
			}
		}
	}
	return missing
}

func compact(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func validateCreationSpecCandidate(candidate creationSpecModel) error {
	if strings.TrimSpace(candidate.Title) == "" || strings.TrimSpace(candidate.VideoType) == "" {
		return fmt.Errorf("creation spec planner must return title and video_type")
	}
	if candidate.DurationSeconds <= 0 {
		return fmt.Errorf("creation spec planner must return a positive duration_seconds")
	}
	kind := strings.ToLower(candidate.VideoType)
	audioOnly := false
	for _, marker := range []string{"music", "audio", "song", "podcast", "音乐", "歌曲", "音频", "播客", "配乐", "纯音乐", "有声"} {
		if strings.Contains(kind, marker) {
			audioOnly = true
			break
		}
	}
	if !audioOnly && strings.TrimSpace(candidate.AspectRatio) == "" {
		return fmt.Errorf("visual creation spec planner must return aspect_ratio")
	}
	return nil
}

func valueOr(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func structMap(value any) map[string]any {
	raw, _ := json.Marshal(value)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func intFromAny(value any) int {
	return int(int64FromAny(value))
}

func stringSliceFromAny(value any) []string {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return compact(values)
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		var parsed json.Number = json.Number(strings.TrimSpace(typed))
		result, _ := parsed.Int64()
		return result
	default:
		return 0
	}
}

func newID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
