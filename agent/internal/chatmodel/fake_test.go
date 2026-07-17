package chatmodel

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// TestFakeRouterAcceptsADKCallOptions 验证真实 ADK 按次注入的 Tool Registry 能驱动首轮稳定 ToolCall。
func TestFakeRouterAcceptsADKCallOptions(t *testing.T) {
	ctx := turncontext.WithPreview(context.Background(), turncontext.Preview{
		ToolCallID: "019f68e8-5108-7000-8000-000000000008",
	})
	message, err := NewFakeRouter().Generate(
		ctx,
		[]*schema.Message{schema.UserMessage(`{"schema_version":"plan_creation_spec.preview.intent.v1"}`)},
		model.WithTools([]*schema.ToolInfo{{Name: plancreationspec.ToolKey}}),
	)
	if err != nil {
		t.Fatalf("按次注入稳定 Tool Registry 后生成失败: %v", err)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Name != plancreationspec.ToolKey {
		t.Fatalf("Router 未生成唯一稳定 ToolCall: %+v", message.ToolCalls)
	}
}

// TestFakeRouterRejectsInvalidToolRegistry 验证缺失、错误、重复或按次覆盖后的 Registry 均失败关闭。
func TestFakeRouterRejectsInvalidToolRegistry(t *testing.T) {
	ctx := turncontext.WithPreview(context.Background(), turncontext.Preview{
		ToolCallID: "019f68e8-5108-7000-8000-000000000008",
	})
	messages := []*schema.Message{schema.UserMessage(`{"schema_version":"plan_creation_spec.preview.intent.v1"}`)}
	valid := []*schema.ToolInfo{{Name: plancreationspec.ToolKey}}
	tests := []struct {
		name    string
		router  model.ToolCallingChatModel
		options []model.Option
	}{
		{name: "missing", router: NewFakeRouter()},
		{name: "wrong", router: NewFakeRouter(), options: []model.Option{model.WithTools([]*schema.ToolInfo{{Name: "wrong_tool"}})}},
		{name: "duplicate", router: NewFakeRouter(), options: []model.Option{model.WithTools([]*schema.ToolInfo{{Name: plancreationspec.ToolKey}, {Name: plancreationspec.ToolKey}})}},
	}
	bound, err := NewFakeRouter().WithTools(valid)
	if err != nil {
		t.Fatalf("绑定稳定 Tool Registry 失败: %v", err)
	}
	tests = append(tests, struct {
		name    string
		router  model.ToolCallingChatModel
		options []model.Option
	}{name: "call option overrides binding", router: bound, options: []model.Option{model.WithTools(nil)}})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.router.Generate(ctx, messages, test.options...); err == nil {
				t.Fatal("非法 Tool Registry 未失败关闭")
			}
		})
	}
}

// TestFakeRouterAcceptsImmutableBinding 保留 ToolCallingChatModel.WithTools 的直接绑定兼容路径。
func TestFakeRouterAcceptsImmutableBinding(t *testing.T) {
	ctx := turncontext.WithPreview(context.Background(), turncontext.Preview{
		ToolCallID: "019f68e8-5108-7000-8000-000000000008",
	})
	bound, err := NewFakeRouter().WithTools([]*schema.ToolInfo{{Name: plancreationspec.ToolKey}})
	if err != nil {
		t.Fatalf("绑定稳定 Tool Registry 失败: %v", err)
	}
	if _, err := bound.Generate(ctx, []*schema.Message{schema.UserMessage(`{}`)}); err != nil {
		t.Fatalf("不可变绑定路径生成失败: %v", err)
	}
}

// TestFakeProposalUsesExactIntentLineBoundary 验证 Project Title 中的普通 intent_json= 文本不会抢占真实 Prompt 边界。
func TestFakeProposalUsesExactIntentLineBoundary(t *testing.T) {
	prompt := "prompt_version=graph_tool.plan_creation_spec.preview.v1\n" +
		"project_title=标题包含 intent_json= 但不是边界\n" +
		`intent_json={"schema_version":"plan_creation_spec.preview.intent.v1","goal":"生成可执行规格","deliverable_type":"video","locale":"zh-CN","constraints":[]}` + "\n" +
		"输出 schema_version=creation_spec.preview.proposal.v1，并保留目标、交付类型、受众和全部硬约束。"
	message, err := NewFakeProposal().Generate(context.Background(), []*schema.Message{schema.UserMessage(prompt)})
	if err != nil {
		t.Fatalf("Fake Proposal 生成失败: %v", err)
	}
	var proposal plancreationspec.Proposal
	if err := json.Unmarshal([]byte(message.Content), &proposal); err != nil {
		t.Fatalf("解码 Fake Proposal 失败: %v", err)
	}
	if proposal.Goal != "生成可执行规格" || proposal.DeliverableType != "video" {
		t.Fatalf("Fake Proposal 读取了错误边界: %+v", proposal)
	}
}

// TestFakeProposalProducesValidTrialBasicProposal 锁定一键验收表单的真实字段组合必须通过正式 Proposal Validator。
func TestFakeProposalProducesValidTrialBasicProposal(t *testing.T) {
	prompt := "prompt_version=graph_tool.plan_creation_spec.preview.v1\n" +
		"project_title=Dora 基本功能一键验收项目\n" +
		`intent_json={"schema_version":"plan_creation_spec.preview.intent.v1","goal":"Dora 基本功能一键验收 1784250000000","deliverable_type":"video","audience":"本地 MVP 验收用户","locale":"zh-CN","constraints":[]}` + "\n" +
		"输出 schema_version=creation_spec.preview.proposal.v1，并保留目标、交付类型、受众和全部硬约束。"
	message, err := NewFakeProposal().Generate(context.Background(), []*schema.Message{schema.UserMessage(prompt)})
	if err != nil {
		t.Fatalf("Fake Proposal 生成失败: %v", err)
	}
	intent, err := plancreationspec.DecodeIntent([]byte(`{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"Dora 基本功能一键验收 1784250000000","deliverable_type":"video","audience":"本地 MVP 验收用户","locale":"zh-CN","constraints":[]}`))
	if err != nil {
		t.Fatalf("解码一键验收 Intent 失败: %v", err)
	}
	if _, _, err := plancreationspec.DecodeAndValidateProposal([]byte(message.Content), intent); err != nil {
		t.Fatalf("一键验收 Fake Proposal 未通过正式 Validator: %v", err)
	}
}
