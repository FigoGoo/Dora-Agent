package chatmodelagent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
)

const (
	// DirectResponseName 是 user_message.runtime.v2preview1 无 Tool Agent 的稳定 Trace 名称。
	DirectResponseName = "dora_user_message_direct_response_agent"
	// DirectResponseDescription 明确该 Agent 只返回安全受理卡，不执行任何 Graph Tool。
	DirectResponseDescription = "Dora 首轮用户消息安全响应 Agent，不具备任何可执行 Tool。"
)

// NewDirectResponse 创建 Development Preview 无 Tool ChatModelAgent。
// 构造器不接收 Tool 参数，从类型边界保证 Executable Tool Registry 恰好为空；
// MaxIterations 固定为 1，且不配置 ADK Retry/Failover，模型调用预算只有一次。
func NewDirectResponse(ctx context.Context, chatModel model.BaseChatModel) (adk.Agent, error) {
	if chatModel == nil {
		return nil, fmt.Errorf("create direct response ChatModelAgent: model is required")
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        DirectResponseName,
		Description: DirectResponseDescription,
		Instruction: "你是 Dora 首轮安全响应 Agent。只能返回约定的单个 Direct Response JSON Card；禁止调用或建议已经执行任何 Tool，禁止复制用户原文，禁止输出 JSON 之外的文本。",
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools: nil,
		}},
		MaxIterations: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("create direct response ChatModelAgent: %w", err)
	}
	return agent, nil
}
