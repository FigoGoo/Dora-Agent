package businesscore_test

import (
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
)

var commonColumnBusinessTables = []string{
	"business_users", "auth_sessions", "business_spaces", "enterprises", "enterprise_members", "enterprise_invites",
	"platform_admins", "platform_admin_bootstraps", "platform_admin_sessions",
	"projects", "project_assets", "project_works",
	"model_providers", "model_provider_credentials", "models", "model_prices", "default_models",
	"tool_definitions", "tool_policies", "tool_pricing_policies", "tool_whitelist_rules",
	"skills", "skill_versions", "skill_tool_bindings", "skill_output_element_schemas", "skill_test_cases", "skill_test_runs", "skill_review_records",
	"credit_accounts", "credit_batches", "credit_estimates", "credit_estimate_items", "credit_freezes", "credit_tool_charge_batches", "credit_tool_charge_items", "redeem_code_batches", "redeem_codes",
	"assets", "asset_storage_objects", "upload_intents", "asset_elements", "asset_element_types",
	"asset_commit_batches", "asset_commit_items",
	"works", "work_assets", "work_public_snapshots", "work_likes", "work_categories",
	"notifications", "notification_create_failures",
	"credit_freeze_batch_items", "generated_asset_object_slots",
}

var appendOnlyBusinessTables = []string{
	"business_audit_logs",
	"credit_ledger_entries",
	"asset_access_logs",
	"admin_login_attempts",
	"redeem_code_redemptions",
	"work_moderation_records",
	"skill_review_records",
	"asset_element_type_change_records",
	"tool_policy_change_records",
	"model_connectivity_tests",
}

var softDeleteUniqueConstraintPolicies = map[string]string{
	"business_users":               "global_identity",
	"auth_sessions":                "secret_or_session_token",
	"business_spaces":              "status_governed_identity",
	"enterprises":                  "global_identity",
	"enterprise_members":           "membership_history",
	"enterprise_invites":           "secret_or_session_token",
	"platform_admins":              "global_identity",
	"platform_admin_bootstraps":    "secret_or_session_token",
	"platform_admin_sessions":      "secret_or_session_token",
	"projects":                     "global_identity",
	"project_assets":               "relationship_status_governed",
	"project_works":                "relationship_status_governed",
	"model_providers":              "global_identity",
	"model_provider_credentials":   "status_governed_identity",
	"models":                       "status_governed_identity",
	"model_prices":                 "immutable_snapshot_identity",
	"default_models":               "status_governed_identity",
	"tool_definitions":             "status_governed_identity",
	"tool_pricing_policies":        "immutable_snapshot_identity",
	"tool_whitelist_rules":         "status_governed_identity",
	"skills":                       "global_identity",
	"skill_versions":               "immutable_snapshot_identity",
	"skill_tool_bindings":          "relationship_status_governed",
	"skill_output_element_schemas": "relationship_status_governed",
	"skill_test_runs":              "idempotency_replay",
	"credit_accounts":              "financial_identity",
	"credit_estimates":             "financial_trace_identity",
	"credit_estimate_items":        "financial_trace_identity",
	"credit_freezes":               "financial_idempotency",
	"credit_tool_charge_batches":   "financial_idempotency",
	"credit_tool_charge_items":     "financial_trace_identity",
	"redeem_code_batches":          "global_identity",
	"redeem_codes":                 "secret_or_session_token",
	"assets":                       "global_identity",
	"asset_storage_objects":        "object_storage_identity",
	"upload_intents":               "idempotency_replay",
	"asset_elements":               "relationship_status_governed",
	"asset_element_types":          "status_governed_identity",
	"asset_commit_batches":         "idempotency_replay",
	"asset_commit_items":           "financial_trace_identity",
	"works":                        "global_identity",
	"work_assets":                  "relationship_status_governed",
	"work_public_snapshots":        "public_share_identity",
	"work_likes":                   "interaction_history",
	"work_categories":              "status_governed_identity",
	"notifications":                "idempotency_replay",
	"notification_create_failures": "idempotency_replay",
	"credit_freeze_batch_items":    "financial_trace_identity",
	"generated_asset_object_slots": "idempotency_replay",
}

