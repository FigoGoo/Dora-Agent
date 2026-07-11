package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
)

const (
	Image2GenerateToolKey = "image2_generate_image"
	DefaultImage2Endpoint = "https://api.change2pro.com/images/generations"
	DefaultImage2Model    = "gpt-image-2"
	DefaultImage2Size     = "1024x1024"
	MaxImage2Outputs      = 4
)

// Image2ToolConfig 汇总 Image2 图片生成工具的外部服务、存储和时间/ID 依赖。
type Image2ToolConfig struct {
	APIKey        string
	Endpoint      string
	HTTPClient    *http.Client
	Assets        Image2AssetStore
	AssetUploader Image2AssetUploader
	NewID         func() string
	Now           func() time.Time
}

// Image2GenerateTool 是 Eino 可调用的图片生成工具，返回素材摘要而不是 UI 事件。
type Image2GenerateTool struct {
	cfg Image2ToolConfig
}

// Image2AssetStore 定义图片生成结果写入素材表所需能力。
type Image2AssetStore interface {
	Save(ctx context.Context, record asset.Asset) (asset.Asset, error)
}

// Image2AssetUploader 定义图片生成结果上传对象存储所需能力。
type Image2AssetUploader interface {
	Upload(ctx context.Context, input asset.UploadInput) (asset.UploadResult, error)
}

// Image2GenerateInput 是 Agent 调用图片生成工具时传入的业务参数。
type Image2GenerateInput struct {
	SessionID       string `json:"session_id,omitempty"`
	UserID          string `json:"user_id,omitempty"`
	TargetType      string `json:"target_type,omitempty"`
	TargetID        string `json:"target_id,omitempty"`
	FilenamePrefix  string `json:"filename_prefix,omitempty"`
	Prompt          string `json:"prompt"`
	Model           string `json:"model,omitempty"`
	N               int    `json:"n,omitempty"`
	Size            string `json:"size,omitempty"`
	SourceJobID     string `json:"source_job_id,omitempty"`
	OutputIndexBase int    `json:"output_index_base,omitempty"`
}

// Image2GenerateResult 是图片生成工具返回给 Agent 的紧凑业务结果。
type Image2GenerateResult struct {
	Model             string                 `json:"model"`
	Created           int64                  `json:"created"`
	Assets            []GeneratedAssetInfo   `json:"assets"`
	StoryboardUpdates []StoryboardUpdateHint `json:"storyboard_updates,omitempty"`
}

// Image2Image 是 provider 返回图片在内部流转时使用的中间结构。
type Image2Image struct {
	AssetID         string `json:"asset_id,omitempty"`
	B64JSON         string `json:"-"`
	DataURL         string `json:"-"`
	URL             string `json:"url,omitempty"`
	ProviderURL     string `json:"-"`
	RevisedPrompt   string `json:"revised_prompt,omitempty"`
	MediaType       string `json:"media_type,omitempty"`
	StorageProvider string `json:"storage_provider,omitempty"`
	Bucket          string `json:"bucket,omitempty"`
	ObjectKey       string `json:"object_key,omitempty"`
}

// Image2Usage 描述 Image2 provider 返回的 token 使用量。
type Image2Usage struct {
	InputTokens         int                 `json:"input_tokens,omitempty"`
	OutputTokens        int                 `json:"output_tokens,omitempty"`
	TotalTokens         int                 `json:"total_tokens,omitempty"`
	InputTokensDetails  *Image2UsageDetails `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *Image2UsageDetails `json:"output_tokens_details,omitempty"`
}

// Image2UsageDetails 描述 Image2 provider 按类型拆分的 token 使用量。
type Image2UsageDetails struct {
	TextTokens      int `json:"text_tokens,omitempty"`
	ImageTokens     int `json:"image_tokens,omitempty"`
	CachedTokens    int `json:"cached_tokens,omitempty"`
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// image2APIRequest 是发送给 Image2 provider 的原始请求体。
type image2APIRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	N      int    `json:"n"`
	Size   string `json:"size"`
}

// image2APIResponse 是 Image2 provider 原始响应的最小解析结构。
type image2APIResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		B64JSON       string `json:"b64_json"`
		URL           string `json:"url"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
	Usage *Image2Usage `json:"usage,omitempty"`
}

// NewImage2GenerateTool 创建图片生成工具，并补齐 endpoint、HTTP client、ID 和时间默认值。
func NewImage2GenerateTool(cfg Image2ToolConfig) Image2GenerateTool {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultImage2Endpoint
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 2 * time.Minute}
	}
	if cfg.NewID == nil {
		cfg.NewID = defaultImageAssetID
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return Image2GenerateTool{cfg: cfg}
}

