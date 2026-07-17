package chatmodelagent

import (
	"context"
	"reflect"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type mvpAgentModel struct {
	bindCalls int
	toolNames []string
}

func (*mvpAgentModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage(`{"status":"ok"}`, nil), nil
}

func (*mvpAgentModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage(`{"status":"ok"}`, nil)}), nil
}

func (m *mvpAgentModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	m.bindCalls++
	m.toolNames = m.toolNames[:0]
	for _, info := range tools {
		if info != nil {
			m.toolNames = append(m.toolNames, info.Name)
		}
	}
	return m, nil
}

type mvpAgentTool struct{ name string }

func (t *mvpAgentTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.name,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"schema_version": {Type: schema.String},
		}),
	}, nil
}

func (*mvpAgentTool) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return `{}`, nil
}

// TestNewMVPAllToolsRequiresExactRegistry 验证统一 Profile 只构造一个稳定名称、四 Tool 的主 Agent。
func TestNewMVPAllToolsRequiresExactRegistry(t *testing.T) {
	ctx := context.Background()
	tools := []einotool.BaseTool{
		&mvpAgentTool{name: plancreationspec.ToolKey},
		&mvpAgentTool{name: analyzematerials.ToolKey},
		&mvpAgentTool{name: planstoryboard.ToolKey},
		&mvpAgentTool{name: writeprompts.ToolKey},
	}
	dispatcher := &mvpAgentModel{}
	agent, err := NewMVPAllTools(ctx, dispatcher, tools, 4)
	if err != nil || agent == nil || agent.Name(ctx) != MVPAllToolsName {
		t.Fatalf("创建统一主 Agent 失败: agent=%v err=%v", agent, err)
	}
	wantTools := []string{
		plancreationspec.ToolKey,
		analyzematerials.ToolKey,
		planstoryboard.ToolKey,
		writeprompts.ToolKey,
	}
	if dispatcher.bindCalls != 1 || !reflect.DeepEqual(dispatcher.toolNames, wantTools) {
		t.Fatalf("统一 Dispatcher 未在启动期冻结完整 Registry: calls=%d tools=%v", dispatcher.bindCalls, dispatcher.toolNames)
	}
	if _, err := NewMVPAllTools(ctx, &mvpAgentModel{}, tools[:3], 4); err == nil {
		t.Fatal("缺少 Tool 的 Registry 未失败关闭")
	}
	duplicate := append([]einotool.BaseTool(nil), tools...)
	duplicate[3] = &mvpAgentTool{name: plancreationspec.ToolKey}
	if _, err := NewMVPAllTools(ctx, &mvpAgentModel{}, duplicate, 4); err == nil {
		t.Fatal("重复 Tool 的 Registry 未失败关闭")
	}
	if _, err := NewMVPAllTools(ctx, &mvpAgentModel{}, tools, 3); err == nil {
		t.Fatal("低于 Creation Spec 的迭代预算未失败关闭")
	}
}

var _ model.ToolCallingChatModel = (*mvpAgentModel)(nil)
var _ einotool.InvokableTool = (*mvpAgentTool)(nil)
