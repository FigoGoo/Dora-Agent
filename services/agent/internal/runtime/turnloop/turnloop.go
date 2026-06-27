package turnloop

import (
	"context"
	"errors"
	"strings"
)

type StartInput struct {
	RunID                  string
	Prompt                 string
	ProjectID              string
	SkillID                string
	ModelID                string
	SafetyResult           string
	HasPendingConfirmation bool
	IdempotencyKey         string
}

type ResumeInput struct {
	RunID          string
	Action         string
	InterruptID    string
	IdempotencyKey string
}

type CancelInput struct {
	RunID          string
	Reason         string
	IdempotencyKey string
}

type Result struct {
	RunID    string
	Status   string
	Phase    string
	Snapshot Snapshot
}

type Snapshot struct {
	RunID          string
	Phase          string
	ProjectID      string
	SkillID        string
	ModelID        string
	IdempotencyKey string
	InterruptID    string
	CancelReason   string
	Steps          []Step
}

type Step struct {
	Name   string
	Status string
}

type TurnLoop struct{}

func New() TurnLoop {
	return TurnLoop{}
}

func (TurnLoop) StartTurn(ctx context.Context, in StartInput) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.RunID) == "" || strings.TrimSpace(in.ProjectID) == "" {
		return Result{}, errors.New("run_id and project_id are required")
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return Result{}, errors.New("idempotency_key is required")
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return Result{}, errors.New("prompt is required")
	}
	snapshot := Snapshot{
		RunID: in.RunID, ProjectID: in.ProjectID, SkillID: in.SkillID, ModelID: in.ModelID, IdempotencyKey: in.IdempotencyKey,
		Steps: []Step{{Name: "prompt_safety", Status: in.SafetyResult}, {Name: "skill_route", Status: presenceStatus(in.SkillID)}, {Name: "model_snapshot", Status: presenceStatus(in.ModelID)}},
	}
	if in.SafetyResult == "blocked" || in.SafetyResult == "failed" {
		snapshot.Phase = "safety_" + in.SafetyResult
		return Result{RunID: in.RunID, Status: "failed", Phase: snapshot.Phase, Snapshot: snapshot}, nil
	}
	if in.HasPendingConfirmation {
		snapshot.Phase = "confirmation_required"
		snapshot.Steps = append(snapshot.Steps, Step{Name: "interrupt", Status: "required"})
		return Result{RunID: in.RunID, Status: "waiting_confirmation", Phase: snapshot.Phase, Snapshot: snapshot}, nil
	}
	if strings.TrimSpace(in.ModelID) == "" {
		snapshot.Phase = "model_unavailable"
		return Result{RunID: in.RunID, Status: "failed", Phase: snapshot.Phase, Snapshot: snapshot}, nil
	}
	phase := "m3_runtime_ready"
	if strings.TrimSpace(in.SkillID) == "" {
		phase = "text_fallback_ready"
	}
	snapshot.Phase = phase
	return Result{RunID: in.RunID, Status: "running", Phase: phase, Snapshot: snapshot}, nil
}

func (TurnLoop) ResumeTurn(ctx context.Context, in ResumeInput) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.RunID) == "" {
		return Result{}, errors.New("run_id is required")
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return Result{}, errors.New("idempotency_key is required")
	}
	phase := "resume:" + in.Action
	return Result{RunID: in.RunID, Status: "running", Phase: phase, Snapshot: Snapshot{
		RunID: in.RunID, Phase: phase, InterruptID: in.InterruptID, IdempotencyKey: in.IdempotencyKey,
		Steps: []Step{{Name: "resume_input", Status: "accepted"}, {Name: "run_state", Status: "running"}},
	}}, nil
}

func (TurnLoop) CancelRun(ctx context.Context, in CancelInput) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.RunID) == "" {
		return Result{}, errors.New("run_id is required")
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return Result{}, errors.New("idempotency_key is required")
	}
	phase := in.Reason
	if strings.TrimSpace(phase) == "" {
		phase = "cancel_requested"
	}
	return Result{RunID: in.RunID, Status: "cancelled", Phase: phase, Snapshot: Snapshot{
		RunID: in.RunID, Phase: phase, IdempotencyKey: in.IdempotencyKey, CancelReason: phase,
		Steps: []Step{{Name: "cancel_task", Status: "requested"}, {Name: "release_m4_freeze", Status: "not_applicable_in_m3"}},
	}}, nil
}

func presenceStatus(value string) string {
	if strings.TrimSpace(value) == "" {
		return "missing"
	}
	return "resolved"
}
