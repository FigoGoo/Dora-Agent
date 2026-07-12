package orchestration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	errNoClaimMutation         = errors.New("no execution claim mutation applied")
	ErrExecutionClaimLost      = errors.New("execution claim lost")
	ErrExecutionFenceExhausted = errors.New("execution fence exhausted")
)

type executionClaim struct {
	StepID  string
	Attempt int
	Epoch   int64
	Owner   string
	Token   string
}

func (s *Scheduler) claimReady(ctx context.Context, run PlanRun) (PlanRun, []executionClaim, error) {
	current := run
	for range maxCASRetries {
		now := s.now()
		if !hasClaimableNode(current, now) {
			return current, nil, nil
		}
		var claims []executionClaim
		claimed, err := s.store.MutateRun(ctx, current.ID, current.Version, func(next *PlanRun) error {
			for _, step := range next.Plan.Steps {
				if len(claims) == s.maxParallel {
					break
				}
				node := next.Nodes[step.ID]
				if node == nil || !nodeClaimable(*next, step, node, now) {
					continue
				}
				if node.ExecutionEpoch == math.MaxInt64 {
					return fmt.Errorf("%w: run %q step %q", ErrExecutionFenceExhausted, next.ID, step.ID)
				}
				rawToken := strings.TrimSpace(s.newToken())
				if rawToken == "" {
					return errors.New("scheduler generated an empty execution token")
				}
				node.ExecutionEpoch++
				if node.Status == NodeStatusPending {
					node.Status = NodeStatusRunning
					if node.Attempt == 0 {
						node.Attempt = 1
					}
				}
				leaseUntil := now.Add(s.leaseTTL)
				node.ExecutionOwner = s.ownerID
				node.ExecutionToken = s.ownerID + ":" + rawToken
				node.LeaseUntil = &leaseUntil
				claims = append(claims, executionClaim{StepID: step.ID, Attempt: node.Attempt, Epoch: node.ExecutionEpoch, Owner: s.ownerID, Token: node.ExecutionToken})
			}
			if len(claims) == 0 {
				return errNoClaimMutation
			}
			return nil
		})
		if err == nil {
			return claimed, claims, nil
		}
		if errors.Is(err, errNoClaimMutation) {
			fresh, getErr := s.store.GetRun(ctx, current.ID)
			return fresh, nil, getErr
		}
		if !errors.Is(err, ErrRunVersionConflict) {
			return current, nil, err
		}
		current, err = s.store.GetRun(ctx, current.ID)
		if err != nil {
			return PlanRun{}, nil, err
		}
		if isTerminalRun(current.Status) || current.Status == RunStatusSuspended {
			return current, nil, nil
		}
	}
	return current, nil, fmt.Errorf("%w: claim exceeded retry limit", ErrRunVersionConflict)
}

func hasClaimableNode(run PlanRun, now time.Time) bool {
	for _, step := range run.Plan.Steps {
		if nodeClaimable(run, step, run.Nodes[step.ID], now) {
			return true
		}
	}
	return false
}

func nodeClaimable(run PlanRun, step PlanStep, node *NodeRun, now time.Time) bool {
	if node == nil {
		return false
	}
	if node.Status == NodeStatusRunning {
		return node.ExecutionToken != "" && node.LeaseUntil != nil && !node.LeaseUntil.After(now)
	}
	if node.Status != NodeStatusPending {
		return false
	}
	for _, dependency := range step.DependsOn {
		dependencyRun := run.Nodes[dependency]
		if dependencyRun == nil || (dependencyRun.Status != NodeStatusSucceeded && dependencyRun.Status != NodeStatusSkipped) {
			return false
		}
	}
	return true
}

func (s *Scheduler) renewClaims(ctx context.Context, runID string, claims []executionClaim) error {
	now := s.now()
	renewed, err := s.mutateMatchingClaims(ctx, runID, claims, func(node *NodeRun) bool {
		return node.LeaseUntil != nil && node.LeaseUntil.After(now)
	}, func(node *NodeRun) {
		leaseUntil := now.Add(s.leaseTTL)
		node.LeaseUntil = &leaseUntil
	})
	if err != nil {
		return err
	}
	for _, claim := range claims {
		node := renewed.Nodes[claim.StepID]
		if !claimMatches(node, claim) || node.LeaseUntil == nil || !node.LeaseUntil.After(now) {
			return fmt.Errorf("%w: run %q step %q epoch %d", ErrExecutionClaimLost, runID, claim.StepID, claim.Epoch)
		}
	}
	return nil
}

func (s *Scheduler) releaseClaims(ctx context.Context, runID string, claims []executionClaim) (PlanRun, error) {
	return s.mutateMatchingClaims(ctx, runID, claims, nil, func(node *NodeRun) {
		node.Status = NodeStatusPending
		clearExecutionClaim(node)
	})
}

func (s *Scheduler) mutateMatchingClaims(ctx context.Context, runID string, claims []executionClaim, eligible func(*NodeRun) bool, mutate func(*NodeRun)) (PlanRun, error) {
	current, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return PlanRun{}, err
	}
	for range maxCASRetries {
		changed, mutateErr := s.store.MutateRun(ctx, runID, current.Version, func(next *PlanRun) error {
			applied := 0
			for _, claim := range claims {
				node := next.Nodes[claim.StepID]
				if !claimMatches(node, claim) || (eligible != nil && !eligible(node)) {
					continue
				}
				mutate(node)
				applied++
			}
			if applied == 0 {
				return errNoClaimMutation
			}
			return nil
		})
		if mutateErr == nil {
			return changed, nil
		}
		if errors.Is(mutateErr, errNoClaimMutation) {
			return current, nil
		}
		if !errors.Is(mutateErr, ErrRunVersionConflict) {
			return current, mutateErr
		}
		current, err = s.store.GetRun(ctx, runID)
		if err != nil {
			return PlanRun{}, err
		}
	}
	return current, fmt.Errorf("%w: execution claim mutation exceeded retry limit", ErrRunVersionConflict)
}

func claimMatches(node *NodeRun, claim executionClaim) bool {
	return node != nil && node.Status == NodeStatusRunning && node.Attempt == claim.Attempt && node.ExecutionEpoch == claim.Epoch &&
		node.ExecutionOwner == claim.Owner && node.ExecutionToken == claim.Token && claim.Token != ""
}

func clearExecutionClaim(node *NodeRun) {
	node.ExecutionOwner = ""
	node.ExecutionToken = ""
	node.LeaseUntil = nil
}

func hasActiveExecutionClaim(run PlanRun) bool {
	for _, node := range run.Nodes {
		if node != nil && node.Status == NodeStatusRunning && node.ExecutionToken != "" {
			return true
		}
	}
	return false
}
