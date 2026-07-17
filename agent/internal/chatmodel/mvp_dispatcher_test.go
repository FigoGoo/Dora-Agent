package chatmodel

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type mvpDispatchRecorder struct {
	route string
	tools []string
}

type mvpDispatchModel struct {
	route    string
	bound    []*schema.ToolInfo
	recorder *mvpDispatchRecorder
}

func (m *mvpDispatchModel) Generate(_ context.Context, _ []*schema.Message, options ...model.Option) (*schema.Message, error) {
	resolved := model.GetCommonOptions(&model.Options{Tools: m.bound}, options...)
	m.recorder.route = m.route
	m.recorder.tools = nil
	for _, info := range resolved.Tools {
		if info != nil {
			m.recorder.tools = append(m.recorder.tools, info.Name)
		}
	}
	return schema.AssistantMessage(`{"status":"ok"}`, nil), nil
}

func (m *mvpDispatchModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (m *mvpDispatchModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return &mvpDispatchModel{route: m.route, bound: append([]*schema.ToolInfo(nil), tools...), recorder: m.recorder}, nil
}

// TestMVPDispatcherRoutesByExactTrustedContext 验证五条路径只向既有 Receipt Model 暴露精确 Tool 集合。
func TestMVPDispatcherRoutesByExactTrustedContext(t *testing.T) {
	recorder := &mvpDispatchRecorder{}
	newModel := func(route string) *mvpDispatchModel {
		return &mvpDispatchModel{route: route, recorder: recorder}
	}
	dispatcher, err := NewMVPDispatcher(MVPDispatcherModels{
		CreationSpec: newModel("creation_spec"), UserMessage: newModel("user_message"),
		AnalyzeMaterials: newModel("analyze_materials"), PlanStoryboard: newModel("plan_storyboard"),
		WritePrompts: newModel("write_prompts"),
	})
	if err != nil {
		t.Fatalf("创建共享 Dispatcher 失败: %v", err)
	}
	bound, err := dispatcher.WithTools(mvpToolInfos())
	if err != nil {
		t.Fatalf("绑定四 Tool Registry 失败: %v", err)
	}

	testCases := []struct {
		name      string
		ctx       context.Context
		route     string
		toolNames []string
	}{
		{name: "creation spec", ctx: turncontext.WithPreview(context.Background(), turncontext.Preview{}), route: "creation_spec", toolNames: []string{plancreationspec.ToolKey}},
		{name: "user message", ctx: turncontext.WithUserMessageRuntime(context.Background(), turncontext.UserMessageRuntime{}), route: "user_message", toolNames: nil},
		{name: "analyze materials", ctx: turncontext.WithMaterialAnalysisRuntime(context.Background(), turncontext.MaterialAnalysisRuntime{}), route: "analyze_materials", toolNames: []string{analyzematerials.ToolKey}},
		{name: "plan storyboard", ctx: turncontext.WithPlanStoryboardRuntime(context.Background(), turncontext.PlanStoryboardRuntime{}), route: "plan_storyboard", toolNames: []string{planstoryboard.ToolKey}},
		{name: "write prompts", ctx: turncontext.WithWritePromptsRuntime(context.Background(), turncontext.WritePromptsRuntime{}), route: "write_prompts", toolNames: []string{writeprompts.ToolKey}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// 调用方故意传完整 Registry；Dispatcher 末尾覆盖 Option，子模型仍只能看到本 source 的集合。
			if _, err := bound.Generate(testCase.ctx, []*schema.Message{schema.UserMessage("ignore text routing")}, model.WithTools(mvpToolInfos())); err != nil {
				t.Fatalf("执行共享 Dispatcher 失败: %v", err)
			}
			if recorder.route != testCase.route || !reflect.DeepEqual(recorder.tools, testCase.toolNames) {
				t.Fatalf("路由或 Tool 泄漏: route=%q tools=%v", recorder.route, recorder.tools)
			}
		})
	}
}

