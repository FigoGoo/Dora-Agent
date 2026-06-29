package modeltool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	einoruntime "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/eino"
)

type Snapshot struct {
	ModelID            string
	ResourceType       string
	ProviderRuntimeRef string
	TimeoutMS          int32
}

type GenerationResult struct {
	Status        string
	Message       *einoruntime.Message
	ArtifactCount int
	Artifacts     []Artifact
	Partial       bool
}

type Adapter interface {
	Generate(ctx context.Context, snapshot Snapshot, prompt *einoruntime.Message) (GenerationResult, error)
}

type Artifact struct {
	ArtifactID      string
	ResourceType    string
	ElementType     string
	Name            string
	ContentType     string
	SizeBytes       int64
	Checksum        string
	MetadataSummary map[string]string
	ElementsSummary map[string]any
	OpenStream      func(context.Context) (io.ReadCloser, error)
}

func (a Artifact) Stream(ctx context.Context) (io.ReadCloser, error) {
	if a.OpenStream == nil {
		return nil, errors.New("artifact stream is not available")
	}
	return a.OpenStream(ctx)
}

type LocalAdapter struct{}

func (LocalAdapter) Generate(ctx context.Context, snapshot Snapshot, prompt *einoruntime.Message) (GenerationResult, error) {
	if strings.TrimSpace(snapshot.ModelID) == "" || strings.TrimSpace(snapshot.ResourceType) == "" {
		return GenerationResult{}, errors.New("model_id and resource_type are required")
	}
	if strings.TrimSpace(snapshot.ProviderRuntimeRef) == "" {
		return GenerationResult{}, errors.New("provider_runtime_ref is required")
	}
	if prompt == nil {
		return GenerationResult{}, errors.New("prompt message is required")
	}
	if snapshot.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(snapshot.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return GenerationResult{}, err
	}
	sum := sha256.Sum256([]byte(snapshot.ModelID + ":" + snapshot.ResourceType + ":" + prompt.Content))
	resourceType := strings.TrimSpace(snapshot.ResourceType)
	contentType := "application/octet-stream"
	elementType := "file_ref"
	switch resourceType {
	case "image":
		contentType = "image/png"
		elementType = "image_ref"
	case "audio", "music":
		contentType = "audio/mpeg"
		elementType = "audio_ref"
	case "video":
		contentType = "video/mp4"
		elementType = "video_ref"
	}
	body := []byte("dora-local-generated-artifact\n" + snapshot.ModelID + "\n" + snapshot.ResourceType + "\n" + prompt.Content)
	bodySum := sha256.Sum256(body)
	artifact := Artifact{
		ArtifactID:   "art_" + fmt.Sprintf("%x", sum[:8]),
		ResourceType: resourceType,
		ElementType:  elementType,
		Name:         "generated-" + resourceType,
		ContentType:  contentType,
		SizeBytes:    int64(len(body)),
		Checksum:     "sha256:" + fmt.Sprintf("%x", bodySum[:]),
		MetadataSummary: map[string]string{
			"model_id":        snapshot.ModelID,
			"adapter":         "local",
			"generation_mode": "test_adapter",
		},
		ElementsSummary: map[string]any{
			"count":                1,
			"primary_element_type": elementType,
		},
		OpenStream: func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		},
	}
	return GenerationResult{Status: "completed", Message: prompt, ArtifactCount: 1, Artifacts: []Artifact{artifact}}, nil
}

type DeepSeekAdapter struct {
	BaseURL    string
	APIKey     string
	Model      string
	MaxTokens  int
	HTTPClient *http.Client
}