// Info 返回 Eino 工具元信息和参数 schema，供 Agent 正确构造调用参数。
func (Image2GenerateTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: Image2GenerateToolKey,
		Desc: "Generate image assets with Image2 gpt-image-2. Provider payloads are never returned to the Agent; the result only contains compact asset and storyboard hints.",
		ParamsOneOf: schema.NewParamsOneOfByParams(toolInvocationEnvelopeParams(map[string]*schema.ParameterInfo{
			"user_id": {
				Type: schema.String,
				Desc: "Current user id, if available.",
			},
			"target_type": {
				Type: schema.String,
				Desc: "Storyboard target type for the asset, such as key_element or shot.",
			},
			"target_id": {
				Type: schema.String,
				Desc: "Storyboard target id for the asset.",
			},
			"prompt": {
				Type:     schema.String,
				Desc:     "Image generation prompt.",
				Required: true,
			},
			"model": {
				Type: schema.String,
				Desc: "Image model. Defaults to gpt-image-2.",
				Enum: []string{DefaultImage2Model},
			},
			"n": {
				Type: schema.Integer,
				Desc: "Number of images to generate. Defaults to 1.",
			},
			"size": {
				Type: schema.String,
				Desc: "Output image size. Defaults to 1024x1024.",
				Enum: []string{"1024x1024", "1024x1536", "1536x1024"},
			},
		})),
	}, nil
}

// InvokableRun 执行图片生成，必要时持久化资产，并返回无 UI 协议的工具结果。
func (t Image2GenerateTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	invocation, err := decodeImage2Invocation(argumentsInJSON)
	if err != nil {
		return "", err
	}
	payload := invocation.Payload
	if strings.TrimSpace(payload.SessionID) == "" {
		payload.SessionID = invocation.SessionID
	}
	input, err := normalizeImage2Input(payload)
	if err != nil {
		return "", err
	}
	if t.cfg.APIKey == "" {
		return "", fmt.Errorf("image2 api key is required")
	}

	apiResp, err := t.generate(ctx, input)
	if err != nil {
		return "", err
	}
	images := convertImage2Images(apiResp)
	if t.shouldPersistAssets(input) {
		images, err = t.persistImages(ctx, input, images)
		if err != nil {
			return "", err
		}
	}
	assets := image2GeneratedAssets(input, images)
	updates := generativeStoryboardUpdates(assets)
	result := ToolResultEnvelope[Image2GenerateResult]{
		Status:         ToolStatusOK,
		RequestID:      invocation.RequestID,
		IdempotencyKey: invocation.IdempotencyKey,
		ArtifactIDs:    generativeArtifactIDs(assets),
		Data: Image2GenerateResult{
			Model:             input.Model,
			Created:           apiResp.Created,
			Assets:            assets,
			StoryboardUpdates: updates,
		},
	}

	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal image2 result: %w", err)
	}
	return string(out), nil
}

