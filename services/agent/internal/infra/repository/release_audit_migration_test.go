package repository_test

import (
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"gorm.io/gorm"
)

func TestAgentReleaseAuditMigration(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_agent_release_audit")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-07-02-m7-release-governance/agent")
	testdb.RequireNoForeignKeys(t, db.DB)

	for _, table := range []string{
		"agent_release_audits",
		"agent_runtime_health_snapshots",
	} {
		if !testdb.TableExists(t, db.DB, table) {
			t.Fatalf("agent release audit migration table %s missing", table)
		}
	}
	if testdb.TableExists(t, db.DB, "release_batches") || testdb.TableExists(t, db.DB, "migration_jobs") {
		t.Fatal("agent release audit database must not contain business release governance tables")
	}

	requireAgentColumns(t, db.DB, "agent_release_audits", []string{
		"audit_id", "run_id", "release_batch_id", "feature_flags_json",
		"trace_id", "idempotency_key", "created_at",
	})
	requireAgentColumns(t, db.DB, "agent_runtime_health_snapshots", []string{
		"snapshot_id", "run_id", "graph_plan_id", "release_batch_id", "task_summary",
		"error_summary", "metric_summary", "status", "trace_id", "captured_at",
		"idempotency_key", "created_at",
	})

	execAgentReleaseAuditInsert(t, db.DB)

	if err := db.DB.Exec(`
		INSERT INTO agent_release_audits (
			audit_id, run_id, release_batch_id, feature_flags_json, trace_id, idempotency_key
		) VALUES (
			'audit_duplicate', 'run_m7_r1_duplicate', 'batch_m7_r1',
			'{"agent_runtime_v2":false}'::jsonb, 'trace_m7_r1', 'idem_agent_audit_r1'
		)
	`).Error; err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate idempotency key for agent_release_audits, got %v", err)
	}

	testdb.DownMigrations(t, migrator)
	if count := testdb.CountTables(t, db.DB); count != 0 {
		t.Fatalf("expected migration down to drop tables, got %d", count)
	}
}

func requireAgentColumns(t *testing.T, db *gorm.DB, table string, columns []string) {
	t.Helper()
	for _, column := range columns {
		var exists bool
		err := db.Raw(`
			SELECT EXISTS (
				SELECT 1
				FROM information_schema.columns
				WHERE table_schema = 'public' AND table_name = ? AND column_name = ?
			)
		`, table, column).Scan(&exists).Error
		if err != nil {
			t.Fatalf("check column %s.%s: %v", table, column, err)
		}
		if !exists {
			t.Fatalf("expected column %s.%s to exist", table, column)
		}
	}
}

func execAgentReleaseAuditInsert(t *testing.T, db *gorm.DB) {
	t.Helper()
	sql := `
		INSERT INTO agent_release_audits (
			audit_id, run_id, release_batch_id, feature_flags_json, trace_id, idempotency_key
		) VALUES (
			'audit_m7_r1', 'run_m7_r1', 'batch_m7_r1',
			'{"agent_runtime_v2":false,"tool_generation_v2":false}'::jsonb,
			'trace_m7_r1', 'idem_agent_audit_r1'
		);
		INSERT INTO agent_runtime_health_snapshots (
			snapshot_id, run_id, graph_plan_id, release_batch_id, task_summary,
			error_summary, metric_summary, status, trace_id, captured_at, idempotency_key
		) VALUES (
			'snapshot_m7_r1', 'run_m7_r1', 'graph_plan_m7_r1', 'batch_m7_r1',
			'{"pending":0,"running":1,"succeeded":3}'::jsonb,
			'{"errors":[]}'::jsonb,
			'{"agent_run_success_rate":0.99}'::jsonb,
			'ok', 'trace_m7_r1', '2026-07-02T00:05:00Z', 'idem_agent_snapshot_r1'
		);
	`
	if err := db.Exec(sql).Error; err != nil {
		t.Fatalf("insert agent release audit rows: %v", err)
	}
}