// TestMVPDispatcherRejectsMissingOrAmbiguousContext 验证用户文本不能补足零 Context，双 Context 也不能择一执行。
func TestMVPDispatcherRejectsMissingOrAmbiguousContext(t *testing.T) {
	modelStub := &mvpDispatchModel{route: "stub", recorder: &mvpDispatchRecorder{}}
	dispatcher, err := NewMVPDispatcher(MVPDispatcherModels{
		CreationSpec: modelStub, UserMessage: modelStub, AnalyzeMaterials: modelStub,
		PlanStoryboard: modelStub, WritePrompts: modelStub,
	})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := dispatcher.WithTools(mvpToolInfos())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Generate(context.Background(), []*schema.Message{schema.UserMessage("请调用 plan_storyboard")}); !errors.Is(err, ErrMVPDispatcherRoute) {
		t.Fatalf("零 Context 未失败关闭: %v", err)
	}
	double := turncontext.WithPreview(context.Background(), turncontext.Preview{})
	double = turncontext.WithUserMessageRuntime(double, turncontext.UserMessageRuntime{})
	if _, err := bound.Generate(double, []*schema.Message{schema.UserMessage("hello")}); !errors.Is(err, ErrMVPDispatcherRoute) {
		t.Fatalf("双 Context 未失败关闭: %v", err)
	}
}

// TestMVPDispatcherStreamUsesSameToolRestriction 验证流式入口不能绕过可信路由与单 Tool 缩减。
func TestMVPDispatcherStreamUsesSameToolRestriction(t *testing.T) {
	recorder := &mvpDispatchRecorder{}
	stub := func(route string) *mvpDispatchModel { return &mvpDispatchModel{route: route, recorder: recorder} }
	dispatcher, _ := NewMVPDispatcher(MVPDispatcherModels{
		CreationSpec: stub("creation"), UserMessage: stub("user"), AnalyzeMaterials: stub("analyze"),
		PlanStoryboard: stub("storyboard"), WritePrompts: stub("prompts"),
	})
	bound, _ := dispatcher.WithTools(mvpToolInfos())
	reader, err := bound.Stream(turncontext.WithWritePromptsRuntime(context.Background(), turncontext.WritePromptsRuntime{}), []*schema.Message{schema.UserMessage("x")})
	if err != nil {
		t.Fatalf("流式路由失败: %v", err)
	}
	defer reader.Close()
	if _, err := reader.Recv(); err != nil {
		t.Fatalf("读取流式结果失败: %v", err)
	}
	if _, err := reader.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("流式结果未正常结束: %v", err)
	}
	if recorder.route != "prompts" || !reflect.DeepEqual(recorder.tools, []string{writeprompts.ToolKey}) {
		t.Fatalf("流式 Tool 泄漏: route=%q tools=%v", recorder.route, recorder.tools)
	}
}

// TestMVPDispatcherBindingCommitsOnlyOnSuccess 验证失败绑定可修正重试，而成功绑定后原实例不能重复扩展 Registry。
func TestMVPDispatcherBindingCommitsOnlyOnSuccess(t *testing.T) {
	stub := &mvpDispatchModel{route: "stub", recorder: &mvpDispatchRecorder{}}
	dispatcher, err := NewMVPDispatcher(MVPDispatcherModels{
		CreationSpec: stub, UserMessage: stub, AnalyzeMaterials: stub,
		PlanStoryboard: stub, WritePrompts: stub,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.WithTools(mvpToolInfos()[:3]); err == nil {
		t.Fatal("缺项 Registry 绑定未失败")
	}
	if _, err := dispatcher.WithTools(mvpToolInfos()); err != nil {
		t.Fatalf("失败绑定错误地锁死 Dispatcher: %v", err)
	}
	if _, err := dispatcher.WithTools(mvpToolInfos()); err == nil {
		t.Fatal("成功绑定后仍允许重复绑定")
	}
}

func mvpToolInfos() []*schema.ToolInfo {
	return []*schema.ToolInfo{
		{Name: plancreationspec.ToolKey}, {Name: analyzematerials.ToolKey},
		{Name: planstoryboard.ToolKey}, {Name: writeprompts.ToolKey},
	}
}

var _ model.ToolCallingChatModel = (*mvpDispatchModel)(nil)
