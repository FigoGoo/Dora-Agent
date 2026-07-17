package analyzematerials

import (
	"context"

	"github.com/cloudwego/eino/compose"
)

// emitCompletedOrPartial 只接受 Validator 已通过的 Candidate，状态完全取自 deterministic coverage。
func (*graphBuilder) emitCompletedOrPartial(ctx context.Context, input analysisRoute) (Outcome, error) {
	if input.Route != routeCandidateValid || input.Candidate == nil || input.Failure != nil {
		return Outcome{}, contractErrorf(ResultCodeInternal, "emit analysis result: candidate route is invalid")
	}
	resultCode := ""
	switch input.Coverage.Status {
	case "completed":
		resultCode = ResultCodeCompleted
	case "partial":
		resultCode = ResultCodePartial
	default:
		return Outcome{}, contractErrorf(ResultCodeInternal, "emit analysis result: coverage status is invalid")
	}
	candidate := cloneCandidate(*input.Candidate)
	coverage := cloneCoverage(input.Coverage)
	result := Result{
		SchemaVersion: ResultSchemaVersion,
		Status:        input.Coverage.Status,
		ResultCode:    resultCode,
		Analysis:      &candidate,
		Coverage:      &coverage,
		EvidenceRefs:  evidenceRefs(input.Selected.Included),
		InvocationRef: InvocationRef{ToolCallID: input.Selected.ValidatedInput.TrustedContext.ToolCallID},
	}
	return storeResult(ctx, result)
}

// emitDependencyFailed 对零可分析 Evidence 返回安全失败，不携带 Candidate/Coverage/Evidence。
func (*graphBuilder) emitDependencyFailed(ctx context.Context, input analysisRoute) (Outcome, error) {
	if input.Route != routeDependencyNotReady || input.Failure == nil ||
		input.Failure.Code != ResultCodeDependencyNotReady {
		return Outcome{}, contractErrorf(ResultCodeInternal, "emit dependency failure: route is invalid")
	}
	retryable := false
	result := Result{
		SchemaVersion: ResultSchemaVersion,
		Status:        "failed",
		ResultCode:    ResultCodeDependencyNotReady,
		InvocationRef: InvocationRef{ToolCallID: input.Selected.ValidatedInput.TrustedContext.ToolCallID},
		Summary:       input.Failure.Summary,
		Retryable:     &retryable,
	}
	return storeResult(ctx, result)
}

// emitCandidateFailed 对 strict Validator 拒绝的模型候选返回安全失败，不泄露模型原文。
func (*graphBuilder) emitCandidateFailed(ctx context.Context, input analysisRoute) (Outcome, error) {
	if input.Route != routeCandidateInvalid || input.Failure == nil ||
		input.Failure.Code != ResultCodeModelOutputInvalid {
		return Outcome{}, contractErrorf(ResultCodeInternal, "emit candidate failure: route is invalid")
	}
	retryable := false
	result := Result{
		SchemaVersion: ResultSchemaVersion,
		Status:        "failed",
		ResultCode:    ResultCodeModelOutputInvalid,
		InvocationRef: InvocationRef{ToolCallID: input.Selected.ValidatedInput.TrustedContext.ToolCallID},
		Summary:       input.Failure.Summary,
		Retryable:     &retryable,
	}
	return storeResult(ctx, result)
}

func storeResult(ctx context.Context, result Result) (Outcome, error) {
	if err := ValidateResult(result); err != nil {
		return Outcome{}, newContractError(ResultCodeInternal, err)
	}
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		stored := CloneResult(result)
		state.Result = &stored
		return nil
	}); err != nil {
		return Outcome{}, err
	}
	return Outcome{Result: CloneResult(result)}, nil
}

func evidenceRefs(input []evidenceUnit) []EvidenceRef {
	result := make([]EvidenceRef, len(input))
	for index, unit := range input {
		result[index] = unit.Ref
	}
	return result
}