func TestBusinessSchemaBaselineWhitelistsMutableAndAppendOnlyTables(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_schema_baseline")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })

	if len(commonColumnBusinessTables) != 53 {
		t.Fatalf("common column table whitelist drifted: got %d", len(commonColumnBusinessTables))
	}
	if len(appendOnlyBusinessTables) != 10 {
		t.Fatalf("append-only table whitelist drifted: got %d", len(appendOnlyBusinessTables))
	}
	for _, table := range commonColumnBusinessTables {
		requireColumns(t, db, table, "created_by", "updated_by", "deleted_at")
	}
	for _, table := range appendOnlyBusinessTables {
		requireAppendOnlyTrigger(t, db, table)
	}
	requireOnlyDocumentedOverlap(t, commonColumnBusinessTables, appendOnlyBusinessTables, map[string]bool{"skill_review_records": true})
}

func TestSoftDeleteUniqueConstraintsHaveExplicitReusePolicy(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_unique_policy")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })

	if len(softDeleteUniqueConstraintPolicies) != 49 {
		t.Fatalf("soft-delete unique policy table whitelist drifted: got %d", len(softDeleteUniqueConstraintPolicies))
	}
	commonTables := setOf(commonColumnBusinessTables)
	for table, policy := range softDeleteUniqueConstraintPolicies {
		if !commonTables[table] {
			t.Fatalf("soft-delete unique policy references non-common table %s", table)
		}
		if strings.TrimSpace(policy) == "" {
			t.Fatalf("soft-delete unique policy for %s is empty", table)
		}
	}

	type uniqueIndex struct {
		TableName string
		IndexName string
		IndexDef  string
		Predicate string
	}
	var rows []uniqueIndex
	err := db.DB.Raw(`
		SELECT
			t.relname AS table_name,
			i.relname AS index_name,
			pg_get_indexdef(i.oid) AS index_def,
			COALESCE(pg_get_expr(ix.indpred, ix.indrelid), '') AS predicate
		FROM pg_index ix
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = 'public'
		  AND ix.indisunique
		  AND NOT ix.indisprimary
		ORDER BY t.relname, i.relname
	`).Scan(&rows).Error
	if err != nil {
		t.Fatalf("list unique indexes: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected business schema unique indexes")
	}
	for _, row := range rows {
		if !commonTables[row.TableName] {
			continue
		}
		policy, ok := softDeleteUniqueConstraintPolicies[row.TableName]
		if !ok {
			t.Fatalf("unique index %s on soft-delete table %s has no reuse policy: %s", row.IndexName, row.TableName, row.IndexDef)
		}
		if strings.Contains(strings.ToLower(row.Predicate), "deleted_at") && policy != "partial_active_reuse" {
			t.Fatalf("unique index %s on %s is partial on deleted_at but policy is %s: %s", row.IndexName, row.TableName, policy, row.IndexDef)
		}
	}
}

func requireColumns(t *testing.T, db *testdb.Database, table string, columns ...string) {
	t.Helper()
	if !testdb.TableExists(t, db.DB, table) {
		t.Fatalf("expected table %s", table)
	}
	for _, column := range columns {
		var exists bool
		err := db.DB.Raw(
			"SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ? AND column_name = ?)",
			table, column,
		).Scan(&exists).Error
		if err != nil {
			t.Fatalf("check column %s.%s: %v", table, column, err)
		}
		if !exists {
			t.Fatalf("expected column %s.%s", table, column)
		}
	}
}

func requireAppendOnlyTrigger(t *testing.T, db *testdb.Database, table string) {
	t.Helper()
	if !testdb.TableExists(t, db.DB, table) {
		t.Fatalf("expected table %s", table)
	}
	var count int
	err := db.DB.Raw(
		"SELECT COUNT(*) FROM pg_trigger WHERE tgrelid = ?::regclass AND tgname = 'trg_append_only' AND NOT tgisinternal",
		table,
	).Scan(&count).Error
	if err != nil {
		t.Fatalf("check append-only trigger for %s: %v", table, err)
	}
	if count != 1 {
		t.Fatalf("expected append-only trigger for %s, got %d", table, count)
	}
}

func setOf(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func requireOnlyDocumentedOverlap(t *testing.T, left, right []string, allowed map[string]bool) {
	t.Helper()
	seen := map[string]bool{}
	for _, value := range left {
		seen[value] = true
	}
	for _, value := range right {
		if seen[value] && !allowed[value] {
			t.Fatalf("table %s is in both schema baseline whitelists without an explicit exception", value)
		}
	}
}
