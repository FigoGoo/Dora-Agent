package capability

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const (
	analyzeMaterialsDescription = "Analyze the user's briefs and media, extract reusable subjects, brand constraints, visual references, and storyboard-ready findings. Persist a versioned analysis artifact and return only a compact summary and reusable asset references."
	planCreationSpecDescription = "Create or revise the complete creation specification from the user's background, goal and confirmed material analysis. Produce a versioned candidate for review and report whether the active storyboard requires whole replanning."
	planStoryboardDescription   = "Create a complete dynamic storyboard revision from the confirmed creation specification. Use only for initial planning or whole-storyboard replanning. Infer required modules, element counts, contents, prompts and dependencies. Never use for local target, prompt or asset edits."
	generateMediaDescription    = "Generate media. Two targets: (1) storyboard (default) — advance normal production for the active storyboard, deterministically selecting the next eligible stage; requires phase+policy. (2) session_deliverable — lightweight direct generation without a storyboard for quick single-intent requests (one image / short video / music / narration); requires media_kind+prompt. Do not use for targeted UI regeneration."
	assembleOutputDescription   = "Validate the active storyboard and confirmed assets, build a versioned assembly plan, report missing dependencies, and optionally dispatch a preview or final render for the requested output type."
)

type intentValidator interface {
	Validate() error
}

type capabilityTool[I intentValidator, D any] struct {
	name        string
	description string
	params      map[string]*schema.ParameterInfo
	executor    Executor[I, D]
}

func (t *capabilityTool[I, D]) Info(context.Context) (*schema.ToolInfo, error) {
	if t == nil || t.executor == nil {
		return nil, fmt.Errorf("%s capability executor is required", t.name)
	}
	return &schema.ToolInfo{
		Name:        t.name,
		Desc:        t.description,
		ParamsOneOf: schema.NewParamsOneOfByParams(t.params),
	}, nil
}

func (t *capabilityTool[I, D]) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	if t == nil || t.executor == nil {
		return "", fmt.Errorf("capability executor is required")
	}
	command, err := RequireCommandContext(ctx)
	if err != nil {
		return "", fmt.Errorf("%s: %w", t.name, err)
	}
	intent, err := decodeIntentStrict[I](argumentsInJSON)
	if err != nil {
		return "", fmt.Errorf("decode %s intent: %w", t.name, err)
	}
	if err := intent.Validate(); err != nil {
		return "", err
	}
	// The Agent middleware freezes model output and injects the frozen ToolCallID
	// into the trusted context. It is the logical call slot; the intent digest
	// binds that slot to immutable arguments. Direct/non-Agent callers fall back
	// to a slot derived from the canonical intent.
	canonicalIntent, err := json.Marshal(intent)
	if err != nil {
		return "", fmt.Errorf("canonicalize %s intent: %w", t.name, err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(append([]byte(t.name+"\x00"), canonicalIntent...)))
	if strings.TrimSpace(command.ToolCallID) == "" {
		command.ToolCallID = t.name + ":" + digest[:16]
	}
	callSlot := fmt.Sprintf("%x", sha256.Sum256([]byte(t.name+"\x00"+strings.TrimSpace(command.ToolCallID))))
	if strings.TrimSpace(command.StageRunID) == "" {
		command.StageRunID = "stage:" + command.ToolCallID
	}
	command.IdempotencyKey = command.IdempotencyKey + ":" + t.name + ":call:" + callSlot[:16] + ":intent:" + digest[:24]
	result, err := t.executor.Execute(ctx, Request[I]{Command: command, Intent: intent})
	if err != nil {
		return "", fmt.Errorf("execute %s capability: %w", t.name, err)
	}
	if err := validateResultStatus(result.Status); err != nil {
		return "", fmt.Errorf("%s returned invalid result: %w", t.name, err)
	}
	if result.StageRunID == "" {
		result.StageRunID = command.StageRunID
	}
	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal %s result: %w", t.name, err)
	}
	return string(out), nil
}

func decodeIntentStrict[I any](raw string) (I, error) {
	var intent I
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return intent, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return intent, fmt.Errorf("multiple JSON values are not allowed")
		}
		return intent, err
	}
	return intent, nil
}

func validateResultStatus(status string) error {
	switch status {
	case StatusCompleted, StatusAccepted, StatusWaitingUser, StatusPartial, StatusFailed, StatusCancelled:
		return nil
	default:
		return fmt.Errorf("unsupported status %q", status)
	}
}

type ToolSet struct {
	AnalyzeMaterials einotool.InvokableTool
	PlanCreationSpec einotool.InvokableTool
	PlanStoryboard   einotool.InvokableTool
	GenerateMedia    einotool.InvokableTool
	AssembleOutput   einotool.InvokableTool
}

