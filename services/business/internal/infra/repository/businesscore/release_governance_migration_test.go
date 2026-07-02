package businesscore_test

import (
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"gorm.io/gorm"
)

func TestBusinessReleaseGovernanceMigration(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_release_governance")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-07-02-m7-release-governance/business")
	testdb.RequireNoForeignKeys(t, db.DB)

	for _, table := range []string{
		"release_batches",
		"migration_jobs",
		"contract_fixture_runs",
		"runtime_health_metrics",
		"operational_incidents",
	} {
		if !testdb.TableExists(t, db.DB, table) {
			t.Fatalf("business release governance migration table %s missing", table)
		}
	}
	if testdb.TableExists(t, db.DB, "agent_release_audits") || testdb.TableExists(t, db.DB, "agent_runtime_health_snapshots") {
		t.Fatal("business release governance database must not contain agent release audit tables")
	}

	requireReleaseGovernanceColumns(t, db.DB, "release_batches", []string{
		"batch_id", "batch_name", "status", "release_scope", "feature_flags_json",
		"gates_json", "rollback_plan", "started_at", "completed_at", "created_by",
		"idempotency_key", "trace_id", "created_at", "updated_at",
	})
	requireReleaseGovernanceColumns(t, db.DB, "migration_jobs", []string{
		"job_id", "release_batch_id", "job_type", "status", "dry_run_result",
		"validation_result", "idempotency_key", "requested_by", "trace_id",
		"started_at", "completed_at", "created_at", "updated_at",
	})
	requireReleaseGovernanceColumns(t, db.DB, "contract_fixture_runs", []string{
		"run_id", "release_batch_id", "fixture_version", "target", "status",
		"diff_summary", "contract_refs", "idempotency_key", "trace_id",
		"created_at", "updated_at",
	})
	requireReleaseGovernanceColumns(t, db.DB, "runtime_health_metrics", []string{
		"metric_id", "release_batch_id", "metric_type", "window_start", "window_end",
		"value_json", "alert_status", "trace_id", "created_at",
	})
	requireReleaseGovernanceColumns(t, db.DB, "operational_incidents", []string{
		"incident_id", "release_batch_id", "severity", "status", "impact_summary",
		"resolution_summary", "trace_refs", "detected_at", "resolved_at",
		"created_by", "idempotency_key", "trace_id", "created_at", "updated_at",
	})

	execBusinessReleaseGovernanceInsert(t, db.DB)

	if err := db.DB.Exec(`
		INSERT INTO migration_jobs (
			job_id, release_batch_id, job_type, status, dry_run_result, validation_result,
			idempotency_key, requested_by, trace_id
		) VALUES (
			'job_m7_duplicate', 'batch_m7_r1', 'schema', 'dry_run',
			'{}'::jsonb, '{}'::jsonb, 'idem_migration_r1', 'admin_001', 'trace_m7_r1'
		)
	`).Error; err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate idempotency key for migration_jobs, got %v", err)
	}

	testdb.DownMigrations(t, migrator)
	if count := testdb.CountTables(t, db.DB); count != 0 {
		t.Fatalf("expected migration down to drop tables, got %d", count)
	}
}

func requireReleaseGovernanceColumns(t *testing.T, db *gorm.DB, table string, columns []string) {
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

func execBusinessReleaseGovernanceInsert(t *testing.T, db *gorm.DB) {
	t.Helper()
	sql := `
		INSERT INTO release_batches (
			batch_id, batch_name, status, release_scope, feature_flags_json,
			gates_json, rollback_plan, started_at, created_by, idempotency_key, trace_id
		) VALUES (
			'batch_m7_r1', 'M7 R1 data foundation', 'running', 'm7_release_governance',
			'{"admin_governance_v2": false}'::jsonb,
			'{"required":["Contract Gate","Migration Gate","Fixture Gate"]}'::jsonb,
			'{"steps":["close_flag","stop_worker","release_freezes"]}'::jsonb,
			'2026-07-02T00:00:00Z', 'admin_001', 'idem_batch_r1', 'trace_m7_r1'
		);
		INSERT INTO migration_jobs (
			job_id, release_batch_id, job_type, status, dry_run_result, validation_result,
			idempotency_key, requested_by, trace_id, started_at
		) VALUES (
			'job_m7_r1', 'batch_m7_r1', 'schema', 'dry_run',
			'{"tables":5}'::jsonb, '{"passed":true}'::jsonb,
			'idem_migration_r1', 'admin_001', 'trace_m7_r1', '2026-07-02T00:01:00Z'
		);
		INSERT INTO contract_fixture_runs (
			run_id, release_batch_id, fixture_version, target, status, diff_summary,
			contract_refs, idempotency_key, trace_id
		) VALUES (
			'fixture_run_m7_r1', 'batch_m7_r1', 'fixtures-2026-07-02',
			'releasegate', 'passed', '{}'::jsonb,
			'["internal/contracts/releasegate/e2e.go"]'::jsonb,
			'idem_fixture_r1', 'trace_m7_r1'
		);
		INSERT INTO runtime_health_metrics (
			metric_id, release_batch_id, metric_type, window_start, window_end,
			value_json, alert_status, trace_id
		) VALUES (
			'metric_m7_r1', 'batch_m7_r1', 'agent_run_success_rate',
			'2026-07-02T00:00:00Z', '2026-07-02T00:05:00Z',
			'{"value":0.99}'::jsonb, 'ok', 'trace_m7_r1'
		);
		INSERT INTO operational_incidents (
			incident_id, release_batch_id, severity, status, impact_summary,
			resolution_summary, trace_refs, detected_at, created_by, idempotency_key, trace_id
		) VALUES (
			'incident_m7_r1', 'batch_m7_r1', 'sev3', 'reviewing',
			'fixture dry-run mismatch', '',
			'["trace_m7_r1"]'::jsonb, '2026-07-02T00:06:00Z',
			'admin_001', 'idem_incident_r1', 'trace_m7_r1'
		);
	`
	if err := db.Exec(sql).Error; err != nil {
		t.Fatalf("insert business release governance rows: %v", err)
	}
}
