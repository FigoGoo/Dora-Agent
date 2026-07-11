package approval

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// CandidateApprovalBatch freezes the exact candidate approvals selected by a
// session/storyboard-level confirmation. Individual Approval decisions remain
// the durable business truth; this record makes a partially applied batch
// recoverable without accidentally including candidates created after the
// user's click.
type CandidateApprovalBatch struct {
	ID                        string                         `json:"id"`
	IdempotencyKey            string                         `json:"idempotency_key"`
	SessionID                 string                         `json:"session_id"`
	StoryboardID              string                         `json:"storyboard_id"`
	ExpectedStoryboardVersion int                            `json:"expected_storyboard_version"`
	Decision                  string                         `json:"decision"`
	ActorID                   string                         `json:"actor_id,omitempty"`
	Reason                    string                         `json:"reason,omitempty"`
	Targets                   []CandidateApprovalBatchTarget `json:"targets"`
	CreatedAt                 time.Time                      `json:"created_at"`
}

type CandidateApprovalBatchTarget struct {
	ApprovalID              string `json:"approval_id"`
	BindingID               string `json:"binding_id"`
	ExpectedDecisionVersion int    `json:"expected_decision_version"`
}

type CandidateApprovalBatchCreateResult struct {
	Batch   CandidateApprovalBatch `json:"batch"`
	Created bool                   `json:"created"`
}

func normalizeCandidateApprovalBatch(value CandidateApprovalBatch, now time.Time) (CandidateApprovalBatch, error) {
	value.ID = strings.TrimSpace(value.ID)
	value.IdempotencyKey = strings.TrimSpace(value.IdempotencyKey)
	value.SessionID = strings.TrimSpace(value.SessionID)
	value.StoryboardID = strings.TrimSpace(value.StoryboardID)
	value.Decision = strings.ToLower(strings.TrimSpace(value.Decision))
	value.ActorID = strings.TrimSpace(value.ActorID)
	value.Reason = strings.TrimSpace(value.Reason)
	if value.ID == "" || value.IdempotencyKey == "" || value.SessionID == "" || value.StoryboardID == "" {
		return CandidateApprovalBatch{}, fmt.Errorf("candidate approval batch id, idempotency key, session id and storyboard id are required")
	}
	if value.ExpectedStoryboardVersion <= 0 {
		return CandidateApprovalBatch{}, fmt.Errorf("candidate approval batch expected storyboard version must be positive")
	}
	if value.Decision != DecisionApprove {
		return CandidateApprovalBatch{}, fmt.Errorf("candidate approval batch only supports approved")
	}
	if len(value.Targets) == 0 {
		return CandidateApprovalBatch{}, fmt.Errorf("candidate approval batch requires at least one target")
	}
	seenApprovals := make(map[string]struct{}, len(value.Targets))
	seenBindings := make(map[string]struct{}, len(value.Targets))
	for index := range value.Targets {
		target := &value.Targets[index]
		target.ApprovalID = strings.TrimSpace(target.ApprovalID)
		target.BindingID = strings.TrimSpace(target.BindingID)
		if target.ApprovalID == "" || target.BindingID == "" || target.ExpectedDecisionVersion < 0 {
			return CandidateApprovalBatch{}, fmt.Errorf("candidate approval batch target requires approval id, binding id and a non-negative decision version")
		}
		if _, duplicate := seenApprovals[target.ApprovalID]; duplicate {
			return CandidateApprovalBatch{}, fmt.Errorf("candidate approval batch has duplicate approval %s", target.ApprovalID)
		}
		if _, duplicate := seenBindings[target.BindingID]; duplicate {
			return CandidateApprovalBatch{}, fmt.Errorf("candidate approval batch has duplicate binding %s", target.BindingID)
		}
		seenApprovals[target.ApprovalID] = struct{}{}
		seenBindings[target.BindingID] = struct{}{}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	value.CreatedAt = now
	value.Targets = slices.Clone(value.Targets)
	return value, nil
}

func sameCandidateApprovalBatch(a, b CandidateApprovalBatch) bool {
	return a.ID == b.ID && a.IdempotencyKey == b.IdempotencyKey && a.SessionID == b.SessionID &&
		a.StoryboardID == b.StoryboardID && a.ExpectedStoryboardVersion == b.ExpectedStoryboardVersion &&
		a.Decision == b.Decision && a.ActorID == b.ActorID && a.Reason == b.Reason && slices.Equal(a.Targets, b.Targets)
}

func cloneCandidateApprovalBatch(value CandidateApprovalBatch) CandidateApprovalBatch {
	value.Targets = slices.Clone(value.Targets)
	return value
}
