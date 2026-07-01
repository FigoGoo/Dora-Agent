package turnloop

import "testing"

func TestStartTurnDecisions(t *testing.T) {
	loop := New()
	base := StartInput{RunID: "run_1", ProjectID: "prj_1", Prompt: "make image", ModelID: "mdl_1", SkillID: "sk_1", SafetyResult: "passed", IdempotencyKey: "idem_1"}

	running, err := loop.StartTurn(t.Context(), base)
	if err != nil || running.Status != "running" || running.Phase != "skill_runtime_ready" || running.Snapshot.IdempotencyKey != "idem_1" {
		t.Fatalf("running decision = %#v err=%v", running, err)
	}

	waitingInput := base
	waitingInput.HasPendingConfirmation = true
	waiting, err := loop.StartTurn(t.Context(), waitingInput)
	if err != nil || waiting.Status != "waiting_confirmation" {
		t.Fatalf("waiting decision = %#v err=%v", waiting, err)
	}

	blockedInput := base
	blockedInput.SafetyResult = "blocked"
	blocked, err := loop.StartTurn(t.Context(), blockedInput)
	if err != nil || blocked.Status != "failed" || blocked.Phase != "safety_blocked" {
		t.Fatalf("blocked decision = %#v err=%v", blocked, err)
	}
}

func TestStartTurnRequiresCoreInput(t *testing.T) {
	loop := New()
	missingModel, err := loop.StartTurn(t.Context(), StartInput{RunID: "run_1", ProjectID: "prj_1", Prompt: "make image", IdempotencyKey: "idem_1"})
	if err != nil || missingModel.Status != "failed" || missingModel.Phase != "model_unavailable" {
		t.Fatalf("missing model decision = %#v err=%v", missingModel, err)
	}
	if _, err := loop.StartTurn(t.Context(), StartInput{RunID: "", ProjectID: "prj_1", Prompt: "make image", ModelID: "mdl_1", IdempotencyKey: "idem_1"}); err == nil {
		t.Fatal("expected missing run_id error")
	}
	if _, err := loop.StartTurn(t.Context(), StartInput{RunID: "run_1", ProjectID: "prj_1", Prompt: "make image", ModelID: "mdl_1"}); err == nil {
		t.Fatal("expected missing idempotency_key error")
	}
}

func TestResumeTurnContinuesRunning(t *testing.T) {
	loop := New()
	resumed, err := loop.ResumeTurn(t.Context(), ResumeInput{RunID: "run_1", Action: "additional_input", IdempotencyKey: "idem_resume"})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed.Status != "running" || resumed.Phase != "resume:additional_input" || resumed.Snapshot.IdempotencyKey != "idem_resume" {
		t.Fatalf("resume decision = %#v", resumed)
	}
}

func TestCancelRunRequiresIdempotencyAndSnapshotsReleaseBoundary(t *testing.T) {
	loop := New()
	if _, err := loop.CancelRun(t.Context(), CancelInput{RunID: "run_1"}); err == nil {
		t.Fatal("expected missing idempotency_key error")
	}
	cancelled, err := loop.CancelRun(t.Context(), CancelInput{RunID: "run_1", IdempotencyKey: "idem_cancel"})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != "cancelled" || cancelled.Phase != "cancel_requested" {
		t.Fatalf("cancel decision = %#v", cancelled)
	}
	if len(cancelled.Snapshot.Steps) != 2 || cancelled.Snapshot.Steps[1].Status != "not_applicable_in_skill_runtime" {
		t.Fatalf("cancel snapshot must mark tool generation freeze release boundary: %#v", cancelled.Snapshot)
	}
}