func (a DeepSeekAdapter) Generate(ctx context.Context, snapshot Snapshot, prompt *einoruntime.Message) (GenerationResult, error) {
	if strings.TrimSpace(snapshot.ModelID) == "" || strings.TrimSpace(snapshot.ResourceType) == "" {
		return GenerationResult{}, errors.New("model_id and resource_type are required")
	}
	if strings.TrimSpace(snapshot.ProviderRuntimeRef) == "" {
		return GenerationResult{}, errors.New("provider_runtime_ref is required")
	}
	if prompt == nil {
		return GenerationResult{}, errors.New("prompt message is required")
	}
	apiKey := strings.TrimSpace(a.APIKey)
	if apiKey == "" {
		return GenerationResult{}, errors.New("deepseek api key is required")
	}
	model := strings.TrimSpace(a.Model)
	if model == "" {
		model = modelFromProviderRuntimeRef(snapshot.ProviderRuntimeRef)
	}
	if model == "" {
		return GenerationResult{}, errors.New("deepseek model is required")
	}
	baseURL := strings.TrimSpace(a.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	endpoint, err := chatCompletionsURL(baseURL)
	if err != nil {
		return GenerationResult{}, err
	}
	if snapshot.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(snapshot.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	requestBody := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are Dora-Agent runtime. Return concise, useful creative output for the user's request."},
			{"role": "user", "content": prompt.Content},
		},
		"stream": false,
	}
	if a.MaxTokens > 0 {
		requestBody["max_tokens"] = a.MaxTokens
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return GenerationResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return GenerationResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := a.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return GenerationResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return GenerationResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GenerationResult{}, fmt.Errorf("deepseek chat completions failed: status=%d body=%s", resp.StatusCode, trimForPreview(string(body), 240))
	}
	var completion chatCompletionResponse
	if err := json.Unmarshal(body, &completion); err != nil {
		return GenerationResult{}, err
	}
	output := strings.TrimSpace(completion.FirstContent())
	if output == "" {
		return GenerationResult{}, errors.New("deepseek response content is empty")
	}
	resourceType := strings.TrimSpace(snapshot.ResourceType)
	if resourceType == "" {
		resourceType = "text"
	}
	outputBytes, contentType, filename := artifactBodyForResource(resourceType, output)
	bodySum := sha256.Sum256(outputBytes)
	idSum := sha256.Sum256([]byte(snapshot.ModelID + ":" + snapshot.ProviderRuntimeRef + ":" + output))
	artifact := Artifact{
		ArtifactID:   "art_" + fmt.Sprintf("%x", idSum[:8]),
		ResourceType: resourceType,
		ElementType:  elementTypeForResource(resourceType),
		Name:         filename,
		ContentType:  contentType,
		SizeBytes:    int64(len(outputBytes)),
		Checksum:     "sha256:" + fmt.Sprintf("%x", bodySum[:]),
		MetadataSummary: map[string]string{
			"adapter":         "deepseek_chat_completions",
			"provider":        "deepseek",
			"model":           model,
			"response_id":     completion.ID,
			"latency_ms":      fmt.Sprintf("%d", time.Since(started).Milliseconds()),
			"finish_reason":   completion.FirstFinishReason(),
			"output_preview":  trimForPreview(output, 600),
			"generation_mode": "chat_completions",
		},
		ElementsSummary: map[string]any{
			"count":                1,
			"primary_element_type": elementTypeForResource(resourceType),
			"text_preview":         trimForPreview(output, 300),
		},
		OpenStream: func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(outputBytes)), nil
		},
	}
	return GenerationResult{Status: "completed", Message: einoruntime.AssistantMessage(output), ArtifactCount: 1, Artifacts: []Artifact{artifact}}, nil
}

type chatCompletionResponse struct {
	ID      string                 `json:"id"`
	Model   string                 `json:"model"`
	Choices []chatCompletionChoice `json:"choices"`
	Usage   map[string]any         `json:"usage"`
}

type chatCompletionChoice struct {
	Message      chatCompletionMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

type chatCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (r chatCompletionResponse) FirstContent() string {
	if len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].Message.Content
}

func (r chatCompletionResponse) FirstFinishReason() string {
	if len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].FinishReason
}

func chatCompletionsURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("openai compatible base url is invalid: %s", baseURL)
	}
	if strings.HasSuffix(parsed.Path, "/chat/completions") {
		return parsed.String(), nil
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/chat/completions"
	return parsed.String(), nil
}

func modelFromProviderRuntimeRef(ref string) string {
	parts := strings.Split(strings.TrimSpace(ref), ":")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func elementTypeForResource(resourceType string) string {
	switch resourceType {
	case "image":
		return "image_ref"
	case "audio", "music":
		return "audio_ref"
	case "video":
		return "video_ref"
	default:
		return "text_ref"
	}
}

func artifactBodyForResource(resourceType string, output string) ([]byte, string, string) {
	switch resourceType {
	case "image":
		return deepSeekPlaceholderPNG(), "image/png", "deepseek-v4-output.png"
	default:
		return []byte(output), "text/plain; charset=utf-8", "deepseek-v4-output.txt"
	}
}

func deepSeekPlaceholderPNG() []byte {
	encoded := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mPk+M9QDwADggGOSHzRgAAAAABJRU5ErkJggg=="
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	}
	return data
}

func trimForPreview(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
