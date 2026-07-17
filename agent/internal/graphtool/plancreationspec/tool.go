package plancreationspec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Tool 使用真实 Eino InvokableTool 接口包装启动期预编译 Graph。
// 它只接受模型可控 Intent 字段，可信身份、ID 与 Fence 必须从 Runtime Context 读取。
type Tool struct {
	graph *CompiledGraph
	store ResultStore
}

// NewTool 创建 plan_creation_spec Tool；未编译 Graph 会阻止 Tool Registry 和 Readiness。
func NewTool(graph *CompiledGraph, store ResultStore) (*Tool, error) {
	if graph == nil || graph.runnable == nil || store == nil {
		return nil, fmt.Errorf("create plan_creation_spec tool: compiled graph and result store are required")
	}
	return &Tool{graph: graph, store: store}, nil
}

// Info 返回不含可信上下文字段的固定 Tool Schema；动态 Skill 或配置不能扩展该 exact-set。
func (t *Tool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ToolKey,
		Desc: "根据用户已提交的创作目标规划一份严格结构化 CreationSpec 开发预览草稿。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"schema_version": {
				Type: schema.String, Required: true, Enum: []string{IntentSchemaVersion},
				Desc: "固定 Intent Schema 版本。",
			},
			"goal": {Type: schema.String, Required: true, Desc: "1 至 2000 字符的 NFC 创作目标。"},
			"deliverable_type": {
				Type: schema.String, Required: true, Enum: []string{"video", "image_set", "audio", "mixed"},
				Desc: "交付物类型。",
			},
			"audience": {Type: schema.String, Required: false, Desc: "可省略的目标受众，最多 500 字符。"},
			"locale": {
				Type: schema.String, Required: true, Enum: []string{"zh-CN", "en-US"}, Desc: "输出 locale。",
			},
			"constraints": {
				Type: schema.Array, Required: true, ElemInfo: &schema.ParameterInfo{Type: schema.String},
				Desc: "零至八条精确去重的硬约束。",
			},
		}),
	}, nil
}

// InvokableRun 再次严格解码模型参数、注入可信 Context 并执行预编译 Graph；不按请求动态 Compile。
func (t *Tool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	intent, err := DecodeIntent([]byte(argumentsInJSON))
	if err != nil {
		return "", err
	}
	// 重新编码严格 DTO 可消除模型输入中的无意义 JSON 空白，同时保留 audience absent/present empty 区别。
	canonicalIntent, err := json.Marshal(intent)
	if err != nil {
		return "", fmt.Errorf("run plan_creation_spec tool: encode strict intent: %w", err)
	}
	trusted, ok := turncontext.PreviewFrom(ctx)
	if !ok {
		return "", fmt.Errorf("run plan_creation_spec tool: trusted turn context is missing")
	}
	trustedContext := TrustedContext{
		Owner: trusted.Owner, RequestID: trusted.RequestID, UserID: trusted.UserID, ProjectID: trusted.ProjectID,
		SessionID: trusted.SessionID, InputID: trusted.InputID, TurnID: trusted.TurnID,
		RunID: trusted.RunID, ToolCallID: trusted.ToolCallID,
		BusinessCommandID: trusted.BusinessCommandID, FenceToken: trusted.FenceToken,
		PromptVersion: trusted.PromptVersion, ValidatorVersion: trusted.ValidatorVersion,
	}
	replayed, err := t.store.ReplayTerminal(ctx, trustedContext)
	if err != nil {
		return "", fmt.Errorf("run plan_creation_spec tool: replay terminal result: %w", err)
	}
	if replayed != nil {
		encoded, encodeErr := json.Marshal(*replayed)
		if encodeErr != nil {
			return "", fmt.Errorf("run plan_creation_spec tool: encode replayed result: %w", encodeErr)
		}
		return string(encoded), nil
	}
	recovery, err := t.store.ReplayRecovery(ctx, trustedContext)
	if err != nil {
		return "", fmt.Errorf("run plan_creation_spec tool: replay recovery receipt: %w", err)
	}
	if recovery != nil {
		outcome, recoverErr := t.graph.Recover(ctx, trustedContext, *recovery)
		return t.finishOutcome(ctx, trustedContext, outcome, recoverErr)
	}
	outcome, err := t.graph.Invoke(ctx, GraphInput{
		TrustedContext: trustedContext,
		IntentJSON:     canonicalIntent,
	})
	return t.finishOutcome(ctx, trustedContext, outcome, err)
}

func (t *Tool) finishOutcome(
	ctx context.Context,
	trustedContext TrustedContext,
	outcome Outcome,
	err error,
) (string, error) {
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, ErrBusinessUnknownOutcome) || errors.Is(err, ErrBusinessTechnical) {
			return "", err
		}
		// 确定错误同样先冻结完整 Result，确保 summary/retryable/receipt_ref 在投影失败后可原样重放。
		result := failedResult(trustedContext, err)
		if freezeErr := t.store.FreezeTerminal(ctx, trustedContext, result); freezeErr != nil {
			return "", fmt.Errorf("run plan_creation_spec tool: freeze failed result: %w", freezeErr)
		}
		encoded, encodeErr := json.Marshal(result)
		if encodeErr != nil {
			return "", fmt.Errorf("run plan_creation_spec tool: encode failed result: %w", encodeErr)
		}
		return string(encoded), nil
	}
	if (outcome.Terminal == nil) == (outcome.Recovery == nil) {
		return "", fmt.Errorf("run plan_creation_spec tool: invalid graph outcome union")
	}
	if outcome.Recovery != nil {
		if err := t.store.MarkRecovery(ctx, trustedContext, *outcome.Recovery); err != nil {
			return "", fmt.Errorf("run plan_creation_spec tool: mark recovery: %w", err)
		}
		// 返回 error 使 Runner 不生成成功终态；Processor 读取已 CAS 的 recovery stage 保持输入未决。
		return "", ErrBusinessUnknownOutcome
	}
	result := *outcome.Terminal
	if result.Status != "completed" && result.Status != "failed" {
		return "", fmt.Errorf("run plan_creation_spec tool: terminal result status is invalid")
	}
	if err := t.store.FreezeTerminal(ctx, trustedContext, result); err != nil {
		return "", fmt.Errorf("run plan_creation_spec tool: freeze terminal result: %w", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("run plan_creation_spec tool: encode result: %w", err)
	}
	return string(encoded), nil
}

// failedResult 把内部错误收敛为稳定、安全、可重放的确定失败 Result，不包含 SQL、Provider 或 Prompt 原文。
func failedResult(trusted TrustedContext, err error) Result {
	code := "CREATION_SPEC_PREVIEW_FAILED"
	summary := "无法生成创作规格预览，请检查输入后重试。"
	retryable := false
	switch {
	case errors.Is(err, ErrBusinessNotFound):
		code = ResultCodeProjectNotFound
		summary = "项目不存在或不可访问。"
	case errors.Is(err, ErrBusinessConflict):
		code = ResultCodeBusinessConflict
		summary = "项目或幂等命令已发生冲突，请刷新后重试。"
	case errors.Is(err, ErrBusinessDisabled):
		code = ResultCodeBusinessDisabled
		summary = "创作规格预览当前未启用。"
	case err != nil:
		code = ResultCodeProtocolInvalid
	}
	return Result{
		Status: "failed", ResultCode: code,
		ReceiptRef: ReceiptRef{ToolCallID: trusted.ToolCallID, BusinessCommandID: trusted.BusinessCommandID},
		Summary:    summary, Retryable: retryable,
	}
}

var _ einotool.InvokableTool = (*Tool)(nil)
