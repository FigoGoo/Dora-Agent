package chatmodel

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/FigoGoo/Dora-Agent/agent/internal/usermessageruntime"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// UserMessageFake 是方案 A 的本地确定性 BaseChatModel。
// 它没有 ToolCallingChatModel 能力，只返回与可信 Turn Context 绑定的固定 Direct Response JSON。
type UserMessageFake struct{}

// NewUserMessageFake 创建无可变配置、无外部调用的本地模型。
func NewUserMessageFake() *UserMessageFake { return &UserMessageFake{} }

// Generate 验证空 Tool Registry 与单条用户输入后生成固定 Card；用户正文不会进入输出。
func (m *UserMessageFake) Generate(
	ctx context.Context,
	messages []*schema.Message,
	options ...model.Option,
) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	common := model.GetCommonOptions(&model.Options{}, options...)
	if len(common.Tools) != 0 {
		return nil, fmt.Errorf("run user message fake model: executable tool registry must be empty")
	}
	trusted, ok := turncontext.UserMessageRuntimeFrom(ctx)
	if !ok || trusted.Profile != usermessageruntime.Profile || trusted.Owner == "" || trusted.FenceToken < 1 ||
		trusted.RunID == "" || trusted.ModelCallID == "" || trusted.OutputID == "" ||
		trusted.Context.SchemaVersion != turncontext.UserMessageTurnContextSchemaVersion ||
		trusted.Context.ToolRegistryRef != usermessageruntime.EmptyToolRegistryRef ||
		trusted.Context.ModelRouteRef != usermessageruntime.LocalFakeModelRouteRef {
		return nil, fmt.Errorf("run user message fake model: trusted turn context is invalid")
	}
	if len(messages) != 2 || messages[0] == nil || messages[0].Role != schema.System || messages[0].Content == "" ||
		messages[1] == nil || messages[1].Role != schema.User || messages[1].Content == "" ||
		len(messages[1].Content) > usermessageruntime.MaxMessagePlaintextBytes {
		return nil, fmt.Errorf("run user message fake model: exact system/user input is required")
	}
	card := usermessageruntime.DirectResponseCard{
		SchemaVersion: usermessageruntime.DirectResponseCardSchemaVersion,
		TurnID:        trusted.Context.TurnID, RunID: trusted.RunID, InputID: trusted.Context.InputID,
		Status: "completed", MessageCode: usermessageruntime.DirectResponseMessageCode,
		Summary:          usermessageruntime.DirectResponseSummary,
		AvailableActions: []string{usermessageruntime.DirectResponseActionOpenToolbox},
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return nil, fmt.Errorf("run user message fake model: encode direct response: %w", err)
	}
	return schema.AssistantMessage(string(encoded), nil), nil
}

// Stream 暴露单块确定性输出；生产 Runner 默认使用非流式路径，但仍完整实现 BaseChatModel。
func (m *UserMessageFake) Stream(
	ctx context.Context,
	messages []*schema.Message,
	options ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

var _ model.BaseChatModel = (*UserMessageFake)(nil)
