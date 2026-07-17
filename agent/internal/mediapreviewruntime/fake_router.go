package mediapreviewruntime

import (
	"context"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// FakeRouter 是媒体本地 Profile 的确定性单 ToolCall 模型；它不生成自然语言。
type FakeRouter struct {
	toolKey string
	tools   []*schema.ToolInfo
}

// NewFakeRouter 创建指定媒体 Tool 的未绑定 Router。
func NewFakeRouter(toolKey string) (*FakeRouter, error) {
	if toolKey != mediapreview.GenerateMediaToolKey && toolKey != mediapreview.AssembleOutputToolKey {
		return nil, fmt.Errorf("create media preview router: unsupported tool")
	}
	return &FakeRouter{toolKey: toolKey}, nil
}

// WithTools 返回只绑定构造期唯一 Tool 的不可变副本。
func (m *FakeRouter) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if m == nil || len(tools) != 1 || tools[0] == nil || tools[0].Name != m.toolKey {
		return nil, fmt.Errorf("bind media preview router: exact tool is required")
	}
	return &FakeRouter{toolKey: m.toolKey, tools: append([]*schema.ToolInfo(nil), tools...)}, nil
}

// Generate 逐字复制已由 ingress canonical 化的 Intent，产生一个稳定 Tool Call。
func (m *FakeRouter) Generate(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tools := model.GetCommonOptions(&model.Options{Tools: append([]*schema.ToolInfo(nil), m.tools...)}, options...).Tools
	if len(tools) != 1 || tools[0] == nil || tools[0].Name != m.toolKey {
		return nil, fmt.Errorf("run media preview router: exact tool is required")
	}
	trusted, routeOK := mediaRouteContext(ctx, m.toolKey)
	if !routeOK || trusted.ToolCallID == "" {
		return nil, fmt.Errorf("run media preview router: trusted context is missing")
	}
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message == nil {
			continue
		}
		if message.Role == schema.Tool {
			return nil, fmt.Errorf("run media preview router: post-tool prose is forbidden")
		}
		if message.Role == schema.User {
			if _, _, err := canonicalIntent(m.toolKey, []byte(message.Content)); err != nil {
				return nil, fmt.Errorf("run media preview router: invalid canonical intent")
			}
			return schema.AssistantMessage("", []schema.ToolCall{{
				ID: trusted.ToolCallID, Type: "function",
				Function: schema.FunctionCall{Name: m.toolKey, Arguments: message.Content},
			}}), nil
		}
	}
	return nil, fmt.Errorf("run media preview router: user message is missing")
}

// Stream 返回与 Generate 相同的单块经典 Message。
func (m *FakeRouter) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func mediaRouteContext(ctx context.Context, toolKey string) (turncontext.MediaPreview, bool) {
	switch toolKey {
	case mediapreview.GenerateMediaToolKey:
		return turncontext.GenerateMediaPreviewFrom(ctx)
	case mediapreview.AssembleOutputToolKey:
		return turncontext.AssembleOutputPreviewFrom(ctx)
	default:
		return turncontext.MediaPreview{}, false
	}
}

var _ model.ToolCallingChatModel = (*FakeRouter)(nil)
