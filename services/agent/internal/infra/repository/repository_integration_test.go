package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/state"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
	"gorm.io/datatypes"
)

func TestAgentMigrationRepositoryAndBoundaries(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_agent")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/20260627_agent_runtime/agent")
	testdb.RequireNoForeignKeys(t, db.DB)
	if !testdb.TableExists(t, db.DB, "agent_sessions") || !testdb.TableExists(t, db.DB, "agent_runtime_configs") {
		t.Fatal("agent runtime tables missing after migration")
	}
	if testdb.TableExists(t, db.DB, "idempotency_records") {
		t.Fatal("agent database must not contain business idempotency table")
	}

	repo := repository.New(db.DB)
	now := time.Now().UTC()
	session := &model.Session{
		ID:             "sess_1",
		TenantID:       "tenant_1",
		SpaceID:        "space_1",
		ProjectID:      "project_1",
		UserID:         "user_1",
		Title:          "M1",
		IdempotencyKey: "session-key",
		TraceID:        "trace-agent-db",
	}
	if err := repo.CreateSession(t.Context(), session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if got, err := repo.GetSessionByIdempotencyKey(t.Context(), "session-key"); err != nil || got.ID != session.ID {
		t.Fatalf("get session by idempotency: got=%#v err=%v", got, err)
	}

	run := &model.Run{
		ID:                   "run_1",
		SessionID:            session.ID,
		ProjectID:            session.ProjectID,
		SpaceID:              session.SpaceID,
		UserID:               session.UserID,
		TurnNo:               1,
		RuntimeConfigVersion: "local-dev",
		IdempotencyKey:       "run-key",
		TraceID:              "trace-agent-db",
	}
	if err := repo.CreateRun(t.Context(), run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := repo.UpdateRunStatus(t.Context(), run.ID, state.RunStatusRunning, "", ""); err != nil {
		t.Fatalf("update run running: %v", err)
	}
	if err := repo.UpdateRunStatus(t.Context(), run.ID, state.RunStatusCompleted, "", ""); err != nil {
		t.Fatalf("update run completed: %v", err)
	}
	if err := repo.UpdateRunStatus(t.Context(), run.ID, state.RunStatusRunning, "", ""); !errors.Is(err, repository.ErrInvalidStateTransition) {
		t.Fatalf("expected invalid state transition, got %v", err)
	}

	events := []model.Event{
		{EventID: "evt_1", Type: "run.started", SessionID: session.ID, RunID: run.ID, ProjectID: session.ProjectID, SpaceID: session.SpaceID, ActorUserID: session.UserID, Sequence: 1, Component: "agent", PayloadSchemaVersion: "v1", TraceID: "trace-agent-db"},
		{EventID: "evt_2", Type: "run.completed", SessionID: session.ID, RunID: run.ID, ProjectID: session.ProjectID, SpaceID: session.SpaceID, ActorUserID: session.UserID, Sequence: 2, Component: "agent", PayloadSchemaVersion: "v1", TraceID: "trace-agent-db"},
	}
	for i := range events {
		if err := repo.AppendEvent(t.Context(), &events[i]); err != nil {
			t.Fatalf("append event: %v", err)
		}
	}
	replay, err := repo.ListEventsAfterSequence(t.Context(), run.ID, 0, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(replay) != 2 || replay[0].Sequence != 1 || replay[1].Sequence != 2 {
		t.Fatalf("unexpected replay events: %#v", replay)
	}

	interrupt := &model.Interrupt{
		ID:            "interrupt_1",
		RunID:         run.ID,
		InterruptType: "human_confirm",
		Reason:        "confirm",
		ExpiresAt:     now.Add(time.Hour),
		TraceID:       "trace-agent-db",
	}
	if err := repo.CreateInterrupt(t.Context(), interrupt); err != nil {
		t.Fatalf("create interrupt: %v", err)
	}
	if got, err := repo.GetPendingInterrupt(t.Context(), run.ID); err != nil || got.ID != interrupt.ID {
		t.Fatalf("get pending interrupt got=%#v err=%v", got, err)
	}
	if err := repo.ResolveInterrupt(t.Context(), interrupt.ID, state.InterruptStatusResolved); err != nil {
		t.Fatalf("resolve interrupt: %v", err)
	}

	artifact := &model.Artifact{
		ID:           "artifact_1",
		SessionID:    session.ID,
		ProjectID:    session.ProjectID,
		RunID:        run.ID,
		ArtifactType: "generated_asset",
		Status:       "draft",
		ElementType:  "image",
		Content:      datatypes.JSON([]byte(`{"name":"draft"}`)),
		TraceID:      "trace-agent-db",
	}
	if err := repo.CreateArtifact(t.Context(), artifact); err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	if artifacts, err := repo.ListArtifacts(t.Context(), session.ID, 10, 0); err != nil || len(artifacts) != 1 {
		t.Fatalf("list artifacts len=%d err=%v", len(artifacts), err)
	}

	safety := &model.SafetyEvaluation{
		SafetyEvidenceID:      "safety_1",
		Scene:                 "asset_publish",
		TargetType:            "artifact",
		TargetRefID:           artifact.ID,
		EvaluatedObjectDigest: "sha256",
		PolicyVersion:         "local-v1",
		EvidenceVersion:       "v1",
		Result:                "allow",
		TraceID:               "trace-agent-db",
		ExpiresAt:             now.Add(24 * time.Hour),
	}
	if err := repo.CreateSafetyEvaluation(t.Context(), safety); err != nil {
		t.Fatalf("create safety: %v", err)
	}
	if got, err := repo.GetSafetyEvaluation(t.Context(), safety.SafetyEvidenceID); err != nil || got.Result != "allow" {
		t.Fatalf("get safety got=%#v err=%v", got, err)
	}

	runtimeConfig := &model.RuntimeConfig{
		ConfigKey:      "agent.default",
		Version:        "local-dev",
		Status:         "active",
		Owner:          "m1",
		Content:        datatypes.JSON([]byte(`{"model":"test"}`)),
		SafeConfigRefs: datatypes.JSON([]byte(`[]`)),
		ActivatedAt:    &now,
	}
	if err := repo.UpsertRuntimeConfig(t.Context(), runtimeConfig); err != nil {
		t.Fatalf("upsert runtime config: %v", err)
	}
	if got, err := repo.GetActiveRuntimeConfig(t.Context(), "agent.default"); err != nil || got.Version != "local-dev" {
		t.Fatalf("get runtime config got=%#v err=%v", got, err)
	}

	testdb.DownMigrations(t, migrator)
	if count := testdb.CountTables(t, db.DB); count != 0 {
		t.Fatalf("expected migration down to drop tables, got %d", count)
	}
}
