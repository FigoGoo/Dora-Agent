package businesscore_test

import (
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
