package admin

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/security"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type App struct {
	repo   *businesscore.Repository
	guard  *idempotency.IdempotencyGuard
	audit  auditlog.Writer
	now    func() time.Time
	secret string
}

func New(repo *businesscore.Repository, guard *idempotency.IdempotencyGuard, audit auditlog.Writer) *App {
	return &App{
		repo:   repo,
		guard:  guard,
		audit:  audit,
		now:    func() time.Time { return time.Now().UTC() },
		secret: "local-m2-admin-preview-secret",
	}
}

type BootstrapInput struct {
	Account             string
	PasswordHash        string
	CredentialSecretRef string
	TraceID             string
}

type AdminAuth struct {
	AdminID     string
	Account     string
	SessionID   string
	AccessToken string
}

type RequestMeta = accountspace.RequestMeta

type AdminLoginInput struct {
	Account  string
	Password string
	Meta     RequestMeta
}

type RotatePasswordInput struct {
	Auth            AdminAuth
	CurrentPassword string
	NewPassword     string
	Reason          string
	Meta            RequestMeta
}

type CreateAdminInput struct {
	Auth            AdminAuth
	Account         string
	InitialPassword string
	Reason          string
	Meta            RequestMeta
}

type DisableAdminInput struct {
	Auth    AdminAuth
	AdminID string
	Reason  string
	Meta    RequestMeta
}

type UserStatusInput struct {
	Auth         AdminAuth
	UserID       string
	TargetStatus string
	Reason       string
	PreviewToken string
	Meta         RequestMeta
}

type ListUsersInput struct {
	Auth   AdminAuth
	Status string
	Limit  int
	Offset int
}

type AuditQueryInput struct {
	Auth           AdminAuth
	BusinessAction string
	TraceID        string
	Limit          int
	Offset         int
}

type AdminSessionDTO struct {
	AdminID            string    `json:"admin_id"`
	Account            string    `json:"account"`
	Status             string    `json:"status"`
	MustRotatePassword bool      `json:"must_rotate_password"`
	CSRFToken          string    `json:"csrf_token"`
	AccessToken        string    `json:"access_token,omitempty"`
	ExpiresAt          time.Time `json:"expires_at"`
	BootstrapStatus    string    `json:"bootstrap_status,omitempty"`
}

type PlatformAdminDTO struct {
	AdminID            string    `json:"admin_id"`
	Account            string    `json:"account"`
	Status             string    `json:"status"`
	MustRotatePassword bool      `json:"must_rotate_password"`
	CreatedAt          time.Time `json:"created_at"`
}

type DashboardDTO struct {
	ActiveUserCount  int64 `json:"active_user_count"`
	ActiveAdminCount int64 `json:"active_admin_count"`
	ProjectCount     int64 `json:"project_count"`
}

