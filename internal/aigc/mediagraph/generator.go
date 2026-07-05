package mediagraph

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
)

const (
	StatusReferenceConfirmed = "reference_confirmed"
	StatusReferenceRejected  = "reference_rejected"
)

const (
	stepRegisterAssets       = "agent_analysis_register_assets"
	stepAssetConfigured      = "asset_configuration_completed"
	stepAnalysisAfterAssets  = "agent_analysis_completed_after_assets"
	stepPromptWritten        = "prompt_writing_completed"
	stepAnalysisAfterPrompts = "agent_analysis_completed_after_prompts"
	stepMediaGenerated       = "media_generation_completed"
)

func init() {
	schema.RegisterName[MediaGeneratorInput]("dora.aigc.mediagraph.input.v1")
	schema.RegisterName[MediaGeneratorState]("dora.aigc.mediagraph.state.v1")
	schema.RegisterName[MediaGeneratorOutput]("dora.aigc.mediagraph.output.v1")
	schema.RegisterName[ReferenceConfirmDecision]("dora.aigc.mediagraph.reference_confirm_decision.v1")
	schema.RegisterName[InterruptRequestPayload]("dora.aigc.mediagraph.interrupt_payload.v1")
}

type Config struct {
	Checkpoints compose.CheckPointStore
	Dispatcher  JobDispatcher
	NewID       func() string
}

type JobDispatcher interface {
	Dispatch(ctx context.Context, job generation.GenerationJob) (generation.GenerationJob, bool, error)
}

type Generator struct {
	runnable compose.Runnable[MediaGeneratorInput, MediaGeneratorOutput]
}

type MediaGeneratorInput struct {
	SessionID         string   `json:"session_id"`
	RunID             string   `json:"run_id,omitempty"`
	StoryboardID      string   `json:"storyboard_id"`
	SpecVersion       int      `json:"spec_version,omitempty"`
	StoryboardVersion int      `json:"storyboard_version"`
	TargetType        string   `json:"target_type"`
	TargetIDs         []string `json:"target_ids,omitempty"`
	MediaKinds        []string `json:"media_kinds,omitempty"`
	BindAssetIDs      []string `json:"bind_asset_ids,omitempty"`
	ProviderHint      string   `json:"provider_hint,omitempty"`
	AsyncMode         string   `json:"async_mode,omitempty"`
}

type MediaGeneratorState struct {
	MediaGeneratorInput
	Progress []ProgressStep `json:"progress,omitempty"`
	JobIDs   []string       `json:"job_ids,omitempty"`
}

type ProgressStep struct {
	Key       string    `json:"key"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

type MediaGeneratorOutput struct {
	StoryboardID      string         `json:"storyboard_id"`
	StoryboardVersion int            `json:"storyboard_version"`
	Status            string         `json:"status"`
	Progress          []ProgressStep `json:"progress,omitempty"`
	JobIDs            []string       `json:"job_ids,omitempty"`
}

type ReferenceConfirmDecision struct {
	Approved bool   `json:"approved"`
	Note     string `json:"note,omitempty"`
}

type RunResult struct {
	Output      MediaGeneratorOutput
	Interrupted bool
	Interrupt   *InterruptEvent
}

type InterruptEvent struct {
	Event   string                  `json:"event"`
	Payload InterruptRequestPayload `json:"payload"`
}

type InterruptRequestPayload struct {
	CheckpointID      string         `json:"checkpoint_id"`
	InterruptID       string         `json:"interrupt_id"`
	SpecVersion       int            `json:"spec_version,omitempty"`
	StoryboardVersion int            `json:"storyboard_version,omitempty"`
	Title             string         `json:"title,omitempty"`
	Message           string         `json:"message,omitempty"`
	Actions           []ActionSchema `json:"actions,omitempty"`
}

type ActionSchema struct {
	Key         string         `json:"key"`
	Label       string         `json:"label"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
}

