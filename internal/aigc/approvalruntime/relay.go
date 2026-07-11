package approvalruntime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
)

// RelayOnce drains durable approval outbox rows. Repeated calls are safe.
func (s *Service) RelayOnce(ctx context.Context, limit int) (int, error) {
	rows, err := s.cfg.Approvals.ListOutbox(ctx, approval.OutboxStatusPending, 0)
	if err != nil {
		return 0, err
	}
	published := 0
	var relayErr error
	now := s.cfg.Now()
	due := 0
	for _, event := range rows {
		if event.AvailableAt.After(now) {
			continue
		}
		if limit > 0 && due >= limit {
			break
		}
		due++
		processed := true
		continuationBusy := false
		deferUntil := time.Time{}
		switch event.EventType {
		case approval.EventSessionInputRequested:
			if s.cfg.Inputs == nil {
				relayErr = errors.Join(relayErr, errors.New("session input enqueuer is required"))
				processed = false
				break
			}
			var input sessionruntime.ResumeRequested
			if err := json.Unmarshal(event.Payload, &input); err != nil {
				relayErr = errors.Join(relayErr, err)
				processed = false
				break
			}
			if _, err := s.cfg.Inputs.Enqueue(ctx, event.SessionID, input); err != nil {
				relayErr = errors.Join(relayErr, err)
				processed = false
			}
		case approval.EventApprovalContinuationRequested:
			var request approval.ApprovalContinuationRequested
			if err := json.Unmarshal(event.Payload, &request); err != nil {
				relayErr = errors.Join(relayErr, err)
				processed = false
				break
			}
			continuation, err := s.cfg.Continuations.GetContinuation(ctx, request.ApprovalID, request.DecisionVersion)
			if err != nil {
				relayErr = errors.Join(relayErr, err)
				processed = false
				break
			}
			applied, err := s.Apply(ctx, continuation)
			if err != nil {
				relayErr = errors.Join(relayErr, err)
				processed = false
				break
			}
			if !applied {
				latest, latestErr := s.GetContinuation(ctx, request.ApprovalID, request.DecisionVersion)
				if latestErr != nil {
					relayErr = errors.Join(relayErr, latestErr)
					processed = false
					break
				}
				if latest.Status == sessionruntime.ContinuationStatusApplied {
					break
				}
				processed = false
				continuationBusy = true
				if latest.LeaseUntil != nil {
					deferUntil = latest.LeaseUntil.UTC()
				}
			}
		case approval.EventApprovalFallbackEnabled:
			// A pre-decision fallback only updates routing/card state.
		default:
			processed = false
		}
		if !processed {
			markAt, maxAttempts := s.cfg.Now(), 10
			if continuationBusy {
				// An active claim is coordination, not a failed delivery. Never dead
				// letter the sole continuation wake-up merely because another owner
				// is still inside its lease; retry just after that lease expires.
				maxAttempts = 0
				if deferUntil.After(markAt) {
					markAt = deferUntil
				}
			}
			if markErr := s.cfg.Approvals.MarkOutboxFailed(ctx, event.ID, markAt, maxAttempts); markErr != nil {
				relayErr = errors.Join(relayErr, markErr)
			}
			continue
		}
		if err := s.cfg.Approvals.MarkOutboxPublished(ctx, event.ID, s.cfg.Now()); err != nil {
			relayErr = errors.Join(relayErr, err)
			continue
		}
		published++
	}
	return published, relayErr
}

func (s *Service) RunRelay(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.RelayOnce(ctx, 100); err != nil && ctx.Err() == nil {
			slog.Error("approval outbox relay iteration failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
