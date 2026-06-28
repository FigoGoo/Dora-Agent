package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/state"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
)

func TestM6IndependentToolChargeClosesRPCChain(t *testing.T) {
	app, gateway := newM6ServiceApp(t)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{ProjectID: "prj_active_1001", InitialTitle: "m6", IdempotencyKey: "idem-m6-session"}, "trace-m6-tool")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: "prj_active_1001", IdempotencyKey: "idem-m6-run-tool-charge",
		UserInput: UserInputDTO{ClientMessageID: "cm_m6_tool", ContentType: "text", Text: "lookup with web fetch"},
	}, "trace-m6-tool")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if !containsCall(gateway.calls, "ListAvailableGenerationModels") {
		t.Fatalf("ListAvailableGenerationModels was not consumed by Agent gateway: %v", gateway.calls)
	}
	want := []string{"EstimateToolCredits", "FreezeCredits", "ChargeToolUsageCredits"}
	if !containsSubsequence(gateway.calls, want) {
		t.Fatalf("missing independent tool billing RPC chain\ncalls=%v\nwant subsequence=%v", gateway.calls, want)
	}
	events, err := app.repo.ListEventsAfterSequence(t.Context(), run.RunID, 0, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if !hasEvent(events, "tool.call.completed") || !hasEvent(events, "credits.charged") {
		t.Fatalf("tool charge events missing: %#v", events)
	}
}

func TestM6IndependentToolChargeFailureReleasesFreeze(t *testing.T) {
	app, gateway := newM6ServiceApp(t)
	gateway.chargeErr = errors.New("STATE_CONFLICT: duplicate estimate item")
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{ProjectID: "prj_active_1001", InitialTitle: "m6 fail", IdempotencyKey: "idem-m6-session-fail"}, "trace-m6-tool-fail")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	_, err = app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: "prj_active_1001", IdempotencyKey: "idem-m6-run-tool-charge-fail",
		UserInput: UserInputDTO{ClientMessageID: "cm_m6_tool_fail", ContentType: "text", Text: "lookup with web fetch"},
	}, "trace-m6-tool-fail")
	if err == nil {
		t.Fatal("expected charge failure")
	}
	want := []string{"EstimateToolCredits", "FreezeCredits", "ChargeToolUsageCredits", "ReleaseFrozenCredits"}
	if !containsSubsequence(gateway.calls, want) {
		t.Fatalf("missing failure release RPC chain\ncalls=%v\nwant subsequence=%v", gateway.calls, want)
	}
	run, err := app.repo.GetRunByIdempotencyKey(t.Context(), "idem-m6-run-tool-charge-fail")
	if err != nil {
		t.Fatalf("get failed run: %v", err)
	}
	events, err := app.repo.ListEventsAfterSequence(t.Context(), run.ID, 0, 100)
	if err != nil {
		t.Fatalf("list failed events: %v", err)
	}
	if !hasEvent(events, "credits.released") || !hasEvent(events, "tool.call.failed") {
		t.Fatalf("release/failure events missing: %#v", events)
	}
}

func TestM6SkillTestConsumesReviewCandidateRPC(t *testing.T) {
	app, gateway := newM6ServiceApp(t)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	result, err := app.RunSkillTestCase(t.Context(), auth, SkillTestCaseRequest{
		SkillID: "sk_review", VersionID: "skv_review", TestRunID: "skrun_m6", TestCaseID: "skcase_m6",
		IdempotencyKey: "skill_test:skrun_m6",
	}, "trace-m6-skilltest")
	if err != nil {
		t.Fatalf("run skill test case: %v", err)
	}
	if result.Status != "passed" || !result.Saved {
		t.Fatalf("unexpected skill test result: %#v", result)
	}
	want := []string{"GetReviewCandidateSkillSpec", "ListAssetElementTypes", "SaveSkillTestResult"}
	if !containsSubsequence(gateway.calls, want) {
		t.Fatalf("missing skill test RPC chain\ncalls=%v\nwant subsequence=%v", gateway.calls, want)
	}
}

