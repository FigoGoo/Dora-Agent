package skill

import (
	"fmt"
	"sync"
	"time"
)

const (
	StagePending     = "pending"
	StageRunning     = "running"
	StageWaitingUser = "waiting_user"
	StageConfirmed   = "confirmed"
	StageSkipped     = "skipped"
	StageFailed      = "failed"
)

type StageRun struct {
	ID                string     `json:"id"`
	SessionID         string     `json:"session_id"`
	SkillID           string     `json:"skill_id"`
	StageKey          string     `json:"stage_key"`
	Status            string     `json:"status"`
	DependsOn         []string   `json:"depends_on,omitempty"`
	ToolKeys          []string   `json:"tool_keys,omitempty"`
	PauseAfter        bool       `json:"pause_after"`
	LastToolCallID    string     `json:"last_tool_call_id,omitempty"`
	LastCheckpointID  string     `json:"last_checkpoint_id,omitempty"`
	InputArtifactIDs  []string   `json:"input_artifact_ids,omitempty"`
	OutputArtifactIDs []string   `json:"output_artifact_ids,omitempty"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
}

type MemoryStageLedger struct {
	mu     sync.RWMutex
	stages map[string]StageRun
}

func NewMemoryStageLedger() *MemoryStageLedger {
	return &MemoryStageLedger{stages: make(map[string]StageRun)}
}

func (l *MemoryStageLedger) LoadPlan(sessionID string, skillID string, plan *SkillPlan) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, stage := range plan.Stages {
		run := StageRun{
			ID:         fmt.Sprintf("%s:%s", sessionID, stage.Key),
			SessionID:  sessionID,
			SkillID:    skillID,
			StageKey:   stage.Key,
			Status:     StagePending,
			DependsOn:  append([]string(nil), stage.DependsOn...),
			ToolKeys:   append([]string(nil), stage.ToolKeys...),
			PauseAfter: stage.PauseAfter,
		}
		l.stages[stage.Key] = run
	}
}

func (l *MemoryStageLedger) Get(stageKey string) (StageRun, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	run, ok := l.stages[stageKey]
	return run, ok
}

func (l *MemoryStageLedger) List() []StageRun {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]StageRun, 0, len(l.stages))
	for _, run := range l.stages {
		out = append(out, run)
	}
	return out
}

func (l *MemoryStageLedger) CanRun(stageKey string) (bool, []string) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	run, ok := l.stages[stageKey]
	if !ok {
		return false, []string{stageKey}
	}
	var missing []string
	for _, dep := range run.DependsOn {
		depRun, ok := l.stages[dep]
		if !ok || (depRun.Status != StageConfirmed && depRun.Status != StageSkipped) {
			missing = append(missing, dep)
		}
	}
	return len(missing) == 0, missing
}

func (l *MemoryStageLedger) Start(stageKey string, toolCallID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	run, ok := l.stages[stageKey]
	if !ok {
		return fmt.Errorf("stage %q not found", stageKey)
	}
	now := time.Now()
	run.Status = StageRunning
	run.LastToolCallID = toolCallID
	run.StartedAt = &now
	l.stages[stageKey] = run
	return nil
}

func (l *MemoryStageLedger) Complete(stageKey string, checkpointID string, outputArtifacts []string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	run, ok := l.stages[stageKey]
	if !ok {
		return fmt.Errorf("stage %q not found", stageKey)
	}
	now := time.Now()
	if run.PauseAfter {
		run.Status = StageWaitingUser
	} else {
		run.Status = StageConfirmed
	}
	run.LastCheckpointID = checkpointID
	run.OutputArtifactIDs = append([]string(nil), outputArtifacts...)
	run.FinishedAt = &now
	l.stages[stageKey] = run
	return nil
}

func (l *MemoryStageLedger) Confirm(stageKey string) error {
	return l.setStatus(stageKey, StageConfirmed)
}

func (l *MemoryStageLedger) Skip(stageKey string) error {
	return l.setStatus(stageKey, StageSkipped)
}

func (l *MemoryStageLedger) Fail(stageKey string) error {
	return l.setStatus(stageKey, StageFailed)
}

func (l *MemoryStageLedger) setStatus(stageKey string, status string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	run, ok := l.stages[stageKey]
	if !ok {
		return fmt.Errorf("stage %q not found", stageKey)
	}
	now := time.Now()
	run.Status = status
	run.FinishedAt = &now
	l.stages[stageKey] = run
	return nil
}
