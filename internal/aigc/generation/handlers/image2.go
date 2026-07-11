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

// Image2JobHandlerConfig 汇总后台 Image2 生成任务所需的工具配置和存储依赖。
type Image2JobHandlerConfig struct {
	APIKey        string
	Endpoint      string
	HTTPClient    *http.Client
	Assets        tools.Image2AssetStore
	AssetUploader tools.Image2AssetUploader
	NewID         func() string
	Now           func() time.Time
}

// Image2JobHandler 把队列中的图片生成任务委托给 Image2 tool 执行。
type Image2JobHandler struct {
	tool tools.Image2GenerateTool
}

// NewImage2JobHandler 创建 Image2 后台任务处理器。
func NewImage2JobHandler(cfg Image2JobHandlerConfig) Image2JobHandler {
	return Image2JobHandler{
		tool: tools.NewImage2GenerateTool(toImage2ToolConfig(cfg)),
	}
}

// Handle 执行单个 Image2 任务，并返回已持久化资产 ID 和业务结果。
func (h Image2JobHandler) Handle(ctx context.Context, job generation.GenerationJob) (generation.HandlerResult, error) {
	input, err := image2InputFromJob(job)
	if err != nil {
		return generation.HandlerResult{}, err
	}
	rawArgs, err := json.Marshal(tools.ToolInvocationEnvelope[tools.Image2GenerateInput]{
		SessionID:      job.SessionID,
		RequestID:      job.ID,
		IdempotencyKey: job.IdempotencyKey,
		Action:         "generate_image",
		Payload:        input,
	})
	if err != nil {
		return generation.HandlerResult{}, fmt.Errorf("marshal image2 job input: %w", err)
	}
	rawResult, err := h.tool.InvokableRun(ctx, string(rawArgs))
	if err != nil {
		return generation.HandlerResult{}, err
	}
	var result tools.ToolResultEnvelope[tools.Image2GenerateResult]
	if err := json.Unmarshal([]byte(rawResult), &result); err != nil {
		return generation.HandlerResult{}, fmt.Errorf("decode image2 tool result: %w", err)
	}
	if result.Status != tools.ToolStatusOK {
		if result.Error != nil {
			return generation.HandlerResult{}, errors.New(result.Error.UserMessage)
		}
		return generation.HandlerResult{}, fmt.Errorf("image2 tool returned status %q", result.Status)
	}
	assetIDs := make([]string, 0, len(result.Data.Assets))
	for _, item := range result.Data.Assets {
		if strings.TrimSpace(item.AssetID) != "" {
			assetIDs = append(assetIDs, strings.TrimSpace(item.AssetID))
		}
	}
	if len(assetIDs) == 0 {
		return generation.HandlerResult{}, fmt.Errorf("image2 job did not return persisted assets")
	}
	return generation.HandlerResult{
		AssetIDs: assetIDs,
		Result: map[string]any{
			"asset_ids":          assetIDs,
			"assets":             result.Data.Assets,
			"storyboard_updates": result.Data.StoryboardUpdates,
			"model":              result.Data.Model,
		},
	}, nil
}

// toImage2ToolConfig 把后台任务配置转换成可复用的 Image2 tool 配置。
func toImage2ToolConfig(cfg Image2JobHandlerConfig) tools.Image2ToolConfig {
	return tools.Image2ToolConfig{
		APIKey:        cfg.APIKey,
		Endpoint:      cfg.Endpoint,
		HTTPClient:    cfg.HTTPClient,
		Assets:        cfg.Assets,
		AssetUploader: cfg.AssetUploader,
		NewID:         cfg.NewID,
		Now:           cfg.Now,
	}
}

// image2InputFromJob 从任务 payload 中解析图片生成输入，并补齐会话/目标信息。
func image2InputFromJob(job generation.GenerationJob) (tools.Image2GenerateInput, error) {
	var input tools.Image2GenerateInput
	if err := generation.ValidateProviderJob(job); err != nil {
		return input, generation.NewExecutionError(generation.ErrorStageProvider, "invalid_provider_input", false, err)
	}
	raw, err := json.Marshal(job.Payload)
	if err != nil {
		return input, fmt.Errorf("marshal image2 job payload: %w", err)
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return input, fmt.Errorf("decode image2 job payload: %w", err)
	}
	input.SessionID = valueOrDefault(input.SessionID, job.SessionID)
	input.TargetType = valueOrDefault(input.TargetType, job.TargetType)
	input.TargetID = valueOrDefault(input.TargetID, job.TargetID)
	input.SourceJobID = job.ID
	input.OutputIndexBase = 0
	if input.FilenamePrefix == "" {
		input.FilenamePrefix = image2FilenamePrefix(job)
	}
	if strings.TrimSpace(input.Prompt) == "" {
		return input, fmt.Errorf("image2 job prompt is required")
	}
	return input, nil
}

// image2FilenamePrefix 为持久化图片生成稳定文件名前缀。
func image2FilenamePrefix(job generation.GenerationJob) string {
	if strings.TrimSpace(job.TargetID) == "" {
		return "image2"
	}
	return "image2-" + strings.TrimSpace(job.TargetID)
}

// valueOrDefault 返回 trim 后的 value，空值时返回 trim 后的 fallback。
func valueOrDefault(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return strings.TrimSpace(fallback)
	}
	return value
}

var _ generation.JobHandler = Image2JobHandler{}
var _ tools.Image2AssetStore = (*asset.PostgresStore)(nil)
