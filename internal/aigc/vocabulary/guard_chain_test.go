package vocabulary

import (
	"context"
	"testing"
)

func TestGuardChainHardSoftAndSafe(t *testing.T) {
	chain := NewGuardChain(GuardConfig{HardTerms: []string{"hard-ban"}, SoftTerms: []string{"soft-risk"}})
	base := Call{SessionID: "session-1", UserID: "user-1"}

	t.Run("hard term wins across input fields and case", func(t *testing.T) {
		call := base
		call.Inputs = map[string]any{"prompt": "safe", "nested": map[string]any{"caption": "contains HARD-BAN"}}
		result := chain.Check(context.Background(), call)
		if result.Fail == nil || result.Fail.Code != "compliance_hard_block" || result.Outputs != nil || result.Suspension != nil {
			t.Fatalf("result=%+v", result)
		}
	})

	t.Run("hard term has priority over soft term", func(t *testing.T) {
		call := base
		call.Inputs = map[string]any{"prompt": "soft-risk and hard-ban"}
		result := chain.Check(context.Background(), call)
		if result.Fail == nil || result.Fail.Code != "compliance_hard_block" || result.Suspension != nil {
			t.Fatalf("result=%+v", result)
		}
	})

	t.Run("soft term asks for user confirmation", func(t *testing.T) {
		call := base
		call.Inputs = map[string]any{"prompt": "contains SOFT-RISK", "count": 3, "enabled": true}
		result := chain.Check(context.Background(), call)
		if result.Fail != nil || result.Outputs != nil || result.Suspension == nil || result.Suspension.Reason != "waiting_user" {
			t.Fatalf("result=%+v", result)
		}
		if result.Suspension.Payload["message"] == "" || result.Suspension.Payload["matched_term"] != "soft-risk" {
			t.Fatalf("payload=%+v", result.Suspension.Payload)
		}
	})

	t.Run("safe input passes", func(t *testing.T) {
		call := base
		call.Inputs = map[string]any{"prompt": "safe prompt", "items": []any{"also safe", 42}}
		result := chain.Check(context.Background(), call)
		if result.Fail != nil || result.Suspension != nil {
			t.Fatalf("result=%+v", result)
		}
	})
}

func TestGuardChainPermissionShortCircuitsAndRulesAreImmutable(t *testing.T) {
	hard := []string{"hard-ban"}
	soft := []string{"soft-risk"}
	chain := NewGuardChain(GuardConfig{HardTerms: hard, SoftTerms: soft})
	hard[0] = "changed-hard"
	soft[0] = "changed-soft"

	missingPermission := chain.Check(context.Background(), Call{Inputs: map[string]any{"prompt": "hard-ban soft-risk"}})
	if missingPermission.Fail == nil || missingPermission.Fail.Code != "permission_denied" || missingPermission.Suspension != nil {
		t.Fatalf("permission result=%+v", missingPermission)
	}

	allowed := Call{SessionID: "session-1", UserID: "user-1", Inputs: map[string]any{"prompt": "hard-ban"}}
	result := chain.Check(context.Background(), allowed)
	if result.Fail == nil || result.Fail.Code != "compliance_hard_block" {
		t.Fatalf("rules changed through caller slices: %+v", result)
	}
}

func TestGuardChainCancelledContextIsStableFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := NewGuardChain(GuardConfig{}).Check(ctx, Call{SessionID: "session-1", UserID: "user-1"})
	if result.Fail == nil || result.Fail.Code != "guard_cancelled" || result.Outputs != nil || result.Suspension != nil {
		t.Fatalf("result=%+v", result)
	}
}