func TestSkillOutputElementsDriveDraftAndFinalArtifacts(t *testing.T) {
	app, gateway := newM6ServiceApp(t)
	gateway.StaticGateway.SkillSpec.OutputElements = []SkillOutputElementDTO{
		{
			ElementType: "image_ref", ElementName: "草稿图", Required: true, UseDraft: true,
			UseFinal: false, Editable: true, Referable: true, DisplayOrder: 1, DisplaySlot: "blackboard",
		},
		{
			ElementType: "image_ref", ElementName: "最终图", Required: true, UseDraft: false,
			UseFinal: true, Editable: false, Referable: true, DisplayOrder: 7, DisplaySlot: "asset_detail",
		},
	}
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{ProjectID: "prj_active_1001", InitialTitle: "skill2", IdempotencyKey: "idem-skill2-session"}, "trace-skill2")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: "prj_active_1001", IdempotencyKey: "idem-skill2-run",
		UserInput: UserInputDTO{ClientMessageID: "cm_skill2", ContentType: "text", Text: "lookup with web fetch"},
	}, "trace-skill2")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	interrupt, err := app.repo.GetRequiredInterrupt(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get confirmation interrupt: %v", err)
	}
	var payload m4ConfirmationPayload
	if err := json.Unmarshal(interrupt.ConfirmationPayload, &payload); err != nil {
		t.Fatalf("decode confirmation payload: %v", err)
	}
	if len(payload.OutputElements) != 2 {
		t.Fatalf("confirmation payload should carry output elements, got %#v", payload.OutputElements)
	}
	_, err = app.AcceptInterrupt(t.Context(), auth, run.RunID, ConfirmInterruptRequest{
		RunID: run.RunID, InterruptID: interrupt.ID, Action: "confirm",
		ConfirmedPayloadDigest: confirmationPayloadDigest(interrupt.ConfirmationPayload),
		IdempotencyKey:         "idem-skill2-confirm",
	}, "trace-skill2")
	if err != nil {
		t.Fatalf("accept interrupt: %v", err)
	}
	if len(gateway.lastCommit.FinalElements) != 1 {
		t.Fatalf("expected exactly one final element from use_final declaration, got %#v", gateway.lastCommit.FinalElements)
	}
	final := gateway.lastCommit.FinalElements[0]
	if final.ElementType != "image_ref" || final.DisplayOrder != 7 {
		t.Fatalf("unexpected final element: %#v", final)
	}
	artifacts, err := app.repo.ListArtifacts(t.Context(), session.SessionID, 20, 0)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	var sawDraft, sawFinalRef bool
	for _, artifact := range artifacts {
		switch {
		case artifact.ArtifactType == "draft_element" && artifact.Status == "draft" && artifact.ElementType == "image_ref":
			sawDraft = true
		case artifact.ArtifactType == "asset_ref" && artifact.Status == "final_ref":
			sawFinalRef = true
		}
	}
	if !sawDraft || !sawFinalRef {
		t.Fatalf("expected draft_element and asset_ref artifacts, got %#v", artifacts)
	}
	tasks, err := app.repo.ListTasksByRun(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("list generation tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].TaskType != "generation_asset_commit" || tasks[0].Status != state.TaskStatusCompleted {
		t.Fatalf("expected completed generation task, got %#v", tasks)
	}
	snapshot, err := app.BuildRunSnapshot(t.Context(), auth, run.RunID, "trace-skill2")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].TaskID != tasks[0].ID {
		t.Fatalf("snapshot should expose persisted generation task, got %#v", snapshot.Tasks)
	}
}

func TestExpiredSafetyEvidenceIsReevaluatedBeforeGenerationFreeze(t *testing.T) {
	app, gateway := newM6ServiceApp(t)
	gateway.refreshEstimate = gateway.StaticGateway.Estimate
	gateway.refreshEstimate.EstimateID = "est_generation_refresh"
	gateway.refreshEstimate.LineItems = []CreditEstimateLineItemDTO{{
		EstimateItemID: "est_item_generation_refresh", ItemType: "model_generation",
		ModelID: "mdl_static_image", ResourceType: "image", BillingUnit: "image", EstimatePoints: 10,
	}}
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{ProjectID: "prj_active_1001", InitialTitle: "gen4", IdempotencyKey: "idem-gen4-session"}, "trace-gen4")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: "prj_active_1001", IdempotencyKey: "idem-gen4-run",
		UserInput: UserInputDTO{ClientMessageID: "cm_gen4", ContentType: "text", Text: "lookup with web fetch"},
	}, "trace-gen4")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	interrupt, err := app.repo.GetRequiredInterrupt(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get confirmation interrupt: %v", err)
	}
	var payload m4ConfirmationPayload
	if err := json.Unmarshal(interrupt.ConfirmationPayload, &payload); err != nil {
		t.Fatalf("decode confirmation payload: %v", err)
	}
	originalEvidenceID := payload.SafetyEvidence.SafetyEvidenceId
	expiredAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	payload.SafetyEvidence.ExpiresAt = &expiredAt
	interrupt.ConfirmationPayload = jsonObject(payload)
	if err := app.repo.DB().WithContext(t.Context()).Model(&model.Interrupt{}).
		Where("id = ?", interrupt.ID).
		Update("confirmation_payload", interrupt.ConfirmationPayload).Error; err != nil {
		t.Fatalf("expire confirmation safety evidence: %v", err)
	}
	interrupt, err = app.repo.GetInterrupt(t.Context(), run.RunID, interrupt.ID)
	if err != nil {
		t.Fatalf("reload expired confirmation interrupt: %v", err)
	}
	gateway.calls = nil

	_, err = app.AcceptInterrupt(t.Context(), auth, run.RunID, ConfirmInterruptRequest{
		RunID: run.RunID, InterruptID: interrupt.ID, Action: "confirm",
		ConfirmedPayloadDigest: confirmationPayloadDigest(interrupt.ConfirmationPayload),
		IdempotencyKey:         "idem-gen4-confirm",
	}, "trace-gen4")
	if err != nil {
		t.Fatalf("accept interrupt with expired evidence: %v", err)
	}
	if !containsSubsequence(gateway.calls, []string{"EstimateGenerationCredits", "FreezeCredits", "CommitGeneratedAssetAndCharge"}) {
		t.Fatalf("expired evidence should be re-estimated before freeze, calls=%v", gateway.calls)
	}
	if gateway.lastCommit.EstimateID != "est_generation_refresh" {
		t.Fatalf("commit should use refreshed estimate, got %s", gateway.lastCommit.EstimateID)
	}
	if len(gateway.lastCommit.Artifacts) != 1 || gateway.lastCommit.Artifacts[0].EstimateItemID != "est_item_generation_refresh" {
		t.Fatalf("commit should map refreshed estimate item, got %#v", gateway.lastCommit.Artifacts)
	}
	if gateway.lastCommit.SafetyEvidence == nil || gateway.lastCommit.SafetyEvidence.SafetyEvidenceId == originalEvidenceID {
		t.Fatalf("commit should use refreshed safety evidence, got %#v", gateway.lastCommit.SafetyEvidence)
	}
	if gateway.lastCommit.SafetyEvidence.ExpiresAt == nil {
		t.Fatal("refreshed safety evidence must carry expires_at")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, *gateway.lastCommit.SafetyEvidence.ExpiresAt)
	if err != nil || !expiresAt.After(time.Now().UTC()) {
		t.Fatalf("refreshed safety evidence should be unexpired, expires_at=%v err=%v", gateway.lastCommit.SafetyEvidence.ExpiresAt, err)
	}
}

