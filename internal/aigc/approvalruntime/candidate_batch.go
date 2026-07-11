package approvalruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

var (
	ErrCandidateBatchStoryboardVersion = errors.New("candidate approval batch storyboard version conflict")
	ErrCandidateBatchGenerationRunning = errors.New("candidate approval batch generation jobs are not terminal")
	ErrNoPendingCandidateApprovals     = errors.New("no pending candidate approvals")
)

type CandidateBatchApproveRequest struct {
	SessionID                 string `json:"session_id"`
	StoryboardID              string `json:"storyboard_id"`
	ExpectedStoryboardVersion int    `json:"expected_storyboard_version"`
	IdempotencyKey            string `json:"idempotency_key"`
	Decision                  string `json:"decision"`
	ActorID                   string `json:"actor_id,omitempty"`
	Reason                    string `json:"reason,omitempty"`
}

type CandidateBatchItemResult struct {
	ApprovalID      string          `json:"approval_id"`
	BindingID       string          `json:"binding_id"`
	Status          approval.Status `json:"status"`
	DecisionVersion int             `json:"decision_version,omitempty"`
	Applied         bool            `json:"applied"`
	Retryable       bool            `json:"retryable"`
	Error           string          `json:"error,omitempty"`
}

type CandidateBatchSummary struct {
	Total       int  `json:"total"`
	Approved    int  `json:"approved"`
	Stale       int  `json:"stale"`
	Conflicts   int  `json:"conflict"`
	Failed      int  `json:"failed"`
	Complete    bool `json:"complete"`
	AllApproved bool `json:"all_approved"`
}

type CandidateBatchApproveResult struct {
	Batch      approval.CandidateApprovalBatch `json:"batch"`
	Summary    CandidateBatchSummary           `json:"summary"`
	Results    []CandidateBatchItemResult      `json:"results"`
	Storyboard storyboard.StoryboardAggregate  `json:"storyboard"`
}

// ApproveCandidateBatch is a recoverable saga over the existing one-approval
// decision transaction. The durable batch freezes the target set. Each child
// uses a stable idempotency key, so a retry resumes command/continuation work
// without repeating a domain mutation or including newer candidates.
func (s *Service) ApproveCandidateBatch(ctx context.Context, request CandidateBatchApproveRequest) (CandidateBatchApproveResult, error) {
	request = normalizeCandidateBatchApproveRequest(request)
	if request.SessionID == "" || request.StoryboardID == "" || request.IdempotencyKey == "" {
		return CandidateBatchApproveResult{}, fmt.Errorf("session id, storyboard id and idempotency key are required")
	}
	if request.ExpectedStoryboardVersion <= 0 {
		return CandidateBatchApproveResult{}, fmt.Errorf("expected_storyboard_version must be positive")
	}
	if request.Decision != approval.DecisionApprove {
		return CandidateBatchApproveResult{}, fmt.Errorf("candidate approval batch only supports approved")
	}
	if s == nil || s.cfg.Approvals == nil || s.cfg.Storyboards == nil {
		return CandidateBatchApproveResult{}, fmt.Errorf("approval and storyboard stores are required")
	}

	batch, err := s.loadOrCreateCandidateBatch(ctx, request)
	if err != nil {
		return CandidateBatchApproveResult{}, err
	}
	if len(batch.Targets) == 0 {
		return CandidateBatchApproveResult{}, ErrNoPendingCandidateApprovals
	}
	result := CandidateBatchApproveResult{
		Batch:   batch,
		Results: make([]CandidateBatchItemResult, 0, len(batch.Targets)),
		Summary: CandidateBatchSummary{Total: len(batch.Targets)},
	}
	for _, target := range batch.Targets {
		item := CandidateBatchItemResult{ApprovalID: target.ApprovalID, BindingID: target.BindingID}
		decided, decideErr := s.Decide(ctx, DecideRequest{
			ApprovalID: target.ApprovalID, ExpectedDecisionVersion: target.ExpectedDecisionVersion,
			IdempotencyKey: candidateBatchChildKey(batch.ID, target.ApprovalID),
			Decision:       batch.Decision, ActorID: batch.ActorID, Reason: batch.Reason,
		})
		if decideErr != nil {
			item.Error = decideErr.Error()
			if errors.Is(decideErr, approval.ErrAlreadyDecided) || errors.Is(decideErr, approval.ErrVersionConflict) || errors.Is(decideErr, approval.ErrIdempotencyConflict) {
				item.Status = "conflict"
				result.Summary.Conflicts++
			} else {
				item.Status = "failed"
				item.Retryable = !errors.Is(decideErr, approval.ErrNotFound)
				result.Summary.Failed++
			}
			result.Results = append(result.Results, item)
			continue
		}
		item.Status = decided.Decision.Approval.Status
		item.DecisionVersion = decided.Decision.Approval.DecisionVersion
		item.Applied = decided.Applied
		switch item.Status {
		case approval.StatusApproved:
			result.Summary.Approved++
		case approval.StatusStale, approval.StatusExpired, approval.StatusCancelled:
			result.Summary.Stale++
		default:
			result.Summary.Conflicts++
		}
		result.Results = append(result.Results, item)
	}
	result.Summary.Complete = result.Summary.Failed == 0
	result.Summary.AllApproved = result.Summary.Approved == result.Summary.Total && result.Summary.Complete
	latest, err := s.cfg.Storyboards.GetAggregate(ctx, batch.StoryboardID)
	if err != nil {
		return result, err
	}
	result.Storyboard = latest.PublicView()
	return result, nil
}

