package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

var (
	ErrRunNotFound        = errors.New("plan run not found")
	ErrRunVersionConflict = errors.New("plan run version conflict")
	ErrRunNotSerializable = errors.New("plan run is not serializable")
)

const (
	RunStatusDraft            = "draft"
	RunStatusRunning          = "running"
	RunStatusSuspended        = "suspended"
	RunStatusSucceeded        = "succeeded"
	RunStatusPartialSucceeded = "partial_succeeded"
	RunStatusFailed           = "failed"
	RunStatusCancelled        = "cancelled"

	SuspendWaitingUser  = "waiting_user"
	SuspendWaitingAgent = "waiting_agent"
	SuspendWaitingJobs  = "waiting_jobs"

	NodeStatusPending   = "pending"
	NodeStatusRunning   = "running"
	NodeStatusSucceeded = "succeeded"
	NodeStatusFailed    = "failed"
	NodeStatusSkipped   = "skipped"
)

type NodeRun struct {
	StepID         string                 `json:"step_id"`
	Status         string                 `json:"status"`
	Attempt        int                    `json:"attempt"`
	Outputs        map[string]any         `json:"outputs,omitempty"`
	Fail           *vocabulary.Failure    `json:"fail,omitempty"`
	Suspension     *vocabulary.Suspension `json:"suspension,omitempty"`
	ResumeKey      string                 `json:"resume_key,omitempty"`
	Resumed        bool                   `json:"resumed,omitempty"`
	ResumeDecision map[string]any         `json:"resume_decision,omitempty"`
}

type PlanRun struct {
	ID              string              `json:"id"`
	SessionID       string              `json:"session_id"`
	UserID          string              `json:"user_id"`
	Plan            ExecutionPlan       `json:"plan"`
	Status          string              `json:"status"`
	SuspendReason   string              `json:"suspend_reason,omitempty"`
	SuspendedNodeID string              `json:"suspended_node_id,omitempty"`
	PreviewRequired bool                `json:"preview_required,omitempty"`
	ResumeKey       string              `json:"resume_key,omitempty"`
	Resumed         bool                `json:"resumed,omitempty"`
	ResumeDecision  map[string]any      `json:"resume_decision,omitempty"`
	Nodes           map[string]*NodeRun `json:"nodes"`
	Version         int                 `json:"version"`
}

type RunStore interface {
	CreateRun(ctx context.Context, run PlanRun) (PlanRun, error)
	GetRun(ctx context.Context, id string) (PlanRun, error)
	MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error)
}

func ValidateRunTransition(from, to string) error {
	if !knownRunStatus(from) || !knownRunStatus(to) {
		return fmt.Errorf("invalid plan run status transition %q -> %q", from, to)
	}
	if from == to {
		return nil
	}
	legal := map[string]map[string]bool{
		RunStatusDraft: {
			RunStatusRunning:   true,
			RunStatusSuspended: true,
			RunStatusCancelled: true,
		},
		RunStatusRunning: {
			RunStatusSuspended:        true,
			RunStatusSucceeded:        true,
			RunStatusPartialSucceeded: true,
			RunStatusFailed:           true,
			RunStatusCancelled:        true,
		},
		RunStatusSuspended: {
			RunStatusRunning:   true,
			RunStatusCancelled: true,
		},
	}
	if legal[from][to] {
		return nil
	}
	return fmt.Errorf("invalid plan run status transition %q -> %q", from, to)
}

func knownRunStatus(status string) bool {
	switch status {
	case RunStatusDraft, RunStatusRunning, RunStatusSuspended, RunStatusSucceeded,
		RunStatusPartialSucceeded, RunStatusFailed, RunStatusCancelled:
		return true
	default:
		return false
	}
}

type MemoryRunStore struct {
	mu   sync.Mutex
	runs map[string]PlanRun
}

func NewMemoryRunStore() *MemoryRunStore {
	return &MemoryRunStore{runs: make(map[string]PlanRun)}
}

func (s *MemoryRunStore) CreateRun(ctx context.Context, run PlanRun) (PlanRun, error) {
	if err := ctx.Err(); err != nil {
		return PlanRun{}, err
	}
	if run.ID == "" {
		return PlanRun{}, errors.New("plan run id is required")
	}
	if err := ValidateRunTransition(run.Status, run.Status); err != nil {
		return PlanRun{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.runs[run.ID]; exists {
		return PlanRun{}, fmt.Errorf("%w: run %q already exists", ErrRunVersionConflict, run.ID)
	}
	run.Version = 1
	stored, err := clonePlanRun(run)
	if err != nil {
		return PlanRun{}, err
	}
	result, err := clonePlanRun(stored)
	if err != nil {
		return PlanRun{}, err
	}
	s.runs[stored.ID] = stored
	return result, nil
}

func (s *MemoryRunStore) GetRun(ctx context.Context, id string) (PlanRun, error) {
	if err := ctx.Err(); err != nil {
		return PlanRun{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	run, exists := s.runs[id]
	if !exists {
		return PlanRun{}, fmt.Errorf("%w: %q", ErrRunNotFound, id)
	}
	cloned, err := clonePlanRun(run)
	if err != nil {
		return PlanRun{}, err
	}
	return cloned, nil
}

func (s *MemoryRunStore) MutateRun(ctx context.Context, id string, expectedVersion int, mutate func(*PlanRun) error) (PlanRun, error) {
	if err := ctx.Err(); err != nil {
		return PlanRun{}, err
	}
	if mutate == nil {
		return PlanRun{}, errors.New("plan run mutation callback is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.runs[id]
	if !exists {
		return PlanRun{}, fmt.Errorf("%w: %q", ErrRunNotFound, id)
	}
	if current.Version != expectedVersion {
		return PlanRun{}, fmt.Errorf("%w: run %q expected version %d, got %d", ErrRunVersionConflict, id, expectedVersion, current.Version)
	}

	next, err := clonePlanRun(current)
	if err != nil {
		return PlanRun{}, err
	}
	if err := mutate(&next); err != nil {
		return PlanRun{}, err
	}
	if err := ValidateRunTransition(current.Status, next.Status); err != nil {
		return PlanRun{}, err
	}
	next.Version = current.Version + 1
	stored, err := clonePlanRun(next)
	if err != nil {
		return PlanRun{}, err
	}
	result, err := clonePlanRun(stored)
	if err != nil {
		return PlanRun{}, err
	}
	s.runs[id] = stored
	return result, nil
}

func clonePlanRun(run PlanRun) (PlanRun, error) {
	data, err := json.Marshal(run)
	if err != nil {
		return PlanRun{}, fmt.Errorf("%w: marshal: %v", ErrRunNotSerializable, err)
	}
	var cloned PlanRun
	if err := json.Unmarshal(data, &cloned); err != nil {
		return PlanRun{}, fmt.Errorf("%w: unmarshal: %v", ErrRunNotSerializable, err)
	}
	return cloned, nil
}
