package chatmodelagent

import (
	"context"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

const (
	// WritePromptsName 是 local-only 单 Tool 主 Agent 的稳定名称。
	WritePromptsName = "dora_write_prompts_preview_agent"
	// WritePromptsDescription 描述本 Profile 唯一可执行的 Prompt 写作能力。
	WritePromptsDescription = "Dora 本地 Prompt 写作开发预览 Agent，只执行 write_prompts 并直接返回 Tool Result。"
)

// NewWritePrompts 创建本 Profile 唯一 ChatModelAgent。
// Registry 必须恰好一个 Tool；ReturnDirectly 保证 Tool Result 后不发生第二次 Router 调用。
func NewWritePrompts(ctx context.Context, router model.BaseChatModel, tools []einotool.BaseTool) (adk.Agent, error) {
	if router == nil || len(tools) != 1 || tools[0] == nil {
		return nil, fmt.Errorf("create write prompts ChatModelAgent: exact model and Tool Registry are required")
	}
	info, err := tools[0].Info(ctx)
	if err != nil || writeprompts.ValidateToolInfo(info) != nil {
		return nil, fmt.Errorf("create write prompts ChatModelAgent: exact write_prompts tool is required")
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        WritePromptsName,
		Description: WritePromptsDescription,
		Instruction: "你是 Dora 本地 Prompt 写作开发预览 Router。只能逐字复制当前结构化 Intent，调用一次 write_prompts；禁止自由回答、改写参数、填写资源字段、调用其他 Tool 或在 Tool Result 后继续推理。",
		Model:       router,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools, ExecuteSequentially: true},
			ReturnDirectly:  map[string]bool{writeprompts.ToolKey: true},
		},
		MaxIterations: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("create write prompts ChatModelAgent: %w", err)
	}
	return agent, nil
}
