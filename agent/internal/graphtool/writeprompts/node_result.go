package writeprompts

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/compose"
)

// buildResult 只从已验证的 Business 权威资源构造 completed Result 与内部完整 Card；不会再次调用模型或保存命令。
func (b *graphBuilder) buildResult(ctx context.Context, input SaveOutcome) (Outcome, error) {
	if input.Status != routeSaved || input.Resource == nil {
		return Outcome{}, fmt.Errorf("build prompt preview result: invalid save outcome")
	}
	resource := *input.Resource
	if err := ValidateResourceForCommand(resource, input.Command); err != nil {
		return Outcome{}, err
	}
	sourceRef := resource.StoryboardPreviewRef
	result := Result{
		SchemaVersion: ResultSchemaVersion, Status: "completed", ResultCode: ResultCodeCompleted,
		PromptPreviewRef:     &ResourceRef{ID: resource.PromptPreviewID, Version: resource.Version, ContentDigest: resource.ContentDigest},
		StoryboardPreviewRef: &sourceRef, TargetCount: len(resource.Content.Prompts),
		InvocationRef: InvocationRef{ToolCallID: input.Command.TrustedContext.ToolCallID, BusinessCommandID: input.Command.TrustedContext.BusinessCommandID},
		Card: &Card{
			SchemaVersion: CardSchemaVersion, InputID: input.Command.TrustedContext.InputID,
			TurnID: input.Command.TrustedContext.TurnID, RunID: input.Command.TrustedContext.RunID,
			ToolCallID: input.Command.TrustedContext.ToolCallID, Status: "completed", ResultCode: ResultCodeCompleted,
			PromptPreviewID: resource.PromptPreviewID, ProjectID: resource.ProjectID,
			StoryboardPreviewRef: &sourceRef, Version: resource.Version, ContentDigest: resource.ContentDigest,
			TargetCount: len(resource.Content.Prompts), Prompts: clonePromptEntriesForCard(resource.Content.Prompts),
			UpdatedAt: b.clock.Now().UTC(),
		},
	}
	if err := ValidateTerminalResult(result, input.Command.TrustedContext); err != nil {
		return Outcome{}, err
	}
	outcome := Outcome{Terminal: &result}
	err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.SaveOutcome = &input
		state.Result = &result
		return nil
	})
	return outcome, err
}

// emitScopeFailed 把零目标或预算超限转换为稳定 Tool failed/Card；其他错误码视为 Graph 契约破坏。
func (b *graphBuilder) emitScopeFailed(ctx context.Context, input targetRoute) (Outcome, error) {
	if input.Route != routeInvalid || (input.FailureCode != ResultCodeNoTargets && input.FailureCode != ResultCodeTargetBudgetExceeded) {
		return Outcome{}, fmt.Errorf("emit prompt preview scope failed: invalid route")
	}
	return b.emitFailedOutcome(ctx, input.TrustedContext, input.FailureCode)
}

// emitCandidateFailed 只处理候选协议失败并生成无 Prompt 正文的安全终态；不调用 Business Store。
func (b *graphBuilder) emitCandidateFailed(ctx context.Context, input candidateRoute) (Outcome, error) {
	if input.Route != routeInvalid || input.FailureCode != ResultCodeCandidateInvalid {
		return Outcome{}, fmt.Errorf("emit prompt preview candidate failed: invalid route")
	}
	return b.emitFailedOutcome(ctx, input.TrustedContext, input.FailureCode)
}

// emitExactSetFailed 只处理目标全集不一致并生成安全终态；部分候选永远不会进入保存节点。
func (b *graphBuilder) emitExactSetFailed(ctx context.Context, input contentRoute) (Outcome, error) {
	if input.Route != routeInvalid || input.FailureCode != ResultCodeExactSetInvalid {
		return Outcome{}, fmt.Errorf("emit prompt preview exact-set failed: invalid route")
	}
	return b.emitFailedOutcome(ctx, input.TrustedContext, input.FailureCode)
}

