package tools

import (
	"bytes"
	"context"
	"crypto/rand"
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
)

type Image2ToolConfig struct {
	APIKey        string
	Endpoint      string
	HTTPClient    *http.Client
	Assets        Image2AssetStore
	AssetUploader Image2AssetUploader
	NewID         func() string
	Now           func() time.Time
}

type Image2GenerateTool struct {
	cfg Image2ToolConfig
}

type Image2AssetStore interface {
	Save(ctx context.Context, record asset.Asset) (asset.Asset, error)
}

type Image2AssetUploader interface {
	Upload(ctx context.Context, input asset.UploadInput) (asset.UploadResult, error)
}

type Image2GenerateInput struct {
	SessionID      string `json:"session_id,omitempty"`
	UserID         string `json:"user_id,omitempty"`
	TargetType     string `json:"target_type,omitempty"`
	TargetID       string `json:"target_id,omitempty"`
	FilenamePrefix string `json:"filename_prefix,omitempty"`
	Prompt         string `json:"prompt"`
	Model          string `json:"model,omitempty"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
}

type Image2GenerateResult struct {
	Model             string                 `json:"model"`
	Created           int64                  `json:"created"`
	Assets            []GeneratedAssetInfo   `json:"assets"`
	StoryboardUpdates []StoryboardUpdateHint `json:"storyboard_updates,omitempty"`
	RenderEvents      []RenderEventHint      `json:"render_events,omitempty"`
}

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

type Image2Usage struct {
	InputTokens         int                 `json:"input_tokens,omitempty"`
	OutputTokens        int                 `json:"output_tokens,omitempty"`
	TotalTokens         int                 `json:"total_tokens,omitempty"`
	InputTokensDetails  *Image2UsageDetails `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *Image2UsageDetails `json:"output_tokens_details,omitempty"`
}

type Image2UsageDetails struct {
	TextTokens      int `json:"text_tokens,omitempty"`
	ImageTokens     int `json:"image_tokens,omitempty"`
	CachedTokens    int `json:"cached_tokens,omitempty"`
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

type image2APIRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	N      int    `json:"n"`
	Size   string `json:"size"`
}

type image2APIResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		B64JSON       string `json:"b64_json"`
		URL           string `json:"url"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
	Usage *Image2Usage `json:"usage,omitempty"`
}

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

func (Image2GenerateTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: Image2GenerateToolKey,
		Desc: "Generate image assets with Image2 gpt-image-2. Provider payloads are never returned to the Agent; the result only contains compact asset, storyboard, and render hints.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"session_id": {
				Type: schema.String,
				Desc: "Current AIGC session id. Required when generated images should be persisted as assets.",
			},
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
		}),
	}, nil
}

func (t Image2GenerateTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	invocation, err := decodeImage2Invocation(argumentsInJSON)
	if err != nil {
		return "", err
	}
	input, err := normalizeImage2Input(invocation.Payload)
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
			RenderEvents:      generativeRenderEvents(assets, updates),
		},
	}

	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal image2 result: %w", err)
	}
	return string(out), nil
}

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
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode image2 response: %w", err)
	}
	return &apiResp, nil
}

func decodeImage2Invocation(argumentsInJSON string) (ToolInvocationEnvelope[Image2GenerateInput], error) {
	var enveloped ToolInvocationEnvelope[Image2GenerateInput]
	if err := json.Unmarshal([]byte(argumentsInJSON), &enveloped); err == nil && enveloped.Payload.Prompt != "" {
		return enveloped, nil
	}

	var direct Image2GenerateInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &direct); err != nil {
		return ToolInvocationEnvelope[Image2GenerateInput]{}, fmt.Errorf("decode image2 input: %w", err)
	}
	return ToolInvocationEnvelope[Image2GenerateInput]{
		Payload: direct,
	}, nil
}

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
	if input.N == 0 {
		input.N = 1
	}
	if input.N < 0 {
		return input, fmt.Errorf("n must be greater than zero")
	}
	if input.Size == "" {
		input.Size = DefaultImage2Size
	}
	return input, nil
}

func (t Image2GenerateTool) shouldPersistAssets(input Image2GenerateInput) bool {
	return t.cfg.Assets != nil && t.cfg.AssetUploader != nil && input.SessionID != ""
}

func (t Image2GenerateTool) persistImages(ctx context.Context, input Image2GenerateInput, images []Image2Image) ([]Image2Image, error) {
	out := make([]Image2Image, 0, len(images))
	for i, image := range images {
		raw, mediaType, err := t.imageContent(ctx, image)
		if err != nil {
			return nil, err
		}
		assetID := t.cfg.NewID()
		filename := imageAssetFilename(input.FilenamePrefix, i, mediaType)
		objectKey := asset.NewObjectKey(input.SessionID, assetID, filename)
		metadata := map[string]any{
			"provider":       "image2",
			"provider_url":   image.URL,
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(image.URL), nil)
	if err != nil {
		return nil, "", fmt.Errorf("create image2 image download request: %w", err)
	}
	resp, err := t.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download image2 provider image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, "", fmt.Errorf("image2 provider image returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read image2 provider image: %w", err)
	}
	if len(raw) == 0 {
		return nil, "", fmt.Errorf("image2 provider returned empty image")
	}
	return raw, imageMediaType(resp.Header.Get("Content-Type"), image.MediaType), nil
}

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

func safeGeneratedAssetURL(image Image2Image) string {
	if strings.TrimSpace(image.AssetID) == "" {
		return ""
	}
	return strings.TrimSpace(image.URL)
}

func imageDataURL(mediaType string, b64 string) string {
	if b64 == "" || strings.HasPrefix(b64, "data:") {
		return b64
	}
	if mediaType == "" {
		mediaType = "image/png"
	}
	return "data:" + mediaType + ";base64," + b64
}

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

func decodeImageB64(b64 string) ([]byte, error) {
	b64 = strings.TrimSpace(b64)
	if comma := strings.Index(b64, ","); comma >= 0 {
		b64 = b64[comma+1:]
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

func defaultImageAssetID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("asset-%d", time.Now().UnixNano())
}

var _ einotool.InvokableTool = Image2GenerateTool{}