func (s *Service) loadOrCreateCandidateBatch(ctx context.Context, request CandidateBatchApproveRequest) (approval.CandidateApprovalBatch, error) {
	if existing, err := s.cfg.Approvals.GetCandidateApprovalBatchByKey(ctx, request.IdempotencyKey); err == nil {
		return validateCandidateBatchReplay(existing, request)
	} else if !errors.Is(err, approval.ErrNotFound) {
		return approval.CandidateApprovalBatch{}, err
	}
	if err := s.ensureCandidateGenerationTerminal(ctx, request.SessionID, request.StoryboardID); err != nil {
		return approval.CandidateApprovalBatch{}, err
	}

	aggregate, err := s.cfg.Storyboards.GetAggregate(ctx, request.StoryboardID)
	if err != nil {
		return approval.CandidateApprovalBatch{}, err
	}
	if aggregate.SessionID != request.SessionID {
		return approval.CandidateApprovalBatch{}, storyboard.ErrAggregateNotFound
	}
	if aggregate.Version != request.ExpectedStoryboardVersion {
		// Close the small race where another identical request persisted the
		// frozen batch and started applying it after our initial lookup.
		if existing, lookupErr := s.cfg.Approvals.GetCandidateApprovalBatchByKey(ctx, request.IdempotencyKey); lookupErr == nil {
			return validateCandidateBatchReplay(existing, request)
		}
		return approval.CandidateApprovalBatch{}, fmt.Errorf("%w: current=%d expected=%d", ErrCandidateBatchStoryboardVersion, aggregate.Version, request.ExpectedStoryboardVersion)
	}
	targets, err := s.pendingCandidateBatchTargets(ctx, aggregate)
	if err != nil {
		return approval.CandidateApprovalBatch{}, err
	}
	if len(targets) == 0 {
		return approval.CandidateApprovalBatch{}, ErrNoPendingCandidateApprovals
	}
	requested := approval.CandidateApprovalBatch{
		ID:             stableCandidateBatchID(request.SessionID, request.StoryboardID, request.IdempotencyKey),
		IdempotencyKey: request.IdempotencyKey, SessionID: request.SessionID, StoryboardID: request.StoryboardID,
		ExpectedStoryboardVersion: request.ExpectedStoryboardVersion, Decision: request.Decision,
		ActorID: request.ActorID, Reason: request.Reason, Targets: targets,
	}
	created, err := s.cfg.Approvals.CreateCandidateApprovalBatch(ctx, requested)
	if err != nil {
		if errors.Is(err, approval.ErrIdempotencyConflict) {
			if existing, lookupErr := s.cfg.Approvals.GetCandidateApprovalBatchByKey(ctx, request.IdempotencyKey); lookupErr == nil {
				return validateCandidateBatchReplay(existing, request)
			}
		}
		return approval.CandidateApprovalBatch{}, err
	}
	return created.Batch, nil
}