// generate 调用 Image2 provider，并解析原始 API 响应。
func (t Image2GenerateTool) generate(ctx context.Context, input Image2GenerateInput) (*image2APIResponse, error) {
	body, err := json.Marshal(image2APIRequest{
		Model:  input.Model,
		Prompt: input.Prompt,
		N:      input.N,
		Size:   input.Size,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal image2 request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create image2 request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+t.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if input.SourceJobID != "" {
		req.Header.Set("Idempotency-Key", input.SourceJobID)
		req.Header.Set("X-Client-Request-Id", input.SourceJobID)
	}

	resp, err := t.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call image2 provider: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("image2 provider returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	var apiResp image2APIResponse
	if err := decodeLimitedProviderJSON(resp.Body, maxProviderJSONBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("decode image2 response: %w", err)
	}
	if err := validateImage2Response(apiResp); err != nil {
		return nil, err
	}
	return &apiResp, nil
}

// validateImage2Response 确保 provider 成功响应里至少有一项可消费的图片内容。
func validateImage2Response(apiResp image2APIResponse) error {
	if len(apiResp.Data) == 0 {
		return fmt.Errorf("image2 provider response did not include images")
	}
	for i, item := range apiResp.Data {
		if strings.TrimSpace(item.B64JSON) == "" && strings.TrimSpace(item.URL) == "" {
			return fmt.Errorf("image2 provider image %d did not include b64_json or URL", i)
		}
	}
	return nil
}

// decodeImage2Invocation 只接受标准工具 envelope，图片生成参数必须放入 payload。
func decodeImage2Invocation(argumentsInJSON string) (ToolInvocationEnvelope[Image2GenerateInput], error) {
	return decodeToolInvocationEnvelope(Image2GenerateToolKey, argumentsInJSON, func(payload Image2GenerateInput) bool {
		return strings.TrimSpace(payload.Prompt) != ""
	})
}

// normalizeImage2Input 清理图片生成输入并补齐模型、数量和尺寸默认值。
func normalizeImage2Input(input Image2GenerateInput) (Image2GenerateInput, error) {
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.UserID = strings.TrimSpace(input.UserID)
	input.TargetType = strings.TrimSpace(input.TargetType)
	input.TargetID = strings.TrimSpace(input.TargetID)
	input.FilenamePrefix = strings.TrimSpace(input.FilenamePrefix)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Model = strings.TrimSpace(input.Model)
	input.Size = strings.TrimSpace(input.Size)
	if input.Prompt == "" {
		return input, fmt.Errorf("prompt is required")
	}
	if input.Model == "" {
		input.Model = DefaultImage2Model
	}
	if input.Model != DefaultImage2Model {
		return input, fmt.Errorf("unsupported image2 model %q", input.Model)
	}
	if input.N == 0 {
		input.N = 1
	}
	if input.N < 1 || input.N > MaxImage2Outputs {
		return input, fmt.Errorf("n must be between 1 and %d", MaxImage2Outputs)
	}
	if input.Size == "" {
		input.Size = DefaultImage2Size
	}
	switch input.Size {
	case "1024x1024", "1024x1536", "1536x1024":
	default:
		return input, fmt.Errorf("unsupported image2 size %q", input.Size)
	}
	return input, nil
}

// shouldPersistAssets 判断本次生成结果是否具备入库和上传条件。
func (t Image2GenerateTool) shouldPersistAssets(input Image2GenerateInput) bool {
	return t.cfg.Assets != nil && t.cfg.AssetUploader != nil && input.SessionID != ""
}

// persistImages 下载/解码 provider 图片，上传对象存储并保存素材记录。
func (t Image2GenerateTool) persistImages(ctx context.Context, input Image2GenerateInput, images []Image2Image) ([]Image2Image, error) {
	out := make([]Image2Image, 0, len(images))
	for i, image := range images {
		raw, mediaType, err := t.imageContent(ctx, image)
		if err != nil {
			return nil, err
		}
		assetID := t.cfg.NewID()
		outputIndex := input.OutputIndexBase + i
		if input.SourceJobID != "" {
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", input.SourceJobID, outputIndex)))
			assetID = "image_" + hex.EncodeToString(sum[:12])
		}
		filename := imageAssetFilename(input.FilenamePrefix, i, mediaType)
		objectKey := asset.NewObjectKey(input.SessionID, assetID, filename)
		metadata := map[string]any{
			"provider":       "image2",
			"revised_prompt": image.RevisedPrompt,
		}
		uploadMetadata := map[string]string{
			"provider": "image2",
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
			return nil, fmt.Errorf("upload image2 asset %s: %w", assetID, err)
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
			OutputIndex:     outputIndex,
			Kind:            asset.KindImage,
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
			return nil, fmt.Errorf("save image2 asset %s: %w", assetID, err)
		}
		out = append(out, Image2Image{
			AssetID:         saved.ID,
			URL:             saved.URL,
			ProviderURL:     image.URL,
			RevisedPrompt:   image.RevisedPrompt,
			MediaType:       mediaType,
			StorageProvider: saved.StorageProvider,
			Bucket:          saved.Bucket,
			ObjectKey:       saved.ObjectKey,
		})
	}
	return out, nil
}

// imageContent 从 b64_json 或 URL 中读取图片字节和媒体类型。
func (t Image2GenerateTool) imageContent(ctx context.Context, image Image2Image) ([]byte, string, error) {
	if strings.TrimSpace(image.B64JSON) != "" {
		raw, err := decodeImageB64(image.B64JSON)
		if err != nil {
			return nil, "", err
		}
		return raw, imageMediaType("", image.MediaType), nil
	}
	if strings.TrimSpace(image.URL) == "" {
		return nil, "", fmt.Errorf("image2 response did not include image bytes or URL")
	}
	raw, contentType, err := downloadProviderObject(ctx, t.cfg.HTTPClient, image.URL, t.cfg.Endpoint, maxImageAssetBytes)
	if err != nil {
		return nil, "", fmt.Errorf("download image2 provider image: %w", err)
	}
	if len(raw) == 0 {
		return nil, "", fmt.Errorf("image2 provider returned empty image")
	}
	return raw, imageMediaType(contentType, image.MediaType), nil
}

// convertImage2Images 把 provider 响应转换成内部图片列表。
func convertImage2Images(apiResp *image2APIResponse) []Image2Image {
	images := make([]Image2Image, 0, len(apiResp.Data))
	for _, item := range apiResp.Data {
		mediaType := inferImageMediaType(item.B64JSON)
		images = append(images, Image2Image{
			B64JSON:       item.B64JSON,
			URL:           item.URL,
			RevisedPrompt: item.RevisedPrompt,
			MediaType:     mediaType,
		})
	}
	return images
}

// image2GeneratedAssets 把图片列表转换成 Agent 可消费的素材摘要。
func image2GeneratedAssets(input Image2GenerateInput, images []Image2Image) []GeneratedAssetInfo {
	out := make([]GeneratedAssetInfo, 0, len(images))
	field := generativeAssetField(asset.KindImage, input.TargetType)
	for _, image := range images {
		status := "generated_not_persisted"
		if strings.TrimSpace(image.AssetID) != "" {
			status = "generated"
		}
		info := GeneratedAssetInfo{
			AssetID:         strings.TrimSpace(image.AssetID),
			Kind:            asset.KindImage,
			URL:             safeGeneratedAssetURL(image),
			TargetType:      input.TargetType,
			TargetID:        input.TargetID,
			Field:           field,
			Status:          status,
			MediaType:       image.MediaType,
			StorageProvider: image.StorageProvider,
			Bucket:          image.Bucket,
			ObjectKey:       image.ObjectKey,
		}
		out = append(out, info)
	}
	return out
}

// safeGeneratedAssetURL 只在素材已持久化后向 Agent 暴露可用 URL。
func safeGeneratedAssetURL(image Image2Image) string {
	if strings.TrimSpace(image.AssetID) == "" {
		return ""
	}
	return strings.TrimSpace(image.URL)
}

// imageDataURL 把裸 base64 图片转换成 data URL。
func imageDataURL(mediaType string, b64 string) string {
	if b64 == "" || strings.HasPrefix(b64, "data:") {
		return b64
	}
	if mediaType == "" {
		mediaType = "image/png"
	}
	return "data:" + mediaType + ";base64," + b64
}

// inferImageMediaType 根据 base64 魔数粗略推断图片媒体类型。
func inferImageMediaType(b64 string) string {
	switch {
	case strings.HasPrefix(b64, "iVBOR"):
		return "image/png"
	case strings.HasPrefix(b64, "/9j/"):
		return "image/jpeg"
	case strings.HasPrefix(b64, "R0lGOD"):
		return "image/gif"
	case strings.HasPrefix(b64, "UklGR"):
		return "image/webp"
	default:
		return "image/png"
	}
}

// decodeImageB64 解码 provider 返回的 b64_json 或 data URL。
func decodeImageB64(b64 string) ([]byte, error) {
	b64 = strings.TrimSpace(b64)
	if comma := strings.Index(b64, ","); comma >= 0 {
		b64 = b64[comma+1:]
	}
	if int64(base64.StdEncoding.DecodedLen(len(b64))) > maxImageAssetBytes {
		return nil, fmt.Errorf("image2 b64_json exceeds %d bytes", maxImageAssetBytes)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode image2 b64_json: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("image2 provider returned empty image")
	}
	return raw, nil
}

// imageMediaType 在下载响应和 fallback 之间选择合法图片媒体类型。
func imageMediaType(downloaded string, fallback string) string {
	mediaType := strings.TrimSpace(downloaded)
	if semi := strings.Index(mediaType, ";"); semi >= 0 {
		mediaType = strings.TrimSpace(mediaType[:semi])
	}
	if !strings.HasPrefix(mediaType, "image/") {
		mediaType = strings.TrimSpace(fallback)
	}
	if mediaType == "" {
		return "image/png"
	}
	return mediaType
}

// imageAssetFilename 根据前缀、序号和媒体类型生成图片文件名。
func imageAssetFilename(prefix string, index int, mediaType string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "image2"
	}
	ext := "png"
	switch mediaType {
	case "image/jpeg":
		ext = "jpg"
	case "image/gif":
		ext = "gif"
	case "image/webp":
		ext = "webp"
	}
	return fmt.Sprintf("%s-%d.%s", prefix, index+1, ext)
}

// defaultImageAssetID 生成图片素材默认 ID，随机源失败时使用时间戳兜底。
func defaultImageAssetID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("asset-%d", time.Now().UnixNano())
}

var _ einotool.InvokableTool = Image2GenerateTool{}