func TestQueuedGenerationAcceptReturnsBeforeM4AndWorkerCompletes(t *testing.T) {
	app, gateway := newM6ServiceApp(t)
	queue := NewMemoryGenerationJobQueue(1)
	app.SetGenerationQueue(queue)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{ProjectID: "prj_active_1001", InitialTitle: "queued", IdempotencyKey: "idem-queued-session"}, "trace-queued")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: "prj_active_1001", IdempotencyKey: "idem-queued-run",
		UserInput: UserInputDTO{ClientMessageID: "cm_queued", ContentType: "text", Text: "lookup with web fetch"},
	}, "trace-queued")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	interrupt, err := app.repo.GetRequiredInterrupt(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get confirmation interrupt: %v", err)
	}
	gateway.calls = nil
	accepted, err := app.AcceptInterrupt(t.Context(), auth, run.RunID, ConfirmInterruptRequest{
		RunID: run.RunID, InterruptID: interrupt.ID, Action: "confirm",
		ConfirmedPayloadDigest: confirmationPayloadDigest(interrupt.ConfirmationPayload),
		IdempotencyKey:         "idem-queued-confirm",
	}, "trace-queued")
	if err != nil {
		t.Fatalf("accept interrupt: %v", err)
	}
	if accepted.Status != state.RunStatusRunning {
		t.Fatalf("queued accept should return before generation completes, got status=%s", accepted.Status)
	}
	if containsCall(gateway.calls, "FreezeCredits") || containsCall(gateway.calls, "CommitGeneratedAssetAndCharge") {
		t.Fatalf("accept should not execute M4 chain when queue is enabled, calls=%v", gateway.calls)
	}
	if queue.Len() != 1 {
		t.Fatalf("expected one queued generation job, got %d", queue.Len())
	}
	events, err := app.repo.ListEventsAfterSequence(t.Context(), run.RunID, 0, 100)
	if err != nil {
		t.Fatalf("list queued events: %v", err)
	}
	if !hasEvent(events, "generation.task.queued") {
		t.Fatalf("queued generation event missing: %#v", events)
	}

	result := app.RunGenerationWorker(t.Context(), 1)
	if result.Processed != 1 || result.Failed != 0 || result.LastError != nil {
		t.Fatalf("unexpected worker result: %#v", result)
	}
	if !containsSubsequence(gateway.calls, []string{"FreezeCredits", "PrepareGeneratedAssetObjects", "CommitGeneratedAssetAndCharge"}) {
		t.Fatalf("worker should execute M4 chain, calls=%v", gateway.calls)
	}
	updated, err := app.repo.GetRun(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get completed run: %v", err)
	}
	if updated.Status != state.RunStatusCompleted {
		t.Fatalf("worker should complete run, got %s", updated.Status)
	}
	tasks, err := app.repo.ListTasksByRun(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("list generation tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Status != state.TaskStatusCompleted {
		t.Fatalf("expected completed worker task, got %#v", tasks)
	}
}

