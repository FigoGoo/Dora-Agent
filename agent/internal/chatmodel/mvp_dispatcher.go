package chatmodel

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// ErrMVPDispatcherRoute 表示统一主 Agent 缺少唯一可信 Runtime Context，禁止从用户正文推断路由。
var ErrMVPDispatcherRoute = errors.New("mvp model dispatcher requires exactly one trusted runtime context")

// MVPDispatcherModels 保存基础五条模型路径及可选的两条确定性媒体 Router。
type MVPDispatcherModels struct {
	// CreationSpec 是 plan_creation_spec Router Receipt Model。
	CreationSpec model.ToolCallingChatModel
	// UserMessage 是普通消息无 Tool Receipt Model。
	UserMessage model.BaseChatModel
	// AnalyzeMaterials 是 analyze_materials Router Receipt Model。
	AnalyzeMaterials model.ToolCallingChatModel
	// PlanStoryboard 是 plan_storyboard Router Receipt Model。
	PlanStoryboard model.ToolCallingChatModel
	// WritePrompts 是 write_prompts Router Receipt Model。
	WritePrompts model.ToolCallingChatModel
	// GenerateMedia 是可选 media.runtime.v3preview1 deterministic Router。
	GenerateMedia model.ToolCallingChatModel
	// AssembleOutput 是可选 media.runtime.v3preview1 deterministic Router。
	AssembleOutput model.ToolCallingChatModel
}

// MVPDispatcher 只按私有可信 Turn Context 委托既有 Receipt Model，并为每条路径缩小 Tool 可见集合。
type MVPDispatcher struct {
	models MVPDispatcherModels
	tools  map[string][]*schema.ToolInfo
	state  atomic.Uint32
}

// NewMVPDispatcher 创建尚未绑定基础四 Tool 或媒体扩展六 Tool Registry 的共享模型路由器。
func NewMVPDispatcher(models MVPDispatcherModels) (*MVPDispatcher, error) {
	if models.CreationSpec == nil || models.UserMessage == nil || models.AnalyzeMaterials == nil ||
		models.PlanStoryboard == nil || models.WritePrompts == nil ||
		(models.GenerateMedia == nil) != (models.AssembleOutput == nil) {
		return nil, fmt.Errorf("create mvp model dispatcher: all receipt models are required")
	}
	return &MVPDispatcher{models: models}, nil
}

// WithTools 一次性校验完整基础或媒体扩展 Registry，再为各委托模型创建只含精确单 Tool 的不可变绑定副本。
func (d *MVPDispatcher) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if d == nil || !d.state.CompareAndSwap(0, 1) {
		return nil, fmt.Errorf("bind mvp model dispatcher: unbound dispatcher is required")
	}
	succeeded := false
	defer func() {
		if !succeeded {
			d.state.Store(0)
		}
	}()
	toolSets, err := d.exactToolSets(tools)
	if err != nil {
		return nil, err
	}
	creationSpec, err := d.models.CreationSpec.WithTools(toolSets[plancreationspec.ToolKey])
	if err != nil {
		return nil, fmt.Errorf("bind mvp creation spec model: %w", err)
	}
	analyzeMaterials, err := d.models.AnalyzeMaterials.WithTools(toolSets[analyzematerials.ToolKey])
	if err != nil {
		return nil, fmt.Errorf("bind mvp analyze materials model: %w", err)
	}
	planStoryboard, err := d.models.PlanStoryboard.WithTools(toolSets[planstoryboard.ToolKey])
	if err != nil {
		return nil, fmt.Errorf("bind mvp plan storyboard model: %w", err)
	}
	writePrompts, err := d.models.WritePrompts.WithTools(toolSets[writeprompts.ToolKey])
	if err != nil {
		return nil, fmt.Errorf("bind mvp write prompts model: %w", err)
	}
	var generateMedia model.ToolCallingChatModel
	var assembleOutput model.ToolCallingChatModel
	if d.models.GenerateMedia != nil {
		generateMedia, err = d.models.GenerateMedia.WithTools(toolSets[mediapreview.GenerateMediaToolKey])
		if err != nil {
			return nil, fmt.Errorf("bind mvp generate media model: %w", err)
		}
		assembleOutput, err = d.models.AssembleOutput.WithTools(toolSets[mediapreview.AssembleOutputToolKey])
		if err != nil {
			return nil, fmt.Errorf("bind mvp assemble output model: %w", err)
		}
	}
	bound := &MVPDispatcher{
		models: MVPDispatcherModels{
			CreationSpec: creationSpec, UserMessage: d.models.UserMessage,
			AnalyzeMaterials: analyzeMaterials, PlanStoryboard: planStoryboard, WritePrompts: writePrompts,
			GenerateMedia: generateMedia, AssembleOutput: assembleOutput,
		},
		tools: toolSets,
	}
	bound.state.Store(2)
	d.state.Store(2)
	succeeded = true
	return bound, nil
}

