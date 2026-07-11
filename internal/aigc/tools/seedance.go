package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
)

const (
	SeedanceGenerateVideoToolKey = "seedance_generate_video"
	DefaultSeedanceEndpoint      = "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks"
	DefaultSeedanceModel         = "doubao-seedance-2-0-fast-260128"
	DefaultSeedancePollInterval  = 5 * time.Second
	DefaultSeedancePollAttempts  = 120
	MaxSeedanceDurationSeconds   = 30
	MaxSeedanceFPS               = 60
)

// SeedanceToolConfig 汇总 Seedance 视频生成工具的 provider、存储和轮询配置。
type SeedanceToolConfig struct {
	APIKey          string
	Endpoint        string
	HTTPClient      *http.Client
	Assets          SeedanceAssetStore
	AssetUploader   SeedanceAssetUploader
	NewID           func() string
	Now             func() time.Time
	PollInterval    time.Duration
	MaxPollAttempts int
}

// SeedanceGenerateTool 是 Eino 可调用的视频生成工具，返回素材摘要而不是 UI 事件。
type SeedanceGenerateTool struct {
	cfg SeedanceToolConfig
}

// SeedanceAssetStore 定义视频生成结果写入素材表所需能力。
type SeedanceAssetStore interface {
	Save(ctx context.Context, record asset.Asset) (asset.Asset, error)
}

// SeedanceAssetUploader 定义视频生成结果上传对象存储所需能力。
type SeedanceAssetUploader interface {
	Upload(ctx context.Context, input asset.UploadInput) (asset.UploadResult, error)
}

// SeedanceGenerateInput 是 Agent 调用视频生成工具时传入的业务参数。
type SeedanceGenerateInput struct {
	SessionID          string   `json:"session_id,omitempty"`
	UserID             string   `json:"user_id,omitempty"`
	TargetType         string   `json:"target_type,omitempty"`
	TargetID           string   `json:"target_id,omitempty"`
	FilenamePrefix     string   `json:"filename_prefix,omitempty"`
	Prompt             string   `json:"prompt"`
	Model              string   `json:"model,omitempty"`
	Ratio              string   `json:"ratio,omitempty"`
	Resolution         string   `json:"resolution,omitempty"`
	DurationSeconds    int      `json:"duration_seconds,omitempty"`
	FPS                int      `json:"fps,omitempty"`
	ReferenceImageURLs []string `json:"reference_image_urls,omitempty"`
	ReferenceVideoURLs []string `json:"reference_video_urls,omitempty"`
	ReferenceAudioURLs []string `json:"reference_audio_urls,omitempty"`
	SourceJobID        string   `json:"source_job_id,omitempty"`
	OutputIndex        int      `json:"output_index,omitempty"`
}