// emitFailedOutcome 使用同一冻结时间源构造并校验 failed Result/Card exact-set，随后只写单次 Graph State。
func (b *graphBuilder) emitFailedOutcome(ctx context.Context, trusted TrustedContext, code string) (Outcome, error) {
	result := failedResult(trusted, code, b.clock.Now())
	if err := ValidateTerminalResult(result, trusted); err != nil {
		return Outcome{}, err
	}
	err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.Error = code
		state.Result = &result
		return nil
	})
	return Outcome{Terminal: &result}, err
}

// deferRecovery 只返回内部 recovery_pending 联合，不把未知 Business Outcome 伪装成 completed 或 failed Tool Result。
func (*graphBuilder) deferRecovery(ctx context.Context, input SaveOutcome) (Outcome, error) {
	if input.Status != routeRecoveryPending || input.Recovery == nil {
		return Outcome{}, fmt.Errorf("defer prompt preview recovery: invalid save outcome")
	}
	err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.SaveOutcome = &input
		state.Error = ResultCodeInternal
		return nil
	})
	return Outcome{Recovery: input.Recovery}, err
}

// failedResult 构造稳定 Tool 失败 Result 与内部 Card；调用方必须随后用 ValidateTerminalResult 失败关闭任何 shape 漂移。
func failedResult(trusted TrustedContext, code string, now time.Time) Result {
	retryable := code == ResultCodeBusinessDisabled || code == ResultCodeInternal
	return Result{
		SchemaVersion: ResultSchemaVersion, Status: "failed", ResultCode: code,
		InvocationRef: InvocationRef{ToolCallID: trusted.ToolCallID, BusinessCommandID: trusted.BusinessCommandID},
		Summary:       stableFailureSummary(code), Retryable: &retryable,
		Card: &Card{
			SchemaVersion: CardSchemaVersion, InputID: trusted.InputID, TurnID: trusted.TurnID, RunID: trusted.RunID,
			ToolCallID: trusted.ToolCallID, Status: "failed", ResultCode: code, UpdatedAt: now.UTC(),
			FailureKind: "tool", Summary: stableFailureSummary(code), Retryable: &retryable,
		},
	}
}

// stableFailureSummary 把允许的稳定结果码映射为不含内部错误、Prompt 或资源细节的用户摘要。
func stableFailureSummary(code string) string {
	switch code {
	case ResultCodeInvalidArgument:
		return "Prompt 写作参数不符合严格协议。"
	case ResultCodeNoTargets:
		return "Storyboard Preview 没有可写作的 Prompt 目标。"
	case ResultCodeTargetBudgetExceeded:
		return "Storyboard Preview 的 Prompt 目标超过本次冻结预算。"
	case ResultCodeCandidateInvalid:
		return "Prompt 候选未通过严格协议校验。"
	case ResultCodeExactSetInvalid:
		return "Prompt 候选没有完整匹配冻结目标全集。"
	case ResultCodeStoryboardNotFound:
		return "Storyboard Preview 不存在或不可访问。"
	case ResultCodeStoryboardConflict:
		return "Storyboard Preview 版本或内容已变化。"
	case ResultCodeBusinessConflict:
		return "Prompt Preview 保存命令发生冲突。"
	case ResultCodeBusinessDisabled:
		return "Prompt Preview 开发预览当前不可用。"
	default:
		return "Prompt Preview 执行失败。"
	}
}

// clonePromptEntriesForCard 深拷贝 Prompt 条目并保留合法的非 null 空约束数组，避免投影修改权威资源。
func clonePromptEntriesForCard(values []PromptEntry) []PromptEntry {
	result := make([]PromptEntry, len(values))
	copy(result, values)
	for index := range result {
		result[index].NegativeConstraints = append([]string{}, values[index].NegativeConstraints...)
	}
	return result
}
