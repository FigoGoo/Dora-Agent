package orchestration

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

var (
	ErrRunNotFound        = errors.New("plan run not found")
	ErrRunVersionConflict = errors.New("plan run version conflict")
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
	StepID     string                 `json:"step_id"`
	Status     string                 `json:"status"`
	Attempt    int                    `json:"attempt"`
	Outputs    map[string]any         `json:"outputs,omitempty"`
	Fail       *vocabulary.Failure    `json:"fail,omitempty"`
	Suspension *vocabulary.Suspension `json:"suspension,omitempty"`
	ResumeKey  string                 `json:"resume_key,omitempty"`
	Resumed    bool                   `json:"resumed,omitempty"`
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
	stored := clonePlanRun(run)
	stored.Version = 1
	s.runs[stored.ID] = stored
	return clonePlanRun(stored), nil
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
	return clonePlanRun(run), nil
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

	next := clonePlanRun(current)
	if err := mutate(&next); err != nil {
		return PlanRun{}, err
	}
	if err := ValidateRunTransition(current.Status, next.Status); err != nil {
		return PlanRun{}, err
	}
	next.Version = current.Version + 1
	s.runs[id] = next
	return clonePlanRun(next), nil
}

func clonePlanRun(run PlanRun) PlanRun {
	cloned := run
	cloned.Plan = cloneExecutionPlan(run.Plan)
	if run.Nodes != nil {
		cloned.Nodes = make(map[string]*NodeRun, len(run.Nodes))
		for id, node := range run.Nodes {
			cloned.Nodes[id] = cloneNodeRun(node)
		}
	}
	return cloned
}

func cloneExecutionPlan(plan ExecutionPlan) ExecutionPlan {
	cloned := plan
	if plan.Steps != nil {
		cloned.Steps = make([]PlanStep, len(plan.Steps))
		for i, step := range plan.Steps {
			cloned.Steps[i] = step
			cloned.Steps[i].Params = cloneStringAnyMap(step.Params)
			cloned.Steps[i].DependsOn = append([]string(nil), step.DependsOn...)
			if step.Expand != nil {
				expand := *step.Expand
				cloned.Steps[i].Expand = &expand
			}
		}
	}
	return cloned
}

func cloneNodeRun(node *NodeRun) *NodeRun {
	if node == nil {
		return nil
	}
	cloned := *node
	cloned.Outputs = cloneStringAnyMap(node.Outputs)
	if node.Fail != nil {
		failure := *node.Fail
		cloned.Fail = &failure
	}
	if node.Suspension != nil {
		suspension := *node.Suspension
		suspension.Payload = cloneStringAnyMap(node.Suspension.Payload)
		cloned.Suspension = &suspension
	}
	return &cloned
}

func cloneStringAnyMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneAny(value)
	}
	return cloned
}

func cloneAny(value any) any {
	if value == nil {
		return nil
	}
	return cloneReflectValue(reflect.ValueOf(value)).Interface()
}

func cloneReflectValue(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := cloneReflectValue(value.Elem())
		wrapped := reflect.New(value.Type()).Elem()
		wrapped.Set(cloned)
		return wrapped
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			cloned.SetMapIndex(cloneReflectValue(iterator.Key()), cloneReflectValue(iterator.Value()))
		}
		return cloned
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := range value.Len() {
			cloned.Index(i).Set(cloneReflectValue(value.Index(i)))
		}
		return cloned
	case reflect.Array:
		cloned := reflect.New(value.Type()).Elem()
		for i := range value.Len() {
			cloned.Index(i).Set(cloneReflectValue(value.Index(i)))
		}
		return cloned
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.New(value.Type().Elem())
		cloned.Elem().Set(cloneReflectValue(value.Elem()))
		return cloned
	default:
		return value
	}
}
