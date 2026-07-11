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

// SeedanceJobHandlerConfig 汇总后台 Seedance 视频生成任务所需的配置和依赖。
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

// SeedanceJobHandler 把队列中的视频生成任务委托给 Seedance tool 执行。
type SeedanceJobHandler struct {
	tool         tools.SeedanceGenerateTool
	assets       SeedanceJobAssetStore
	pollInterval time.Duration
}

// SeedanceJobAssetStore 扩展 Seedance tool 的素材存储能力，用于读取已绑定素材详情。
type SeedanceJobAssetStore interface {
	tools.SeedanceAssetStore
	Get(ctx context.Context, assetID string) (asset.Asset, error)
}

// NewSeedanceJobHandler 创建 Seedance 后台任务处理器。
func NewSeedanceJobHandler(cfg SeedanceJobHandlerConfig) SeedanceJobHandler {
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = tools.DefaultSeedancePollInterval
	}
	return SeedanceJobHandler{
		tool: tools.NewSeedanceGenerateTool(toSeedanceToolConfig(cfg)), assets: cfg.Assets,
		pollInterval: pollInterval,
	}
}

// Submit implements the durable provider boundary: it returns immediately
// after task creation so LifecycleWorker persists ProviderTaskID before any
// polling occurs.
func (h SeedanceJobHandler) Submit(ctx context.Context, job generation.GenerationJob) (generation.ProviderResponse, error) {
	input, err := h.seedanceInputFromJob(ctx, job)
	if err != nil {
		return generation.ProviderResponse{}, err
	}
	taskID, err := h.tool.SubmitTask(ctx, input)
	if err != nil {
		return generation.ProviderResponse{}, generation.NewExecutionError(generation.ErrorStageProvider, "seedance_submit_failed", generation.ProviderErrorRetryable(err), err)
	}
	return generation.ProviderResponse{State: generation.ProviderStateAccepted, TaskID: taskID, RequestID: job.ID, Status: generation.ProviderStateAccepted, RetryAfter: h.pollInterval}, nil
}

func (h SeedanceJobHandler) Poll(ctx context.Context, job generation.GenerationJob) (generation.ProviderResponse, error) {
	task, err := h.tool.QueryTask(ctx, job.ProviderTaskID)
	if err != nil {
		return generation.ProviderResponse{}, generation.NewExecutionError(generation.ErrorStageProvider, "seedance_poll_failed", generation.ProviderErrorRetryable(err), err)
	}
	if strings.TrimSpace(task.ID) == "" {
		task.ID = job.ProviderTaskID
	}
	status := strings.ToLower(strings.TrimSpace(task.Status))
	switch status {
	case "succeeded", "success", "completed":
		input, err := h.seedanceInputFromJob(ctx, job)
		if err != nil {
			return generation.ProviderResponse{}, err
		}
		result, err := h.tool.PersistCompletedTask(ctx, input, task)
		if err != nil {
			return generation.ProviderResponse{}, generation.NewExecutionError(generation.ErrorStageArtifact, "seedance_asset_persist_failed", generation.ProviderErrorRetryable(err), err)
		}
		payload := map[string]any{}
		raw, _ := json.Marshal(result)
		_ = json.Unmarshal(raw, &payload)
		return generation.ProviderResponse{State: generation.ProviderStateCompleted, TaskID: task.ID, RequestID: job.ID, Status: task.Status, Result: generation.ProviderResult{TaskID: task.ID, RequestID: job.ID, Status: task.Status, AssetIDs: []string{result.AssetID}, Payload: payload}}, nil
	case "cancelled", "canceled":
		return generation.ProviderResponse{State: generation.ProviderStateCancelled, TaskID: task.ID, RequestID: job.ID, Status: task.Status}, nil
	case "failed", "expired":
		return generation.ProviderResponse{State: generation.ProviderStateFailed, TaskID: task.ID, RequestID: job.ID, Status: task.Status, Result: generation.ProviderResult{Payload: map[string]any{"error_code": task.ErrorCode, "error_message": task.ErrorMessage}}}, nil
	case "queued", "pending", "running", "processing", "submitted", "in_progress":
		return generation.ProviderResponse{State: generation.ProviderStatePending, TaskID: job.ProviderTaskID, RequestID: job.ID, Status: task.Status, RetryAfter: h.pollInterval}, nil
	default:
		return generation.ProviderResponse{}, generation.NewExecutionError(generation.ErrorStageProvider, "seedance_unknown_status", false, fmt.Errorf("seedance task %s returned unsupported status %q", task.ID, task.Status))
	}
}

func (h SeedanceJobHandler) Cancel(ctx context.Context, job generation.GenerationJob) (generation.ProviderCancelResult, error) {
	confirmed, err := h.tool.CancelTask(ctx, job.ProviderTaskID)
	if err != nil {
		return generation.ProviderCancelResult{}, generation.NewExecutionError(generation.ErrorStageProvider, "seedance_cancel_failed", generation.ProviderErrorRetryable(err), err)
	}
	return generation.ProviderCancelResult{Confirmed: confirmed, Status: "cancel_requested"}, nil
}

// Handle 执行单个 Seedance 任务，并返回已持久化视频资产和业务结果。
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
			"model":              result.Data.Model,
		},
	}, nil
}

// toSeedanceToolConfig 把后台任务配置转换成可复用的 Seedance tool 配置。
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

// seedanceInputFromJob 从任务 payload 中解析视频生成输入，并补齐引用素材。
func (h SeedanceJobHandler) seedanceInputFromJob(ctx context.Context, job generation.GenerationJob) (tools.SeedanceGenerateInput, error) {
	var input tools.SeedanceGenerateInput
	if err := generation.ValidateProviderJob(job); err != nil {
		return input, generation.NewExecutionError(generation.ErrorStageProvider, "invalid_provider_input", false, err)
	}
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
	input.SourceJobID = job.ID
	input.OutputIndex = 0
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

// addBoundAssetReferences 把故事板已绑定素材转换成 Seedance 可用的参考 URL。
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

// boundAssetIDs 从任务 payload 中解析需要作为参考的素材 ID 列表。
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

// appendMissingString 追加非空且未重复的字符串。
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

// seedanceFilenamePrefix 为持久化视频生成稳定文件名前缀。
func seedanceFilenamePrefix(job generation.GenerationJob) string {
	if strings.TrimSpace(job.TargetID) == "" {
		return "seedance"
	}
	return "seedance-" + strings.TrimSpace(job.TargetID)
}

var _ generation.JobHandler = SeedanceJobHandler{}
var _ generation.ProviderAdapter = SeedanceJobHandler{}
var _ tools.SeedanceAssetStore = (*asset.PostgresStore)(nil)