func TestQueuedGenerationRedeliveryRecoversRunningTaskWithoutDuplicateM4(t *testing.T) {
	app, gateway := newM6ServiceApp(t)
	queue := NewMemoryGenerationJobQueue(1)
	app.SetGenerationQueue(queue)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{ProjectID: "prj_active_1001", InitialTitle: "redelivery", IdempotencyKey: "idem-redelivery-session"}, "trace-redelivery")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: "prj_active_1001", IdempotencyKey: "idem-redelivery-run",
		UserInput: UserInputDTO{ClientMessageID: "cm_redelivery", ContentType: "text", Text: "lookup with web fetch"},
	}, "trace-redelivery")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	interrupt, err := app.repo.GetRequiredInterrupt(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get confirmation interrupt: %v", err)
	}
	_, err = app.AcceptInterrupt(t.Context(), auth, run.RunID, ConfirmInterruptRequest{
		RunID: run.RunID, InterruptID: interrupt.ID, Action: "confirm",
		ConfirmedPayloadDigest: confirmationPayloadDigest(interrupt.ConfirmationPayload),
		IdempotencyKey:         "idem-redelivery-confirm",
	}, "trace-redelivery")
	if err != nil {
		t.Fatalf("accept interrupt: %v", err)
	}
	stale := time.Now().UTC().Add(-10 * time.Minute)
	task := &model.Task{
		ID: "task_redelivery_generation", RunID: run.RunID, TaskType: "generation_asset_commit", ResourceType: "image",
		Status: state.TaskStatusRunning, ProgressPercent: 25,
		ProgressDetail: jsonObject(map[string]any{
			"stage": "credits_frozen", "freeze_id": "frz_redelivery", "frozen_points": int64(10),
			"estimate_id": "est_generation_m6", "idempotency_key": "idem-redelivery-confirm",
			"auth": map[string]any{"actor_user_id": auth.ActorUserID, "login_identity_type": auth.LoginIdentityType, "space_id": auth.SpaceID},
		}),
		StartedAt: &stale, UpdatedAt: stale, TraceID: "trace-redelivery",
	}
	if err := app.repo.CreateTask(t.Context(), task); err != nil {
		t.Fatalf("create running task: %v", err)
	}
	gateway.calls = nil
	result := app.RunGenerationWorker(t.Context(), 1)
	if result.Processed != 1 || result.Failed != 0 || result.LastError != nil {
		t.Fatalf("unexpected worker result: %#v", result)
	}
	if containsCall(gateway.calls, "CommitGeneratedAssetAndCharge") {
		t.Fatalf("redelivered job must not duplicate asset commit, calls=%v", gateway.calls)
	}
	if !containsCall(gateway.calls, "ReleaseFrozenCredits") {
		t.Fatalf("redelivered running task should recover/release, calls=%v", gateway.calls)
	}
	updatedTask, err := app.repo.GetTask(t.Context(), task.ID)
	if err != nil {
		t.Fatalf("get recovered task: %v", err)
	}
	if updatedTask.Status != state.TaskStatusFailed || updatedTask.ErrorCode != "RESTART_RECOVERED" {
		t.Fatalf("expected recovered task, got status=%s error=%s", updatedTask.Status, updatedTask.ErrorCode)
	}
}

func TestRecoverGenerationTasksReleasesFrozenTaskAfterRestart(t *testing.T) {
	app, gateway := newM6ServiceApp(t)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{ProjectID: "prj_active_1001", InitialTitle: "recover", IdempotencyKey: "idem-recover-session"}, "trace-recover")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: "prj_active_1001", IdempotencyKey: "idem-recover-run",
		UserInput: UserInputDTO{ClientMessageID: "cm_recover", ContentType: "text", Text: "lookup with web fetch"},
	}, "trace-recover")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	stale := time.Now().UTC().Add(-10 * time.Minute)
	task := &model.Task{
		ID: "task_recover_generation", RunID: run.RunID, TaskType: "generation_asset_commit", ResourceType: "image",
		Status: state.TaskStatusRunning, ProgressPercent: 25,
		ProgressDetail: jsonObject(map[string]any{
			"stage": "credits_frozen", "freeze_id": "frz_recover", "frozen_points": int64(10),
			"estimate_id": "est_generation_m6", "idempotency_key": "idem-recover-confirm",
			"auth": map[string]any{"actor_user_id": auth.ActorUserID, "login_identity_type": auth.LoginIdentityType, "space_id": auth.SpaceID},
		}),
		StartedAt: &stale, UpdatedAt: stale, TraceID: "trace-recover",
	}
	if err := app.repo.CreateTask(t.Context(), task); err != nil {
		t.Fatalf("create stale task: %v", err)
	}
	if err := app.repo.UpdateRunStatus(t.Context(), run.RunID, state.RunStatusResuming, "", ""); err != nil {
		t.Fatalf("mark run resuming: %v", err)
	}

	result, err := app.RecoverGenerationTasks(t.Context(), time.Minute, 10, "trace-recover")
	if err != nil {
		t.Fatalf("recover generation tasks: %v", err)
	}
	if result.Scanned != 1 || result.Released != 1 || result.ReleaseFails != 0 {
		t.Fatalf("unexpected recovery result: %#v", result)
	}
	if !containsCall(gateway.calls, "ReleaseFrozenCredits") {
		t.Fatalf("recovery should release frozen credits, calls=%v", gateway.calls)
	}
	updatedTask, err := app.repo.GetTask(t.Context(), task.ID)
	if err != nil {
		t.Fatalf("get recovered task: %v", err)
	}
	if updatedTask.Status != state.TaskStatusFailed || updatedTask.ErrorCode != "RESTART_RECOVERED" {
		t.Fatalf("expected recovered failed task, got status=%s error=%s", updatedTask.Status, updatedTask.ErrorCode)
	}
	updatedRun, err := app.repo.GetRun(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get recovered run: %v", err)
	}
	if updatedRun.Status != state.RunStatusCancelled || updatedRun.ErrorCode != "RESTART_RECOVERED" {
		t.Fatalf("expected recovered run cancellation, got status=%s error=%s", updatedRun.Status, updatedRun.ErrorCode)
	}
	events, err := app.repo.ListEventsAfterSequence(t.Context(), run.RunID, 0, 100)
	if err != nil {
		t.Fatalf("list recovered events: %v", err)
	}
	if !hasEvent(events, "credits.released") || !hasEvent(events, "agent.run.cancelled") {
		t.Fatalf("recovery events missing: %#v", events)
	}
}