type UserSummaryDTO struct {
	UserID          string     `json:"user_id"`
	Status          string     `json:"status"`
	PublicNickname  string     `json:"public_nickname"`
	EmailMasked     string     `json:"email_masked,omitempty"`
	PhoneMasked     string     `json:"phone_masked,omitempty"`
	PersonalSpaceID string     `json:"personal_space_id,omitempty"`
	RegisteredAt    time.Time  `json:"registered_at"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
}

type UserDetailDTO struct {
	Summary               UserSummaryDTO               `json:"summary"`
	Spaces                []AdminUserSpaceDTO          `json:"spaces"`
	EnterpriseMemberships []AdminUserEnterpriseRoleDTO `json:"enterprise_memberships"`
	RecentAuditRefs       []AdminUserAuditRefDTO       `json:"recent_audit_refs"`
}

type AdminUserSpaceDTO struct {
	SpaceID   string `json:"space_id"`
	SpaceType string `json:"space_type"`
	Status    string `json:"status"`
}

type AdminUserEnterpriseRoleDTO struct {
	EnterpriseID string `json:"enterprise_id"`
	Role         string `json:"role"`
	Status       string `json:"status"`
}

type AdminUserAuditRefDTO struct {
	AuditID        string `json:"audit_id"`
	BusinessAction string `json:"business_action"`
	TraceID        string `json:"trace_id"`
}

type UserStatusPreviewDTO struct {
	PreviewToken             string    `json:"preview_token"`
	CurrentStatus            string    `json:"current_status"`
	TargetStatus             string    `json:"target_status"`
	ImpactSummary            []string  `json:"impact_summary"`
	PublicContentRetained    bool      `json:"public_content_retained"`
	PrivateContentNotExposed bool      `json:"private_content_not_exposed"`
	ExpiresAt                time.Time `json:"expires_at"`
}

type AuditLogDTO struct {
	AuditID          string    `json:"audit_id"`
	OperatorType     string    `json:"operator_type"`
	OperatorIDMasked string    `json:"operator_id_masked,omitempty"`
	BusinessAction   string    `json:"business_action"`
	ResourceType     string    `json:"resource_type"`
	ResourceID       string    `json:"resource_id,omitempty"`
	Result           string    `json:"result"`
	ErrorCode        string    `json:"error_code,omitempty"`
	TraceID          string    `json:"trace_id"`
	CreatedAt        time.Time `json:"created_at"`
}

type Page[T any] struct {
	Items  []T   `json:"items"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

const (
	AdminModuleAdmins         = "platform_admins"
	AdminModuleUsers          = "users"
	AdminModuleSystemSkills   = "system_skills"
	AdminModuleSkillReviews   = "skill_reviews"
	AdminModuleModelProviders = "model_providers"
	AdminModuleModels         = "models"
	AdminModuleTools          = "tools"
	AdminModuleCreditGrants   = "credit_grants"
	AdminModuleRedeemCodes    = "redeem_codes"
	AdminModuleFeaturedWorks  = "featured_works"
	AdminModuleAuditLogs      = "audit_logs"
)

type AdminModuleOwner struct {
	Module      string
	DisplayName string
	OwnerDomain string
	AuditScope  string
}

var AdminModuleOwners = []AdminModuleOwner{
	{Module: AdminModuleAdmins, DisplayName: "平台管理员账号", OwnerDomain: "admin", AuditScope: "platform_admin"},
	{Module: AdminModuleUsers, DisplayName: "用户管理", OwnerDomain: "admin", AuditScope: "user"},
	{Module: AdminModuleSystemSkills, DisplayName: "系统 Skill", OwnerDomain: "skill", AuditScope: "skill"},
	{Module: AdminModuleSkillReviews, DisplayName: "Skill 审核", OwnerDomain: "skill", AuditScope: "skill_review"},
	{Module: AdminModuleModelProviders, DisplayName: "模型供应商", OwnerDomain: "model", AuditScope: "model_provider"},
	{Module: AdminModuleModels, DisplayName: "模型管理", OwnerDomain: "model", AuditScope: "model"},
	{Module: AdminModuleTools, DisplayName: "Tool 管理", OwnerDomain: "tool", AuditScope: "tool"},
	{Module: AdminModuleCreditGrants, DisplayName: "积分发放", OwnerDomain: "credit", AuditScope: "credit"},
	{Module: AdminModuleRedeemCodes, DisplayName: "兑换码", OwnerDomain: "credit", AuditScope: "redeem_code"},
	{Module: AdminModuleFeaturedWorks, DisplayName: "精选作品", OwnerDomain: "work", AuditScope: "public_work"},
	{Module: AdminModuleAuditLogs, DisplayName: "审计日志", OwnerDomain: "audit", AuditScope: "business_audit_log"},
}

func AdminModuleOwnerByModule(module string) (AdminModuleOwner, bool) {
	for _, owner := range AdminModuleOwners {
		if owner.Module == module {
			return owner, true
		}
	}
	return AdminModuleOwner{}, false
}

func (a *App) BootstrapInitialAdmin(ctx context.Context, in BootstrapInput) (PlatformAdminDTO, error) {
	var existing businesscore.PlatformAdmin
	err := a.repo.DB().WithContext(ctx).Where("status = ?", accountspace.StatusActive).Order("created_at ASC").First(&existing).Error
	if err == nil {
		return adminDTO(existing), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return PlatformAdminDTO{}, err
	}
	account := strings.TrimSpace(in.Account)
	if account == "" {
		account = "admin"
	}
	passwordHash := strings.TrimSpace(in.PasswordHash)
	if passwordHash == "" {
		return PlatformAdminDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "ADMIN_BOOTSTRAP_PASSWORD_HASH is required when no active admin exists")
	}
	now := a.now()
	adminID := security.RandomID("adm_")
	admin := businesscore.PlatformAdmin{
		ID: adminID, AdminAccount: account, PasswordHash: passwordHash, DisplayName: "Dora Root Admin",
		Role: "super_admin", Status: accountspace.StatusActive, MustRotatePassword: true,
		CreatedBy: optionalString("system_seed"), UpdatedBy: optionalString("system_seed"), CreatedAt: now, UpdatedAt: now,
	}
	secretRef := optionalString(in.CredentialSecretRef)
	bootstrap := businesscore.PlatformAdminBootstrap{
		ID: security.RandomID("boot_"), AdminID: adminID, BootstrapAccount: account, InitializedBy: "system_seed",
		CredentialSecretRef: secretRef, Status: "initialized", InitializedAt: now,
		CreatedBy: optionalString("system_seed"), UpdatedBy: optionalString("system_seed"), CreatedAt: now, UpdatedAt: now,
	}
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&admin).Error; err != nil {
			return err
		}
		if err := tx.Create(&bootstrap).Error; err != nil {
			return err
		}
		audit := auditRecord(in.TraceID, admin.ID, auditlog.ActionAdminBootstrap, "platform_admin", admin.ID, "success")
		return tx.Create(audit).Error
	})
	if err != nil {
		return PlatformAdminDTO{}, err
	}
	return adminDTO(admin), nil
}

