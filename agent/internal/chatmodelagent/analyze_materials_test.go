package chatmodelagent_test

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodel"
	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodelagent"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type analyzeAgentToolStub struct{}

func (*analyzeAgentToolStub) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: analyzematerials.ToolKey, ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"schema_version": {Type: schema.String}})}, nil
}
func (*analyzeAgentToolStub) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return `{}`, nil
}

func TestNewAnalyzeMaterialsRequiresExactOneTool(t *testing.T) {
	ctx := context.Background()
	agent, err := chatmodelagent.NewAnalyzeMaterials(ctx, chatmodel.NewAnalyzeMaterialsFakeRouter(), []einotool.BaseTool{&analyzeAgentToolStub{}})
	if err != nil || agent.Name(ctx) != chatmodelagent.AnalyzeMaterialsName {
		t.Fatalf("创建单 Tool Agent 失败: agent=%v err=%v", agent, err)
	}
	if _, err := chatmodelagent.NewAnalyzeMaterials(ctx, chatmodel.NewAnalyzeMaterialsFakeRouter(), nil); err == nil {
		t.Fatal("空 Tool Registry 未失败关闭")
	}
}

var _ einotool.InvokableTool = (*analyzeAgentToolStub)(nil)
