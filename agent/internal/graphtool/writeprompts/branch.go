package writeprompts

import (
	"context"
	"fmt"
)

// routeScope 只根据确定性 Scope 校验结果选择 Prompt 或独立失败终点；未知路由值视为 Graph 契约错误。
func (*graphBuilder) routeScope(_ context.Context, input targetRoute) (string, error) {
	switch input.Route {
	case routeValid:
		return nodeBuildPrompt, nil
	case routeInvalid:
		return nodeEmitScopeFailed, nil
	default:
		return "", fmt.Errorf("route prompt preview scope: invalid route")
	}
}

// routeCandidate 只根据候选协议 Validator 结果选择 exact-set 校验或独立失败终点；不修改 State 或产生副作用。
func (*graphBuilder) routeCandidate(_ context.Context, input candidateRoute) (string, error) {
	switch input.Route {
	case routeValid:
		return nodeValidateExactSet, nil
	case routeInvalid:
		return nodeEmitCandidateFailed, nil
	default:
		return "", fmt.Errorf("route prompt preview candidate: invalid route")
	}
}

// routeExactSet 只允许目标全集通过的 Content 进入保存节点；未知路由值失败关闭，避免部分 Prompt 被保存。
func (*graphBuilder) routeExactSet(_ context.Context, input contentRoute) (string, error) {
	switch input.Route {
	case routeValid:
		return nodeSavePromptDraft, nil
	case routeInvalid:
		return nodeEmitExactSetFailed, nil
	default:
		return "", fmt.Errorf("route prompt preview exact set: invalid route")
	}
}

// routeSave 把确定成功交给结果节点，把响应未知交给原命令查询；其他状态表示 Command 契约损坏。
func (*graphBuilder) routeSave(_ context.Context, input SaveOutcome) (string, error) {
	switch input.Status {
	case routeSaved:
		return nodeBuildSavedResult, nil
	case routeUnknown:
		return nodeQuerySaveReceipt, nil
	default:
		return "", fmt.Errorf("route prompt preview save: invalid status")
	}
}

// routeQuery 只接受权威 completed 或内部 recovery_pending；查询不会生成新命令或重发保存。
func (*graphBuilder) routeQuery(_ context.Context, input SaveOutcome) (string, error) {
	switch input.Status {
	case routeSaved:
		return nodeBuildQueriedResult, nil
	case routeRecoveryPending:
		return nodeDeferRecovery, nil
	default:
		return "", fmt.Errorf("route prompt preview query: invalid status")
	}
}