func TestRecoverGenerationTasksReplaysAssetCommitAndCompletesIdempotently(t *testing.T) {
	app, gateway := newM6ServiceApp(t)
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{ProjectID: "prj_active_1001", InitialTitle: "recover commit", IdempotencyKey: "idem-recover-commit-session"}, "trace-recover-commit")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: "prj_active_1001", IdempotencyKey: "idem-recover-commit-run",
		UserInput: UserInputDTO{ClientMessageID: "cm_recover_commit", ContentType: "text", Text: "lookup with web fetch"},
	}, "trace-recover-commit")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	interrupt, err := app.repo.GetRequiredInterrupt(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get required interrupt: %v", err)
	}
	if err := app.repo.ResolveInterrupt(t.Context(), interrupt.ID, state.InterruptStatusAccepted); err != nil {
		t.Fatalf("accept interrupt: %v", err)
	}
	if err := app.repo.UpdateRunStatus(t.Context(), run.RunID, state.RunStatusResuming, "", ""); err != nil {
		t.Fatalf("mark run resuming: %v", err)
	}
	stale := time.Now().UTC().Add(-10 * time.Minute)
	commitReq := CommitGeneratedAssetAndChargeRequest{
		ProjectID: session.ProjectID, SessionID: session.SessionID, RunID: run.RunID, FreezeID: "frz_recover_commit",
		EstimateID: "est_generation_m6", IdempotencyKey: "idem-recover-commit-confirm:commit",
		SafetyEvidence: &businessagent.SafetyEvidenceDTO{
			SafetyEvidenceId: "se_recover_commit", Scene: "generation", Result_: state.SafetyResultPassed,
			TargetType: "prompt", EvaluatedObjectDigest: "digest_recover_commit", PolicyVersion: "policy_test",
			EvidenceVersion: "v1", EvaluatedAt: time.Now().UTC().Format(time.RFC3339Nano), TraceId: "trace-recover-commit",
		},
		Artifacts: []CommitArtifactDTO{{
			ArtifactID: "artifact_recover_commit", ResourceType: "image", ElementType: "image_ref",
			EstimateItemID: "est_item_generation_m6", ToolName: "model_generation", ToolType: "image", ChargeQuantity: 1,
			StorageObjectRef: CommitStorageObjectRefDTO{
				ObjectKey: "local/recover/artifact_recover_commit", Bucket: "dora-public", ContentType: "image/png",
				SizeBytes: 123, Checksum: "sha256:recover", Etag: "etag-recover",
			},
		}},
		FinalElements: []FinalElementDTO{{ElementType: "image_ref", ElementPayloadJSON: `{"artifact_id":"artifact_recover_commit"}`, DisplayOrder: 1}},
	}
	task := &model.Task{
		ID: "task_recover_commit_generation", RunID: run.RunID, TaskType: "generation_asset_commit", ResourceType: "image",
		Status: state.TaskStatusRunning, ProgressPercent: 85,
		ProgressDetail: jsonObject(map[string]any{
			"stage": "asset_commit_started", "freeze_id": commitReq.FreezeID, "frozen_points": int64(10),
			"estimate_id": commitReq.EstimateID, "idempotency_key": "idem-recover-commit-confirm",
			"confirmation_id": interrupt.ID, "commit_request": commitReq,
			"auth": map[string]any{"actor_user_id": auth.ActorUserID, "login_identity_type": auth.LoginIdentityType, "space_id": auth.SpaceID},
		}),
		StartedAt: &stale, UpdatedAt: stale, TraceID: "trace-recover-commit",
	}
	if err := app.repo.CreateTask(t.Context(), task); err != nil {
		t.Fatalf("create stale commit task: %v", err)
	}
	gateway.calls = nil

	result, err := app.RecoverGenerationTasks(t.Context(), time.Minute, 10, "trace-recover-commit")
	if err != nil {
		t.Fatalf("recover generation tasks: %v", err)
	}
	if result.Scanned != 1 || result.Reconcile != 1 || result.Released != 0 || result.ReleaseFails != 0 {
		t.Fatalf("unexpected recovery result: %#v", result)
	}
	if !containsCall(gateway.calls, "CommitGeneratedAssetAndCharge") {
		t.Fatalf("recovery should replay asset commit, calls=%v", gateway.calls)
	}
	if containsCall(gateway.calls, "ReleaseFrozenCredits") {
		t.Fatalf("recovery after asset commit must not release frozen credits, calls=%v", gateway.calls)
	}
	updatedRun, err := app.repo.GetRun(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get recovered run: %v", err)
	}
	if updatedRun.Status != state.RunStatusCompleted {
		t.Fatalf("expected completed run after replay, got %s", updatedRun.Status)
	}
	updatedTask, err := app.repo.GetTask(t.Context(), task.ID)
	if err != nil {
		t.Fatalf("get recovered task: %v", err)
	}
	if updatedTask.Status != state.TaskStatusCompleted {
		t.Fatalf("expected completed task after replay, got %s", updatedTask.Status)
	}
	updatedInterrupt, err := app.repo.GetInterrupt(t.Context(), run.RunID, interrupt.ID)
	if err != nil {
		t.Fatalf("get recovered interrupt: %v", err)
	}
	if updatedInterrupt.Status != state.InterruptStatusResolved {
		t.Fatalf("expected resolved interrupt after replay, got %s", updatedInterrupt.Status)
	}
	artifacts, err := app.repo.ListArtifacts(t.Context(), session.SessionID, 20, 0)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	messages, err := app.repo.ListMessages(t.Context(), session.SessionID, 20, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	artifactCount := len(artifacts)
	messageCount := len(messages)

	gateway.calls = nil
	if recovered, err := app.recoverAssetCommitStartedTask(t.Context(), updatedRun, *updatedTask, jsonMap(updatedTask.ProgressDetail), "trace-recover-commit-retry"); err != nil || !recovered {
		t.Fatalf("second replay should be idempotent, recovered=%v err=%v", recovered, err)
	}
	artifactsAfter, err := app.repo.ListArtifacts(t.Context(), session.SessionID, 20, 0)
	if err != nil {
		t.Fatalf("list artifacts after second replay: %v", err)
	}
	messagesAfter, err := app.repo.ListMessages(t.Context(), session.SessionID, 20, 0)
	if err != nil {
		t.Fatalf("list messages after second replay: %v", err)
	}
	if len(artifactsAfter) != artifactCount || len(messagesAfter) != messageCount {
		t.Fatalf("second replay duplicated artifacts/messages: artifacts %d -> %d messages %d -> %d", artifactCount, len(artifactsAfter), messageCount, len(messagesAfter))
	}
	if containsCall(gateway.calls, "ReleaseFrozenCredits") {
		t.Fatalf("second replay must not release frozen credits, calls=%v", gateway.calls)
	}
}

