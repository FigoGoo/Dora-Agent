package chatmodelagent_test

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodelagent"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/internal/planstoryboardruntime"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type storyboardAgentToolStub struct{ info *schema.ToolInfo }

func (stub *storyboardAgentToolStub) Info(context.Context) (*schema.ToolInfo, error) {
	return stub.info, nil
}

func (*storyboardAgentToolStub) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return `{}`, nil
}

func TestNewPlanStoryboardRequiresExactSingleTool(t *testing.T) {
	ctx := context.Background()
	canonical, err := planstoryboard.CanonicalToolInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tool := &storyboardAgentToolStub{info: canonical}
	agent, err := chatmodelagent.NewPlanStoryboard(ctx, planstoryboardruntime.NewFakeRouter(), []einotool.BaseTool{tool})
	if err != nil || agent.Name(ctx) != chatmodelagent.PlanStoryboardName {
		t.Fatalf("创建单 Tool Agent 失败: agent=%v err=%v", agent, err)
	}
	wrongName, _ := planstoryboard.CanonicalToolInfo(ctx)
	wrongName.Name = "other"
	relaxed := &schema.ToolInfo{
		Name: planstoryboard.ToolKey, Desc: canonical.Desc,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"schema_version": {Type: schema.String, Required: true},
		}),
	}
	for name, tools := range map[string][]einotool.BaseTool{
		"empty":        nil,
		"two":          {tool, tool},
		"wrong":        {&storyboardAgentToolStub{info: wrongName}},
		"schema drift": {&storyboardAgentToolStub{info: relaxed}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := chatmodelagent.NewPlanStoryboard(ctx, planstoryboardruntime.NewFakeRouter(), tools); err == nil {
				t.Fatal("非法 Tool Registry 未失败关闭")
			}
		})
	}
}

var _ einotool.InvokableTool = (*storyboardAgentToolStub)(nil)