func (a *App) Login(ctx context.Context, in AdminLoginInput) (AdminSessionDTO, error) {
	account := strings.TrimSpace(in.Account)
	if account == "" || in.Password == "" {
		return AdminSessionDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "account and password are required")
	}
	var admin businesscore.PlatformAdmin
	accountHash := security.HashIdentifier(account)
	if err := a.repo.DB().WithContext(ctx).Where("admin_account = ?", account).First(&admin).Error; err != nil {
		a.writeAttempt(ctx, account, accountHash, "failed", "UNAUTHENTICATED", in.Meta.TraceID)
		return AdminSessionDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "invalid account or password")
	}
	if admin.Status != accountspace.StatusActive {
		a.writeAttempt(ctx, account, accountHash, "disabled", "PERMISSION_DENIED", in.Meta.TraceID)
		return AdminSessionDTO{}, bizerrors.New(bizerrors.CodePermissionDenied, "admin is disabled")
	}
	if !security.VerifyPassword(admin.PasswordHash, in.Password) {
		a.writeAttempt(ctx, account, accountHash, "failed", "UNAUTHENTICATED", in.Meta.TraceID)
		return AdminSessionDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "invalid account or password")
	}
	var dto AdminSessionDTO
	err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		accessToken, tokenHash := security.NewOpaqueToken("admin")
		csrfToken, csrfHash := security.NewOpaqueToken("csrf")
		session := businesscore.PlatformAdminSession{
			ID: security.RandomID("asess_"), AdminID: admin.ID, SessionTokenDigest: tokenHash, CSRFTokenHash: &csrfHash,
			Status: accountspace.StatusActive, ExpiresAt: now.Add(12 * time.Hour),
			CreatedBy: optionalString(admin.ID), UpdatedBy: optionalString(admin.ID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.PlatformAdmin{}).Where("id = ?", admin.ID).Updates(map[string]any{"last_login_at": now, "updated_by": admin.ID, "updated_at": now}).Error; err != nil {
			return err
		}
		dto = AdminSessionDTO{
			AdminID: admin.ID, Account: admin.AdminAccount, Status: admin.Status, MustRotatePassword: admin.MustRotatePassword,
			CSRFToken: csrfToken, AccessToken: accessToken, ExpiresAt: session.ExpiresAt, BootstrapStatus: bootstrapStatus(admin.MustRotatePassword),
		}
		audit := auditRecord(in.Meta.TraceID, admin.ID, auditlog.ActionAdminAuthLogin, "platform_admin", admin.ID, "success")
		return tx.Create(audit).Error
	})
	if err != nil {
		return AdminSessionDTO{}, err
	}
	a.writeAttempt(ctx, account, accountHash, "success", "", in.Meta.TraceID)
	return dto, nil
}

