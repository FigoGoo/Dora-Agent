package http

import (
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/assetdict"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/skillcatalog"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/toolpolicy"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	"github.com/gin-gonic/gin"
)

func TestAdminPaginationQueryAcceptsOpenAPIAndLegacyParams(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		wantLimit  int
		wantOffset int
	}{
		{name: "openapi page size and token", target: "/api/admin/users?page_size=20&page_token=40", wantLimit: 20, wantOffset: 40},
		{name: "legacy limit and offset", target: "/api/admin/users?limit=30&offset=60", wantLimit: 30, wantOffset: 60},
		{name: "legacy limit wins while page token supplies offset", target: "/api/admin/users?page_size=20&limit=50&page_token=70", wantLimit: 50, wantOffset: 70},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(nethttp.MethodGet, tc.target, nil)

			if got := adminPageLimit(ctx, 10); got != tc.wantLimit {
				t.Fatalf("limit=%d want=%d", got, tc.wantLimit)
			}
			if got := adminPageOffset(ctx); got != tc.wantOffset {
				t.Fatalf("offset=%d want=%d", got, tc.wantOffset)
			}
		})
	}
}

func TestAdminAuthenticatedRequestReturnsRenewedSessionExpiryHeader(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_admin_session_header")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	repo := businesscore.New(db.DB)
	if err := repo.DB().WithContext(t.Context()).Model(&businesscore.PlatformAdmin{}).
		Where("id = ?", "adm_root").Update("must_rotate_password", false).Error; err != nil {
		t.Fatalf("prep admin: %v", err)
	}
	adminApp := admin.New(repo, idempotency.NewGuard(db.DB, time.Hour, time.Hour), auditlog.NewGormWriter(db.DB))
	router := NewRouter(RouterOptions{Admin: adminApp})
	token := loginAdmin(t, router, "admin@dora.local", "local-admin-change-me")

	req := httptest.NewRequest(nethttp.MethodGet, "/api/admin/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusOK {
		t.Fatalf("dashboard status=%d body=%s", rec.Code, rec.Body.String())
	}
	rawExpiry := rec.Header().Get("X-Admin-Session-Expires-At")
	if rawExpiry == "" {
		t.Fatalf("expected renewed admin session expiry header")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, rawExpiry)
	if err != nil {
		t.Fatalf("parse expiry header %q: %v", rawExpiry, err)
	}
	if time.Until(expiresAt) < 6*24*time.Hour {
		t.Fatalf("renewed admin session expiry is too soon: %s", expiresAt)
	}
}

func TestAdminSkillReviewRequestAcceptsOpenAPIDecisionFields(t *testing.T) {
	req := adminSkillReviewRequest{Decision: "reject", Reason: "证据不足"}

	if got := req.normalizedAction(); got != "reject" {
		t.Fatalf("normalized action=%q", got)
	}
	if got := req.normalizedComment(); got != "证据不足" {
		t.Fatalf("normalized comment=%q", got)
	}
}

func TestAdminRegisterToolPersistsReasonAndPolicies(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_admin_tool_register")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	repo := businesscore.New(db.DB)
	adminApp := admin.New(repo, idempotency.NewGuard(db.DB, time.Hour, time.Hour), auditlog.NewGormWriter(db.DB))
	toolApp := toolpolicy.New(repo)
	router := NewRouter(RouterOptions{Admin: adminApp, Tool: toolApp})
	token := loginAdmin(t, router, "admin@dora.local", "local-admin-change-me")

	resp := requestJSON(t, router, nethttp.MethodPost, "/api/admin/tools", token, "idem-admin-tool-register", map[string]any{
		"tool_name":             "admin_storyboard_extract",
		"tool_type":             "builtin",
		"display_name":          "分镜提取",
		"description":           "从剧本文本中提取镜头、人物和场景信息。",
		"status":                "active",
		"version":               "1.0.0",
		"input_schema_json":     `{"type":"object"}`,
		"output_schema_json":    `{"type":"object"}`,
		"allowed":               true,
		"risk_level":            "medium",
		"requires_confirmation": true,
		"timeout_ms":            45000,
		"retry_policy":          map[string]any{"max_retries": "1"},
		"cancel_policy":         map[string]any{"cancelable": "true"},
		"charge_mode":           "per_call",
		"billing_unit":          "call",
		"unit_points":           5,
		"free_quota":            1,
		"min_charge_points":     1,
		"reason":                "注册内置分镜 Tool",
	})
	data := resp["data"].(map[string]any)
	if data["tool_key"] != "admin_storyboard_extract:builtin" || data["pricing_policy_id"] == "" {
		t.Fatalf("register tool response mismatch: %#v", data)
	}

	var change businesscore.ToolPolicyChangeRecord
	if err := repo.DB().WithContext(t.Context()).
		Where("tool_name = ? AND tool_type = ? AND change_type = ?", "admin_storyboard_extract", "builtin", "tool.register").
		First(&change).Error; err != nil {
		t.Fatalf("load register change record: %v", err)
	}
	var after map[string]any
	if err := json.Unmarshal(change.AfterJSON, &after); err != nil {
		t.Fatalf("decode after json: %v", err)
	}
	if after["reason"] != "注册内置分镜 Tool" {
		t.Fatalf("register reason missing from change record: %#v", after)
	}
}

func TestAdminCreateSystemSkillPersistsOutputElements(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_admin_skill_outputs")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	repo := businesscore.New(db.DB)
	guard := idempotency.NewGuard(db.DB, time.Hour, time.Hour)
	auditWriter := auditlog.NewGormWriter(db.DB)
	adminApp := admin.New(repo, guard, auditWriter)
	dictionaryApp := assetdict.New(repo)
	skillApp := skillcatalog.New(repo)
	skillApp.SetDictionary(dictionaryApp)
	router := NewRouter(RouterOptions{Admin: adminApp, Skill: skillApp})
	token := loginAdmin(t, router, "admin@dora.local", "local-admin-change-me")

	resp := requestJSON(t, router, nethttp.MethodPost, "/api/admin/skills/system", token, "idem-admin-skill-output-elements", map[string]any{
		"skill_key":       "admin_output_elements",
		"skill_name":      "Admin Output Elements",
		"version":         "1.0.0",
		"route_hints":     map[string]string{"keyword": "admin-output-elements"},
		"skill_spec_json": "{}",
		"output_elements": []map[string]any{{
			"element_type":  "short_text",
			"element_name":  "标题",
			"required":      true,
			"use_draft":     true,
			"use_final":     true,
			"editable":      true,
			"referable":     true,
			"display_order": 10,
			"display_slot":  "asset_detail",
			"schema_json":   `{"max_length":64}`,
		}},
	})
	data := resp["data"].(map[string]any)
	skillID := data["skill_id"].(string)
	if data["latest_version_id"] == "" {
		t.Fatalf("create system skill response missing latest_version_id: %#v", data)
	}

	var rows []businesscore.SkillOutputElementSchema
	if err := repo.DB().WithContext(t.Context()).Where("skill_id = ?", skillID).Find(&rows).Error; err != nil {
		t.Fatalf("load output elements: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected output element persisted, got %d rows", len(rows))
	}
	row := rows[0]
	if row.ElementType != "short_text" || row.ElementName != "标题" || !row.Required || !row.UseDraft || !row.UseFinal || !row.Editable || !row.Referable || row.DisplayOrder != 10 || row.DisplaySlot != "asset_detail" {
		t.Fatalf("unexpected output element row: %#v", row)
	}
}