func TestPermissionLossCancelsActiveRunBeforeConfirm(t *testing.T) {
	app, gateway := newM6ServiceApp(t)
	gateway.StaticGateway.Space = SpaceContextDTO{
		SpaceID: "sp_enterprise_1001", SpaceType: "enterprise", EnterpriseID: "ent_1001", EnterpriseRole: "member",
		CreditAccountScope: "enterprise", CreditAccountID: "ca_ent_1001",
	}
	auth := AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "enterprise_member", SpaceID: "sp_enterprise_1001", EnterpriseID: "ent_1001", EnterpriseRole: "member"}
	session, err := app.CreateSession(t.Context(), auth, CreateSessionRequest{ProjectID: "prj_active_1001", InitialTitle: "acct4", IdempotencyKey: "idem-acct4-session"}, "trace-acct4")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	run, err := app.CreateRun(t.Context(), auth, CreateRunRequest{
		SessionID: session.SessionID, ProjectID: "prj_active_1001", IdempotencyKey: "idem-acct4-run",
		UserInput: UserInputDTO{ClientMessageID: "cm_acct4", ContentType: "text", Text: "lookup with web fetch"},
	}, "trace-acct4")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	interrupt, err := app.repo.GetRequiredInterrupt(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get confirmation interrupt: %v", err)
	}

	gateway.accessErr = errors.New("PERMISSION_DENIED: enterprise membership is unavailable")
	_, err = app.AcceptInterrupt(t.Context(), auth, run.RunID, ConfirmInterruptRequest{
		RunID: run.RunID, InterruptID: interrupt.ID, Action: "confirm",
		ConfirmedPayloadDigest: confirmationPayloadDigest(interrupt.ConfirmationPayload),
		IdempotencyKey:         "idem-acct4-confirm",
	}, "trace-acct4")
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected permission denied after member removal, got %v", err)
	}
	updated, err := app.repo.GetRun(t.Context(), run.RunID)
	if err != nil {
		t.Fatalf("get run after permission loss: %v", err)
	}
	if updated.Status != state.RunStatusCancelled || updated.ErrorCode != "PERMISSION_REVOKED" {
		t.Fatalf("active run should be cancelled after permission loss, got status=%s error=%s", updated.Status, updated.ErrorCode)
	}
	events, err := app.repo.ListEventsAfterSequence(t.Context(), run.RunID, 0, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if !hasEvent(events, "agent.run.cancelled") {
		t.Fatalf("permission loss should emit cancellation event: %#v", events)
	}
	if _, err := app.repo.GetRequiredInterrupt(t.Context(), run.RunID); err == nil {
		t.Fatal("permission loss should close required interrupt")
	}
}