func (a *App) Logout(ctx context.Context, auth AdminAuth, meta RequestMeta) error {
	if auth.SessionID == "" {
		return bizerrors.New(bizerrors.CodeUnauthenticated, "admin session is required")
	}
	return a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		if err := tx.Model(&businesscore.PlatformAdminSession{}).Where("id = ? AND admin_id = ?", auth.SessionID, auth.AdminID).Updates(map[string]any{"status": "revoked", "updated_by": auth.AdminID, "updated_at": now}).Error; err != nil {
			return err
		}
		audit := auditRecord(meta.TraceID, auth.AdminID, auditlog.ActionAdminAuthLogout, "platform_admin_session", auth.SessionID, "success")
		return tx.Create(audit).Error
	})
}

func (a *App) RotatePassword(ctx context.Context, in RotatePasswordInput) (AdminSessionDTO, error) {
	if in.Auth.AdminID == "" {
		return AdminSessionDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin session is required")
	}
	if in.Reason == "" {
		return AdminSessionDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "reason is required")
	}
	hash := in.Meta.RequestHash
	if hash == "" {
		hash = security.HashIdentifier(in.Auth.AdminID + ":rotate:" + in.Reason)
	}
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "admin:" + in.Auth.AdminID, Scope: "admin.password.rotate", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.AdminID,
	})
	if err != nil {
		return AdminSessionDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return AdminSessionDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "idempotency key conflicts with another password rotation")
	}
	var dto AdminSessionDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row businesscore.PlatformAdmin
		if err := tx.Where("id = ? AND status = ?", in.Auth.AdminID, accountspace.StatusActive).First(&row).Error; err != nil {
			return bizerrors.New(bizerrors.CodePermissionDenied, "admin is unavailable")
		}
		if !security.VerifyPassword(row.PasswordHash, in.CurrentPassword) {
			return bizerrors.New(bizerrors.CodeUnauthenticated, "current password is invalid")
		}
		newHash, err := security.HashPassword(in.NewPassword)
		if err != nil {
			return bizerrors.New(bizerrors.CodeInvalidArgument, err.Error())
		}
		now := a.now()
		if err := tx.Model(&businesscore.PlatformAdmin{}).Where("id = ?", row.ID).Updates(map[string]any{
			"password_hash": newHash, "must_rotate_password": false, "password_rotated_at": now, "updated_by": row.ID, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.PlatformAdminBootstrap{}).Where("admin_id = ?", row.ID).Updates(map[string]any{"status": "rotated", "rotated_at": now, "updated_by": row.ID, "updated_at": now}).Error; err != nil {
			return err
		}
		audit := auditRecord(in.Meta.TraceID, row.ID, auditlog.ActionAdminPasswordRotate, "platform_admin", row.ID, "success")
		if err := tx.Create(audit).Error; err != nil {
			return err
		}
		dto = AdminSessionDTO{AdminID: row.ID, Account: row.AdminAccount, Status: row.Status, MustRotatePassword: false, CSRFToken: "", AccessToken: in.Auth.AccessToken, ExpiresAt: now.Add(12 * time.Hour), BootstrapStatus: "rotated"}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return AdminSessionDTO{}, err
	}
	if err := a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "platform_admin", ID: dto.AdminID}); err != nil {
		return AdminSessionDTO{}, err
	}
	return dto, nil
}

