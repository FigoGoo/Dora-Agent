package smoke

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/credit"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/security"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RequestMeta = accountspace.RequestMeta

type App struct {
	repo   *businesscore.Repository
	credit *credit.App
	now    func() time.Time
}

func New(repo *businesscore.Repository, creditApp ...*credit.App) *App {
	app := &App{repo: repo, now: func() time.Time { return time.Now().UTC() }}
	if len(creditApp) > 0 {
		app.credit = creditApp[0]
	}
	return app
}

type Page[T any] struct {
	Items  []T   `json:"items"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

type FeatureFlagDTO struct {
	FlagKey        string `json:"flag_key"`
	Enabled        bool   `json:"enabled"`
	DefaultEnabled bool   `json:"default_enabled"`
	Description    string `json:"description,omitempty"`
	UpdatedAt      string `json:"updated_at"`
}

type UpdateFeatureFlagInput struct {
	Auth        admin.AdminAuth
	Meta        RequestMeta
	FlagKey     string
	Enabled     bool
	Description string
}

type SmokeRunInput struct {
	Auth     admin.AdminAuth
	Meta     RequestMeta
	SuiteKey string
}

type SmokeRunDTO struct {
	RunID      string         `json:"run_id"`
	SuiteKey   string         `json:"suite_key"`
	Status     string         `json:"status"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
	Summary    map[string]any `json:"summary"`
	Steps      []SmokeStepDTO `json:"steps,omitempty"`
}

