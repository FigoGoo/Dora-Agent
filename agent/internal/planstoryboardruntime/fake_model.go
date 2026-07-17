package planstoryboardruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// FakeRouter 是本地 Profile 唯一允许的确定性 Tool Router。
type FakeRouter struct{ tools []*schema.ToolInfo }

// NewFakeRouter 创建未绑定能力的本地 Router。
func NewFakeRouter() *FakeRouter { return &FakeRouter{} }

// WithTools 返回恰好绑定 plan_storyboard 的不可变副本。
func (*FakeRouter) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if err := validateRouterTools(tools); err != nil {
		return nil, err
	}
	canonical, err := planstoryboard.CanonicalToolInfo(context.Background())
	if err != nil {
		return nil, fmt.Errorf("bind plan storyboard fake router: %w", err)
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
		return nil, fmt.Errorf("run plan storyboard fake router: %w", err)
	}
	trusted, ok := turncontext.PlanStoryboardRuntimeFrom(ctx)
	if !ok || trusted.Context.Profile != Profile || trusted.Context.ToolCallID == "" || trusted.IntentJSON == "" {
		return nil, fmt.Errorf("run plan storyboard fake router: trusted runtime context is invalid")
	}
	var user *schema.Message
	for _, message := range messages {
		if message == nil {
			continue
		}
		if message.Role == schema.Tool {
			return nil, fmt.Errorf("run plan storyboard fake router: ReturnDirectly forbids a second router call")
		}
		if message.Role == schema.User {
			user = message
		}
	}
	if user == nil || user.Content != trusted.IntentJSON || len(user.ToolCalls) != 0 {
		return nil, fmt.Errorf("run plan storyboard fake router: exact frozen Intent user message is required")
	}
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID: trusted.Context.ToolCallID, Type: "function",
		Function: schema.FunctionCall{Name: planstoryboard.ToolKey, Arguments: trusted.IntentJSON},
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
	if len(tools) != 1 || planstoryboard.ValidateToolInfo(tools[0]) != nil {
		return fmt.Errorf("exact plan_storyboard Tool Registry is required")
	}
	return nil
}

// FakePlanningModel 是 Graph 内部唯一候选生成的本地确定性模型。
type FakePlanningModel struct{}

// NewFakePlanningModel 创建不访问外部 Provider 的 Graph Planning ChatModel。
func NewFakePlanningModel() *FakePlanningModel { return &FakePlanningModel{} }

// Generate 只读取版本化 Prompt 的 Intent 与 CreationSpec 数据边界，输出一个严格局部键候选。
func (*FakePlanningModel) Generate(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(model.GetCommonOptions(&model.Options{}, options...).Tools) != 0 {
		return nil, fmt.Errorf("run plan storyboard fake planning model: tools are forbidden")
	}
	if len(messages) != 2 || messages[0] == nil || messages[0].Role != schema.System ||
		messages[1] == nil || messages[1].Role != schema.User {
		return nil, fmt.Errorf("run plan storyboard fake planning model: exact system/user prompt is required")
	}
	intentJSON, err := extractPromptValue(messages[1].Content, "\nintent_json=", "\nproject_json=")
	if err != nil {
		return nil, err
	}
	creationSpecJSON, err := extractPromptValue(messages[1].Content, "\ncreation_spec_json=", "\n使用 section_N")
	if err != nil {
		return nil, err
	}
	var intent planstoryboard.Intent
	if err := json.Unmarshal([]byte(intentJSON), &intent); err != nil || planstoryboard.ValidateIntent(intent) != nil {
		return nil, fmt.Errorf("run plan storyboard fake planning model: intent boundary is invalid")
	}
	var creationSpec planstoryboard.CreationSpecContent
	if err := json.Unmarshal([]byte(creationSpecJSON), &creationSpec); err != nil || planstoryboard.ValidateCreationSpecContent(creationSpec) != nil {
		return nil, fmt.Errorf("run plan storyboard fake planning model: CreationSpec boundary is invalid")
	}
	duration := 30
	if intent.TargetDurationSeconds != nil {
		duration = *intent.TargetDurationSeconds
	}
	candidate := planstoryboard.Candidate{
		SchemaVersion: planstoryboard.CandidateSchemaVersion,
		Title:         creationSpec.Title,
		Summary:       "依据当前 CreationSpec 生成的本地开发预览故事板。",
		Sections: []planstoryboard.Section{{
			Key: "section_1", Title: "主体", Objective: "落实当前 CreationSpec 的创作目标。",
		}},
		Elements: []planstoryboard.Element{{
			Key: "element_1", SectionKey: "section_1", Order: 1, ElementType: "scene",
			Title: creationSpec.Phases[0].Title, NarrativePurpose: creationSpec.Phases[0].Objective,
			DurationSeconds: duration, SourcePhaseKey: creationSpec.Phases[0].Key, DependencyKeys: []string{},
		}},
		// 统一 MVP 主链必须向 write_prompts 提供至少一个真实 exact target；
		// 这里仍只生成 Preview 局部键和需求语义，不生成最终 Prompt、Asset 或生产稳定 ID。
		Slots: []planstoryboard.Slot{{
			Key: "slot_1", ElementKey: "element_1", SlotType: "image",
			Purpose: "呈现当前创作规格的核心主视觉", Required: true,
		}},
	}
	encoded, err := json.Marshal(candidate)
	if err != nil {
		return nil, fmt.Errorf("run plan storyboard fake planning model: encode candidate: %w", err)
	}
	return schema.AssistantMessage(string(encoded), nil), nil
}

// Stream 返回单块严格候选响应。
func (planning *FakePlanningModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := planning.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func extractPromptValue(content string, start string, end string) (string, error) {
	startIndex := strings.Index(content, start)
	if startIndex < 0 {
		return "", fmt.Errorf("run plan storyboard fake planning model: prompt boundary %q is missing", start)
	}
	startIndex += len(start)
	endIndex := strings.Index(content[startIndex:], end)
	if endIndex < 0 {
		return "", fmt.Errorf("run plan storyboard fake planning model: prompt boundary %q is incomplete", end)
	}
	return content[startIndex : startIndex+endIndex], nil
}

var _ model.ToolCallingChatModel = (*FakeRouter)(nil)
var _ model.BaseChatModel = (*FakePlanningModel)(nil)
