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
		TenantID:       "tenant_space_1",
		SpaceID:        "space_1",
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
	if err := guard.Succeed(t.Context(), decision.Record.ID, idempotency.ResultRef{Type: "project", ID: "project_1"}); err != nil {
		t.Fatalf("succeed idempotency: %v", err)
	}
	replay, err := guard.Begin(t.Context(), input)
	if err != nil {
		t.Fatalf("begin replay idempotency: %v", err)
	}
	if replay.Mode != idempotency.DecisionReplay {
		t.Fatalf("expected replay, got %s", replay.Mode)
	}
	if replay.ReplayResult == nil || replay.ReplayResult.Type != "project" || replay.ReplayResult.ID != "project_1" {
		t.Fatalf("unexpected replay result: %#v", replay.ReplayResult)
	}
	input.RequestHash = idempotency.HashRequest([]byte(`{"action":"different"}`))
	conflict, err := guard.Begin(t.Context(), input)
	if err != nil {
		t.Fatalf("begin conflict idempotency: %v", err)
	}
	if conflict.Mode != idempotency.DecisionConflict {
		t.Fatalf("expected conflict, got %s", conflict.Mode)
	}
	otherTenant := input
	otherTenant.TenantID = "tenant_space_2"
	otherTenant.SpaceID = "space_2"
	otherTenant.RequestHash = idempotency.HashRequest([]byte(`{"action":"different"}`))
	crossTenant, err := guard.Begin(t.Context(), otherTenant)
	if err != nil {
		t.Fatalf("begin cross-tenant idempotency: %v", err)
	}
	if crossTenant.Mode != idempotency.DecisionProceed {
		t.Fatalf("expected cross-tenant proceed, got %s", crossTenant.Mode)
	}

	writer := auditlog.NewGormWriter(db.DB)
	actor := "user_1"
	if err := writer.Write(t.Context(), &auditlog.AuditRecord{
		TraceID:        "trace-business-db",
		OperatorType:   "user",
		OperatorID:     &actor,
		TenantID:       "tenant_space_1",
		SpaceID:        ptr("space_1"),
		BusinessAction: "project.create",
		ResourceType:   "project",
		ResourceID:     ptr("project_1"),
		Result:         "success",
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
	var uniqueCount int
	if err := db.DB.Raw("SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'idempotency_records' AND indexdef LIKE '%tenant_id%' AND indexdef LIKE '%scope%' AND indexdef LIKE '%idempotency_key%'").Scan(&uniqueCount).Error; err != nil {
		t.Fatalf("check tenant idempotency unique index: %v", err)
	}
	if uniqueCount == 0 {
		t.Fatal("expected tenant/scope/idempotency_key unique index")
	}

	testdb.DownMigrations(t, migrator)
	if count := testdb.CountTables(t, db.DB); count != 0 {
		t.Fatalf("expected migration down to drop tables, got %d", count)
	}
}

func ptr(value string) *string {
	return &value
}
