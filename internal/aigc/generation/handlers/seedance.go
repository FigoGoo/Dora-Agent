package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

type SeedanceJobHandlerConfig struct {
	APIKey          string
	Endpoint        string
	HTTPClient      *http.Client
	Assets          SeedanceJobAssetStore
	AssetUploader   tools.SeedanceAssetUploader
	NewID           func() string
	Now             func() time.Time
	PollInterval    time.Duration
	MaxPollAttempts int
}

type SeedanceJobHandler struct {
	tool   tools.SeedanceGenerateTool
	assets SeedanceJobAssetStore
}

type SeedanceJobAssetStore interface {
	tools.SeedanceAssetStore
	Get(ctx context.Context, assetID string) (asset.Asset, error)
}

func NewSeedanceJobHandler(cfg SeedanceJobHandlerConfig) SeedanceJobHandler {
	return SeedanceJobHandler{
		tool:   tools.NewSeedanceGenerateTool(toSeedanceToolConfig(cfg)),
		assets: cfg.Assets,
	}
}

func (h SeedanceJobHandler) Handle(ctx context.Context, job generation.GenerationJob) (generation.HandlerResult, error) {
	input, err := h.seedanceInputFromJob(ctx, job)
	if err != nil {
		return generation.HandlerResult{}, err
	}
	rawArgs, err := json.Marshal(tools.ToolInvocationEnvelope[tools.SeedanceGenerateInput]{
		SessionID:      job.SessionID,
		RequestID:      job.ID,
		IdempotencyKey: job.IdempotencyKey,
		Action:         "generate_video",
		Payload:        input,
	})
	if err != nil {
		return generation.HandlerResult{}, fmt.Errorf("marshal seedance job input: %w", err)
	}
	rawResult, err := h.tool.InvokableRun(ctx, string(rawArgs))
	if err != nil {
		return generation.HandlerResult{}, err
	}
	var result tools.ToolResultEnvelope[tools.SeedanceGenerateResult]
	if err := json.Unmarshal([]byte(rawResult), &result); err != nil {
		return generation.HandlerResult{}, fmt.Errorf("decode seedance tool result: %w", err)
	}
	if result.Status != tools.ToolStatusOK {
		if result.Error != nil {
			return generation.HandlerResult{}, errors.New(result.Error.UserMessage)
		}
		return generation.HandlerResult{}, fmt.Errorf("seedance tool returned status %q", result.Status)
	}
	assetID := strings.TrimSpace(result.Data.AssetID)
	if assetID == "" {
		return generation.HandlerResult{}, fmt.Errorf("seedance job did not return a persisted asset")
	}
	return generation.HandlerResult{
		AssetIDs: []string{assetID},
		Result: map[string]any{
			"asset_id":           assetID,
			"assets":             result.Data.Assets,
			"provider_task_id":   result.Data.ProviderTaskID,
			"provider_status":    result.Data.ProviderStatus,
			"url":                result.Data.URL,
			"storyboard_updates": result.Data.StoryboardUpdates,
			"render_events":      result.Data.RenderEvents,
			"model":              result.Data.Model,
		},
	}, nil
}

func toSeedanceToolConfig(cfg SeedanceJobHandlerConfig) tools.SeedanceToolConfig {
	return tools.SeedanceToolConfig{
		APIKey:          cfg.APIKey,
		Endpoint:        cfg.Endpoint,
		HTTPClient:      cfg.HTTPClient,
		Assets:          cfg.Assets,
		AssetUploader:   cfg.AssetUploader,
		NewID:           cfg.NewID,
		Now:             cfg.Now,
		PollInterval:    cfg.PollInterval,
		MaxPollAttempts: cfg.MaxPollAttempts,
	}
}

func (h SeedanceJobHandler) seedanceInputFromJob(ctx context.Context, job generation.GenerationJob) (tools.SeedanceGenerateInput, error) {
	var input tools.SeedanceGenerateInput
	raw, err := json.Marshal(job.Payload)
	if err != nil {
		return input, fmt.Errorf("marshal seedance job payload: %w", err)
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return input, fmt.Errorf("decode seedance job payload: %w", err)
	}
	input.SessionID = valueOrDefault(input.SessionID, job.SessionID)
	input.TargetType = valueOrDefault(input.TargetType, job.TargetType)
	input.TargetID = valueOrDefault(input.TargetID, job.TargetID)
	if input.FilenamePrefix == "" {
		input.FilenamePrefix = seedanceFilenamePrefix(job)
	}
	if strings.TrimSpace(input.Prompt) == "" {
		return input, fmt.Errorf("seedance job prompt is required")
	}
	if err := h.addBoundAssetReferences(ctx, job.Payload, &input); err != nil {
		return input, err
	}
	return input, nil
}

func (h SeedanceJobHandler) addBoundAssetReferences(ctx context.Context, payload map[string]any, input *tools.SeedanceGenerateInput) error {
	if h.assets == nil {
		return nil
	}
	for _, assetID := range boundAssetIDs(payload) {
		record, err := h.assets.Get(ctx, assetID)
		if err != nil {
			return fmt.Errorf("get bound asset %s: %w", assetID, err)
		}
		if strings.TrimSpace(record.URL) == "" {
			return fmt.Errorf("bound asset %s url is empty", assetID)
		}
		switch record.Kind {
		case asset.KindImage, asset.KindReference:
			input.ReferenceImageURLs = appendMissingString(input.ReferenceImageURLs, record.URL)
		case asset.KindVideo:
			input.ReferenceVideoURLs = appendMissingString(input.ReferenceVideoURLs, record.URL)
		case asset.KindAudio:
			input.ReferenceAudioURLs = appendMissingString(input.ReferenceAudioURLs, record.URL)
		}
	}
	return nil
}

func boundAssetIDs(payload map[string]any) []string {
	if payload == nil {
		return nil
	}
	raw := payload["bind_asset_ids"]
	switch ids := raw.(type) {
	case []string:
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id != "" {
				out = append(out, id)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(ids))
		for _, value := range ids {
			id, _ := value.(string)
			id = strings.TrimSpace(id)
			if id != "" {
				out = append(out, id)
			}
		}
		return out
	default:
		return nil
	}
}

func appendMissingString(values []string, next string) []string {
	next = strings.TrimSpace(next)
	if next == "" {
		return values
	}
	for _, value := range values {
		if strings.TrimSpace(value) == next {
			return values
		}
	}
	return append(values, next)
}

func seedanceFilenamePrefix(job generation.GenerationJob) string {
	if strings.TrimSpace(job.TargetID) == "" {
		return "seedance"
	}
	return "seedance-" + strings.TrimSpace(job.TargetID)
}

var _ generation.JobHandler = SeedanceJobHandler{}
var _ tools.SeedanceAssetStore = (*asset.PostgresStore)(nil)