func (s *Service) ensureCandidateGenerationTerminal(ctx context.Context, sessionID, storyboardID string) error {
	if s.cfg.GenerationJobs == nil {
		return fmt.Errorf("generation job source is required for candidate approval batches")
	}
	jobs, err := s.cfg.GenerationJobs.ListBySession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("list candidate generation jobs: %w", err)
	}
	pending := make([]string, 0)
	for _, job := range jobs {
		jobStoryboardID := strings.TrimSpace(job.StoryboardID)
		if jobStoryboardID == "" {
			jobStoryboardID = strings.TrimSpace(job.BindingToken.StoryboardID)
		}
		if jobStoryboardID != storyboardID || generation.IsTerminalJobStatus(job.Status) {
			continue
		}
		pending = append(pending, fmt.Sprintf("%s(%s)", job.ID, job.Status))
	}
	if len(pending) > 0 {
		return fmt.Errorf("%w: %s", ErrCandidateBatchGenerationRunning, strings.Join(pending, ", "))
	}
	return nil
}

func (s *Service) pendingCandidateBatchTargets(ctx context.Context, aggregate storyboard.StoryboardAggregate) ([]approval.CandidateApprovalBatchTarget, error) {
	targets := make([]approval.CandidateApprovalBatchTarget, 0)
	for _, binding := range aggregate.Bindings {
		if binding.State != storyboard.BindingStateCandidate || strings.TrimSpace(binding.ApprovalID) == "" {
			continue
		}
		record, err := s.cfg.Approvals.Get(ctx, binding.ApprovalID)
		if err != nil {
			return nil, fmt.Errorf("load candidate approval %s: %w", binding.ApprovalID, err)
		}
		if record.SessionID != aggregate.SessionID || record.ArtifactType != "candidate_asset" ||
			record.Binding.StoryboardID != aggregate.ID || record.Binding.ArtifactID != binding.ID {
			return nil, fmt.Errorf("candidate approval %s does not match storyboard binding %s", record.ID, binding.ID)
		}
		if record.Status != approval.StatusPending {
			continue
		}
		targets = append(targets, approval.CandidateApprovalBatchTarget{
			ApprovalID: record.ID, BindingID: binding.ID, ExpectedDecisionVersion: record.DecisionVersion,
		})
	}
	return targets, nil
}

func validateCandidateBatchReplay(batch approval.CandidateApprovalBatch, request CandidateBatchApproveRequest) (approval.CandidateApprovalBatch, error) {
	if batch.SessionID != request.SessionID || batch.StoryboardID != request.StoryboardID ||
		batch.ExpectedStoryboardVersion != request.ExpectedStoryboardVersion || batch.Decision != request.Decision ||
		batch.ActorID != request.ActorID || batch.Reason != request.Reason {
		return approval.CandidateApprovalBatch{}, fmt.Errorf("%w: candidate approval batch request changed", approval.ErrIdempotencyConflict)
	}
	return batch, nil
}

func normalizeCandidateBatchApproveRequest(request CandidateBatchApproveRequest) CandidateBatchApproveRequest {
	request.SessionID = strings.TrimSpace(request.SessionID)
	request.StoryboardID = strings.TrimSpace(request.StoryboardID)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.Decision = strings.ToLower(strings.TrimSpace(request.Decision))
	request.ActorID = strings.TrimSpace(request.ActorID)
	request.Reason = strings.TrimSpace(request.Reason)
	return request
}

func stableCandidateBatchID(sessionID, storyboardID, key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sessionID) + "\x00" + strings.TrimSpace(storyboardID) + "\x00" + strings.TrimSpace(key)))
	return "candidate-batch:" + hex.EncodeToString(sum[:16])
}

func candidateBatchChildKey(batchID, approvalID string) string {
	return strings.TrimSpace(batchID) + ":approval:" + strings.TrimSpace(approvalID)
}
