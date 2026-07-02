package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
)

// ChatCompletionClient 抽象 OpenAI 兼容的 chat completions 调用，便于测试与换供应商。
type ChatCompletionClient interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// LLMRouter 按 docs/02-M1 实现 ChatModel Router：
// LLM 结构化输出 -> JSON 解析 -> Router Guard 确定性校验 -> 契约校验；
// 任一步失败降级到 mock 路由（Fallback），保证路由永不失败。
type LLMRouter struct {
	Client   ChatCompletionClient
	Fallback Router
	Timeout  time.Duration
	Retries  int
}

func NewLLMRouter(client ChatCompletionClient) LLMRouter {
	return LLMRouter{
		Client:   client,
		Fallback: NewChatModelRouter(),
		Timeout:  15 * time.Second,
		Retries:  1,
	}
}

type llmDecisionEnvelope struct {
	Decision              string                            `json:"decision"`
	SkillID               string                            `json:"skill_id"`
	ListingID             string                            `json:"listing_id"`
	SkillSource           string                            `json:"skill_source"`
	Confidence            float64                           `json:"confidence"`
	ReasonCode            string                            `json:"reason_code"`
	ExtractedParams       map[string]any                    `json:"extracted_params"`
	MissingFields         []string                          `json:"missing_fields"`
	CandidateSkills       []foundation.CandidateSkill       `json:"candidate_skills"`
	MarketplaceCandidates []foundation.MarketplaceCandidate `json:"marketplace_candidates"`
	ClarifyQuestion       string                            `json:"clarify_question"`
	SuggestedQuestions    []string                          `json:"suggested_questions"`
}

func (r LLMRouter) Route(ctx context.Context, input Input) (Result, error) {
	if r.Client == nil {
		return r.fallback(ctx, input)
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	userPrompt, err := BuildRouterUserPrompt(input)
	if err != nil {
		return r.fallback(ctx, input)
	}
	attempts := r.Retries + 1
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		raw, callErr := r.Client.Complete(callCtx, routerSystemPrompt, userPrompt)
		cancel()
		if callErr != nil {
			lastErr = callErr
			continue
		}
		envelope, parseErr := parseLLMDecision(raw)
		if parseErr != nil {
			lastErr = parseErr
			continue
		}
		decision := envelope.toDecision()
		decision = ApplyRouterGuard(decision, input)
		if validateErr := foundation.ValidateRouterDecision(decision); validateErr != nil {
			lastErr = validateErr
			continue
		}
		result := Result{
			Decision:           decision,
			ClarifyQuestion:    strings.TrimSpace(envelope.ClarifyQuestion),
			SuggestedQuestions: limitStrings(envelope.SuggestedQuestions, 3),
			Source:             "llm",
		}
		if result.ClarifyQuestion == "" {
			result.ClarifyQuestion = humanClarifyQuestion(decision)
		}
		return result, nil
	}
	_ = lastErr // Router 失败必须降级（docs/02 §13），错误交由降级结果的 reason 观测。
	return r.fallback(ctx, input)
}

func (r LLMRouter) fallback(ctx context.Context, input Input) (Result, error) {
	fallback := r.Fallback
	if fallback == nil {
		fallback = NewChatModelRouter()
	}
	return fallback.Route(ctx, input)
}

func (e llmDecisionEnvelope) toDecision() foundation.RouterDecision {
	decision := foundation.RouterDecision{
		SchemaVersion:         foundation.SchemaVersionRouterDecision,
		Decision:              strings.TrimSpace(e.Decision),
		Confidence:            e.Confidence,
		ReasonCode:            strings.TrimSpace(e.ReasonCode),
		SafeToExecute:         false,
		ExtractedParams:       e.ExtractedParams,
		MissingFields:         e.MissingFields,
		CandidateSkills:       e.CandidateSkills,
		MarketplaceCandidates: e.MarketplaceCandidates,
	}
	if skillID := strings.TrimSpace(e.SkillID); skillID != "" && skillID != "null" {
		decision.SkillID = &skillID
	}
	if listingID := strings.TrimSpace(e.ListingID); listingID != "" && listingID != "null" {
		decision.ListingID = &listingID
	}
	if source := strings.TrimSpace(e.SkillSource); source != "" && source != "null" {
		decision.SkillSource = &source
	}
	return decision
}

// parseLLMDecision 容忍模型输出被 Markdown 代码块包裹或前后带说明文本。
func parseLLMDecision(raw string) (llmDecisionEnvelope, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return llmDecisionEnvelope{}, errors.New("router llm output is empty")
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return llmDecisionEnvelope{}, errors.New("router llm output has no json object")
	}
	var envelope llmDecisionEnvelope
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &envelope); err != nil {
		return llmDecisionEnvelope{}, fmt.Errorf("router llm output json invalid: %w", err)
	}
	if strings.TrimSpace(envelope.Decision) == "" {
		return llmDecisionEnvelope{}, errors.New("router llm output missing decision")
	}
	return envelope, nil
}

// DeepSeekChatClient 复用 DeepSeek OpenAI 兼容 chat completions 接口做路由推理。
type DeepSeekChatClient struct {
	BaseURL    string
	APIKey     string
	Model      string
	MaxTokens  int
	HTTPClient *http.Client
}

func (c DeepSeekChatClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	apiKey := strings.TrimSpace(c.APIKey)
	if apiKey == "" {
		return "", errors.New("router llm api key is required")
	}
	model := strings.TrimSpace(c.Model)
	if model == "" {
		return "", errors.New("router llm model is required")
	}
	baseURL := strings.TrimSpace(c.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	endpoint, err := url.JoinPath(baseURL, "chat/completions")
	if err != nil {
		return "", err
	}
	requestBody := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature":     0.2,
		"stream":          false,
		"response_format": map[string]string{"type": "json_object"},
	}
	if c.MaxTokens > 0 {
		requestBody["max_tokens"] = c.MaxTokens
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("router llm completion failed: status=%d", response.StatusCode)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &completion); err != nil {
		return "", err
	}
	if len(completion.Choices) == 0 {
		return "", errors.New("router llm completion has no choices")
	}
	return completion.Choices[0].Message.Content, nil
}