func newM6ServiceApp(t *testing.T) (*App, *recordingGateway) {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_agent_m6_service")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/20260627_agent_runtime/agent")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	gateway := &recordingGateway{StaticGateway: StaticGateway{
		Auth:   AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"},
		Space:  SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: ProjectAccessDTO{Allowed: true, ProjectStatus: "active", CreativeAllowed: true, AllowedActions: []string{"view", "continue_creation"}},
		Skills: []SkillSummaryDTO{{SkillID: "sk_tool", SkillName: "Tool Skill", SkillScope: "public", Version: "1.0.0", Status: "published", RouteHints: map[string]string{"intent": "lookup"}}},
		SkillSpec: SkillSpecDTO{
			SkillID: "sk_tool", Version: "1.0.0", SkillSpecJSON: `{"name":"tool"}`, OutputSchemaJSON: `{"type":"object"}`,
			ToolRefs: []string{"web_fetch:browser"}, ConfirmationPolicyJSON: `{"requires_confirmation":false}`,
		},
		Models: []ModelSummaryDTO{{ModelID: "mdl_static_image", DisplayName: "Static Image", IsDefault: true, PricingSnapshotID: "price_static_image", ResourceType: "image"}},
		Model:  ModelSummaryDTO{ModelID: "mdl_static_image", DisplayName: "Static Image", IsDefault: true, PricingSnapshotID: "price_static_image", ResourceType: "image"},
		ModelSnapshot: ModelRuntimeSnapshotDTO{
			ModelID: "mdl_static_image", DisplayName: "Static Image", ResourceType: "image", PricingSnapshotID: "price_static_image",
			ProviderRuntimeRef: "static:local", TimeoutMS: 30000,
		},
		ToolEstimate: CreditEstimateDTO{
			EstimateID: "est_tool_m6", EstimatePoints: 4, AvailablePoints: 100, CreditAccountScope: "personal", CreditAccountID: "ca_personal_1001",
			LineItems: []CreditEstimateLineItemDTO{{EstimateItemID: "est_item_tool_m6", ItemType: "tool_usage", ToolName: "web_fetch", ToolType: "browser", BillingUnit: "call", EstimatePoints: 4}},
			ExpiresAt: "2026-06-28T00:00:00Z",
		},
		Freeze: FreezeCreditsDTO{FreezeID: "frz_tool_m6", FrozenPoints: 4, ExpiresAt: "2026-06-28T00:15:00Z"},
		ToolCharge: ToolChargeDTO{
			ToolChargeID: "toolchg_m6", ChargedPoints: 4, ReleasedPoints: 0, FreezeStatus: "charged",
			LedgerEntryIDs:   []string{"cled_tool_m6"},
			ChargedLineItems: []ChargedLineItemDTO{{EstimateItemID: "est_item_tool_m6", ChargedPoints: 4, Status: "charged", ToolCallID: "tool_m6"}},
		},
		Estimate: CreditEstimateDTO{
			EstimateID: "est_generation_m6", EstimatePoints: 10, AvailablePoints: 100, CreditAccountScope: "personal", CreditAccountID: "ca_personal_1001",
			PricingSnapshotID: "price_static_image",
			LineItems:         []CreditEstimateLineItemDTO{{EstimateItemID: "est_item_generation_m6", ItemType: "model_generation", ModelID: "mdl_static_image", ResourceType: "image", BillingUnit: "image", EstimatePoints: 10}},
			ExpiresAt:         "2026-06-28T00:00:00Z",
		},
		ReviewSpec: ReviewCandidateSkillSpecDTO{
			SkillID: "sk_review", VersionID: "skv_review", SkillSpecJSON: `{"name":"review"}`,
			InputSchemaJSON: `{"type":"object"}`, OutputSchemaJSON: `{"required":["structured_object"]}`,
			ToolRefs: []string{"web_fetch:browser"}, MemoryPolicyJSON: `{"enabled":false}`,
			ConfirmationPolicyJSON: `{"requires_confirmation":false}`, TestInputJSON: `{"prompt":"safe"}`,
			ExpectedElementsJSON: `["structured_object"]`,
		},
		ElementTypes: []AssetElementTypeDTO{{
			ElementType: "structured_object", DisplayName: "Structured Object", Category: "data", SchemaVersion: "2026-06-28",
			SchemaHintJSON: `{"type":"object"}`, RenderHintJSON: `{"component":"json"}`, Active: true, ResourceType: "data",
			Status: "active", UsageStage: "draft_final", DraftEnabled: true, FinalEnabled: true, Editable: true, Referable: true,
		}},
	}}
	return New(repository.New(db.DB), gateway, "m6-service"), gateway
}

type recordingGateway struct {
	StaticGateway
	calls                   []string
	lastCommit              CommitGeneratedAssetAndChargeRequest
	refreshEstimate         CreditEstimateDTO
	generationEstimateCalls int
	chargeErr               error
	accessErr               error
}

func (g *recordingGateway) record(call string) {
	g.calls = append(g.calls, call)
}