func (a *App) Dashboard(ctx context.Context, auth AdminAuth) (DashboardDTO, error) {
	if err := a.requireAdmin(ctx, auth, false); err != nil {
		return DashboardDTO{}, err
	}
	var dto DashboardDTO
	db := a.repo.DB().WithContext(ctx)
	if err := db.Model(&businesscore.User{}).Where("status = ?", accountspace.StatusActive).Count(&dto.ActiveUserCount).Error; err != nil {
		return DashboardDTO{}, err
	}
	if err := db.Model(&businesscore.PlatformAdmin{}).Where("status = ?", accountspace.StatusActive).Count(&dto.ActiveAdminCount).Error; err != nil {
		return DashboardDTO{}, err
	}
	if err := db.Model(&businesscore.Project{}).Count(&dto.ProjectCount).Error; err != nil {
		return DashboardDTO{}, err
	}
	return dto, nil
}

func (a *App) ListAdmins(ctx context.Context, auth AdminAuth, limit, offset int) (Page[PlatformAdminDTO], error) {
	if err := a.requireAdmin(ctx, auth, false); err != nil {
		return Page[PlatformAdminDTO]{}, err
	}
	limit, offset = normalizePage(limit, offset)
	var rows []businesscore.PlatformAdmin
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.PlatformAdmin{}).Order("created_at DESC")
	if err := db.Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[PlatformAdminDTO]{}, err
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[PlatformAdminDTO]{}, err
	}
	items := make([]PlatformAdminDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, adminDTO(row))
	}
	return Page[PlatformAdminDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) CreateAdmin(ctx context.Context, in CreateAdminInput) (PlatformAdminDTO, error) {
	if err := a.requireAdmin(ctx, in.Auth, false); err != nil {
		return PlatformAdminDTO{}, err
	}
	if strings.TrimSpace(in.Account) == "" || in.InitialPassword == "" || in.Reason == "" {
		return PlatformAdminDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "account, initial_password and reason are required")
	}
	hash := in.Meta.RequestHash
	if hash == "" {
		hash = security.HashIdentifier(in.Account + ":" + in.Reason)
	}
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "admin:" + in.Auth.AdminID, Scope: "admin.create", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.AdminID,
	})
	if err != nil {
		return PlatformAdminDTO{}, err
	}
	var dto PlatformAdminDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		passwordHash, err := security.HashPassword(in.InitialPassword)
		if err != nil {
			return bizerrors.New(bizerrors.CodeInvalidArgument, err.Error())
		}
		now := a.now()
		row := businesscore.PlatformAdmin{
			ID: security.RandomID("adm_"), AdminAccount: strings.TrimSpace(in.Account), PasswordHash: passwordHash,
			DisplayName: strings.TrimSpace(in.Account), Role: "super_admin", Status: accountspace.StatusActive, MustRotatePassword: true,
			CreatedBy: optionalString(in.Auth.AdminID), UpdatedBy: optionalString(in.Auth.AdminID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		dto = adminDTO(row)
		audit := auditRecord(in.Meta.TraceID, in.Auth.AdminID, auditlog.ActionAdminCreate, "platform_admin", row.ID, "success")
		return tx.Create(audit).Error
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return PlatformAdminDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "platform_admin", ID: dto.AdminID})
	return dto, nil
}

func (a *App) DisableAdmin(ctx context.Context, in DisableAdminInput) (PlatformAdminDTO, error) {
	if err := a.requireAdmin(ctx, in.Auth, false); err != nil {
		return PlatformAdminDTO{}, err
	}
	if in.AdminID == in.Auth.AdminID {
		return PlatformAdminDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "admin cannot disable current session admin")
	}
	if in.Reason == "" {
		return PlatformAdminDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "reason is required")
	}
	hash := in.Meta.RequestHash
	if hash == "" {
		hash = security.HashIdentifier(in.AdminID + ":" + in.Reason)
	}
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "admin:" + in.Auth.AdminID, Scope: "admin.disable", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.AdminID,
	})
	if err != nil {
		return PlatformAdminDTO{}, err
	}
	var dto PlatformAdminDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		row, err := a.lockDisableTargetAdmin(tx, in.AdminID)
		if err != nil {
			return err
		}
		if err := tx.Model(&businesscore.PlatformAdmin{}).Where("id = ?", in.AdminID).Updates(map[string]any{
			"status": "disabled", "disabled_by": in.Auth.AdminID, "disabled_reason": in.Reason, "updated_by": in.Auth.AdminID, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", in.AdminID).First(&row).Error; err != nil {
			return err
		}
		dto = adminDTO(row)
		audit := auditRecord(in.Meta.TraceID, in.Auth.AdminID, auditlog.ActionAdminDisable, "platform_admin", row.ID, "success")
		return tx.Create(audit).Error
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return PlatformAdminDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "platform_admin", ID: dto.AdminID})
	return dto, nil
}

