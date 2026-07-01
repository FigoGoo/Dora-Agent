package http

import (
	"net/http"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/credit"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
)

func TestCreditB0RechargeHTTPAndAdminQueries(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_credit_b0_http")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	repo := businesscore.New(db.DB)
	guard := idempotency.NewGuard(db.DB, time.Hour, time.Hour)
	auditWriter := auditlog.NewGormWriter(db.DB)
	accountApp := accountspace.New(repo, guard, auditWriter)
	adminApp := admin.New(repo, guard, auditWriter)
	creditApp := credit.New(repo, guard, auditWriter)
	router := NewRouter(RouterOptions{AccountSpace: accountApp, Admin: adminApp, Credit: creditApp})

	userToken := loginUser(t, router, "user1001@dora.local", "local-user-change-me")
	packages := requestJSON(t, router, http.MethodGet, "/api/credits/recharge-packages", userToken, "", nil)
	if packages["data"].(map[string]any)["total"].(float64) < 2 {
		t.Fatalf("expected seeded recharge packages: %#v", packages)
	}

	beforeLots := requestJSON(t, router, http.MethodGet, "/api/credits/lots", userToken, "", nil)
	if beforeLots["data"].(map[string]any)["total"].(float64) < 1 {
		t.Fatalf("expected seeded credit lots: %#v", beforeLots)
	}

	orderResp := requestJSON(t, router, http.MethodPost, "/api/credits/recharge-orders", userToken, "idem-http-recharge-create", map[string]any{
		"package_id": "pkg_1000_1m",
	})
	order := orderResp["data"].(map[string]any)
	orderID := order["order_id"].(string)
	if order["payment_status"] != "pending" || order["points"].(float64) != 1000 {
		t.Fatalf("unexpected created recharge order: %#v", order)
	}

	paidResp := requestJSON(t, router, http.MethodPost, "/api/credits/recharge-orders/"+orderID+"/mock-pay", userToken, "idem-http-recharge-pay", map[string]any{
		"payment_result":          "success",
		"provider_transaction_id": "mock_txn_http_recharge_success",
	})
	paid := paidResp["data"].(map[string]any)
	if paid["payment_status"] != "paid" || paid["credit_lot_id"] == "" {
		t.Fatalf("unexpected paid recharge order: %#v", paid)
	}

	orders := requestJSON(t, router, http.MethodGet, "/api/credits/recharge-orders?payment_status=paid", userToken, "", nil)
	if orders["data"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("expected one paid recharge order: %#v", orders)
	}
	rechargeLots := requestJSON(t, router, http.MethodGet, "/api/credits/lots?source_type=recharge_package", userToken, "", nil)
	if rechargeLots["data"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("expected one recharge credit lot: %#v", rechargeLots)
	}

	if err := repo.DB().WithContext(t.Context()).Model(&businesscore.PlatformAdmin{}).
		Where("id = ?", "adm_root").Update("must_rotate_password", false).Error; err != nil {
		t.Fatalf("prep admin: %v", err)
	}
	adminToken := loginAdmin(t, router, "admin@dora.local", "local-admin-change-me")
	adminAccounts := requestJSON(t, router, http.MethodGet, "/api/admin/credits/accounts?account_type=personal", adminToken, "", nil)
	if adminAccounts["data"].(map[string]any)["total"].(float64) < 2 {
		t.Fatalf("expected personal credit accounts: %#v", adminAccounts)
	}
	adminLots := requestJSON(t, router, http.MethodGet, "/api/admin/credits/lots?source_type=recharge_package", adminToken, "", nil)
	if adminLots["data"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("expected admin recharge lot visibility: %#v", adminLots)
	}
	adminOrders := requestJSON(t, router, http.MethodGet, "/api/admin/orders?payment_status=paid", adminToken, "", nil)
	if adminOrders["data"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("expected admin paid recharge order visibility: %#v", adminOrders)
	}
}

