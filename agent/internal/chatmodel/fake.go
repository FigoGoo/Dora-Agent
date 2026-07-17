// Package chatmodel 创建 V1 可选 Fake/DeepSeek ChatModel，并保持 classic schema.Message 路径。
package chatmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// FakeRouter 是 CI/本地预览使用的确定性主 Agent 路由模型。
// 它只会选择不可变绑定或单次调用注入的 plan_creation_spec，不直接执行 Graph 或 Business Command。
type FakeRouter struct {
	tools []*schema.ToolInfo
}

// NewFakeRouter 创建未绑定 Tool 的基础模型；ADK 可在每次模型调用时通过 model.WithTools 注入稳定 Registry。
func NewFakeRouter() *FakeRouter { return &FakeRouter{} }

// WithTools 返回不可变 Tool 绑定副本，并拒绝 plan_creation_spec 之外的动态能力。
func (m *FakeRouter) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if len(tools) != 1 || tools[0] == nil || tools[0].Name != plancreationspec.ToolKey {
		return nil, fmt.Errorf("bind fake router tools: exact plan_creation_spec tool is required")
	}
	return &FakeRouter{tools: append([]*schema.ToolInfo(nil), tools...)}, nil
}

// Generate 首轮生成稳定 ToolCall，收到配对 Tool Result 后生成最小 Assistant 完成消息。
func (m *FakeRouter) Generate(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tools := model.GetCommonOptions(&model.Options{
		Tools: append([]*schema.ToolInfo(nil), m.tools...),
	}, options...).Tools
	if err := validateFakeRouterTools(tools); err != nil {
		return nil, fmt.Errorf("run fake router: %w", err)
	}
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message == nil {
			continue
		}
		if message.Role == schema.Tool {
			return schema.AssistantMessage("CreationSpec Preview 已处理，请以持久化工作台事件为准。", nil), nil
		}
		if message.Role == schema.User {
			trusted, ok := turncontext.PreviewFrom(ctx)
			if !ok || trusted.ToolCallID == "" {
				return nil, fmt.Errorf("run fake router: trusted turn context is missing")
			}
			// Router 只复制模型可控 Intent JSON 到 Tool arguments；身份、Project 版本和 Fence 均由 Context 注入。
			return schema.AssistantMessage("", []schema.ToolCall{{
				ID: trusted.ToolCallID, Type: "function",
				Function: schema.FunctionCall{Name: plancreationspec.ToolKey, Arguments: message.Content},
			}}), nil
		}
	}
	return nil, fmt.Errorf("run fake router: user message is missing")
}

// validateFakeRouterTools 对不可变绑定和 ADK 单次调用注入执行同一份精确 Tool Registry 校验。
func validateFakeRouterTools(tools []*schema.ToolInfo) error {
	if len(tools) != 1 || tools[0] == nil || tools[0].Name != plancreationspec.ToolKey {
		return fmt.Errorf("exact plan_creation_spec tool is required")
	}
	return nil
}

// Stream 以单块 StreamReader 暴露与 Generate 完全相同的确定性结果。
func (m *FakeRouter) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

// FakeProposal 是 Graph call_model Node 使用的确定性候选模型；它仍通过真实 Eino ChatModel Node 执行。
type FakeProposal struct{}

// NewFakeProposal 创建不支持 Tool 绑定的最小 BaseChatModel。
func NewFakeProposal() *FakeProposal { return &FakeProposal{} }

// Generate 从版本化 Prompt 的隔离 intent_json 区段读取 Intent，并生成仍需独立 Validator 的 Proposal。
func (m *FakeProposal) Generate(ctx context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(messages) == 0 || messages[len(messages)-1] == nil {
		return nil, fmt.Errorf("run fake proposal model: prompt is missing")
	}
	content := messages[len(messages)-1].Content
	const startMarker = "\nintent_json="
	const endMarker = "\n输出 schema_version="
	start := strings.LastIndex(content, startMarker)
	end := -1
	if start >= 0 {
		bodyStart := start + len(startMarker)
		if relativeEnd := strings.Index(content[bodyStart:], endMarker); relativeEnd >= 0 {
			end = bodyStart + relativeEnd
		}
	}
	if start < 0 || end <= start+len(startMarker) {
		return nil, fmt.Errorf("run fake proposal model: prompt boundary is invalid")
	}
	intent, err := plancreationspec.DecodeIntent([]byte(content[start+len(startMarker) : end]))
	if err != nil {
		return nil, err
	}
	audience := ""
	if intent.Audience != nil {
		audience = *intent.Audience
	}
	proposal := plancreationspec.Proposal{
		SchemaVersion: ProposalSchemaVersion(),
		Title:         proposalTitle(intent.DeliverableType), Goal: intent.Goal,
		DeliverableType: intent.DeliverableType, Audience: audience,
		Phases: []plancreationspec.Phase{{
			Key: "phase_1", Title: "创作规划", Objective: "冻结目标、结构与交付边界", Output: "可执行创作规格",
		}},
		// Proposal 契约要求 constraints 即使为空也编码为 []；nil 会被编码为 null 并被严格 Validator 拒绝。
		Constraints:        append([]string{}, intent.Constraints...),
		AcceptanceCriteria: []string{"交付结果符合已冻结目标、类型和全部硬约束"},
	}
	encoded, err := json.Marshal(proposal)
	if err != nil {
		return nil, fmt.Errorf("run fake proposal model: encode proposal: %w", err)
	}
	return schema.AssistantMessage(string(encoded), nil), nil
}

// Stream 以单块 StreamReader 暴露 Fake Proposal，确保 Graph Stream 路径也能正确关闭 Reader。
func (m *FakeProposal) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

// ProposalSchemaVersion 隔离包间常量读取，便于 Fake 输出与 Graph Validator 共享唯一版本。
func ProposalSchemaVersion() string { return plancreationspec.ProposalSchemaVersion }

// proposalTitle 为 Fake 候选生成不超过 80 字符的确定性标题，不承担业务状态决策。
func proposalTitle(deliverableType string) string {
	switch deliverableType {
	case "video":
		return "视频创作规格"
	case "image_set":
		return "图片组创作规格"
	case "audio":
		return "音频创作规格"
	default:
		return "混合媒体创作规格"
	}
}

var _ model.ToolCallingChatModel = (*FakeRouter)(nil)
var _ model.BaseChatModel = (*FakeProposal)(nil)
