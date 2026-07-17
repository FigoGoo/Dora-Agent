package chatmodelagent

import (
	"context"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

const (
	// AnalyzeMaterialsName 是 local-only 单 Tool 主 Agent 的稳定名称。
	AnalyzeMaterialsName = "dora_analyze_materials_preview_agent"
	// AnalyzeMaterialsDescription 描述本 Profile 的唯一可执行能力。
	AnalyzeMaterialsDescription = "Dora 本地素材分析开发预览 Agent，只执行 analyze_materials 并直接返回 Tool Result。"
)

// NewAnalyzeMaterials 创建本 Profile 唯一 ChatModelAgent。
// Registry 必须恰好一个 Tool，ReturnDirectly 保证 Tool Result 后没有第二次 Router Model 调用。
func NewAnalyzeMaterials(ctx context.Context, router model.BaseChatModel, tools []einotool.BaseTool) (adk.Agent, error) {
	if router == nil || len(tools) != 1 || tools[0] == nil {
		return nil, fmt.Errorf("create analyze materials ChatModelAgent: exact model and Tool Registry are required")
	}
	info, err := tools[0].Info(ctx)
	if err != nil || info == nil || info.Name != analyzematerials.ToolKey {
		return nil, fmt.Errorf("create analyze materials ChatModelAgent: exact analyze_materials tool is required")
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        AnalyzeMaterialsName,
		Description: AnalyzeMaterialsDescription,
		Instruction: "你是 Dora 本地素材分析开发预览 Router。只能逐字复制当前结构化 Intent，调用一次 analyze_materials；禁止自由回答、改写参数、调用其他 Tool 或在 Tool Result 后继续推理。",
		Model:       router,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools, ExecuteSequentially: true},
			ReturnDirectly:  map[string]bool{analyzematerials.ToolKey: true},
		},
		MaxIterations: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("create analyze materials ChatModelAgent: %w", err)
	}
	return agent, nil
}
