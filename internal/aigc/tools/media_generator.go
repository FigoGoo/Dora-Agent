package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/mediagraph"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

const MediaGeneratorToolKey = "media_generator"

// MediaGeneratorStoryboardReader lets the tool enumerate the storyboard's real
// key elements when the agent invokes media_generator without explicit target_ids.
// Optional (nil-safe): when absent the tool falls back to the raw invocation input.
type MediaGeneratorStoryboardReader interface {
	GetLatestBySession(ctx context.Context, sessionID string) (storyboard.Storyboard, error)
}

type MediaGeneratorToolConfig struct {
	Checkpoints compose.CheckPointStore
	Dispatcher  mediagraph.JobDispatcher
	Storyboards MediaGeneratorStoryboardReader
	NewID       func() string
}

type MediaGeneratorTool struct {
	cfg MediaGeneratorToolConfig
}

type MediaGeneratorPayload struct {
	StoryboardID string   `json:"storyboard_id"`
	TargetType   string   `json:"target_type"`
	TargetIDs    []string `json:"target_ids,omitempty"`
	MediaKinds   []string `json:"media_kinds,omitempty"`
	BindAssetIDs []string `json:"bind_asset_ids,omitempty"`
	ProviderHint string   `json:"provider_hint,omitempty"`
	AsyncMode    string   `json:"async_mode,omitempty"`
	CheckpointID string   `json:"checkpoint_id,omitempty"`
}

type MediaGeneratorToolResult struct {
	Interrupted bool                            `json:"interrupted"`
	Interrupt   *mediagraph.InterruptEvent      `json:"interrupt,omitempty"`
	Output      mediagraph.MediaGeneratorOutput `json:"output,omitempty"`
}

func NewMediaGeneratorTool(cfg MediaGeneratorToolConfig) MediaGeneratorTool {
	return MediaGeneratorTool{cfg: cfg}
}

func (MediaGeneratorTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: MediaGeneratorToolKey,
		Desc: "Run the media generation graph for storyboard assets. It plans/registers assets, writes prompts, dispatches generation work, syncs storyboard state, and returns an interrupt request when reference image confirmation is required.",
		ParamsOneOf: schema.NewParamsOneOfByParams(toolInvocationEnvelopeParams(map[string]*schema.ParameterInfo{
			"storyboard_id": {
				Type:     schema.String,
				Desc:     "Storyboard ID to generate media for.",
				Required: true,
			},
			"target_type": {
				Type:     schema.String,
				Desc:     "Target type: key_element, shot, audio_layer, or all.",
				Required: true,
			},
			"target_ids": {
				Type: schema.Array,
				Desc: "Optional target IDs.",
			},
			"media_kinds": {
				Type:     schema.Array,
				Desc:     "Media kinds to generate: image, keyframe, video, audio.",
				Required: true,
			},
			"checkpoint_id": {
				Type: schema.String,
				Desc: "Optional graph checkpoint ID. Defaults to idempotency key.",
			},
		})),
	}, nil
}