// Generate 对一次非流式模型调用执行唯一可信路由，并用末尾 Option 覆盖完整 Registry，防止能力泄漏。
func (d *MVPDispatcher) Generate(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	delegate, tools, err := d.delegate(ctx)
	if err != nil {
		return nil, err
	}
	return delegate.Generate(ctx, messages, append(options, model.WithTools(tools))...)
}

// Stream 与 Generate 使用同一个可信路由和 Tool 缩减规则，禁止流式入口形成旁路。
func (d *MVPDispatcher) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	delegate, tools, err := d.delegate(ctx)
	if err != nil {
		return nil, err
	}
	return delegate.Stream(ctx, messages, append(options, model.WithTools(tools))...)
}

// delegate 只统计基础五个及可选媒体两个 Runtime 私有 Context；Tool Core 的二级 Context 不参与主 Agent 路由。
func (d *MVPDispatcher) delegate(ctx context.Context) (model.BaseChatModel, []*schema.ToolInfo, error) {
	if d == nil || d.state.Load() != 2 || ctx == nil {
		return nil, nil, ErrMVPDispatcherRoute
	}
	type candidate struct {
		present  bool
		delegate model.BaseChatModel
		toolKey  string
	}
	_, creationSpec := turncontext.PreviewFrom(ctx)
	_, userMessage := turncontext.UserMessageRuntimeFrom(ctx)
	_, analyzeMaterials := turncontext.MaterialAnalysisRuntimeFrom(ctx)
	_, planStoryboard := turncontext.PlanStoryboardRuntimeFrom(ctx)
	_, writePrompts := turncontext.WritePromptsRuntimeFrom(ctx)
	_, generateMedia := turncontext.GenerateMediaPreviewFrom(ctx)
	_, assembleOutput := turncontext.AssembleOutputPreviewFrom(ctx)
	candidates := []candidate{
		{creationSpec, d.models.CreationSpec, plancreationspec.ToolKey},
		{userMessage, d.models.UserMessage, ""},
		{analyzeMaterials, d.models.AnalyzeMaterials, analyzematerials.ToolKey},
		{planStoryboard, d.models.PlanStoryboard, planstoryboard.ToolKey},
		{writePrompts, d.models.WritePrompts, writeprompts.ToolKey},
		{generateMedia, d.models.GenerateMedia, mediapreview.GenerateMediaToolKey},
		{assembleOutput, d.models.AssembleOutput, mediapreview.AssembleOutputToolKey},
	}
	var selected *candidate
	for index := range candidates {
		if !candidates[index].present {
			continue
		}
		if selected != nil {
			return nil, nil, ErrMVPDispatcherRoute
		}
		selected = &candidates[index]
	}
	if selected == nil || selected.delegate == nil {
		return nil, nil, ErrMVPDispatcherRoute
	}
	if selected.toolKey == "" {
		return selected.delegate, nil, nil
	}
	return selected.delegate, d.tools[selected.toolKey], nil
}

// exactToolSets 把基础四 Tool 或媒体扩展六 Tool Registry 转成稳定单元素切片。
func (d *MVPDispatcher) exactToolSets(tools []*schema.ToolInfo) (map[string][]*schema.ToolInfo, error) {
	expected := 4
	if d.models.GenerateMedia != nil {
		expected = 6
	}
	if len(tools) != expected {
		return nil, fmt.Errorf("bind mvp model dispatcher: exact approved tool registry is required")
	}
	required := map[string]bool{
		plancreationspec.ToolKey: false,
		analyzematerials.ToolKey: false,
		planstoryboard.ToolKey:   false,
		writeprompts.ToolKey:     false,
	}
	if d.models.GenerateMedia != nil {
		required[mediapreview.GenerateMediaToolKey] = false
		required[mediapreview.AssembleOutputToolKey] = false
	}
	result := make(map[string][]*schema.ToolInfo, len(required))
	for _, info := range tools {
		if info == nil {
			return nil, fmt.Errorf("bind mvp model dispatcher: nil tool definition")
		}
		seen, exists := required[info.Name]
		if !exists || seen {
			return nil, fmt.Errorf("bind mvp model dispatcher: unsupported or duplicate tool %q", info.Name)
		}
		required[info.Name] = true
		result[info.Name] = []*schema.ToolInfo{info}
	}
	for toolKey, seen := range required {
		if !seen {
			return nil, fmt.Errorf("bind mvp model dispatcher: required tool %q is missing", toolKey)
		}
	}
	return result, nil
}

var _ model.ToolCallingChatModel = (*MVPDispatcher)(nil)
