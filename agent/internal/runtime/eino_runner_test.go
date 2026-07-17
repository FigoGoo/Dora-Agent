package runtime_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	previewchatmodel "github.com/FigoGoo/Dora-Agent/agent/internal/chatmodel"
	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodelagent"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	previewruntime "github.com/FigoGoo/Dora-Agent/agent/internal/runtime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// TestEinoRunnerExecutesFakeRouterThroughRealADKLoop 验证真实 ADK Runner 会把可信 Context
// 传入 Router 与 Tool，并完整完成经典 schema.Message 的 ReAct 循环。
func TestEinoRunnerExecutesFakeRouterThroughRealADKLoop(t *testing.T) {
	store := &runnerReceiptStore{entries: make(map[string]*schema.Message)}
	router, err := previewchatmodel.NewReceiptModel(
		previewchatmodel.NewFakeRouter(), store, previewchatmodel.ReceiptCallRouter,
	)
	if err != nil {
		t.Fatalf("创建 Router 回执模型失败: %v", err)
	}
	tool := &runnerPreviewTool{}
	agent, err := chatmodelagent.New(context.Background(), router, []einotool.BaseTool{tool}, 4)
	if err != nil {
		t.Fatalf("创建主 Agent 失败: %v", err)
	}
	runner, err := previewruntime.NewEinoRunner(context.Background(), agent)
	if err != nil {
		t.Fatalf("创建 Eino Runner 失败: %v", err)
	}
	claim := previewruntime.Claim{
		Owner: "runner-test", RequestID: "019f68e8-5101-7000-8000-000000000001",
		SessionID:         "019f68e8-5102-7000-8000-000000000002",
		UserID:            "019f68e8-5103-7000-8000-000000000003",
		ProjectID:         "019f68e8-5104-7000-8000-000000000004",
		InputID:           "019f68e8-5105-7000-8000-000000000005",
		TurnID:            "019f68e8-5106-7000-8000-000000000006",
		RunID:             "019f68e8-5107-7000-8000-000000000007",
		ToolCallID:        "019f68e8-5108-7000-8000-000000000008",
		BusinessCommandID: "019f68e8-5109-7000-8000-000000000009",
		FenceToken:        1,
		Intent:            []byte(`{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"测试真实 ADK Runner","deliverable_type":"video","locale":"zh-CN","constraints":[]}`),
	}
	if err := runner.Run(context.Background(), claim); err != nil {
		t.Fatalf("真实 ADK Runner 执行失败: %v", err)
	}
	if tool.calls != 1 || store.freezeCalls != 2 {
		t.Fatalf("Router/Tool 循环不完整: tool_calls=%d model_freezes=%d", tool.calls, store.freezeCalls)
	}
}

// runnerPreviewTool 是 Runner 集成测试使用的无外部副作用 Tool，只验证可信 Context 透传。
type runnerPreviewTool struct {
	calls int
}

// Info 返回与生产 Registry 相同的稳定 Tool Key。
func (t *runnerPreviewTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: plancreationspec.ToolKey,
		Desc: "测试真实 ADK Runner 的 plan_creation_spec Tool。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"schema_version":   {Type: schema.String, Required: true},
			"goal":             {Type: schema.String, Required: true},
			"deliverable_type": {Type: schema.String, Required: true},
			"locale":           {Type: schema.String, Required: true},
			"constraints": {Type: schema.Array, Required: true,
				ElemInfo: &schema.ParameterInfo{Type: schema.String}},
		}),
	}, nil
}

// InvokableRun 确认 Tool 执行仍处于当前可信 Preview Context，并返回最小完成结果。
func (t *runnerPreviewTool) InvokableRun(ctx context.Context, _ string, _ ...einotool.Option) (string, error) {
	trusted, ok := turncontext.PreviewFrom(ctx)
	if !ok || trusted.ToolCallID == "" {
		return "", fmt.Errorf("runner preview tool: trusted context is missing")
	}
	t.calls++
	return `{"status":"completed"}`, nil
}

// runnerReceiptStore 为真实 ADK Runner 测试保存模型回执，避免引入数据库之外的编排差异。
type runnerReceiptStore struct {
	mu          sync.Mutex
	entries     map[string]*schema.Message
	freezeCalls int
}

// ReplayOrReserveModel 返回已冻结响应，或授权首次模型执行。
func (s *runnerReceiptStore) ReplayOrReserveModel(
	_ context.Context,
	identity previewchatmodel.ReceiptIdentity,
	callIndex int,
	_ string,
) (*schema.Message, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s/%d", identity.ToolCallID, callIndex)
	response, exists := s.entries[key]
	if !exists {
		return nil, true, nil
	}
	copy := *response
	copy.ToolCalls = append([]schema.ToolCall(nil), response.ToolCalls...)
	return &copy, false, nil
}

// FreezeModel 只保存每个稳定调用序号的首个完整响应。
func (s *runnerReceiptStore) FreezeModel(
	_ context.Context,
	identity previewchatmodel.ReceiptIdentity,
	callIndex int,
	_ string,
	response *schema.Message,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s/%d", identity.ToolCallID, callIndex)
	if _, exists := s.entries[key]; !exists {
		copy := *response
		copy.ToolCalls = append([]schema.ToolCall(nil), response.ToolCalls...)
		s.entries[key] = &copy
		s.freezeCalls++
	}
	return nil
}

var _ einotool.InvokableTool = (*runnerPreviewTool)(nil)
var _ previewchatmodel.ReceiptStore = (*runnerReceiptStore)(nil)
