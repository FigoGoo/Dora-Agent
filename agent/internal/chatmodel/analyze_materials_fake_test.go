package chatmodel

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestAnalyzeMaterialsFakeRouterProducesExactStableToolCall(t *testing.T) {
	intent := `{"schema_version":"analyze_materials.preview.intent.v1"}`
	ctx := turncontext.WithMaterialAnalysisRuntime(context.Background(), turncontext.MaterialAnalysisRuntime{IntentJSON: intent, Context: turncontext.MaterialAnalysisTurnContext{Profile: turncontext.MaterialAnalysisRuntimeProfile, ToolCallID: "019f68e8-7501-7000-8000-000000000001"}})
	toolInfo := &schema.ToolInfo{Name: analyzematerials.ToolKey, ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"schema_version": {Type: schema.String}})}
	message, err := NewAnalyzeMaterialsFakeRouter().Generate(ctx, []*schema.Message{schema.UserMessage(intent)}, model.WithTools([]*schema.ToolInfo{toolInfo}))
	if err != nil || len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "019f68e8-7501-7000-8000-000000000001" || message.ToolCalls[0].Function.Arguments != intent {
		t.Fatalf("稳定 ToolCall 异常: message=%+v err=%v", message, err)
	}
	if _, err := NewAnalyzeMaterialsFakeRouter().Generate(ctx, []*schema.Message{{Role: schema.Tool, Content: `{}`}}, model.WithTools([]*schema.ToolInfo{toolInfo})); err == nil {
		t.Fatal("ReturnDirectly 后第二次 Router 调用未失败关闭")
	}
}

func TestAnalyzeMaterialsFakeModelBuildsStrictCandidate(t *testing.T) {
	prompt := "prompt_version=v\nincluded_evidence_json=" + `[{"evidence_id":"019f68e8-7511-7000-8000-000000000011","asset_id":"019f68e8-7512-7000-8000-000000000012"}]` + "\nmissing_requirements_json=[]"
	message, err := NewAnalyzeMaterialsFakeModel().Generate(context.Background(), []*schema.Message{schema.SystemMessage("system"), schema.UserMessage(prompt)})
	if err != nil {
		t.Fatalf("Fake Analysis 生成失败: %v", err)
	}
	var candidate analyzematerials.Candidate
	if err := json.Unmarshal([]byte(message.Content), &candidate); err != nil || len(candidate.AssetSummaries) != 1 || len(candidate.AssetSummaries[0].Observations) != 1 {
		t.Fatalf("Fake Candidate 异常: %+v err=%v", candidate, err)
	}
}