type SeedanceTaskSnapshot struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	VideoURL     string `json:"video_url,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// SeedanceGenerateResult 是视频生成工具返回给 Agent 的紧凑业务结果。
type SeedanceGenerateResult struct {
	Model             string                 `json:"model"`
	ProviderTaskID    string                 `json:"provider_task_id"`
	ProviderStatus    string                 `json:"provider_status"`
	AssetID           string                 `json:"asset_id,omitempty"`
	URL               string                 `json:"url,omitempty"`
	MediaType         string                 `json:"media_type,omitempty"`
	StorageProvider   string                 `json:"storage_provider,omitempty"`
	Bucket            string                 `json:"bucket,omitempty"`
	ObjectKey         string                 `json:"object_key,omitempty"`
	Assets            []GeneratedAssetInfo   `json:"assets,omitempty"`
	StoryboardUpdates []StoryboardUpdateHint `json:"storyboard_updates,omitempty"`
}

// seedanceAPIRequest 是发送给 Seedance provider 的任务创建请求体。
type seedanceAPIRequest struct {
	Model   string            `json:"model"`
	Content []seedanceContent `json:"content"`
}

// seedanceContent 表示 Seedance 多模态 content 数组中的一个输入块。
type seedanceContent struct {
	Type     string              `json:"type"`
	Text     string              `json:"text,omitempty"`
	ImageURL *seedanceURLContent `json:"image_url,omitempty"`
	VideoURL *seedanceURLContent `json:"video_url,omitempty"`
	AudioURL *seedanceURLContent `json:"audio_url,omitempty"`
	Role     string              `json:"role,omitempty"`
}

// seedanceURLContent 表示 Seedance 参考素材 URL。
type seedanceURLContent struct {
	URL string `json:"url"`
}

// seedanceCreateResponse 是 Seedance 创建任务响应的最小解析结构。
type seedanceCreateResponse struct {
	ID string `json:"id"`
}

// seedanceTaskResponse 是 Seedance 查询任务响应的最小解析结构。
type seedanceTaskResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Output struct {
		VideoURL string `json:"video_url"`
		URL      string `json:"url"`
	} `json:"output"`
	Error *seedanceProviderError `json:"error,omitempty"`
}

// seedanceProviderError 描述 Seedance provider 返回的任务错误。
type seedanceProviderError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// NewSeedanceGenerateTool 创建视频生成工具，并补齐 endpoint、HTTP client、轮询和 ID 默认值。
func NewSeedanceGenerateTool(cfg SeedanceToolConfig) SeedanceGenerateTool {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultSeedanceEndpoint
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Minute}
	}
	if cfg.NewID == nil {
		cfg.NewID = defaultSeedanceAssetID
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultSeedancePollInterval
	}
	if cfg.MaxPollAttempts <= 0 {
		cfg.MaxPollAttempts = DefaultSeedancePollAttempts
	}
	return SeedanceGenerateTool{cfg: cfg}
}

// SubmitTask performs only the provider create call. The caller must persist
// the returned task id before polling so a process restart never resubmits an
// already accepted paid generation.
func (t SeedanceGenerateTool) SubmitTask(ctx context.Context, input SeedanceGenerateInput) (string, error) {
	normalized, err := normalizeSeedanceInput(input)
	if err != nil {
		return "", err
	}
	if t.cfg.APIKey == "" {
		return "", fmt.Errorf("seedance api key is required")
	}
	return t.createTask(ctx, normalized)
}

func (t SeedanceGenerateTool) QueryTask(ctx context.Context, taskID string) (SeedanceTaskSnapshot, error) {
	task, err := t.getTask(ctx, taskID)
	if err != nil {
		return SeedanceTaskSnapshot{}, err
	}
	videoURL := strings.TrimSpace(task.Output.VideoURL)
	if videoURL == "" {
		videoURL = strings.TrimSpace(task.Output.URL)
	}
	out := SeedanceTaskSnapshot{ID: strings.TrimSpace(task.ID), Status: strings.TrimSpace(task.Status), VideoURL: videoURL}
	if task.Error != nil {
		out.ErrorCode, out.ErrorMessage = strings.TrimSpace(task.Error.Code), strings.TrimSpace(task.Error.Message)
	}
	return out, nil
}

func (t SeedanceGenerateTool) PersistCompletedTask(ctx context.Context, input SeedanceGenerateInput, task SeedanceTaskSnapshot) (SeedanceGenerateResult, error) {
	normalized, err := normalizeSeedanceInput(input)
	if err != nil {
		return SeedanceGenerateResult{}, err
	}
	if strings.TrimSpace(task.VideoURL) == "" {
		return SeedanceGenerateResult{}, fmt.Errorf("seedance task %s completed without video url", task.ID)
	}
	result := SeedanceGenerateResult{Model: normalized.Model, ProviderTaskID: task.ID, ProviderStatus: task.Status, MediaType: "video/mp4"}
	if t.shouldPersistAsset(normalized) {
		result, err = t.persistVideo(ctx, normalized, result, task.VideoURL)
		if err != nil {
			return SeedanceGenerateResult{}, err
		}
	} else {
		result.Assets = seedanceGeneratedAssets(normalized, result)
	}
	result.StoryboardUpdates = generativeStoryboardUpdates(result.Assets)
	return result, nil
}

func (t SeedanceGenerateTool) CancelTask(ctx context.Context, taskID string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.cfg.Endpoint+"/"+url.PathEscape(strings.TrimSpace(taskID)), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+t.cfg.APIKey)
	resp, err := t.cfg.HTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if (resp.StatusCode >= 200 && resp.StatusCode < 300 && resp.StatusCode != http.StatusAccepted) || resp.StatusCode == http.StatusNotFound {
		return true, nil
	}
	if resp.StatusCode == http.StatusAccepted {
		return false, nil
	}
	if resp.StatusCode == http.StatusConflict {
		return false, nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return false, fmt.Errorf("seedance cancel task returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
}

// Info 返回 Eino 工具元信息和参数 schema，供 Agent 正确构造调用参数。
func (SeedanceGenerateTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: SeedanceGenerateVideoToolKey,
		Desc: "Generate video assets with Volcengine Ark Seedance. Provider payloads are never returned to the Agent; the result only contains compact asset and storyboard hints.",
		ParamsOneOf: schema.NewParamsOneOfByParams(toolInvocationEnvelopeParams(map[string]*schema.ParameterInfo{
			"user_id": {
				Type: schema.String,
				Desc: "Current user id, if available.",
			},
			"target_type": {
				Type: schema.String,
				Desc: "Storyboard target type for the asset, such as shot.",
			},
			"target_id": {
				Type: schema.String,
				Desc: "Storyboard target id for the video asset.",
			},
			"prompt": {
				Type:     schema.String,
				Desc:     "Video generation prompt.",
				Required: true,
			},
			"model": {
				Type: schema.String,
				Desc: "Seedance model. Defaults to doubao-seedance-2-0-fast-260128.",
				Enum: []string{DefaultSeedanceModel, "doubao-seedance-2-0-260128"},
			},
			"ratio": {
				Type: schema.String,
				Desc: "Output aspect ratio, such as 16:9 or 9:16.",
			},
			"resolution": {
				Type: schema.String,
				Desc: "Output resolution, such as 480p or 720p.",
			},
			"duration_seconds": {
				Type: schema.Integer,
				Desc: "Output duration in seconds.",
			},
			"fps": {
				Type: schema.Integer,
				Desc: "Output frames per second.",
			},
		})),
	}, nil
}

// InvokableRun 创建并轮询 Seedance 任务，必要时持久化视频资产。
func (t SeedanceGenerateTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	invocation, err := decodeSeedanceInvocation(argumentsInJSON)
	if err != nil {
		return "", err
	}
	payload := invocation.Payload
	if strings.TrimSpace(payload.SessionID) == "" {
		payload.SessionID = invocation.SessionID
	}
	input, err := normalizeSeedanceInput(payload)
	if err != nil {
		return "", err
	}
	if t.cfg.APIKey == "" {
		return "", fmt.Errorf("seedance api key is required")
	}

	taskID, err := t.createTask(ctx, input)
	if err != nil {
		return "", err
	}
	task, err := t.pollTask(ctx, taskID)
	if err != nil {
		return "", err
	}
	videoURL := strings.TrimSpace(task.Output.VideoURL)
	if videoURL == "" {
		videoURL = strings.TrimSpace(task.Output.URL)
	}
	if videoURL == "" {
		return "", fmt.Errorf("seedance task %s succeeded without video url", taskID)
	}

	result := SeedanceGenerateResult{
		Model:          input.Model,
		ProviderTaskID: taskID,
		ProviderStatus: task.Status,
		MediaType:      "video/mp4",
	}
	if t.shouldPersistAsset(input) {
		result, err = t.persistVideo(ctx, input, result, videoURL)
		if err != nil {
			return "", err
		}
	} else {
		result.Assets = seedanceGeneratedAssets(input, result)
	}
	result.StoryboardUpdates = generativeStoryboardUpdates(result.Assets)

	out, err := json.Marshal(ToolResultEnvelope[SeedanceGenerateResult]{
		Status:         ToolStatusOK,
		RequestID:      invocation.RequestID,
		IdempotencyKey: invocation.IdempotencyKey,
		ArtifactIDs:    generativeArtifactIDs(result.Assets),
		Data:           result,
	})
	if err != nil {
		return "", fmt.Errorf("marshal seedance result: %w", err)
	}
	return string(out), nil
}

// createTask 调用 Seedance provider 创建异步视频生成任务。
func (t SeedanceGenerateTool) createTask(ctx context.Context, input SeedanceGenerateInput) (string, error) {
	body, err := json.Marshal(seedanceAPIRequest{
		Model:   input.Model,
		Content: seedanceContents(input),
	})
	if err != nil {
		return "", fmt.Errorf("marshal seedance request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create seedance request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+t.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if input.SourceJobID != "" {
		req.Header.Set("Idempotency-Key", input.SourceJobID)
		req.Header.Set("X-Client-Request-Id", input.SourceJobID)
	}

	resp, err := t.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call seedance create task: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("seedance create task returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var out seedanceCreateResponse
	if err := decodeLimitedProviderJSON(resp.Body, 2<<20, &out); err != nil {
		return "", fmt.Errorf("decode seedance create task response: %w", err)
	}
	out.ID = strings.TrimSpace(out.ID)
	if out.ID == "" {
		return "", fmt.Errorf("seedance create task response missing id")
	}
	return out.ID, nil
}

// pollTask 按配置轮询 Seedance 任务，直到成功、失败或超时。
func (t SeedanceGenerateTool) pollTask(ctx context.Context, taskID string) (seedanceTaskResponse, error) {
	var last seedanceTaskResponse
	for attempt := 0; attempt < t.cfg.MaxPollAttempts; attempt++ {
		task, err := t.getTask(ctx, taskID)
		if err != nil {
			return seedanceTaskResponse{}, err
		}
		last = task
		switch strings.ToLower(strings.TrimSpace(task.Status)) {
		case "succeeded", "success", "completed":
			return task, nil
		case "failed", "cancelled", "canceled", "expired":
			if task.Error != nil && strings.TrimSpace(task.Error.Message) != "" {
				return seedanceTaskResponse{}, fmt.Errorf("seedance task %s failed: %s", taskID, strings.TrimSpace(task.Error.Message))
			}
			return seedanceTaskResponse{}, fmt.Errorf("seedance task %s reached terminal status %q", taskID, task.Status)
		case "queued", "pending", "running", "processing", "submitted", "in_progress":
			// Continue with the configured durable polling budget.
		default:
			return seedanceTaskResponse{}, fmt.Errorf("seedance task %s returned unsupported status %q", taskID, task.Status)
		}
		if attempt+1 >= t.cfg.MaxPollAttempts {
			break
		}
		timer := time.NewTimer(t.cfg.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return seedanceTaskResponse{}, ctx.Err()
		case <-timer.C:
		}
	}
	return seedanceTaskResponse{}, fmt.Errorf("seedance task %s did not finish after %d polls, last status %q", taskID, t.cfg.MaxPollAttempts, last.Status)
}

// getTask 查询单个 Seedance 任务的当前状态。
func (t SeedanceGenerateTool) getTask(ctx context.Context, taskID string) (seedanceTaskResponse, error) {
	queryURL := t.cfg.Endpoint + "/" + url.PathEscape(strings.TrimSpace(taskID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
	if err != nil {
		return seedanceTaskResponse{}, fmt.Errorf("create seedance query request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+t.cfg.APIKey)

	resp, err := t.cfg.HTTPClient.Do(req)
	if err != nil {
		return seedanceTaskResponse{}, fmt.Errorf("call seedance query task: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return seedanceTaskResponse{}, fmt.Errorf("seedance query task returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var out seedanceTaskResponse
	if err := decodeLimitedProviderJSON(resp.Body, 2<<20, &out); err != nil {
		return seedanceTaskResponse{}, fmt.Errorf("decode seedance query task response: %w", err)
	}
	return out, nil
}

// decodeSeedanceInvocation 只接受标准工具 envelope，视频生成参数必须放入 payload。
func decodeSeedanceInvocation(argumentsInJSON string) (ToolInvocationEnvelope[SeedanceGenerateInput], error) {
	return decodeToolInvocationEnvelope(SeedanceGenerateVideoToolKey, argumentsInJSON, func(payload SeedanceGenerateInput) bool {
		return strings.TrimSpace(payload.Prompt) != ""
	})
}

// normalizeSeedanceInput 清理视频生成输入，并补齐默认模型和参考 URL 列表。
func normalizeSeedanceInput(input SeedanceGenerateInput) (SeedanceGenerateInput, error) {
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.UserID = strings.TrimSpace(input.UserID)
	input.TargetType = strings.TrimSpace(input.TargetType)
	input.TargetID = strings.TrimSpace(input.TargetID)
	input.FilenamePrefix = strings.TrimSpace(input.FilenamePrefix)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Model = strings.TrimSpace(input.Model)
	input.Ratio = strings.TrimSpace(input.Ratio)
	input.Resolution = strings.TrimSpace(input.Resolution)
	input.ReferenceImageURLs = normalizeURLList(input.ReferenceImageURLs)
	input.ReferenceVideoURLs = normalizeURLList(input.ReferenceVideoURLs)
	input.ReferenceAudioURLs = normalizeURLList(input.ReferenceAudioURLs)
	if input.Prompt == "" {
		return input, fmt.Errorf("prompt is required")
	}
	if input.Model == "" {
		input.Model = DefaultSeedanceModel
	}
	switch input.Model {
	case DefaultSeedanceModel, "doubao-seedance-2-0-260128":
	default:
		return input, fmt.Errorf("unsupported seedance model %q", input.Model)
	}
	if input.Ratio != "" {
		switch input.Ratio {
		case "16:9", "9:16", "1:1", "4:3", "3:4", "21:9":
		default:
			return input, fmt.Errorf("unsupported seedance ratio %q", input.Ratio)
		}
	}
	input.Resolution = strings.ToLower(input.Resolution)
	if input.Resolution != "" {
		switch input.Resolution {
		case "480p", "720p", "1080p":
		default:
			return input, fmt.Errorf("unsupported seedance resolution %q", input.Resolution)
		}
	}
	if input.DurationSeconds < 0 || input.DurationSeconds > MaxSeedanceDurationSeconds {
		return input, fmt.Errorf("duration_seconds must be between 0 and %d", MaxSeedanceDurationSeconds)
	}
	if input.FPS < 0 || input.FPS > MaxSeedanceFPS {
		return input, fmt.Errorf("fps must be between 0 and %d", MaxSeedanceFPS)
	}
	return input, nil
}

// seedanceContents 把 prompt 和参考素材 URL 转成 Seedance 多模态 content。
func seedanceContents(input SeedanceGenerateInput) []seedanceContent {
	out := []seedanceContent{{
		Type: "text",
		Text: seedancePromptText(input),
	}}
	for _, rawURL := range input.ReferenceImageURLs {
		out = append(out, seedanceContent{
			Type:     "image_url",
			ImageURL: &seedanceURLContent{URL: rawURL},
			Role:     "reference_image",
		})
	}
	for _, rawURL := range input.ReferenceVideoURLs {
		out = append(out, seedanceContent{
			Type:     "video_url",
			VideoURL: &seedanceURLContent{URL: rawURL},
			Role:     "reference_video",
		})
	}
	for _, rawURL := range input.ReferenceAudioURLs {
		out = append(out, seedanceContent{
			Type:     "audio_url",
			AudioURL: &seedanceURLContent{URL: rawURL},
			Role:     "reference_audio",
		})
	}
	return out
}

// seedancePromptText 把比例、帧率、时长和分辨率参数附加到 Seedance prompt。
func seedancePromptText(input SeedanceGenerateInput) string {
	parts := []string{input.Prompt}
	if input.Ratio != "" {
		parts = append(parts, "--ratio "+input.Ratio)
	}
	if input.FPS > 0 {
		parts = append(parts, fmt.Sprintf("--fps %d", input.FPS))
	}
	if input.DurationSeconds > 0 {
		parts = append(parts, fmt.Sprintf("--dur %d", input.DurationSeconds))
	}
	if input.Resolution != "" {
		parts = append(parts, "--resolution "+input.Resolution)
	}
	return strings.Join(parts, " ")
}

// shouldPersistAsset 判断本次视频结果是否具备上传和入库条件。
func (t SeedanceGenerateTool) shouldPersistAsset(input SeedanceGenerateInput) bool {
	return t.cfg.Assets != nil && t.cfg.AssetUploader != nil && input.SessionID != ""
}

// persistVideo 下载 provider 视频，上传对象存储并保存素材记录。
func (t SeedanceGenerateTool) persistVideo(ctx context.Context, input SeedanceGenerateInput, result SeedanceGenerateResult, providerVideoURL string) (SeedanceGenerateResult, error) {
	raw, mediaType, err := t.downloadVideo(ctx, providerVideoURL)
	if err != nil {
		return result, err
	}
	if mediaType == "" || mediaType == "application/octet-stream" {
		mediaType = "video/mp4"
	}
	assetID := t.cfg.NewID()
	if input.SourceJobID != "" {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", input.SourceJobID, input.OutputIndex)))
		assetID = "video_" + hex.EncodeToString(sum[:12])
	}
	filename := seedanceAssetFilename(input.FilenamePrefix, mediaType)
	objectKey := asset.NewObjectKey(input.SessionID, assetID, filename)
	metadata := map[string]any{
		"provider":         "seedance",
		"provider_task_id": result.ProviderTaskID,
		"model":            input.Model,
	}
	uploadMetadata := map[string]string{
		"provider":         "seedance",
		"provider_task_id": result.ProviderTaskID,
	}
	if input.TargetType != "" {
		metadata["target_type"] = input.TargetType
		uploadMetadata["target_type"] = input.TargetType
	}
	if input.TargetID != "" {
		metadata["target_id"] = input.TargetID
		uploadMetadata["target_id"] = input.TargetID
	}

	uploadResult, err := t.cfg.AssetUploader.Upload(ctx, asset.UploadInput{
		ObjectKey:     objectKey,
		Content:       bytes.NewReader(raw),
		ContentLength: int64(len(raw)),
		MIMEType:      mediaType,
		Filename:      filename,
		Metadata:      uploadMetadata,
	})
	if err != nil {
		return result, fmt.Errorf("upload seedance asset %s: %w", assetID, err)
	}
	if uploadResult.ObjectKey == "" {
		uploadResult.ObjectKey = objectKey
	}
	if uploadResult.SizeBytes == 0 {
		uploadResult.SizeBytes = int64(len(raw))
	}
	now := t.cfg.Now()
	saved, err := t.cfg.Assets.Save(ctx, asset.Asset{
		ID:              assetID,
		SessionID:       input.SessionID,
		UserID:          input.UserID,
		SourceJobID:     input.SourceJobID,
		OutputIndex:     input.OutputIndex,
		Kind:            asset.KindVideo,
		Source:          asset.SourceGenerated,
		MIMEType:        mediaType,
		Filename:        filename,
		SizeBytes:       uploadResult.SizeBytes,
		StorageProvider: uploadResult.Provider,
		Bucket:          uploadResult.Bucket,
		ObjectKey:       uploadResult.ObjectKey,
		URL:             uploadResult.URL,
		Metadata:        metadata,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return result, fmt.Errorf("save seedance asset %s: %w", assetID, err)
	}
	result.AssetID = saved.ID
	result.URL = saved.URL
	result.MediaType = saved.MIMEType
	result.StorageProvider = saved.StorageProvider
	result.Bucket = saved.Bucket
	result.ObjectKey = saved.ObjectKey
	result.Assets = seedanceGeneratedAssets(input, result)
	return result, nil
}

// seedanceGeneratedAssets 把 Seedance 结果转换成 Agent 可消费的素材摘要。
func seedanceGeneratedAssets(input SeedanceGenerateInput, result SeedanceGenerateResult) []GeneratedAssetInfo {
	status := "generated_not_persisted"
	if strings.TrimSpace(result.AssetID) != "" {
		status = "generated"
	}
	return []GeneratedAssetInfo{{
		AssetID:         strings.TrimSpace(result.AssetID),
		Kind:            asset.KindVideo,
		URL:             safeSeedanceAssetURL(result),
		TargetType:      input.TargetType,
		TargetID:        input.TargetID,
		Field:           generativeAssetField(asset.KindVideo, input.TargetType),
		Status:          status,
		MediaType:       result.MediaType,
		StorageProvider: result.StorageProvider,
		Bucket:          result.Bucket,
		ObjectKey:       result.ObjectKey,
	}}
}

// safeSeedanceAssetURL 只在视频已持久化后向 Agent 暴露可用 URL。
func safeSeedanceAssetURL(result SeedanceGenerateResult) string {
	if strings.TrimSpace(result.AssetID) == "" {
		return ""
	}
	return strings.TrimSpace(result.URL)
}

// downloadVideo 从 provider URL 下载生成视频并返回媒体类型。
func (t SeedanceGenerateTool) downloadVideo(ctx context.Context, rawURL string) ([]byte, string, error) {
	raw, mediaType, err := downloadProviderObject(ctx, t.cfg.HTTPClient, rawURL, t.cfg.Endpoint, maxVideoAssetBytes)
	if err != nil {
		return nil, "", fmt.Errorf("download seedance video: %w", err)
	}
	return raw, mediaType, nil
}

// seedanceAssetFilename 根据前缀和媒体类型生成视频文件名。
func seedanceAssetFilename(prefix string, mediaType string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "seedance"
	}
	ext := "mp4"
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "video/webm":
		ext = "webm"
	case "video/quicktime":
		ext = "mov"
	}
	if strings.HasSuffix(strings.ToLower(prefix), "."+ext) {
		return prefix
	}
	return prefix + "." + ext
}

// normalizeURLList 清理参考素材 URL 列表并移除空字符串。
func normalizeURLList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// defaultSeedanceAssetID 生成视频素材默认 ID，随机源失败时使用时间戳兜底。
func defaultSeedanceAssetID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("asset-%d", time.Now().UnixNano())
}

var _ einotool.InvokableTool = SeedanceGenerateTool{}
