package analyzematerials

import "context"

const (
	routeAnalyze            = "analyze"
	routeDependencyNotReady = "dependency_not_ready"
	routeCandidateValid     = "valid"
	routeCandidateInvalid   = "invalid"
)

// routeEvidenceGate 只接受设计冻结的 evidence gate 判别值。
func (*graphBuilder) routeEvidenceGate(_ context.Context, input analysisRoute) (string, error) {
	switch input.Route {
	case routeAnalyze:
		return nodeBuildPrimaryPrompt, nil
	case routeDependencyNotReady:
		return nodeEmitDependencyFailed, nil
	default:
		return "", contractErrorf(ResultCodeInternal, "%s: unknown route", branchRouteEvidenceGate)
	}
}

// routeCandidateValidation 只接受独立 Validator 产生的合法/非法判别值。
func (*graphBuilder) routeCandidateValidation(_ context.Context, input analysisRoute) (string, error) {
	switch input.Route {
	case routeCandidateValid:
		return nodeEmitCompletedOrPartial, nil
	case routeCandidateInvalid:
		return nodeEmitCandidateFailed, nil
	default:
		return "", contractErrorf(ResultCodeInternal, "%s: unknown route", branchRouteCandidateValidation)
	}
}