func (t MediaGeneratorTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	invocation, err := decodeMediaGeneratorInvocation(argumentsInJSON)
	if err != nil {
		return "", err
	}
	generator, err := mediagraph.NewGenerator(ctx, mediagraph.Config{
		Checkpoints: t.cfg.Checkpoints,
		Dispatcher:  t.cfg.Dispatcher,
		NewID:       t.cfg.NewID,
	})
	if err != nil {
		return "", err
	}
	sessionID := firstNonEmpty(invocation.SessionID, sessionIDFromContext(ctx))
	input := mediagraph.MediaGeneratorInput{
		SessionID:         sessionID,
		StoryboardID:      invocation.Payload.StoryboardID,
		SpecVersion:       invocation.ExpectedSpecVersion,
		StoryboardVersion: invocation.ExpectedStoryboardVersion,
		TargetType:        invocation.Payload.TargetType,
		TargetIDs:         invocation.Payload.TargetIDs,
		MediaKinds:        invocation.Payload.MediaKinds,
		BindAssetIDs:      invocation.Payload.BindAssetIDs,
		ProviderHint:      invocation.Payload.ProviderHint,
		AsyncMode:         invocation.Payload.AsyncMode,
	}
	input = t.resolveReferenceTargets(ctx, input)
	checkpointID := mediaGraphCheckpointID(invocation, sessionID, input.StoryboardID)

	result, err := generator.Run(ctx, input, checkpointID)
	if err != nil {
		return "", err
	}

	status := ToolStatusOK
	nextConfirmationID := ""
	if result.Interrupted {
		status = ToolStatusQueued
		nextConfirmationID = result.Interrupt.Payload.InterruptID
	}
	out, err := json.Marshal(ToolResultEnvelope[MediaGeneratorToolResult]{
		Status:             status,
		RequestID:          invocation.RequestID,
		IdempotencyKey:     invocation.IdempotencyKey,
		SpecVersion:        invocation.ExpectedSpecVersion,
		StoryboardVersion:  invocation.ExpectedStoryboardVersion,
		NextConfirmationID: nextConfirmationID,
		Data: MediaGeneratorToolResult{
			Interrupted: result.Interrupted,
			Interrupt:   result.Interrupt,
			Output:      result.Output,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal media generator result: %w", err)
	}
	return string(out), nil
}

// mediaGraphCheckpointID derives a checkpoint id that is UNIQUE per session (and
// storyboard). Critical: the media graph interrupts and checkpoints after dispatch;
// if two sessions share a checkpoint id, the second resumes the first's already-
// interrupted checkpoint and never dispatches its own jobs. The agent frequently
// omits checkpoint_id/idempotency_key (both empty → the old "media_graph:" collided
// across every session), so we fall back to session+storyboard scoping.
func mediaGraphCheckpointID(invocation ToolInvocationEnvelope[MediaGeneratorPayload], sessionID, storyboardID string) string {
	if cp := strings.TrimSpace(invocation.Payload.CheckpointID); cp != "" {
		return cp
	}
	scope := strings.TrimSpace(invocation.IdempotencyKey)
	if scope == "" {
		scope = strings.TrimSpace(sessionID)
		if sb := strings.TrimSpace(storyboardID); sb != "" {
			scope = scope + ":" + sb
		}
	}
	return "media_graph:" + scope
}

// resolveReferenceTargets fills in real, still-unbound key element targets when the
// agent invoked media_generator without explicit target_ids (e.g. target_type="all").
// This is the Flova "阶段 4：关键元素参考图" step: generate one image per key element
// so the reference-confirm interrupt corresponds to real, bindable assets — not a
// single bogus "storyboard"/"all" target that can never bind to any element.
// No-op when a storyboard reader is absent or the agent already gave explicit targets.
func (t MediaGeneratorTool) resolveReferenceTargets(ctx context.Context, in mediagraph.MediaGeneratorInput) mediagraph.MediaGeneratorInput {
	if t.cfg.Storyboards == nil || len(in.TargetIDs) > 0 {
		return in
	}
	if strings.TrimSpace(in.SessionID) == "" {
		return in
	}
	board, err := t.cfg.Storyboards.GetLatestBySession(ctx, in.SessionID)
	if err != nil || len(board.KeyElements) == 0 {
		return in
	}
	keys := make([]string, 0, len(board.KeyElements))
	for _, element := range board.KeyElements {
		key := strings.TrimSpace(element.Key)
		if key == "" || len(element.AssetIDs) > 0 { // skip already-bound elements
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return in
	}
	in.TargetIDs = keys
	in.TargetType = generation.TargetKeyElement
	in.MediaKinds = []string{"image"} // stage 4 dispatches reference images only
	if strings.TrimSpace(in.StoryboardID) == "" {
		in.StoryboardID = board.ID
	}
	return in
}

func decodeMediaGeneratorInvocation(argumentsInJSON string) (ToolInvocationEnvelope[MediaGeneratorPayload], error) {
	return decodeToolInvocationEnvelope(MediaGeneratorToolKey, argumentsInJSON, func(payload MediaGeneratorPayload) bool {
		return strings.TrimSpace(payload.StoryboardID) != ""
	})
}

var _ einotool.InvokableTool = MediaGeneratorTool{}