func (a *App) lockDisableTargetAdmin(tx *gorm.DB, adminID string) (businesscore.PlatformAdmin, error) {
	var row businesscore.PlatformAdmin
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", adminID).First(&row).Error; err != nil {
		return row, err
	}
	if row.Status != accountspace.StatusActive {
		return row, bizerrors.New(bizerrors.CodeStateConflict, "admin is not active")
	}
	var remainingActive int64
	if err := tx.Model(&businesscore.PlatformAdmin{}).
		Where("id <> ? AND status = ?", adminID, accountspace.StatusActive).
		Count(&remainingActive).Error; err != nil {
		return row, err
	}
	if remainingActive == 0 {
		return row, bizerrors.New(bizerrors.CodeStateConflict, "cannot disable the last active admin")
	}
	return row, nil
}

func (a *App) ListUsers(ctx context.Context, in ListUsersInput) (Page[UserSummaryDTO], error) {
	if err := a.requireAdmin(ctx, in.Auth, false); err != nil {
		return Page[UserSummaryDTO]{}, err
	}
	limit, offset := normalizePage(in.Limit, in.Offset)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.User{}).Where("deleted_at IS NULL")
	if in.Status != "" {
		db = db.Where("status = ?", in.Status)
	}
	var rows []businesscore.User
	if err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[UserSummaryDTO]{}, err
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[UserSummaryDTO]{}, err
	}
	items := make([]UserSummaryDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, userDTO(row))
	}
	return Page[UserSummaryDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) GetUserSummary(ctx context.Context, auth AdminAuth, userID string) (UserDetailDTO, error) {
	if err := a.requireAdmin(ctx, auth, false); err != nil {
		return UserDetailDTO{}, err
	}
	var user businesscore.User
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND deleted_at IS NULL", userID).First(&user).Error; err != nil {
		return UserDetailDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "user not found")
	}
	// ACCT-8 管理员越权红线：平台管理通道只读用户平台级元数据，不展开业务空间归属明细
	// (空间 / 企业成员 / 归属审计)。这些字段保持空占位是有意为之，不是待补 TODO——如需查看
	// 用户业务空间内容须走独立留痕的只读通道并单独鉴权，不得在此复用 admin 身份跨入业务归属。
	// 见 docs/standards/安全规范.md「管理员越权红线」。
	return UserDetailDTO{
		Summary:               userDTO(user),
		Spaces:                []AdminUserSpaceDTO{},
		EnterpriseMemberships: []AdminUserEnterpriseRoleDTO{},
		RecentAuditRefs:       []AdminUserAuditRefDTO{},
	}, nil
}

func (a *App) PreviewSetUserStatus(ctx context.Context, in UserStatusInput) (UserStatusPreviewDTO, error) {
	if err := a.requireAdmin(ctx, in.Auth, false); err != nil {
		return UserStatusPreviewDTO{}, err
	}
	var user businesscore.User
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND deleted_at IS NULL", in.UserID).First(&user).Error; err != nil {
		return UserStatusPreviewDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "user not found")
	}
	if in.TargetStatus != accountspace.StatusActive && in.TargetStatus != "disabled" {
		return UserStatusPreviewDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "target_status must be active or disabled")
	}
	expires := a.now().Add(10 * time.Minute)
	return UserStatusPreviewDTO{
		PreviewToken:  security.SignPreviewToken(a.secret, "user_status", in.UserID+":"+in.TargetStatus, 10*time.Minute),
		CurrentStatus: user.Status, TargetStatus: in.TargetStatus,
		ImpactSummary:         []string{"private operations will follow the target account status", "public content is not deleted"},
		PublicContentRetained: true, PrivateContentNotExposed: true, ExpiresAt: expires,
	}, nil
}