func (g *recordingGateway) ListAvailableGenerationModels(ctx context.Context, auth AuthContextDTO, resourceType string, limit int, cursor string, traceID string) ([]ModelSummaryDTO, string, error) {
	g.record("ListAvailableGenerationModels")
	return g.StaticGateway.ListAvailableGenerationModels(ctx, auth, resourceType, limit, cursor, traceID)
}

func (g *recordingGateway) CheckProjectAccess(ctx context.Context, auth AuthContextDTO, projectID string, purpose businessagent.ProjectAccessPurpose, traceID string) (ProjectAccessDTO, error) {
	g.record("CheckProjectAccess")
	if g.accessErr != nil {
		return ProjectAccessDTO{}, g.accessErr
	}
	return g.StaticGateway.CheckProjectAccess(ctx, auth, projectID, purpose, traceID)
}

func (g *recordingGateway) GetReviewCandidateSkillSpec(ctx context.Context, auth AuthContextDTO, skillID string, versionID string, testCaseID string, testRunID string, traceID string) (ReviewCandidateSkillSpecDTO, error) {
	g.record("GetReviewCandidateSkillSpec")
	return g.StaticGateway.GetReviewCandidateSkillSpec(ctx, auth, skillID, versionID, testCaseID, testRunID, traceID)
}

func (g *recordingGateway) ListAssetElementTypes(ctx context.Context, auth AuthContextDTO, pageSize int, schemaVersion string, traceID string) ([]AssetElementTypeDTO, string, error) {
	g.record("ListAssetElementTypes")
	return g.StaticGateway.ListAssetElementTypes(ctx, auth, pageSize, schemaVersion, traceID)
}

func (g *recordingGateway) SaveSkillTestResult(ctx context.Context, auth AuthContextDTO, req SkillTestResultRequest, traceID string) (SkillTestResultDTO, error) {
	g.record("SaveSkillTestResult")
	return g.StaticGateway.SaveSkillTestResult(ctx, auth, req, traceID)
}

func (g *recordingGateway) EstimateToolCredits(ctx context.Context, auth AuthContextDTO, req EstimateToolCreditsRequest, traceID string) (CreditEstimateDTO, error) {
	g.record("EstimateToolCredits")
	return g.StaticGateway.EstimateToolCredits(ctx, auth, req, traceID)
}

func (g *recordingGateway) EstimateGenerationCredits(ctx context.Context, auth AuthContextDTO, req EstimateGenerationCreditsRequest, traceID string) (CreditEstimateDTO, error) {
	g.record("EstimateGenerationCredits")
	g.generationEstimateCalls++
	if g.generationEstimateCalls > 1 && g.refreshEstimate.EstimateID != "" {
		return g.refreshEstimate, nil
	}
	return g.StaticGateway.EstimateGenerationCredits(ctx, auth, req, traceID)
}

func (g *recordingGateway) FreezeCredits(ctx context.Context, auth AuthContextDTO, req FreezeCreditsRequest, traceID string) (FreezeCreditsDTO, error) {
	g.record("FreezeCredits")
	return g.StaticGateway.FreezeCredits(ctx, auth, req, traceID)
}

func (g *recordingGateway) ChargeToolUsageCredits(ctx context.Context, auth AuthContextDTO, req ChargeToolUsageCreditsRequest, traceID string) (ToolChargeDTO, error) {
	g.record("ChargeToolUsageCredits")
	if g.chargeErr != nil {
		return ToolChargeDTO{}, g.chargeErr
	}
	return g.StaticGateway.ChargeToolUsageCredits(ctx, auth, req, traceID)
}

func (g *recordingGateway) PrepareGeneratedAssetObjects(ctx context.Context, auth AuthContextDTO, req PrepareGeneratedAssetObjectsRequest, traceID string) ([]GeneratedUploadSlotDTO, error) {
	g.record("PrepareGeneratedAssetObjects")
	return g.StaticGateway.PrepareGeneratedAssetObjects(ctx, auth, req, traceID)
}

func (g *recordingGateway) CommitGeneratedAssetAndCharge(ctx context.Context, auth AuthContextDTO, req CommitGeneratedAssetAndChargeRequest, traceID string) (AssetCommitDTO, error) {
	g.record("CommitGeneratedAssetAndCharge")
	g.lastCommit = req
	return g.StaticGateway.CommitGeneratedAssetAndCharge(ctx, auth, req, traceID)
}

func (g *recordingGateway) ReleaseFrozenCredits(ctx context.Context, auth AuthContextDTO, req ReleaseFrozenCreditsRequest, traceID string) (ReleaseCreditsDTO, error) {
	g.record("ReleaseFrozenCredits")
	return g.StaticGateway.ReleaseFrozenCredits(ctx, auth, req, traceID)
}

func containsSubsequence(calls, want []string) bool {
	if len(want) == 0 {
		return true
	}
	pos := 0
	for _, call := range calls {
		if call == want[pos] {
			pos++
			if pos == len(want) {
				return true
			}
		}
	}
	return false
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

func hasEvent(events []model.Event, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