func NewGenerator(ctx context.Context, cfg Config) (*Generator, error) {
	store := cfg.Checkpoints
	if store == nil {
		store = NewMemoryCheckpointStore()
	}
	newID := cfg.NewID
	if newID == nil {
		newID = defaultJobID
	}

	graph := compose.NewGraph[MediaGeneratorInput, MediaGeneratorOutput]()
	if err := graph.AddLambdaNode("register_asset_requirements", compose.InvokableLambda(registerAssetRequirements)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode("asset_configuration_completed", compose.InvokableLambda(markAssetConfigured)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode("analysis_after_assets", compose.InvokableLambda(markAnalysisAfterAssets)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode("write_generation_prompts", compose.InvokableLambda(writeGenerationPrompts)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode("analysis_after_prompts", compose.InvokableLambda(markAnalysisAfterPrompts)); err != nil {
		return nil, err
	}
	dispatchMediaAssets := func(ctx context.Context, state MediaGeneratorState) (MediaGeneratorState, error) {
		return generateMediaAssets(ctx, state, cfg.Dispatcher, newID)
	}
	if err := graph.AddLambdaNode("generate_media_assets", compose.InvokableLambda(dispatchMediaAssets)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode("request_reference_confirm", compose.InvokableLambda(requestReferenceConfirm)); err != nil {
		return nil, err
	}

	for _, edge := range [][2]string{
		{compose.START, "register_asset_requirements"},
		{"register_asset_requirements", "asset_configuration_completed"},
		{"asset_configuration_completed", "analysis_after_assets"},
		{"analysis_after_assets", "write_generation_prompts"},
		{"write_generation_prompts", "analysis_after_prompts"},
		{"analysis_after_prompts", "generate_media_assets"},
		{"generate_media_assets", "request_reference_confirm"},
		{"request_reference_confirm", compose.END},
	} {
		if err := graph.AddEdge(edge[0], edge[1]); err != nil {
			return nil, err
		}
	}

	runnable, err := graph.Compile(ctx,
		compose.WithGraphName("media_generator"),
		compose.WithNodeTriggerMode(compose.AllPredecessor),
		compose.WithCheckPointStore(store),
	)
	if err != nil {
		return nil, err
	}
	return &Generator{runnable: runnable}, nil
}

func (g *Generator) Run(ctx context.Context, input MediaGeneratorInput, checkpointID string) (RunResult, error) {
	return g.invoke(ctx, input, checkpointID)
}

func (g *Generator) Resume(ctx context.Context, checkpointID string, interruptID string, decision ReferenceConfirmDecision) (RunResult, error) {
	resumeCtx := compose.ResumeWithData(ctx, interruptID, decision)
	return g.invoke(resumeCtx, MediaGeneratorInput{}, checkpointID)
}

func (g *Generator) invoke(ctx context.Context, input MediaGeneratorInput, checkpointID string) (RunResult, error) {
	if g == nil || g.runnable == nil {
		return RunResult{}, fmt.Errorf("media generator graph is required")
	}
	output, err := g.runnable.Invoke(ctx, input, compose.WithCheckPointID(checkpointID))
	if err == nil {
		return RunResult{Output: output}, nil
	}
	info, ok := compose.ExtractInterruptInfo(err)
	if !ok || len(info.InterruptContexts) == 0 {
		return RunResult{}, err
	}
	payload, err := interruptPayload(info.InterruptContexts[0].Info)
	if err != nil {
		return RunResult{}, err
	}
	payload.CheckpointID = checkpointID
	payload.InterruptID = info.InterruptContexts[0].ID
	return RunResult{
		Interrupted: true,
		Interrupt: &InterruptEvent{
			Event:   "a2ui.interrupt_request",
			Payload: payload,
		},
	}, nil
}

func registerAssetRequirements(_ context.Context, input MediaGeneratorInput) (MediaGeneratorState, error) {
	return MediaGeneratorState{
		MediaGeneratorInput: input,
		Progress:            []ProgressStep{newStep(stepRegisterAssets, "Agent analysis: register all element assets")},
	}, nil
}

func markAssetConfigured(_ context.Context, state MediaGeneratorState) (MediaGeneratorState, error) {
	return appendProgress(state, stepAssetConfigured, "Asset configuration completed"), nil
}

func markAnalysisAfterAssets(_ context.Context, state MediaGeneratorState) (MediaGeneratorState, error) {
	return appendProgress(state, stepAnalysisAfterAssets, "Agent analysis completed"), nil
}

func writeGenerationPrompts(_ context.Context, state MediaGeneratorState) (MediaGeneratorState, error) {
	return appendProgress(state, stepPromptWritten, "Prompt writing completed"), nil
}

func markAnalysisAfterPrompts(_ context.Context, state MediaGeneratorState) (MediaGeneratorState, error) {
	return appendProgress(state, stepAnalysisAfterPrompts, "Agent analysis completed"), nil
}

func generateMediaAssets(ctx context.Context, state MediaGeneratorState, dispatcher JobDispatcher, newID func() string) (MediaGeneratorState, error) {
	state = appendProgress(state, stepMediaGenerated, "Media generation completed")
	if dispatcher == nil {
		state.JobIDs = append(state.JobIDs, fmt.Sprintf("job:%s:%d", state.StoryboardID, len(state.Progress)))
		return state, nil
	}
	for _, targetID := range generationTargetIDs(state) {
		for _, mediaKind := range generationMediaKinds(state) {
			jobID := strings.TrimSpace(newID())
			if jobID == "" {
				jobID = defaultJobID()
			}
			job := generation.GenerationJob{
				ID:             jobID,
				SessionID:      state.SessionID,
				StoryboardID:   state.StoryboardID,
				IdempotencyKey: generationIdempotencyKey(state, targetID, mediaKind),
				Provider:       generationProvider(state.ProviderHint, mediaKind),
				TargetType:     generationTargetType(state.TargetType),
				TargetID:       targetID,
				Status:         generation.StatusQueued,
				MaxRetries:     2,
				Payload: map[string]any{
					"session_id":         state.SessionID,
					"storyboard_id":      state.StoryboardID,
					"storyboard_version": state.StoryboardVersion,
					"target_type":        generationTargetType(state.TargetType),
					"target_id":          targetID,
					"media_kind":         mediaKind,
					"stage_key":          generationStageKey(state, mediaKind),
					"prompt":             generationPrompt(state, targetID, mediaKind),
					"filename_prefix":    generationFilenamePrefix(state, targetID, mediaKind),
					"bind_asset_ids":     append([]string(nil), state.BindAssetIDs...),
				},
			}
			dispatched, _, err := dispatcher.Dispatch(ctx, job)
			if err != nil {
				return state, err
			}
			state.JobIDs = append(state.JobIDs, dispatched.ID)
		}
	}
	return state, nil
}

func generationTargetType(targetType string) string {
	targetType = strings.TrimSpace(targetType)
	if targetType == "" {
		return "storyboard"
	}
	return targetType
}

func generationTargetIDs(state MediaGeneratorState) []string {
	if len(state.TargetIDs) > 0 {
		out := make([]string, 0, len(state.TargetIDs))
		for _, targetID := range state.TargetIDs {
			targetID = strings.TrimSpace(targetID)
			if targetID != "" {
				out = append(out, targetID)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if strings.TrimSpace(state.StoryboardID) != "" {
		return []string{strings.TrimSpace(state.StoryboardID)}
	}
	return []string{"storyboard"}
}

func generationMediaKinds(state MediaGeneratorState) []string {
	if len(state.MediaKinds) == 0 {
		return []string{"image"}
	}
	out := make([]string, 0, len(state.MediaKinds))
	seen := map[string]bool{}
	for _, mediaKind := range state.MediaKinds {
		mediaKind = strings.ToLower(strings.TrimSpace(mediaKind))
		if mediaKind == "" || seen[mediaKind] {
			continue
		}
		seen[mediaKind] = true
		out = append(out, mediaKind)
	}
	if len(out) == 0 {
		return []string{"image"}
	}
	return out
}

func generationProvider(providerHint string, mediaKind string) string {
	providerHint = strings.TrimSpace(providerHint)
	if providerHint != "" {
		return providerHint
	}
	switch strings.ToLower(strings.TrimSpace(mediaKind)) {
	case "video":
		return generation.ProviderSeedance
	case "audio", "music":
		return generation.ProviderAudio
	default:
		return generation.ProviderImage2
	}
}

func generationPrompt(state MediaGeneratorState, targetID string, mediaKind string) string {
	return fmt.Sprintf(
		"Generate %s asset for %s %s in storyboard %s. Keep consistency with the confirmed final video spec and storyboard style.",
		strings.ToLower(strings.TrimSpace(mediaKind)),
		generationTargetType(state.TargetType),
		strings.TrimSpace(targetID),
		strings.TrimSpace(state.StoryboardID),
	)
}

func generationStageKey(state MediaGeneratorState, mediaKind string) string {
	return strings.Join([]string{
		"generate",
		strings.TrimSpace(generationTargetType(state.TargetType)),
		strings.ToLower(strings.TrimSpace(mediaKind)),
	}, "_")
}

func generationFilenamePrefix(state MediaGeneratorState, targetID string, mediaKind string) string {
	parts := []string{"aigc", strings.ToLower(strings.TrimSpace(mediaKind)), generationTargetType(state.TargetType), strings.TrimSpace(targetID)}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, " /")
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, "-")
}

func generationIdempotencyKey(state MediaGeneratorState, targetID string, mediaKind string) string {
	return strings.Join([]string{
		"media_graph",
		strings.TrimSpace(state.SessionID),
		strings.TrimSpace(state.StoryboardID),
		fmt.Sprintf("v%d", state.StoryboardVersion),
		generationTargetType(state.TargetType),
		strings.TrimSpace(targetID),
		strings.ToLower(strings.TrimSpace(mediaKind)),
	}, ":")
}

func requestReferenceConfirm(ctx context.Context, state MediaGeneratorState) (MediaGeneratorOutput, error) {
	wasInterrupted, hasState, saved := compose.GetInterruptState[*MediaGeneratorState](ctx)
	if wasInterrupted && hasState && saved != nil {
		state = *saved
	}

	if !wasInterrupted {
		return MediaGeneratorOutput{}, compose.StatefulInterrupt(ctx, referenceConfirmPayload(state), &state)
	}

	isResume, hasData, decision := compose.GetResumeContext[ReferenceConfirmDecision](ctx)
	if !isResume {
		return MediaGeneratorOutput{}, compose.StatefulInterrupt(ctx, referenceConfirmPayload(state), &state)
	}

	status := StatusReferenceConfirmed
	if hasData && !decision.Approved {
		status = StatusReferenceRejected
	}
	return MediaGeneratorOutput{
		StoryboardID:      state.StoryboardID,
		StoryboardVersion: state.StoryboardVersion,
		Status:            status,
		Progress:          append([]ProgressStep(nil), state.Progress...),
		JobIDs:            append([]string(nil), state.JobIDs...),
	}, nil
}

func referenceConfirmPayload(state MediaGeneratorState) InterruptRequestPayload {
	return InterruptRequestPayload{
		SpecVersion:       state.SpecVersion,
		StoryboardVersion: state.StoryboardVersion,
		Title:             "确认参考图",
		Message:           "素材已生成并同步到故事板，请确认参考图是否可用于后续镜头生成。",
		Actions: []ActionSchema{
			{
				Key:         "confirm_reference_image",
				Label:       "确认参考图",
				Description: "继续使用当前参考图生成后续素材。",
			},
			{
				Key:         "revise_reference_image",
				Label:       "调整参考图",
				Description: "暂停后续生成，并让 Agent 根据反馈修改参考图。",
			},
		},
	}
}

func interruptPayload(value any) (InterruptRequestPayload, error) {
	switch payload := value.(type) {
	case InterruptRequestPayload:
		return payload, nil
	case *InterruptRequestPayload:
		return *payload, nil
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return InterruptRequestPayload{}, err
		}
		var out InterruptRequestPayload
		if err := json.Unmarshal(raw, &out); err != nil {
			return InterruptRequestPayload{}, err
		}
		return out, nil
	}
}

func appendProgress(state MediaGeneratorState, key string, title string) MediaGeneratorState {
	state.Progress = append(state.Progress, newStep(key, title))
	return state
}

func newStep(key string, title string) ProgressStep {
	return ProgressStep{
		Key:       key,
		Title:     title,
		CreatedAt: time.Now(),
	}
}

func defaultJobID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("job-%d", time.Now().UnixNano())
}

type MemoryCheckpointStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func NewMemoryCheckpointStore() *MemoryCheckpointStore {
	return &MemoryCheckpointStore{data: map[string][]byte{}}
}

func (s *MemoryCheckpointStore) Get(_ context.Context, checkPointID string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.data[checkPointID]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), value...), true, nil
}

func (s *MemoryCheckpointStore) Set(_ context.Context, checkPointID string, checkPoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[checkPointID] = append([]byte(nil), checkPoint...)
	return nil
}
