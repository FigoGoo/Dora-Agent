package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/mediagraph"
)

const MediaGeneratorToolKey = "media_generator"

type MediaGeneratorToolConfig struct {
	Checkpoints compose.CheckPointStore
	Dispatcher  mediagraph.JobDispatcher
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
	checkpointID := invocation.Payload.CheckpointID
	if checkpointID == "" {
		checkpointID = "media_graph:" + invocation.IdempotencyKey
	}

	generator, err := mediagraph.NewGenerator(ctx, mediagraph.Config{
		Checkpoints: t.cfg.Checkpoints,
		Dispatcher:  t.cfg.Dispatcher,
		NewID:       t.cfg.NewID,
	})
	if err != nil {
		return "", err
	}
	result, err := generator.Run(ctx, mediagraph.MediaGeneratorInput{
		SessionID:         invocation.SessionID,
		StoryboardID:      invocation.Payload.StoryboardID,
		SpecVersion:       invocation.ExpectedSpecVersion,
		StoryboardVersion: invocation.ExpectedStoryboardVersion,
		TargetType:        invocation.Payload.TargetType,
		TargetIDs:         invocation.Payload.TargetIDs,
		MediaKinds:        invocation.Payload.MediaKinds,
		BindAssetIDs:      invocation.Payload.BindAssetIDs,
		ProviderHint:      invocation.Payload.ProviderHint,
		AsyncMode:         invocation.Payload.AsyncMode,
	}, checkpointID)
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

func decodeMediaGeneratorInvocation(argumentsInJSON string) (ToolInvocationEnvelope[MediaGeneratorPayload], error) {
	return decodeToolInvocationEnvelope(MediaGeneratorToolKey, argumentsInJSON, func(payload MediaGeneratorPayload) bool {
		return strings.TrimSpace(payload.StoryboardID) != ""
	})
}

var _ einotool.InvokableTool = MediaGeneratorTool{}
