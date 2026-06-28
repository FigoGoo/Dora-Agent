package safety

import (
	"context"
	"strings"
	"time"
)

const (
	ResultPassed  = "passed"
	ResultBlocked = "blocked"
	ResultFailed  = "failed"
)

type Evidence struct {
	EvidenceID  string
	Scene       string
	TargetType  string
	TargetRefID string
	Result      string
	Reason      string
	EvaluatedAt time.Time
}

type Evaluator struct {
	BlockedTerms []string
	now          func() time.Time
}

func NewEvaluator(blockedTerms []string) Evaluator {
	return Evaluator{BlockedTerms: blockedTerms, now: func() time.Time { return time.Now().UTC() }}
}

func (e Evaluator) Evaluate(ctx context.Context, scene, targetType, targetRefID, text string) Evidence {
	_ = ctx
	normalized := strings.ToLower(text)
	result := ResultPassed
	reason := ""
	for _, term := range e.BlockedTerms {
		if term != "" && strings.Contains(normalized, strings.ToLower(term)) {
			result = ResultBlocked
			reason = "blocked_term"
			break
		}
	}
	return Evidence{EvidenceID: "safety_" + targetRefID, Scene: scene, TargetType: targetType, TargetRefID: targetRefID, Result: result, Reason: reason, EvaluatedAt: e.now()}
}