func (a *App) ConfirmSetUserStatus(ctx context.Context, in UserStatusInput) (UserSummaryDTO, error) {
	if err := a.requireAdmin(ctx, in.Auth, false); err != nil {
		return UserSummaryDTO{}, err
	}
	if in.Reason == "" {
		return UserSummaryDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "reason is required")
	}
	if !security.VerifyPreviewToken(a.secret, in.PreviewToken, "user_status", in.UserID+":"+in.TargetStatus) {
		return UserSummaryDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "preview token is invalid or expired")
	}
	hash := in.Meta.RequestHash
	if hash == "" {
		hash = security.HashIdentifier(in.UserID + ":" + in.TargetStatus + ":" + in.Reason)
	}
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "admin:" + in.Auth.AdminID, Scope: "admin.user.status", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.AdminID,
	})
	if err != nil {
		return UserSummaryDTO{}, err
	}
	var dto UserSummaryDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user businesscore.User
		if err := tx.Where("id = ? AND deleted_at IS NULL", in.UserID).First(&user).Error; err != nil {
			return bizerrors.New(bizerrors.CodeResourceNotFound, "user not found")
		}
		before := user.Status
		now := a.now()
		if err := tx.Model(&businesscore.User{}).Where("id = ?", user.ID).Updates(map[string]any{"status": in.TargetStatus, "disabled_reason": in.Reason, "updated_by": in.Auth.AdminID, "updated_at": now}).Error; err != nil {
			return err
		}
		user.Status = in.TargetStatus
		dto = userDTO(user)
		audit := auditRecord(in.Meta.TraceID, in.Auth.AdminID, auditlog.ActionUserStatusSet, "user", user.ID, "success")
		audit.BeforeStatus = &before
		audit.AfterStatus = &in.TargetStatus
		audit.Reason = &in.Reason
		return tx.Create(audit).Error
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return UserSummaryDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "user", ID: dto.UserID})
	return dto, nil
}

func (a *App) ListAuditLogs(ctx context.Context, in AuditQueryInput) (Page[AuditLogDTO], error) {
	if err := a.requireAdmin(ctx, in.Auth, false); err != nil {
		return Page[AuditLogDTO]{}, err
	}
	limit, offset := normalizePage(in.Limit, in.Offset)
	db := a.repo.DB().WithContext(ctx).Model(&auditlog.AuditRecord{})
	if in.BusinessAction != "" {
		db = db.Where("business_action = ?", in.BusinessAction)
	}
	if in.TraceID != "" {
		db = db.Where("trace_id = ?", in.TraceID)
	}
	var rows []auditlog.AuditRecord
	if err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[AuditLogDTO]{}, err
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[AuditLogDTO]{}, err
	}
	items := make([]AuditLogDTO, 0, len(rows))
	for _, row := range rows {
		resourceID := ""
		if row.ResourceID != nil {
			resourceID = *row.ResourceID
		}
		errorCode := ""
		if row.ErrorCode != nil {
			errorCode = *row.ErrorCode
		}
		operator := ""
		if row.OperatorID != nil {
			operator = maskID(*row.OperatorID)
		}
		items = append(items, AuditLogDTO{
			AuditID: row.AuditID, OperatorType: row.OperatorType, OperatorIDMasked: operator,
			BusinessAction: row.BusinessAction, ResourceType: row.ResourceType, ResourceID: resourceID,
			Result: row.Result, ErrorCode: errorCode, TraceID: row.TraceID, CreatedAt: row.CreatedAt,
		})
	}
	return Page[AuditLogDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) AuthenticateToken(ctx context.Context, rawToken string) (AdminAuth, error) {
	rawToken = strings.TrimSpace(strings.TrimPrefix(rawToken, "Bearer "))
	if rawToken == "" {
		return AdminAuth{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin authorization token is required")
	}
	tokenHash := security.HashOpaque(rawToken)
	var session businesscore.PlatformAdminSession
	if err := a.repo.DB().WithContext(ctx).Where("session_token_digest = ? AND status = ? AND expires_at > ?", tokenHash, accountspace.StatusActive, a.now()).First(&session).Error; err != nil {
		return AdminAuth{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin session is invalid")
	}
	var admin businesscore.PlatformAdmin
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND status = ?", session.AdminID, accountspace.StatusActive).First(&admin).Error; err != nil {
		return AdminAuth{}, bizerrors.New(bizerrors.CodePermissionDenied, "admin is unavailable")
	}
	return AdminAuth{AdminID: admin.ID, Account: admin.AdminAccount, SessionID: session.ID, AccessToken: rawToken}, nil
}

func (a *App) requireAdmin(ctx context.Context, auth AdminAuth, allowRotateOnly bool) error {
	if auth.AdminID == "" {
		return bizerrors.New(bizerrors.CodeUnauthenticated, "admin session is required")
	}
	var admin businesscore.PlatformAdmin
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND status = ?", auth.AdminID, accountspace.StatusActive).First(&admin).Error; err != nil {
		return bizerrors.New(bizerrors.CodePermissionDenied, "admin is unavailable")
	}
	if admin.MustRotatePassword && !allowRotateOnly {
		return bizerrors.New(bizerrors.CodeStateConflict, "admin password rotation is required")
	}
	return nil
}