func TestBillingPackageAdminAndEnterprisePurchaseHTTP(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_billing_package_http")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	repo := businesscore.New(db.DB)
	guard := idempotency.NewGuard(db.DB, time.Hour, time.Hour)
	auditWriter := auditlog.NewGormWriter(db.DB)
	accountApp := accountspace.New(repo, guard, auditWriter)
	adminApp := admin.New(repo, guard, auditWriter)
	creditApp := credit.New(repo, guard, auditWriter)
	router := NewRouter(RouterOptions{AccountSpace: accountApp, Admin: adminApp, Credit: creditApp})

	if err := repo.DB().WithContext(t.Context()).Model(&businesscore.PlatformAdmin{}).
		Where("id = ?", "adm_root").Update("must_rotate_password", false).Error; err != nil {
		t.Fatalf("prep admin: %v", err)
	}
	adminToken := loginAdmin(t, router, "admin@dora.local", "local-admin-change-me")
	packages := requestJSON(t, router, http.MethodGet, "/api/admin/billing/packages?target_scope=enterprise", adminToken, "", nil)
	if packages["data"].(map[string]any)["total"].(float64) < 2 {
		t.Fatalf("expected enterprise billing packages: %#v", packages)
	}
	created := requestJSON(t, router, http.MethodPost, "/api/admin/billing/packages", adminToken, "idem-admin-billing-package-create", map[string]any{
		"package_id":           "pkg_test_admin_100",
		"package_type":         "personal_credit_pack",
		"name":                 "测试 100 积分包",
		"target_scope":         "personal",
		"billing_mode":         "one_time",
		"price_amount":         1000,
		"currency":             "CNY",
		"granted_points":       100,
		"bonus_points":         0,
		"credit_expiry_policy": "P7D",
		"spend_scope":          []string{"tool_generation", "skill_usage"},
		"settlement_eligible":  true,
		"entitlement_policy":   map[string]any{"priority_queue": false},
		"renewal_policy":       map[string]any{"mode": "none"},
		"refund_policy":        map[string]any{"mode": "unused_refund"},
		"visible_scope":        "all_users",
		"status":               "active",
		"reason":               "billing package smoke",
	})
	if created["data"].(map[string]any)["package_id"] != "pkg_test_admin_100" {
		t.Fatalf("unexpected created billing package: %#v", created)
	}
	status := requestJSON(t, router, http.MethodPost, "/api/admin/billing/packages/pkg_test_admin_100/status", adminToken, "idem-admin-billing-package-status", map[string]any{
		"status": "paused",
		"reason": "pause after smoke",
	})
	if status["data"].(map[string]any)["status"] != "paused" {
		t.Fatalf("billing package status not updated: %#v", status)
	}
	sku := requestJSON(t, router, http.MethodPost, "/api/admin/billing/skus", adminToken, "idem-admin-billing-sku-create", map[string]any{
		"package_id":   "pkg_test_admin_100",
		"sku_id":       "sku_pkg_test_admin_100_default",
		"channel_code": "default",
		"price_amount": 1000,
		"currency":     "CNY",
		"reason":       "billing sku smoke",
	})
	if sku["data"].(map[string]any)["sku_id"] != "sku_pkg_test_admin_100_default" {
		t.Fatalf("unexpected billing sku: %#v", sku)
	}

	enterpriseToken := loginUser(t, router, "enterprise_admin_demo@dora.local", "local-user-change-me")
	requestJSON(t, router, http.MethodPost, "/api/account/switch-identity", enterpriseToken, "idem-billing-enterprise-switch", map[string]any{
		"target_identity_type": "enterprise_member",
		"target_enterprise_id": "ent_demo",
	})
	orderResp := requestJSON(t, router, http.MethodPost, "/api/credits/recharge-orders", enterpriseToken, "idem-billing-enterprise-order", map[string]any{
		"package_id":          "pkg_enterprise_basic_monthly",
		"sku_id":              "sku_enterprise_basic_monthly_cny_default",
		"target_account_type": "enterprise",
	})
	order := orderResp["data"].(map[string]any)
	orderID := order["order_id"].(string)
	if order["target_scope"] != "enterprise" || order["points"].(float64) != 30000 {
		t.Fatalf("unexpected enterprise order: %#v", order)
	}
	paidResp := requestJSON(t, router, http.MethodPost, "/api/credits/recharge-orders/"+orderID+"/mock-pay", enterpriseToken, "idem-billing-enterprise-pay", map[string]any{
		"payment_result":          "success",
		"provider_transaction_id": "mock_txn_billing_enterprise_success",
	})
	paid := paidResp["data"].(map[string]any)
	if paid["payment_status"] != "paid" || paid["credit_lot_id"] == "" || paid["entitlement_snapshot_id"] == "" {
		t.Fatalf("enterprise package did not create lot and entitlement: %#v", paid)
	}
	contracts := requestJSON(t, router, http.MethodGet, "/api/admin/billing/enterprise-contracts?enterprise_id=ent_demo", adminToken, "", nil)
	if contracts["data"].(map[string]any)["total"].(float64) < 2 {
		t.Fatalf("expected seed and purchased enterprise contracts: %#v", contracts)
	}
	if requestJSON(t, router, http.MethodGet, "/api/admin/billing/invoices?enterprise_id=ent_demo", adminToken, "", nil)["data"].(map[string]any)["total"].(float64) < 1 {
		t.Fatal("expected seed billing invoice")
	}
	if requestJSON(t, router, http.MethodGet, "/api/admin/billing/promotions", adminToken, "", nil)["data"].(map[string]any)["total"].(float64) < 1 {
		t.Fatal("expected seed billing promotion")
	}
}
