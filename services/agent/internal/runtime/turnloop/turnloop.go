package turnloop

import "context"

type StartInput struct {
	RunID     string
	Prompt    string
	ProjectID string
}

type ResumeInput struct {
	RunID  string
	Action string
}

type CancelInput struct {
	RunID  string
	Reason string
}

type Result struct {
	RunID  string
	Status string
	Phase  string
}

type TurnLoop struct{}

func New() TurnLoop {
	return TurnLoop{}
}

func (TurnLoop) StartTurn(ctx context.Context, in StartInput) (Result, error) {
	_ = ctx
	return Result{RunID: in.RunID, Status: "pending", Phase: "m3_config_loaded"}, nil
}

func (TurnLoop) ResumeTurn(ctx context.Context, in ResumeInput) (Result, error) {
	_ = ctx
	return Result{RunID: in.RunID, Status: "resuming", Phase: in.Action}, nil
}

func (TurnLoop) CancelRun(ctx context.Context, in CancelInput) (Result, error) {
	_ = ctx
	return Result{RunID: in.RunID, Status: "cancelled", Phase: in.Reason}, nil
}