type SmokeStepDTO struct {
	StepKey      string         `json:"step_key"`
	Status       string         `json:"status"`
	Evidence     map[string]any `json:"evidence"`
	ErrorMessage string         `json:"error_message,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

type FakeProviderTaskDTO struct {
	ProviderTaskID string         `json:"provider_task_id"`
	ProviderKey    string         `json:"provider_key"`
	ToolID         string         `json:"tool_id"`
	Scenario       string         `json:"scenario"`
	LatencyMS      int            `json:"latency_ms"`
	ArtifactURI    string         `json:"artifact_uri,omitempty"`
	Status         string         `json:"status"`
	Result         map[string]any `json:"result"`
}

type seedCheck struct {
	key   string
	table string
	where string
	args  []any
	min   int64
}

var defaultFeatureFlags = []struct {
	key         string
	enabled     bool
	description string
}{
	{"auth.enabled", true, "登录注册开关"},
	{"credit_lot.enabled", true, "积分批次和有效期开关"},
	{"redeem_code.enabled", true, "兑换码入口"},
	{"mock_payment.enabled", true, "测试环境 Mock 支付"},
	{"fake_provider.enabled", true, "Fake Provider 开关"},
	{"agent_core_refactor.enabled", false, "新 Agent Runtime 主开关"},
	{"skill_marketplace.enabled", false, "Skill 市场开关"},
	{"paid_marketplace_skill.enabled", false, "付费 Skill 使用费开关"},
}

func (a *App) ListFeatureFlags(ctx context.Context, auth admin.AdminAuth, limit, offset int) (Page[FeatureFlagDTO], error) {
	if auth.AdminID == "" {
		return Page[FeatureFlagDTO]{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	if err := a.ensureDefaultFeatureFlags(ctx, auth.AdminID); err != nil {
		return Page[FeatureFlagDTO]{}, err
	}
	limit, offset = normalizePage(limit, offset, 100)
	var total int64
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.SystemFeatureFlag{})
	if err := db.Count(&total).Error; err != nil {
		return Page[FeatureFlagDTO]{}, err
	}
	var rows []businesscore.SystemFeatureFlag
	if err := db.Order("flag_key ASC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[FeatureFlagDTO]{}, err
	}
	items := make([]FeatureFlagDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, featureFlagDTO(row))
	}
	return Page[FeatureFlagDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) UpdateFeatureFlag(ctx context.Context, in UpdateFeatureFlagInput) (FeatureFlagDTO, error) {
	if in.Auth.AdminID == "" {
		return FeatureFlagDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	flagKey := strings.TrimSpace(in.FlagKey)
	if flagKey == "" {
		return FeatureFlagDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "flag_key is required")
	}
	if err := a.ensureDefaultFeatureFlags(ctx, in.Auth.AdminID); err != nil {
		return FeatureFlagDTO{}, err
	}
	var row businesscore.SystemFeatureFlag
	err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("flag_key = ?", flagKey).First(&row).Error; err != nil {
			return bizerrors.New(bizerrors.CodeResourceNotFound, "feature flag not found")
		}
		now := a.now()
		row.Enabled = in.Enabled
		if strings.TrimSpace(in.Description) != "" {
			row.Description = optionalString(in.Description)
		}
		row.UpdatedBy = optionalString(in.Auth.AdminID)
		row.UpdatedAt = now
		return tx.Save(&row).Error
	})
	if err != nil {
		return FeatureFlagDTO{}, err
	}
	return featureFlagDTO(row), nil
}

func (a *App) ListFakeProviderTasks(ctx context.Context, auth admin.AdminAuth, providerKey, scenario string, limit, offset int) (Page[FakeProviderTaskDTO], error) {
	if auth.AdminID == "" {
		return Page[FakeProviderTaskDTO]{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	limit, offset = normalizePage(limit, offset, 100)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.FakeProviderTask{})
	if strings.TrimSpace(providerKey) != "" {
		db = db.Where("provider_key = ?", strings.TrimSpace(providerKey))
	}
	if strings.TrimSpace(scenario) != "" {
		db = db.Where("scenario = ?", strings.TrimSpace(scenario))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[FakeProviderTaskDTO]{}, err
	}
	var rows []businesscore.FakeProviderTask
	if err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[FakeProviderTaskDTO]{}, err
	}
	items := make([]FakeProviderTaskDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, fakeProviderTaskDTO(row))
	}
	return Page[FakeProviderTaskDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) RunSmokeSeed(ctx context.Context, in SmokeRunInput) (SmokeRunDTO, error) {
	if in.Auth.AdminID == "" {
		return SmokeRunDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	if err := a.ensureDefaultFeatureFlags(ctx, in.Auth.AdminID); err != nil {
		return SmokeRunDTO{}, err
	}
	now := a.now()
	summary, status := a.seedSummary(ctx)
	run := businesscore.TestSeedRun{
		ID: security.RandomID("tsr_"), SeedRunID: security.RandomID("seed_run_"), FixtureID: "b0_smoke_seed_001",
		Status: status, SummaryJSON: mustJSON(summary), TraceID: optionalString(in.Meta.TraceID),
		CreatedBy: optionalString(in.Auth.AdminID), UpdatedBy: optionalString(in.Auth.AdminID), CreatedAt: now, UpdatedAt: now,
	}
	if err := a.repo.DB().WithContext(ctx).Create(&run).Error; err != nil {
		return SmokeRunDTO{}, err
	}
	return SmokeRunDTO{RunID: run.SeedRunID, SuiteKey: run.FixtureID, Status: run.Status, StartedAt: run.CreatedAt, FinishedAt: &run.UpdatedAt, Summary: summary}, nil
}

func (a *App) RunSmokeSuite(ctx context.Context, in SmokeRunInput) (SmokeRunDTO, error) {
	if in.Auth.AdminID == "" {
		return SmokeRunDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	now := a.now()
	suiteKey := strings.TrimSpace(in.SuiteKey)
	if suiteKey == "" {
		suiteKey = "b0_business_smoke"
	}
	runID := security.RandomID("smoke_run_")
	stepResults := a.evaluateSmokeSteps(ctx, in.Meta.TraceID)
	status := "passed"
	for _, step := range stepResults {
		if step.Status != "passed" {
			status = "failed"
			break
		}
	}
	summary := map[string]any{"step_count": len(stepResults), "status": status}
	run := businesscore.SmokeTestRun{
		ID: security.RandomID("str_"), SmokeRunID: runID, SuiteKey: suiteKey, Status: status, StartedAt: now, FinishedAt: &now,
		SummaryJSON: mustJSON(summary), TraceID: optionalString(in.Meta.TraceID),
		CreatedBy: optionalString(in.Auth.AdminID), UpdatedBy: optionalString(in.Auth.AdminID), CreatedAt: now, UpdatedAt: now,
	}
	err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		for _, step := range stepResults {
			row := businesscore.SmokeTestStep{
				ID: security.RandomID("sts_"), SmokeRunID: runID, StepKey: step.StepKey, Status: step.Status,
				EvidenceJSON: mustJSON(step.Evidence), ErrorMessage: optionalString(step.ErrorMessage),
				CreatedBy: optionalString(in.Auth.AdminID), UpdatedBy: optionalString(in.Auth.AdminID), CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return SmokeRunDTO{}, err
	}
	return SmokeRunDTO{RunID: run.SmokeRunID, SuiteKey: run.SuiteKey, Status: run.Status, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, Summary: summary, Steps: stepResults}, nil
}

func (a *App) GetSmokeRunResult(ctx context.Context, auth admin.AdminAuth, runID string) (SmokeRunDTO, error) {
	if auth.AdminID == "" {
		return SmokeRunDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	var run businesscore.SmokeTestRun
	if err := a.repo.DB().WithContext(ctx).Where("smoke_run_id = ?", strings.TrimSpace(runID)).First(&run).Error; err != nil {
		return SmokeRunDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "smoke run not found")
	}
	var steps []businesscore.SmokeTestStep
	if err := a.repo.DB().WithContext(ctx).Where("smoke_run_id = ?", run.SmokeRunID).Order("created_at ASC").Find(&steps).Error; err != nil {
		return SmokeRunDTO{}, err
	}
	out := smokeRunDTO(run)
	for _, step := range steps {
		out.Steps = append(out.Steps, smokeStepDTO(step))
	}
	return out, nil
}

func (a *App) ensureDefaultFeatureFlags(ctx context.Context, operatorID string) error {
	now := a.now()
	for _, item := range defaultFeatureFlags {
		row := businesscore.SystemFeatureFlag{
			ID: security.RandomID("flag_"), FlagKey: item.key, Enabled: item.enabled, DefaultEnabled: item.enabled,
			Description: optionalString(item.description), CreatedBy: optionalString(operatorID), UpdatedBy: optionalString(operatorID),
			CreatedAt: now, UpdatedAt: now,
		}
		if err := a.repo.DB().WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "flag_key"}},
			DoUpdates: clause.Assignments(map[string]any{
				"default_enabled": row.DefaultEnabled,
				"description":     row.Description,
				"updated_by":      operatorID,
				"updated_at":      now,
			}),
		}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func (a *App) seedSummary(ctx context.Context) (map[string]any, string) {
	checks := []seedCheck{
		{key: "users", table: "business_users", where: "id IN ?", args: []any{[]string{"user_demo", "creator_demo", "enterprise_admin_demo", "enterprise_member_demo"}}, min: 4},
		{key: "admins", table: "platform_admins", where: "id IN ?", args: []any{[]string{"adm_demo", "adm_reviewer_demo", "adm_finance_demo", "adm_qa_demo"}}, min: 4},
		{key: "enterprise", table: "enterprises", where: "id = ?", args: []any{"ent_demo"}, min: 1},
		{key: "credit_accounts", table: "credit_accounts", where: "id IN ?", args: []any{[]string{"ca_user_demo", "ca_ent_demo"}}, min: 2},
		{key: "redeem_codes", table: "redeem_codes", where: "id IN ?", args: []any{[]string{"rc_smoke1000", "rc_gift300", "rc_toolonly500"}}, min: 3},
		{key: "recharge_packages", table: "recharge_packages", where: "package_id IN ?", args: []any{[]string{"pkg_1000_1m", "pkg_5000_1m", "pkg_enterprise_basic_monthly"}}, min: 3},
		{key: "billing_skus", table: "billing_package_skus", where: "sku_id IN ?", args: []any{[]string{"sku_pkg_1000_1m_cny_default", "sku_enterprise_basic_monthly_cny_default"}}, min: 2},
		{key: "enterprise_contracts", table: "enterprise_contracts", where: "enterprise_id = ?", args: []any{"ent_demo"}, min: 1},
		{key: "feature_flags", table: "system_feature_flags", where: "flag_key IN ?", args: []any{[]string{"auth.enabled", "credit_lot.enabled", "mock_payment.enabled", "fake_provider.enabled"}}, min: 4},
		{key: "fake_provider_tasks", table: "fake_provider_tasks", where: "provider_task_id IN ?", args: []any{[]string{"fake_task_image_success", "fake_task_video_timeout", "fake_task_music_partial", "fake_task_safety_blocked"}}, min: 4},
	}
	summary := map[string]any{}
	status := "passed"
	for _, check := range checks {
		count := a.count(ctx, check.table, check.where, check.args...)
		passed := count >= check.min
		summary[check.key] = map[string]any{"count": count, "required": check.min, "passed": passed}
		if !passed {
			status = "failed"
		}
	}
	return summary, status
}

func (a *App) evaluateSmokeSteps(ctx context.Context, traceID string) []SmokeStepDTO {
	summary, seedStatus := a.seedSummary(ctx)
	return []SmokeStepDTO{
		{StepKey: "smoke_0_seed", Status: seedStatus, Evidence: summary, CreatedAt: a.now()},
		{StepKey: "smoke_2_redeem_fixture", Status: passIf(a.count(ctx, "redeem_codes", "code_digest IN ?", []string{"sha256:SMOKE1000", "sha256:GIFT300", "sha256:TOOLONLY500"}) >= 3), Evidence: map[string]any{"fixed_codes": []string{"SMOKE1000", "GIFT300", "TOOLONLY500"}}, CreatedAt: a.now()},
		{StepKey: "smoke_3_recharge_fixture", Status: passIf(a.count(ctx, "recharge_packages", "package_id IN ?", []string{"pkg_1000_1m", "pkg_5000_1m"}) >= 2), Evidence: map[string]any{"packages": []string{"pkg_1000_1m", "pkg_5000_1m"}}, CreatedAt: a.now()},
		{StepKey: "smoke_4_fake_provider", Status: passIf(a.count(ctx, "fake_provider_tasks", "scenario IN ?", []string{"success", "timeout", "partial_success", "safety_blocked"}) >= 4), Evidence: map[string]any{"scenarios": []string{"success", "timeout", "partial_success", "safety_blocked"}}, CreatedAt: a.now()},
		a.runPersonalPackageSmoke(ctx, traceID),
		a.runEnterprisePackageSmoke(ctx, traceID),
		{StepKey: "smoke_14_admin_audit", Status: passIf(a.tableExists(ctx, "business_audit_logs")), Evidence: map[string]any{"audit_table": "business_audit_logs"}},
	}
}

func (a *App) runPersonalPackageSmoke(ctx context.Context, traceID string) SmokeStepDTO {
	if a.credit == nil {
		return smokeStepFailed("smoke_12_personal_package_purchase", "credit app is not configured")
	}
	suffix := strings.ToLower(security.RandomID("")[0:8])
	auth := credit.AuthContext{UserID: "user_demo", SpaceID: "sp_user_demo", LoginIdentityType: accountspace.IdentityPersonal}
	order, err := a.credit.CreateRechargeOrder(ctx, credit.CreateRechargeOrderInput{
		Auth:      auth,
		Meta:      RequestMeta{TraceID: defaultString(traceID, "trace_smoke_12"), IdempotencyKey: "idem-smoke-12-create-" + suffix, Source: "smoke"},
		PackageID: "pkg_1000_1m",
		SKUID:     "sku_pkg_1000_1m_cny_default",
	})
	if err != nil {
		return smokeStepFailed("smoke_12_personal_package_purchase", err.Error())
	}
	paid, err := a.credit.MockPayRechargeOrder(ctx, credit.MockPayRechargeOrderInput{
		Auth:    auth,
		Meta:    RequestMeta{TraceID: defaultString(traceID, "trace_smoke_12"), IdempotencyKey: "idem-smoke-12-pay-" + suffix, Source: "smoke"},
		OrderID: order.OrderID, PaymentResult: "success", ProviderTransactionID: "mock_txn_smoke_12_" + suffix,
	})
	if err != nil {
		return smokeStepFailed("smoke_12_personal_package_purchase", err.Error())
	}
	ok := paid.PaymentStatus == "paid" && paid.CreditLotID != "" && paid.EntitlementSnapshotID != "" &&
		a.count(ctx, "credit_ledger_entries", "source_type = ? AND source_id = ?", "recharge_order", paid.OrderID) == 1
	return SmokeStepDTO{
		StepKey: "smoke_12_personal_package_purchase", Status: passIf(ok),
		Evidence: map[string]any{
			"order_id": paid.OrderID, "credit_lot_id": paid.CreditLotID, "entitlement_snapshot_id": paid.EntitlementSnapshotID,
			"points": paid.Points, "package_id": paid.PackageID,
		},
		CreatedAt: a.now(),
	}
}

func (a *App) runEnterprisePackageSmoke(ctx context.Context, traceID string) SmokeStepDTO {
	if a.credit == nil {
		return smokeStepFailed("smoke_13_enterprise_package_purchase", "credit app is not configured")
	}
	suffix := strings.ToLower(security.RandomID("")[0:8])
	auth := credit.AuthContext{
		UserID: "enterprise_admin_demo", SpaceID: "sp_ent_demo", EnterpriseID: "ent_demo",
		EnterpriseRole: accountspace.RoleOwner, LoginIdentityType: accountspace.IdentityEnterprise,
	}
	order, err := a.credit.CreateRechargeOrder(ctx, credit.CreateRechargeOrderInput{
		Auth:      auth,
		Meta:      RequestMeta{TraceID: defaultString(traceID, "trace_smoke_13"), IdempotencyKey: "idem-smoke-13-create-" + suffix, Source: "smoke"},
		PackageID: "pkg_enterprise_basic_monthly",
		SKUID:     "sku_enterprise_basic_monthly_cny_default",
	})
	if err != nil {
		return smokeStepFailed("smoke_13_enterprise_package_purchase", err.Error())
	}
	paid, err := a.credit.MockPayRechargeOrder(ctx, credit.MockPayRechargeOrderInput{
		Auth:    auth,
		Meta:    RequestMeta{TraceID: defaultString(traceID, "trace_smoke_13"), IdempotencyKey: "idem-smoke-13-pay-" + suffix, Source: "smoke"},
		OrderID: order.OrderID, PaymentResult: "success", ProviderTransactionID: "mock_txn_smoke_13_" + suffix,
	})
	if err != nil {
		return smokeStepFailed("smoke_13_enterprise_package_purchase", err.Error())
	}
	contractCount := a.count(ctx, "enterprise_contracts", "order_id = ?", paid.OrderID)
	ok := paid.PaymentStatus == "paid" && paid.TargetScope == "enterprise" && paid.CreditLotID != "" && paid.EntitlementSnapshotID != "" && contractCount == 1
	return SmokeStepDTO{
		StepKey: "smoke_13_enterprise_package_purchase", Status: passIf(ok),
		Evidence: map[string]any{
			"order_id": paid.OrderID, "credit_lot_id": paid.CreditLotID, "entitlement_snapshot_id": paid.EntitlementSnapshotID,
			"contract_count": contractCount, "points": paid.Points, "package_id": paid.PackageID,
		},
		CreatedAt: a.now(),
	}
}

func smokeStepFailed(stepKey, message string) SmokeStepDTO {
	return SmokeStepDTO{StepKey: stepKey, Status: "failed", Evidence: map[string]any{}, ErrorMessage: message, CreatedAt: time.Now().UTC()}
}

func (a *App) count(ctx context.Context, table, where string, args ...any) int64 {
	var count int64
	_ = a.repo.DB().WithContext(ctx).Table(table).Where(where, args...).Count(&count).Error
	return count
}

func (a *App) tableExists(ctx context.Context, table string) bool {
	var exists bool
	_ = a.repo.DB().WithContext(ctx).Raw(
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = ?)",
		table,
	).Scan(&exists).Error
	return exists
}

func featureFlagDTO(row businesscore.SystemFeatureFlag) FeatureFlagDTO {
	return FeatureFlagDTO{
		FlagKey: row.FlagKey, Enabled: row.Enabled, DefaultEnabled: row.DefaultEnabled,
		Description: value(row.Description), UpdatedAt: row.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func fakeProviderTaskDTO(row businesscore.FakeProviderTask) FakeProviderTaskDTO {
	return FakeProviderTaskDTO{
		ProviderTaskID: row.ProviderTaskID, ProviderKey: row.ProviderKey, ToolID: row.ToolID,
		Scenario: row.Scenario, LatencyMS: row.LatencyMS, ArtifactURI: value(row.ArtifactURI), Status: row.Status,
		Result: jsonMap(row.ResultJSON),
	}
}

func smokeRunDTO(row businesscore.SmokeTestRun) SmokeRunDTO {
	return SmokeRunDTO{
		RunID: row.SmokeRunID, SuiteKey: row.SuiteKey, Status: row.Status, StartedAt: row.StartedAt,
		FinishedAt: row.FinishedAt, Summary: jsonMap(row.SummaryJSON),
	}
}

func smokeStepDTO(row businesscore.SmokeTestStep) SmokeStepDTO {
	return SmokeStepDTO{
		StepKey: row.StepKey, Status: row.Status, Evidence: jsonMap(row.EvidenceJSON),
		ErrorMessage: value(row.ErrorMessage), CreatedAt: row.CreatedAt,
	}
}

func passIf(ok bool) string {
	if ok {
		return "passed"
	}
	return "failed"
}

func mustJSON(value any) datatypes.JSON {
	data, err := json.Marshal(value)
	if err != nil {
		return datatypes.JSON([]byte(`{}`))
	}
	return datatypes.JSON(data)
}

func jsonMap(raw datatypes.JSON) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func normalizePage(limit, offset, max int) (int, int) {
	if limit <= 0 {
		limit = 10
	}
	if limit > max {
		limit = max
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
