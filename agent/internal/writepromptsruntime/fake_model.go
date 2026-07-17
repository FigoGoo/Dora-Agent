package writepromptsruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// FakeRouter 是本地 Profile 唯一允许的确定性 Tool Router。
type FakeRouter struct{ tools []*schema.ToolInfo }

// NewFakeRouter 创建未绑定能力的本地 Router。
func NewFakeRouter() *FakeRouter { return &FakeRouter{} }

// WithTools 返回恰好绑定 write_prompts 的不可变副本。
func (*FakeRouter) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if err := validateRouterTools(tools); err != nil {
		return nil, err
	}
	canonical, err := writeprompts.CanonicalToolInfo(context.Background())
	if err != nil {
		return nil, fmt.Errorf("bind write prompts fake router: %w", err)
	}
	return &FakeRouter{tools: []*schema.ToolInfo{canonical}}, nil
}

// Generate 逐字复制可信 canonical Intent，生成唯一稳定 ToolCall；Tool Result 后调用失败关闭。
func (router *FakeRouter) Generate(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tools := model.GetCommonOptions(&model.Options{Tools: append([]*schema.ToolInfo(nil), router.tools...)}, options...).Tools
	if err := validateRouterTools(tools); err != nil {
		return nil, fmt.Errorf("run write prompts fake router: %w", err)
	}
	trusted, ok := turncontext.WritePromptsRuntimeFrom(ctx)
	if !ok || trusted.Context.Profile != Profile || trusted.Context.ToolCallID == "" || trusted.IntentJSON == "" {
		return nil, fmt.Errorf("run write prompts fake router: trusted runtime context is invalid")
	}
	var user *schema.Message
	for _, message := range messages {
		if message == nil {
			continue
		}
		if message.Role == schema.Tool {
			return nil, fmt.Errorf("run write prompts fake router: ReturnDirectly forbids a second router call")
		}
		if message.Role == schema.User {
			user = message
		}
	}
	if user == nil || user.Content != trusted.IntentJSON || len(user.ToolCalls) != 0 {
		return nil, fmt.Errorf("run write prompts fake router: exact frozen Intent user message is required")
	}
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID: trusted.Context.ToolCallID, Type: "function",
		Function: schema.FunctionCall{Name: writeprompts.ToolKey, Arguments: trusted.IntentJSON},
	}}), nil
}

// Stream 返回单块确定性 ToolCall。
func (router *FakeRouter) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := router.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func validateRouterTools(tools []*schema.ToolInfo) error {
	if len(tools) != 1 || writeprompts.ValidateToolInfo(tools[0]) != nil {
		return fmt.Errorf("exact write_prompts Tool Registry is required")
	}
	return nil
}

// FakePromptModel 是 Graph 内部唯一候选生成的本地确定性模型。
type FakePromptModel struct{}

// NewFakePromptModel 创建不访问外部 Provider 的 Graph Prompt ChatModel。
func NewFakePromptModel() *FakePromptModel { return &FakePromptModel{} }

// Generate 只读取版本化 Prompt 的 exact target 数据边界，输出覆盖全部目标的严格候选。
func (*FakePromptModel) Generate(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(model.GetCommonOptions(&model.Options{}, options...).Tools) != 0 {
		return nil, fmt.Errorf("run write prompts fake prompt model: tools are forbidden")
	}
	if len(messages) != 2 || messages[0] == nil || messages[0].Role != schema.System ||
		messages[1] == nil || messages[1].Role != schema.User {
		return nil, fmt.Errorf("run write prompts fake prompt model: exact system/user prompt is required")
	}
	targetsJSON, err := extractPromptValue(messages[1].Content, "\nexact_targets_json=", "\n为每个 exact target")
	if err != nil {
		return nil, err
	}
	var targets []writeprompts.PromptTarget
	if err := json.Unmarshal([]byte(targetsJSON), &targets); err != nil || len(targets) == 0 || len(targets) > 96 {
		return nil, fmt.Errorf("run write prompts fake prompt model: exact target boundary is invalid")
	}
	candidate := writeprompts.Candidate{
		SchemaVersion: writeprompts.CandidateSchemaVersion,
		Prompts:       make([]writeprompts.CandidatePrompt, len(targets)),
	}
	for index, target := range targets {
		if target.TargetLocalKey == "" || target.Purpose == "" {
			return nil, fmt.Errorf("run write prompts fake prompt model: target is invalid")
		}
		candidate.Prompts[index] = writeprompts.CandidatePrompt{
			TargetLocalKey:      target.TargetLocalKey,
			PositivePrompt:      fmt.Sprintf("%s：%s，突出%s。", target.ElementTitle, target.Purpose, target.NarrativePurpose),
			NegativeConstraints: []string{},
		}
	}
	encoded, err := json.Marshal(candidate)
	if err != nil {
		return nil, fmt.Errorf("run write prompts fake prompt model: encode candidate: %w", err)
	}
	return schema.AssistantMessage(string(encoded), nil), nil
}

// Stream 返回单块严格候选响应。
func (prompt *FakePromptModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := prompt.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func extractPromptValue(content string, start string, end string) (string, error) {
	startIndex := strings.Index(content, start)
	if startIndex < 0 {
		return "", fmt.Errorf("run write prompts fake prompt model: prompt boundary %q is missing", start)
	}
	startIndex += len(start)
	endIndex := strings.Index(content[startIndex:], end)
	if endIndex < 0 {
		return "", fmt.Errorf("run write prompts fake prompt model: prompt boundary %q is incomplete", end)
	}
	return content[startIndex : startIndex+endIndex], nil
}

var _ model.ToolCallingChatModel = (*FakeRouter)(nil)
var _ model.BaseChatModel = (*FakePromptModel)(nil)
