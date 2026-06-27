package idempotency_test

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
)

func TestBusinessMigrationSeedIdempotencyAuditAndBoundaries(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	testdb.RequireNoForeignKeys(t, db.DB)
	if !testdb.TableExists(t, db.DB, "idempotency_records") || !testdb.TableExists(t, db.DB, "business_audit_logs") {
		t.Fatal("business infra tables missing after migration")
	}
	if testdb.TableExists(t, db.DB, "agent_sessions") {
		t.Fatal("business database must not contain agent runtime table")
	}
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	guard := idempotency.NewGuard(db.DB, time.Hour, time.Hour)
	hash := idempotency.HashRequest([]byte(`{"action":"create"}`))
	input := idempotency.BeginInput{
		Scope:          "project.create",
		IdempotencyKey: "idem-key",
		RequestHash:    hash,
		ActorUserID:    "user_1",
	}
	decision, err := guard.Begin(t.Context(), input)
	if err != nil {
		t.Fatalf("begin idempotency: %v", err)
	}
	if decision.Mode != idempotency.DecisionProceed {
		t.Fatalf("expected proceed, got %s", decision.Mode)
	}
	processing, err := guard.Begin(t.Context(), input)
	if err != nil {
		t.Fatalf("begin processing idempotency: %v", err)
	}
	if processing.Mode != idempotency.DecisionProcessing {
		t.Fatalf("expected processing, got %s", processing.Mode)
	}
	if err := guard.Succeed(t.Context(), decision.Record.ID, "OK", []byte(`{"id":"project_1"}`)); err != nil {
		t.Fatalf("succeed idempotency: %v", err)
	}
	replay, err := guard.Begin(t.Context(), input)
	if err != nil {
		t.Fatalf("begin replay idempotency: %v", err)
	}
	if replay.Mode != idempotency.DecisionReplay {
		t.Fatalf("expected replay, got %s", replay.Mode)
	}
	input.RequestHash = idempotency.HashRequest([]byte(`{"action":"different"}`))
	conflict, err := guard.Begin(t.Context(), input)
	if err != nil {
		t.Fatalf("begin conflict idempotency: %v", err)
	}
	if conflict.Mode != idempotency.DecisionConflict {
		t.Fatalf("expected conflict, got %s", conflict.Mode)
	}

	writer := auditlog.NewGormWriter(db.DB)
	actor := "user_1"
	if err := writer.Write(t.Context(), &auditlog.AuditRecord{
		TraceID:           "trace-business-db",
		RequestID:         "request_1",
		Source:            "http",
		ActorUserID:       &actor,
		LoginIdentityType: "user",
		Action:            "project.create",
		ResourceType:      "project",
		ResourceID:        ptr("project_1"),
		Result:            "success",
	}); err != nil {
		t.Fatalf("write audit: %v", err)
	}
	var auditCount int64
	if err := db.DB.Table("business_audit_logs").Where("trace_id = ?", "trace-business-db").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit record, got %d", auditCount)
	}

	testdb.DownMigrations(t, migrator)
	if count := testdb.CountTables(t, db.DB); count != 0 {
		t.Fatalf("expected migration down to drop tables, got %d", count)
	}
}

func ptr(value string) *string {
	return &value
}
