package vocabulary

import (
	"context"
	"sort"
	"strings"
)

const (
	guardPermissionDenied = "permission_denied"
	guardHardBlock        = "compliance_hard_block"
	guardCancelled        = "guard_cancelled"
)

// Guard is the narrow policy hook used before media tool execution.
type Guard interface {
	Check(context.Context, Call) Result
}

type GuardConfig struct {
	HardTerms []string
	SoftTerms []string
}

// GuardChain applies the fixed R1 order: permission, compliance, credit reserve.
type GuardChain struct {
	hardTerms []string
	softTerms []string
}

func NewGuardChain(cfg GuardConfig) *GuardChain {
	return &GuardChain{
		hardTerms: normalizeGuardTerms(cfg.HardTerms),
		softTerms: normalizeGuardTerms(cfg.SoftTerms),
	}
}

func (g *GuardChain) Check(ctx context.Context, call Call) Result {
	if strings.TrimSpace(call.SessionID) == "" || strings.TrimSpace(call.UserID) == "" {
		return guardFailure(guardPermissionDenied, "media generation requires a session and user")
	}
	if err := ctx.Err(); err != nil {
		return guardFailure(guardCancelled, err.Error())
	}

	values := guardStringValues(call.Inputs)
	if term := firstMatchedTerm(g.hardTerms, values); term != "" {
		return guardFailure(guardHardBlock, "media input violates a mandatory compliance rule")
	}
	if term := firstMatchedTerm(g.softTerms, values); term != "" {
		return Result{Suspension: &Suspension{
			Reason: "waiting_user",
			Payload: map[string]any{
				"message":      "This media request may be sensitive. Confirm before continuing.",
				"matched_term": term,
				"decision_key": "approved",
				"options":      []any{true, false},
			},
		}}
	}

	// R1 credit reservation is intentionally a pass-through after compliance.
	return Result{}
}

func guardFailure(code, message string) Result {
	return Result{Fail: &Failure{Code: code, Message: message}}
}

func normalizeGuardTerms(terms []string) []string {
	normalized := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term != "" {
			normalized = append(normalized, term)
		}
	}
	return normalized
}

func firstMatchedTerm(terms, values []string) string {
	for _, term := range terms {
		for _, value := range values {
			if strings.Contains(value, term) {
				return term
			}
		}
	}
	return ""
}

func guardStringValues(value any) []string {
	values := make([]string, 0)
	collectGuardStrings(value, &values)
	return values
}

func collectGuardStrings(value any, values *[]string) {
	switch typed := value.(type) {
	case string:
		*values = append(*values, strings.ToLower(typed))
	case []any:
		for _, item := range typed {
			collectGuardStrings(item, values)
		}
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			collectGuardStrings(typed[key], values)
		}
	}
}