func (a *App) writeAttempt(ctx context.Context, account, accountHash, result, failure, traceID string) {
	attempt := businesscore.AdminLoginAttempt{
		ID: security.RandomID("alat_"), AdminAccount: account, AccountHash: &accountHash, Result: result,
		FailureReason: optionalString(failure), TraceID: traceID, CreatedAt: a.now(),
	}
	_ = a.repo.DB().WithContext(ctx).Create(&attempt).Error
}

func adminDTO(row businesscore.PlatformAdmin) PlatformAdminDTO {
	return PlatformAdminDTO{AdminID: row.ID, Account: row.AdminAccount, Status: row.Status, MustRotatePassword: row.MustRotatePassword, CreatedAt: row.CreatedAt}
}

func userDTO(user businesscore.User) UserSummaryDTO {
	email := ""
	if user.Email != nil {
		email = security.MaskEmail(*user.Email)
	}
	phone := ""
	if user.Phone != nil {
		phone = "***" + last4(*user.Phone)
	}
	spaceID := ""
	if user.DefaultSpaceID != nil {
		spaceID = *user.DefaultSpaceID
	}
	return UserSummaryDTO{
		UserID: user.ID, Status: user.Status, PublicNickname: user.DisplayName,
		EmailMasked: email, PhoneMasked: phone, PersonalSpaceID: spaceID, RegisteredAt: user.CreatedAt, LastLoginAt: user.LastLoginAt,
	}
}

func auditRecord(traceID, adminID, action, resourceType, resourceID, result string) *auditlog.AuditRecord {
	return &auditlog.AuditRecord{
		AuditID: security.RandomID("audit_"), TraceID: traceID, OperatorType: "platform_admin", OperatorID: &adminID,
		TenantID: "platform", BusinessAction: action, ResourceType: resourceType, ResourceID: &resourceID, Result: result,
		MetadataSummary: datatypes.JSON([]byte(`{}`)), CreatedAt: time.Now().UTC(),
	}
}

func normalizePage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func bootstrapStatus(mustRotate bool) string {
	if mustRotate {
		return "initialized"
	}
	return "rotated"
}

func maskID(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:4] + "***" + value[len(value)-4:]
}

func last4(value string) string {
	if len(value) <= 4 {
		return value
	}
	return value[len(value)-4:]
}

func errorCode(err error) string {
	var businessErr *bizerrors.BusinessError
	if errors.As(err, &businessErr) {
		return string(businessErr.Code)
	}
	return string(bizerrors.CodeInternal)
}