func NewToolSet(executors Executors) (ToolSet, error) {
	if executors.AnalyzeMaterials == nil || executors.PlanCreationSpec == nil || executors.PlanStoryboard == nil || executors.GenerateMedia == nil || executors.AssembleOutput == nil {
		return ToolSet{}, fmt.Errorf("all five capability executors are required")
	}
	return ToolSet{
		AnalyzeMaterials: &capabilityTool[AnalyzeMaterialsIntent, AnalyzeMaterialsData]{
			name: AnalyzeMaterialsToolKey, description: analyzeMaterialsDescription, params: analyzeMaterialsParams(), executor: executors.AnalyzeMaterials,
		},
		PlanCreationSpec: &capabilityTool[PlanCreationSpecIntent, PlanCreationSpecData]{
			name: PlanCreationSpecToolKey, description: planCreationSpecDescription, params: planCreationSpecParams(), executor: executors.PlanCreationSpec,
		},
		PlanStoryboard: &capabilityTool[PlanStoryboardIntent, PlanStoryboardData]{
			name: PlanStoryboardToolKey, description: planStoryboardDescription, params: planStoryboardParams(), executor: executors.PlanStoryboard,
		},
		GenerateMedia: &capabilityTool[GenerateMediaIntent, GenerateMediaData]{
			name: GenerateMediaToolKey, description: generateMediaDescription, params: generateMediaParams(), executor: executors.GenerateMedia,
		},
		AssembleOutput: &capabilityTool[AssembleOutputIntent, AssembleOutputData]{
			name: AssembleOutputToolKey, description: assembleOutputDescription, params: assembleOutputParams(), executor: executors.AssembleOutput,
		},
	}, nil
}

func NewToolSetFromHandlers(ctx context.Context, handlers Handlers) (ToolSet, error) {
	executors, err := CompileExecutors(ctx, handlers)
	if err != nil {
		return ToolSet{}, err
	}
	return NewToolSet(executors)
}

func (s ToolSet) Ordered() []einotool.BaseTool {
	return []einotool.BaseTool{s.AnalyzeMaterials, s.PlanCreationSpec, s.PlanStoryboard, s.GenerateMedia, s.AssembleOutput}
}

func analyzeMaterialsParams() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"asset_ids":   {Type: schema.Array, ElemInfo: &schema.ParameterInfo{Type: schema.String}, Desc: "Uploaded or existing normalized asset IDs to analyze."},
		"goal":        {Type: schema.String, Desc: "Material analysis goal."},
		"instruction": {Type: schema.String, Desc: "Optional analysis instruction."},
	}
}

func planCreationSpecParams() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"mode":        {Type: schema.String, Desc: "Create or revise the complete creation specification.", Enum: []string{"create", "revise"}, Required: true},
		"background":  {Type: schema.String, Desc: "Creation background supplied by the user."},
		"goal":        {Type: schema.String, Desc: "Creation goal supplied by the user."},
		"instruction": {Type: schema.String, Desc: "Optional revision instruction."},
	}
}

func planStoryboardParams() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"mode":                     {Type: schema.String, Desc: "Create the first complete plan or replan the whole storyboard.", Enum: []string{"create", "replan"}, Required: true},
		"instruction":              {Type: schema.String, Desc: "Optional whole-storyboard planning instruction."},
		"preserve_approved_assets": {Type: schema.Boolean, Desc: "Preserve compatible approved assets during reconciliation."},
	}
}

func generateMediaParams() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"phase":        {Type: schema.String, Desc: "Storyboard target only: production phase selected at a high level; the graph selects concrete targets.", Enum: []string{"auto_next", "element_images", "keyframes", "videos", "audio"}},
		"policy":       {Type: schema.String, Desc: "Storyboard target only: dispatch one next stage or all currently eligible targets.", Enum: []string{"single_next", "all_eligible"}},
		"target":       {Type: schema.String, Desc: "Generation target. Omit for storyboard production; use session_deliverable for lightweight direct generation without a storyboard.", Enum: []string{"storyboard", "session_deliverable"}},
		"media_kind":   {Type: schema.String, Desc: "session_deliverable only: modality direction.", Enum: []string{"image", "video", "music", "audio"}},
		"prompt":       {Type: schema.String, Desc: "session_deliverable only: complete generation prompt."},
		"count":        {Type: schema.Integer, Desc: "session_deliverable only: number of variants (1-4, default 1)."},
		"aspect_ratio": {Type: schema.String, Desc: "session_deliverable only: optional aspect ratio, e.g. 16:9."},
	}
}

func assembleOutputParams() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"mode":        {Type: schema.String, Desc: "Validate, plan, preview, or export the output.", Enum: []string{"validate", "plan", "preview", "export"}, Required: true},
		"output_type": {Type: schema.String, Desc: "Requested output type or rendition."},
		"instruction": {Type: schema.String, Desc: "Optional assembly instruction."},
	}
}

var _ einotool.InvokableTool = (*capabilityTool[AnalyzeMaterialsIntent, AnalyzeMaterialsData])(nil)
