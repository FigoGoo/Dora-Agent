package accountspace

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/security"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	IdentityPersonal   = "personal"
	IdentityEnterprise = "enterprise_member"
	SpacePersonal      = "personal"
	SpaceEnterprise    = "enterprise"
	RoleOwner          = "owner"
	RoleMember         = "member"
	StatusActive       = "active"
	StatusRemoved      = "removed"
)

type RequestMeta struct {
	TraceID        string
	RequestID      string
	IdempotencyKey string
	Source         string
	RequestHash    string
}

type AuthContext struct {
	UserID            string
	LoginIdentityType string
	SpaceID           string
	EnterpriseID      string
	EnterpriseRole    string
	SessionID         string
	AccessToken       string
}

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
		secret: "local-m2-preview-secret",
	}
}

type RegisterInput struct {
	Email       string
	Phone       string
	Password    string
	DisplayName string
	Meta        RequestMeta
}

type LoginInput struct {
	LoginType    string
	Account      string
	Password     string
	EnterpriseID string
	Meta         RequestMeta
}

type SwitchIdentityInput struct {
	Auth               AuthContext
	TargetIdentityType string
	TargetEnterpriseID string
	Meta               RequestMeta
}

type CreateEnterpriseInput struct {
	Auth             AuthContext
	EnterpriseName   string
	OwnerDisplayName string
	ContactEmail     string
	Meta             RequestMeta
}

type InviteInput struct {
	Auth          AuthContext
	Email         string
	Phone         string
	InviteMessage string
	ExpiresInDays int
	Meta          RequestMeta
}

type RemoveMemberInput struct {
	Auth         AuthContext
	MemberID     string
	Reason       string
	PreviewToken string
	Meta         RequestMeta
}

type TransferOwnerInput struct {
	Auth           AuthContext
	TargetMemberID string
	Reason         string
	PreviewToken   string
	Meta           RequestMeta
}

type PageRequest struct {
	Limit  int
	Offset int
}

type AuthSessionDTO struct {
	UserID              string             `json:"user_id"`
	CurrentSpaceID      string             `json:"current_space_id"`
	LoginIdentityType   string             `json:"login_identity_type"`
	EnterpriseID        string             `json:"enterprise_id,omitempty"`
	EnterpriseRole      string             `json:"enterprise_role,omitempty"`
	AccessToken         string             `json:"access_token"`
	CSRFToken           string             `json:"csrf_token,omitempty"`
	ExpiresAt           time.Time          `json:"expires_at"`
	User                UserProfileDTO     `json:"user,omitempty"`
	CurrentSpace        SpaceContextDTO    `json:"current_space,omitempty"`
	AvailableIdentities []LoginIdentityDTO `json:"available_identities,omitempty"`
}

type UserProfileDTO struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Status      string `json:"status"`
}

type LoginIdentityDTO struct {
	IdentityType   string `json:"identity_type"`
	SpaceID        string `json:"space_id"`
	SpaceType      string `json:"space_type"`
	EnterpriseID   string `json:"enterprise_id,omitempty"`
	EnterpriseName string `json:"enterprise_name,omitempty"`
	EnterpriseRole string `json:"enterprise_role,omitempty"`
	IsCurrent      bool   `json:"is_current"`
}

type SpaceContextDTO struct {
	UserID             string            `json:"user_id,omitempty"`
	SpaceID            string            `json:"space_id"`
	SpaceType          string            `json:"space_type"`
	EnterpriseID       string            `json:"enterprise_id,omitempty"`
	EnterpriseRole     string            `json:"enterprise_role,omitempty"`
	CreditAccountScope string            `json:"credit_account_scope"`
	CreditAccountID    string            `json:"credit_account_id"`
	SkillScopeKeys     []string          `json:"skill_scope_keys"`
	PermissionSummary  map[string]string `json:"permission_summary"`
}

type EnterpriseSummaryDTO struct {
	EnterpriseID    string `json:"enterprise_id"`
	SpaceID         string `json:"space_id"`
	Name            string `json:"name"`
	OwnerUserID     string `json:"owner_user_id"`
	CurrentUserRole string `json:"current_user_role,omitempty"`
	Status          string `json:"status"`
	MemberCount     int64  `json:"member_count"`
}

type EnterpriseMemberDTO struct {
	MemberID     string    `json:"member_id"`
	EnterpriseID string    `json:"enterprise_id"`
	UserID       string    `json:"user_id"`
	DisplayName  string    `json:"display_name,omitempty"`
	EmailMasked  string    `json:"email_masked,omitempty"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	JoinedAt     time.Time `json:"joined_at"`
}

type EnterpriseInviteDTO struct {
	InviteID            string    `json:"invite_id"`
	EnterpriseID        string    `json:"enterprise_id"`
	TargetAccountMasked string    `json:"target_account_masked"`
	Status              string    `json:"status"`
	ExpiresAt           time.Time `json:"expires_at"`
}

type PreviewDTO struct {
	PreviewToken string    `json:"preview_token"`
	ImpactItems  []string  `json:"impact_items"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type TransferPreviewDTO struct {
	PreviewToken string    `json:"preview_token"`
	ImpactItems  []string  `json:"impact_items"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type Page[T any] struct {
	Items  []T   `json:"items"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

func (a *App) RegisterPersonalAccount(ctx context.Context, in RegisterInput) (AuthSessionDTO, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	phone := strings.TrimSpace(in.Phone)
	if email == "" && phone == "" {
		return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "email or phone is required")
	}
	if strings.TrimSpace(in.Password) == "" || strings.TrimSpace(in.DisplayName) == "" {
		return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "password and display_name are required")
	}
	actorKey := email
	if actorKey == "" {
		actorKey = phone
	}
	emailHash := optionalHash(email)
	phoneHash := optionalHash(phone)
	requestHash := in.Meta.RequestHash
	if requestHash == "" {
		requestHash = security.HashIdentifier(actorKey + ":register")
	}
	idempotencyKey := businessIdempotencyKey(in.Meta, "auth.register", requestHash)
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID:       authTenantID(actorKey),
		Scope:          "auth.register",
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash,
		ActorUserID:    security.HashIdentifier(actorKey),
	})
	if err != nil {
		return AuthSessionDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "idempotency key conflicts with a different registration request")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeProcessing, "registration request is still processing")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.sessionDTOByID(ctx, decision.ReplayResult.ID, "")
	}

	var sessionDTO AuthSessionDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing int64
		query := tx.Model(&businesscore.User{})
		switch {
		case emailHash != nil && phoneHash != nil:
			query = query.Where("email_hash = ? OR phone_hash = ?", *emailHash, *phoneHash)
		case emailHash != nil:
			query = query.Where("email_hash = ?", *emailHash)
		default:
			query = query.Where("phone_hash = ?", *phoneHash)
		}
		if err := query.Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return bizerrors.New(bizerrors.CodeStateConflict, "account already exists")
		}
		now := a.now()
		userID := security.RandomID("usr_")
		spaceID := security.RandomID("sp_")
		creditAccountID := security.RandomID("ca_")
		passwordHash, err := security.HashPassword(in.Password)
		if err != nil {
			return bizerrors.New(bizerrors.CodeInvalidArgument, err.Error())
		}
		user := businesscore.User{
			ID:               userID,
			AccountNo:        "U" + userID[4:],
			Email:            optionalString(email),
			Phone:            optionalString(phone),
			EmailHash:        emailHash,
			PhoneHash:        phoneHash,
			PasswordHash:     passwordHash,
			DisplayName:      strings.TrimSpace(in.DisplayName),
			Status:           StatusActive,
			DefaultSpaceID:   &spaceID,
			RegisteredSource: "web",
			CreatedBy:        optionalString(userID),
			UpdatedBy:        optionalString(userID),
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		space := businesscore.Space{
			ID:              spaceID,
			OwnerUserID:     userID,
			SpaceType:       SpacePersonal,
			DisplayName:     strings.TrimSpace(in.DisplayName),
			Status:          StatusActive,
			CreditAccountID: &creditAccountID,
			CreatedBy:       optionalString(userID),
			UpdatedBy:       optionalString(userID),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := tx.Create(&space).Error; err != nil {
			return err
		}
		credit := businesscore.CreditAccount{
			ID:              creditAccountID,
			AccountType:     SpacePersonal,
			OwnerUserID:     &userID,
			Status:          StatusActive,
			AvailablePoints: 0,
			CreatedBy:       optionalString(userID),
			UpdatedBy:       optionalString(userID),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := tx.Create(&credit).Error; err != nil {
			return err
		}
		dto, err := a.createUserSession(ctx, tx, user, space, nil, RoleOwner, in.Meta.TraceID)
		if err != nil {
			return err
		}
		sessionDTO = dto
		audit := auditRecord(in.Meta.TraceID, "user", userID, spaceID, auditlog.ActionAuthRegister, "user", userID, "success")
		if err := tx.Create(audit).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return AuthSessionDTO{}, err
	}
	if err := a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "auth_session", ID: sessionDTO.CurrentSpaceID + ":" + sessionDTO.UserID}); err != nil {
		return AuthSessionDTO{}, err
	}
	return sessionDTO, nil
}

func (a *App) Login(ctx context.Context, in LoginInput) (AuthSessionDTO, error) {
	account := strings.ToLower(strings.TrimSpace(in.Account))
	if account == "" || in.Password == "" {
		return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "account and password are required")
	}
	loginType := strings.ToLower(strings.TrimSpace(in.LoginType))
	if loginType == "" {
		loginType = IdentityPersonal
	}
	var user businesscore.User
	accountHash := security.HashIdentifier(account)
	if err := a.repo.DB().WithContext(ctx).
		Where("(email_hash = ? OR phone_hash = ? OR lower(email) = ? OR phone = ?) AND deleted_at IS NULL", accountHash, accountHash, account, account).
		First(&user).Error; err != nil {
		a.writeLoginAudit(ctx, in.Meta.TraceID, account, "failed", "UNAUTHENTICATED")
		return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "invalid account or password")
	}
	if !security.VerifyPassword(user.PasswordHash, in.Password) {
		a.writeLoginAudit(ctx, in.Meta.TraceID, account, "failed", "UNAUTHENTICATED")
		return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "invalid account or password")
	}
	if user.Status != StatusActive {
		a.writeLoginAudit(ctx, in.Meta.TraceID, account, "disabled", "PERMISSION_DENIED")
		return AuthSessionDTO{}, bizerrors.New(bizerrors.CodePermissionDenied, "user is disabled")
	}
	var space businesscore.Space
	var member *businesscore.EnterpriseMember
	var enterprise *businesscore.Enterprise
	if loginType == IdentityEnterprise {
		if in.EnterpriseID == "" {
			return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "enterprise_id is required")
		}
		m, ent, err := a.requireActiveEnterpriseMember(ctx, user.ID, in.EnterpriseID)
		if err != nil {
			return AuthSessionDTO{}, err
		}
		member = &m
		enterprise = &ent
		if ent.DefaultSpaceID == nil {
			return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "enterprise space is missing")
		}
		if err := a.repo.DB().WithContext(ctx).Where("id = ? AND status = ?", *ent.DefaultSpaceID, StatusActive).First(&space).Error; err != nil {
			return AuthSessionDTO{}, bizerrors.New(bizerrors.CodePermissionDenied, "enterprise space is unavailable")
		}
	} else {
		if user.DefaultSpaceID == nil {
			return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "personal space is missing")
		}
		if err := a.repo.DB().WithContext(ctx).Where("id = ? AND status = ?", *user.DefaultSpaceID, StatusActive).First(&space).Error; err != nil {
			return AuthSessionDTO{}, bizerrors.New(bizerrors.CodePermissionDenied, "personal space is unavailable")
		}
	}
	var session AuthSessionDTO
	err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		role := RoleOwner
		if member != nil {
			role = member.Role
		}
		dto, err := a.createUserSession(ctx, tx, user, space, enterprise, role, in.Meta.TraceID)
		if err != nil {
			return err
		}
		session = dto
		now := a.now()
		if err := tx.Model(&businesscore.User{}).Where("id = ?", user.ID).Updates(map[string]any{"last_login_at": now, "updated_by": user.ID, "updated_at": now}).Error; err != nil {
			return err
		}
		audit := auditRecord(in.Meta.TraceID, "user", user.ID, space.ID, auditlog.ActionAuthLogin, "auth_session", dto.UserID, "success")
		return tx.Create(audit).Error
	})
	if err != nil {
		return AuthSessionDTO{}, err
	}
	return session, nil
}

func (a *App) Logout(ctx context.Context, auth AuthContext, meta RequestMeta) error {
	if auth.SessionID == "" {
		return bizerrors.New(bizerrors.CodeUnauthenticated, "session is required")
	}
	err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		if err := tx.Model(&businesscore.AuthSession{}).
			Where("id = ? AND user_id = ?", auth.SessionID, auth.UserID).
			Updates(map[string]any{"status": "revoked", "updated_by": auth.UserID, "updated_at": now}).Error; err != nil {
			return err
		}
		audit := auditRecord(meta.TraceID, "user", auth.UserID, auth.SpaceID, auditlog.ActionAuthLogout, "auth_session", auth.SessionID, "success")
		return tx.Create(audit).Error
	})
	return err
}

func (a *App) ResolveCurrentSpaceContext(ctx context.Context, auth AuthContext, expectedSpaceID string) (SpaceContextDTO, error) {
	if auth.UserID == "" {
		return SpaceContextDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "actor_user_id is required")
	}
	var user businesscore.User
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND deleted_at IS NULL", auth.UserID).First(&user).Error; err != nil {
		return SpaceContextDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "user is not authenticated")
	}
	if user.Status != StatusActive {
		return SpaceContextDTO{}, bizerrors.New(bizerrors.CodePermissionDenied, "user is disabled")
	}
	spaceID := auth.SpaceID
	if spaceID == "" && user.DefaultSpaceID != nil {
		spaceID = *user.DefaultSpaceID
	}
	if expectedSpaceID != "" && spaceID != expectedSpaceID {
		return SpaceContextDTO{}, bizerrors.New(bizerrors.CodeCrossSpaceDenied, "expected space does not match current identity")
	}
	var space businesscore.Space
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND status = ?", spaceID, StatusActive).First(&space).Error; err != nil {
		return SpaceContextDTO{}, bizerrors.New(bizerrors.CodePermissionDenied, "space is unavailable")
	}
	if space.SpaceType == SpacePersonal {
		if space.OwnerUserID != user.ID {
			return SpaceContextDTO{}, bizerrors.New(bizerrors.CodeCrossSpaceDenied, "personal space does not belong to actor")
		}
		return a.spaceContextFromSpace(ctx, user.ID, space, "", "")
	}
	if space.EnterpriseID == nil {
		return SpaceContextDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "enterprise space is missing enterprise_id")
	}
	member, _, err := a.requireActiveEnterpriseMember(ctx, user.ID, *space.EnterpriseID)
	if err != nil {
		return SpaceContextDTO{}, err
	}
	return a.spaceContextFromSpace(ctx, user.ID, space, *space.EnterpriseID, member.Role)
}

func (a *App) CurrentSpaceFromSession(ctx context.Context, auth AuthContext) (SpaceContextDTO, error) {
	return a.ResolveCurrentSpaceContext(ctx, auth, "")
}

func (a *App) SwitchIdentity(ctx context.Context, in SwitchIdentityInput) (AuthSessionDTO, error) {
	if in.Auth.SessionID == "" {
		return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "session is required")
	}
	target := strings.ToLower(strings.TrimSpace(in.TargetIdentityType))
	if target == "" {
		target = IdentityPersonal
	}
	hash := in.Meta.RequestHash
	if hash == "" {
		hash = security.HashIdentifier(in.Auth.UserID + ":" + target + ":" + in.TargetEnterpriseID)
	}
	idempotencyKey := businessIdempotencyKey(in.Meta, "account.switch_identity", hash)
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID:       "user:" + in.Auth.UserID,
		SpaceID:        in.Auth.SpaceID,
		Scope:          "account.switch_identity",
		IdempotencyKey: idempotencyKey,
		RequestHash:    hash,
		ActorUserID:    in.Auth.UserID,
	})
	if err != nil {
		return AuthSessionDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "idempotency key conflicts with another switch request")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeProcessing, "switch identity request is still processing")
	}
	var dto AuthSessionDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user businesscore.User
		if err := tx.Where("id = ? AND status = ?", in.Auth.UserID, StatusActive).First(&user).Error; err != nil {
			return bizerrors.New(bizerrors.CodePermissionDenied, "user is unavailable")
		}
		var space businesscore.Space
		var enterprise *businesscore.Enterprise
		role := RoleOwner
		if target == IdentityEnterprise {
			member, ent, err := a.requireActiveEnterpriseMemberTx(tx, user.ID, in.TargetEnterpriseID)
			if err != nil {
				return err
			}
			enterprise = &ent
			role = member.Role
			if ent.DefaultSpaceID == nil {
				return bizerrors.New(bizerrors.CodeStateConflict, "enterprise space is missing")
			}
			if err := tx.Where("id = ? AND status = ?", *ent.DefaultSpaceID, StatusActive).First(&space).Error; err != nil {
				return bizerrors.New(bizerrors.CodePermissionDenied, "enterprise space is unavailable")
			}
		} else {
			if user.DefaultSpaceID == nil {
				return bizerrors.New(bizerrors.CodeStateConflict, "personal space is missing")
			}
			if err := tx.Where("id = ? AND status = ?", *user.DefaultSpaceID, StatusActive).First(&space).Error; err != nil {
				return bizerrors.New(bizerrors.CodePermissionDenied, "personal space is unavailable")
			}
		}
		updates := map[string]any{
			"login_identity_type": target,
			"current_space_id":    space.ID,
			"updated_by":          user.ID,
			"updated_at":          a.now(),
		}
		if enterprise != nil {
			updates["current_enterprise_id"] = enterprise.ID
			updates["current_enterprise_role"] = role
			updates["enterprise_role"] = role
		} else {
			updates["current_enterprise_id"] = nil
			updates["current_enterprise_role"] = nil
			updates["enterprise_role"] = nil
		}
		if err := tx.Model(&businesscore.AuthSession{}).Where("id = ? AND user_id = ?", in.Auth.SessionID, user.ID).Updates(updates).Error; err != nil {
			return err
		}
		dto = a.authSessionDTO(user, space, enterprise, role, in.Auth.AccessToken, "", a.now().Add(24*time.Hour))
		audit := auditRecord(in.Meta.TraceID, "user", user.ID, space.ID, auditlog.ActionAccountSwitchIdentity, "auth_session", in.Auth.SessionID, "success")
		return tx.Create(audit).Error
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return AuthSessionDTO{}, err
	}
	if err := a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "auth_session", ID: in.Auth.SessionID}); err != nil {
		return AuthSessionDTO{}, err
	}
	return dto, nil
}

func (a *App) CreateEnterprise(ctx context.Context, in CreateEnterpriseInput) (EnterpriseSummaryDTO, error) {
	if in.Auth.UserID == "" || in.Auth.SpaceID == "" {
		return EnterpriseSummaryDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "user session is required")
	}
	name := strings.TrimSpace(in.EnterpriseName)
	if name == "" {
		return EnterpriseSummaryDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "enterprise_name is required")
	}
	hash := in.Meta.RequestHash
	if hash == "" {
		hash = security.HashIdentifier(in.Auth.UserID + ":" + name)
	}
	idempotencyKey := businessIdempotencyKey(in.Meta, "enterprise.create", hash)
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID:       "user:" + in.Auth.UserID,
		SpaceID:        in.Auth.SpaceID,
		Scope:          "enterprise.create",
		IdempotencyKey: idempotencyKey,
		RequestHash:    hash,
		ActorUserID:    in.Auth.UserID,
	})
	if err != nil {
		return EnterpriseSummaryDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return EnterpriseSummaryDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "idempotency key conflicts with another enterprise request")
	}
	var summary EnterpriseSummaryDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user businesscore.User
		if err := tx.Where("id = ? AND status = ?", in.Auth.UserID, StatusActive).First(&user).Error; err != nil {
			return bizerrors.New(bizerrors.CodePermissionDenied, "user is unavailable")
		}
		now := a.now()
		entID := security.RandomID("ent_")
		spaceID := security.RandomID("sp_")
		creditID := security.RandomID("ca_")
		ent := businesscore.Enterprise{
			ID:              entID,
			EnterpriseNo:    "E" + entID[4:],
			Name:            name,
			OwnerUserID:     user.ID,
			DefaultSpaceID:  &spaceID,
			CreditAccountID: &creditID,
			Status:          StatusActive,
			CreatedBy:       optionalString(user.ID),
			UpdatedBy:       optionalString(user.ID),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := tx.Create(&ent).Error; err != nil {
			return err
		}
		space := businesscore.Space{
			ID:              spaceID,
			OwnerUserID:     user.ID,
			SpaceType:       SpaceEnterprise,
			EnterpriseID:    &entID,
			DisplayName:     name,
			Status:          StatusActive,
			CreditAccountID: &creditID,
			CreatedBy:       optionalString(user.ID),
			UpdatedBy:       optionalString(user.ID),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := tx.Create(&space).Error; err != nil {
			return err
		}
		joined := now
		member := businesscore.EnterpriseMember{
			ID:           security.RandomID("mem_"),
			EnterpriseID: entID,
			UserID:       user.ID,
			Role:         RoleOwner,
			Status:       StatusActive,
			JoinedAt:     &joined,
			CreatedBy:    optionalString(user.ID),
			UpdatedBy:    optionalString(user.ID),
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := tx.Create(&member).Error; err != nil {
			return err
		}
		credit := businesscore.CreditAccount{
			ID:              creditID,
			AccountType:     SpaceEnterprise,
			EnterpriseID:    &entID,
			Status:          StatusActive,
			AvailablePoints: 0,
			CreatedBy:       optionalString(user.ID),
			UpdatedBy:       optionalString(user.ID),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := tx.Create(&credit).Error; err != nil {
			return err
		}
		summary = EnterpriseSummaryDTO{EnterpriseID: ent.ID, SpaceID: space.ID, Name: ent.Name, OwnerUserID: ent.OwnerUserID, CurrentUserRole: RoleOwner, Status: ent.Status, MemberCount: 1}
		audit := auditRecord(in.Meta.TraceID, "user", user.ID, space.ID, auditlog.ActionEnterpriseCreate, "enterprise", ent.ID, "success")
		return tx.Create(audit).Error
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return EnterpriseSummaryDTO{}, err
	}
	if err := a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "enterprise", ID: summary.EnterpriseID}); err != nil {
		return EnterpriseSummaryDTO{}, err
	}
	return summary, nil
}

func (a *App) GetEnterpriseSummary(ctx context.Context, auth AuthContext) (EnterpriseSummaryDTO, error) {
	if auth.EnterpriseID == "" {
		return EnterpriseSummaryDTO{}, bizerrors.New(bizerrors.CodePermissionDenied, "enterprise identity is required")
	}
	member, ent, err := a.requireActiveEnterpriseMember(ctx, auth.UserID, auth.EnterpriseID)
	if err != nil {
		return EnterpriseSummaryDTO{}, err
	}
	var count int64
	if err := a.repo.DB().WithContext(ctx).Model(&businesscore.EnterpriseMember{}).Where("enterprise_id = ? AND status = ?", ent.ID, StatusActive).Count(&count).Error; err != nil {
		return EnterpriseSummaryDTO{}, err
	}
	spaceID := ""
	if ent.DefaultSpaceID != nil {
		spaceID = *ent.DefaultSpaceID
	}
	return EnterpriseSummaryDTO{EnterpriseID: ent.ID, SpaceID: spaceID, Name: ent.Name, OwnerUserID: ent.OwnerUserID, CurrentUserRole: member.Role, Status: ent.Status, MemberCount: count}, nil
}

func (a *App) ListEnterpriseMembers(ctx context.Context, auth AuthContext, page PageRequest) (Page[EnterpriseMemberDTO], error) {
	if err := a.requireEnterpriseOwner(ctx, auth); err != nil {
		return Page[EnterpriseMemberDTO]{}, err
	}
	limit, offset := normalizePage(page.Limit, page.Offset)
	var members []businesscore.EnterpriseMember
	db := a.repo.DB().WithContext(ctx).Where("enterprise_id = ?", auth.EnterpriseID).Order("created_at DESC")
	if err := db.Limit(limit).Offset(offset).Find(&members).Error; err != nil {
		return Page[EnterpriseMemberDTO]{}, err
	}
	var total int64
	if err := db.Model(&businesscore.EnterpriseMember{}).Count(&total).Error; err != nil {
		return Page[EnterpriseMemberDTO]{}, err
	}
	userIDs := make([]string, 0, len(members))
	for _, member := range members {
		userIDs = append(userIDs, member.UserID)
	}
	users := map[string]businesscore.User{}
	if len(userIDs) > 0 {
		var rows []businesscore.User
		if err := a.repo.DB().WithContext(ctx).Where("id IN ?", userIDs).Find(&rows).Error; err != nil {
			return Page[EnterpriseMemberDTO]{}, err
		}
		for _, row := range rows {
			users[row.ID] = row
		}
	}
	items := make([]EnterpriseMemberDTO, 0, len(members))
	for _, member := range members {
		user := users[member.UserID]
		joinedAt := member.CreatedAt
		if member.JoinedAt != nil {
			joinedAt = *member.JoinedAt
		}
		email := ""
		if user.Email != nil {
			email = security.MaskEmail(*user.Email)
		}
		items = append(items, EnterpriseMemberDTO{
			MemberID: member.ID, EnterpriseID: member.EnterpriseID, UserID: member.UserID,
			DisplayName: user.DisplayName, EmailMasked: email, Role: member.Role, Status: member.Status, JoinedAt: joinedAt,
		})
	}
	return Page[EnterpriseMemberDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) CreateMemberInvite(ctx context.Context, in InviteInput) (EnterpriseInviteDTO, error) {
	if err := a.requireEnterpriseOwner(ctx, in.Auth); err != nil {
		return EnterpriseInviteDTO{}, err
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	phone := strings.TrimSpace(in.Phone)
	if email == "" && phone == "" {
		return EnterpriseInviteDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "email or phone is required")
	}
	if in.ExpiresInDays <= 0 {
		in.ExpiresInDays = 7
	}
	hash := in.Meta.RequestHash
	if hash == "" {
		hash = security.HashIdentifier(in.Auth.EnterpriseID + ":" + email + ":" + phone)
	}
	idempotencyKey := businessIdempotencyKey(in.Meta, "enterprise.invite.create", hash)
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID:       "enterprise:" + in.Auth.EnterpriseID,
		SpaceID:        in.Auth.SpaceID,
		Scope:          "enterprise.invite.create",
		IdempotencyKey: idempotencyKey,
		RequestHash:    hash,
		ActorUserID:    in.Auth.UserID,
		EnterpriseID:   &in.Auth.EnterpriseID,
	})
	if err != nil {
		return EnterpriseInviteDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return EnterpriseInviteDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "idempotency key conflicts with another invite request")
	}
	var dto EnterpriseInviteDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		rawToken, tokenHash := security.NewOpaqueToken("inv")
		invite := businesscore.EnterpriseInvite{
			ID:               security.RandomID("inv_"),
			EnterpriseID:     in.Auth.EnterpriseID,
			InviteeEmail:     optionalString(email),
			InviteePhone:     optionalString(phone),
			TargetEmailHash:  optionalHash(email),
			TargetPhoneHash:  optionalHash(phone),
			Role:             RoleMember,
			InviteCodeDigest: tokenHash,
			InviteTokenHash:  &tokenHash,
			Status:           "pending",
			InvitedByUserID:  in.Auth.UserID,
			ExpiresAt:        now.Add(time.Duration(in.ExpiresInDays) * 24 * time.Hour),
			CreatedBy:        optionalString(in.Auth.UserID),
			UpdatedBy:        optionalString(in.Auth.UserID),
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := tx.Create(&invite).Error; err != nil {
			return err
		}
		dto = inviteDTO(invite, rawToken)
		audit := auditRecord(in.Meta.TraceID, "user", in.Auth.UserID, in.Auth.SpaceID, auditlog.ActionEnterpriseInviteCreate, "enterprise_invite", invite.ID, "success")
		return tx.Create(audit).Error
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return EnterpriseInviteDTO{}, err
	}
	if err := a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "enterprise_invite", ID: dto.InviteID}); err != nil {
		return EnterpriseInviteDTO{}, err
	}
	return dto, nil
}

func (a *App) PreviewRemoveMember(ctx context.Context, in RemoveMemberInput) (PreviewDTO, error) {
	if err := a.requireEnterpriseOwner(ctx, in.Auth); err != nil {
		return PreviewDTO{}, err
	}
	var member businesscore.EnterpriseMember
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND enterprise_id = ?", in.MemberID, in.Auth.EnterpriseID).First(&member).Error; err != nil {
		return PreviewDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "member not found")
	}
	if member.Role == RoleOwner {
		return PreviewDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "owner cannot be removed")
	}
	expires := a.now().Add(10 * time.Minute)
	return PreviewDTO{
		PreviewToken: security.SignPreviewToken(a.secret, "remove_member", member.ID, 10*time.Minute),
		ImpactItems:  []string{"member loses enterprise workspace access", "member private projects remain invisible to owner"},
		ExpiresAt:    expires,
	}, nil
}

func (a *App) ConfirmRemoveMember(ctx context.Context, in RemoveMemberInput) (EnterpriseMemberDTO, error) {
	if err := a.requireEnterpriseOwner(ctx, in.Auth); err != nil {
		return EnterpriseMemberDTO{}, err
	}
	if !security.VerifyPreviewToken(a.secret, in.PreviewToken, "remove_member", in.MemberID) {
		return EnterpriseMemberDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "preview token is invalid or expired")
	}
	hash := in.Meta.RequestHash
	if hash == "" {
		hash = security.HashIdentifier(in.Auth.EnterpriseID + ":" + in.MemberID + ":" + in.Reason)
	}
	idempotencyKey := businessIdempotencyKey(in.Meta, "enterprise.member.remove", hash)
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID:       "enterprise:" + in.Auth.EnterpriseID,
		SpaceID:        in.Auth.SpaceID,
		Scope:          "enterprise.member.remove",
		IdempotencyKey: idempotencyKey,
		RequestHash:    hash,
		ActorUserID:    in.Auth.UserID,
		EnterpriseID:   &in.Auth.EnterpriseID,
	})
	if err != nil {
		return EnterpriseMemberDTO{}, err
	}
	var dto EnterpriseMemberDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var member businesscore.EnterpriseMember
		if err := tx.Where("id = ? AND enterprise_id = ?", in.MemberID, in.Auth.EnterpriseID).First(&member).Error; err != nil {
			return bizerrors.New(bizerrors.CodeResourceNotFound, "member not found")
		}
		if member.Role == RoleOwner {
			return bizerrors.New(bizerrors.CodeStateConflict, "owner cannot be removed")
		}
		now := a.now()
		if err := tx.Model(&businesscore.EnterpriseMember{}).Where("id = ?", member.ID).Updates(map[string]any{
			"status": StatusRemoved, "removed_at": now, "removed_by": in.Auth.UserID, "remove_reason": in.Reason, "updated_by": in.Auth.UserID, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		member.Status = StatusRemoved
		member.RemovedAt = &now
		dto = EnterpriseMemberDTO{MemberID: member.ID, EnterpriseID: member.EnterpriseID, UserID: member.UserID, Role: member.Role, Status: member.Status, JoinedAt: now}
		audit := auditRecord(in.Meta.TraceID, "user", in.Auth.UserID, in.Auth.SpaceID, auditlog.ActionEnterpriseMemberRemove, "enterprise_member", member.ID, "success")
		return tx.Create(audit).Error
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return EnterpriseMemberDTO{}, err
	}
	if err := a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "enterprise_member", ID: dto.MemberID}); err != nil {
		return EnterpriseMemberDTO{}, err
	}
	return dto, nil
}

func (a *App) PreviewTransferOwner(ctx context.Context, in TransferOwnerInput) (TransferPreviewDTO, error) {
	if err := a.requireEnterpriseOwner(ctx, in.Auth); err != nil {
		return TransferPreviewDTO{}, err
	}
	var member businesscore.EnterpriseMember
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND enterprise_id = ? AND status = ?", in.TargetMemberID, in.Auth.EnterpriseID, StatusActive).First(&member).Error; err != nil {
		return TransferPreviewDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "target member not found")
	}
	if member.UserID == in.Auth.UserID {
		return TransferPreviewDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "owner is already target member")
	}
	return TransferPreviewDTO{
		PreviewToken: security.SignPreviewToken(a.secret, "transfer_owner", member.ID, 10*time.Minute),
		ImpactItems:  []string{"current owner becomes member", "target member becomes owner"},
		ExpiresAt:    a.now().Add(10 * time.Minute),
	}, nil
}

func (a *App) ConfirmTransferOwner(ctx context.Context, in TransferOwnerInput) (EnterpriseSummaryDTO, error) {
	if err := a.requireEnterpriseOwner(ctx, in.Auth); err != nil {
		return EnterpriseSummaryDTO{}, err
	}
	if !security.VerifyPreviewToken(a.secret, in.PreviewToken, "transfer_owner", in.TargetMemberID) {
		return EnterpriseSummaryDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "preview token is invalid or expired")
	}
	hash := in.Meta.RequestHash
	if hash == "" {
		hash = security.HashIdentifier(in.Auth.EnterpriseID + ":" + in.TargetMemberID + ":" + in.Reason)
	}
	idempotencyKey := businessIdempotencyKey(in.Meta, "enterprise.owner.transfer", hash)
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID:       "enterprise:" + in.Auth.EnterpriseID,
		SpaceID:        in.Auth.SpaceID,
		Scope:          "enterprise.owner.transfer",
		IdempotencyKey: idempotencyKey,
		RequestHash:    hash,
		ActorUserID:    in.Auth.UserID,
		EnterpriseID:   &in.Auth.EnterpriseID,
	})
	if err != nil {
		return EnterpriseSummaryDTO{}, err
	}
	var summary EnterpriseSummaryDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var target businesscore.EnterpriseMember
		if err := tx.Where("id = ? AND enterprise_id = ? AND status = ?", in.TargetMemberID, in.Auth.EnterpriseID, StatusActive).First(&target).Error; err != nil {
			return bizerrors.New(bizerrors.CodeResourceNotFound, "target member not found")
		}
		now := a.now()
		if err := tx.Model(&businesscore.EnterpriseMember{}).Where("enterprise_id = ? AND user_id = ?", in.Auth.EnterpriseID, in.Auth.UserID).Updates(map[string]any{"role": RoleMember, "updated_by": in.Auth.UserID, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.EnterpriseMember{}).Where("id = ?", target.ID).Updates(map[string]any{"role": RoleOwner, "updated_by": in.Auth.UserID, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.Enterprise{}).Where("id = ?", in.Auth.EnterpriseID).Updates(map[string]any{"owner_user_id": target.UserID, "updated_by": in.Auth.UserID, "updated_at": now}).Error; err != nil {
			return err
		}
		var ent businesscore.Enterprise
		if err := tx.Where("id = ?", in.Auth.EnterpriseID).First(&ent).Error; err != nil {
			return err
		}
		spaceID := ""
		if ent.DefaultSpaceID != nil {
			spaceID = *ent.DefaultSpaceID
		}
		summary = EnterpriseSummaryDTO{EnterpriseID: ent.ID, SpaceID: spaceID, Name: ent.Name, OwnerUserID: target.UserID, CurrentUserRole: RoleMember, Status: ent.Status, MemberCount: 0}
		audit := auditRecord(in.Meta.TraceID, "user", in.Auth.UserID, in.Auth.SpaceID, auditlog.ActionEnterpriseOwnerTransfer, "enterprise", ent.ID, "success")
		return tx.Create(audit).Error
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return EnterpriseSummaryDTO{}, err
	}
	if err := a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "enterprise", ID: summary.EnterpriseID}); err != nil {
		return EnterpriseSummaryDTO{}, err
	}
	return summary, nil
}

func (a *App) AuthenticateToken(ctx context.Context, rawToken string) (AuthContext, error) {
	rawToken = strings.TrimSpace(strings.TrimPrefix(rawToken, "Bearer "))
	if rawToken == "" {
		return AuthContext{}, bizerrors.New(bizerrors.CodeUnauthenticated, "authorization token is required")
	}
	tokenHash := security.HashOpaque(rawToken)
	var session businesscore.AuthSession
	if err := a.repo.DB().WithContext(ctx).
		Where("(session_token_hash = ? OR session_token_digest = ?) AND status = ? AND expires_at > ?", tokenHash, tokenHash, StatusActive, a.now()).
		First(&session).Error; err != nil {
		return AuthContext{}, bizerrors.New(bizerrors.CodeUnauthenticated, "session is invalid")
	}
	var user businesscore.User
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND status = ?", session.UserID, StatusActive).First(&user).Error; err != nil {
		return AuthContext{}, bizerrors.New(bizerrors.CodePermissionDenied, "user is not active")
	}
	role := ""
	if session.EnterpriseRole != nil {
		role = *session.EnterpriseRole
	} else if session.CurrentEnterpriseRole != nil {
		role = *session.CurrentEnterpriseRole
	}
	enterpriseID := ""
	if session.CurrentEnterpriseID != nil {
		enterpriseID = *session.CurrentEnterpriseID
	}
	if enterpriseID != "" {
		member, _, err := a.requireActiveEnterpriseMember(ctx, session.UserID, enterpriseID)
		if err != nil {
			return AuthContext{}, err
		}
		role = member.Role
	}
	return AuthContext{
		UserID: session.UserID, LoginIdentityType: session.LoginIdentityType, SpaceID: session.CurrentSpaceID,
		EnterpriseID: enterpriseID, EnterpriseRole: role, SessionID: session.ID, AccessToken: rawToken,
	}, nil
}

func (a *App) createUserSession(ctx context.Context, tx *gorm.DB, user businesscore.User, space businesscore.Space, enterprise *businesscore.Enterprise, role string, traceID string) (AuthSessionDTO, error) {
	now := a.now()
	accessToken, tokenHash := security.NewOpaqueToken("user")
	csrfToken, csrfHash := security.NewOpaqueToken("csrf")
	identity := IdentityPersonal
	var enterpriseID *string
	var enterpriseRole *string
	if enterprise != nil {
		identity = IdentityEnterprise
		enterpriseID = &enterprise.ID
		enterpriseRole = &role
	}
	session := businesscore.AuthSession{
		ID:                    security.RandomID("sess_"),
		UserID:                user.ID,
		LoginIdentityType:     identity,
		CurrentSpaceID:        space.ID,
		CurrentEnterpriseID:   enterpriseID,
		CurrentEnterpriseRole: enterpriseRole,
		EnterpriseRole:        enterpriseRole,
		SessionTokenDigest:    tokenHash,
		SessionTokenHash:      tokenHash,
		CSRFTokenHash:         &csrfHash,
		Status:                StatusActive,
		ExpiresAt:             now.Add(24 * time.Hour),
		CreatedBy:             optionalString(user.ID),
		UpdatedBy:             optionalString(user.ID),
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := tx.Create(&session).Error; err != nil {
		return AuthSessionDTO{}, err
	}
	return a.authSessionDTO(user, space, enterprise, role, accessToken, csrfToken, session.ExpiresAt), nil
}

func (a *App) authSessionDTO(user businesscore.User, space businesscore.Space, enterprise *businesscore.Enterprise, role, accessToken, csrfToken string, expiresAt time.Time) AuthSessionDTO {
	ctx, _ := a.spaceContextFromSpace(context.Background(), user.ID, space, optionalEnterpriseID(enterprise), role)
	dto := AuthSessionDTO{
		UserID: user.ID, CurrentSpaceID: space.ID, LoginIdentityType: IdentityPersonal,
		AccessToken: accessToken, CSRFToken: csrfToken, ExpiresAt: expiresAt,
		User:         UserProfileDTO{UserID: user.ID, DisplayName: user.DisplayName, Status: user.Status},
		CurrentSpace: ctx,
	}
	if enterprise != nil {
		dto.LoginIdentityType = IdentityEnterprise
		dto.EnterpriseID = enterprise.ID
		dto.EnterpriseRole = role
	}
	return dto
}

func (a *App) sessionDTOByID(ctx context.Context, resultID, token string) (AuthSessionDTO, error) {
	parts := strings.Split(resultID, ":")
	if len(parts) != 2 {
		return AuthSessionDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "idempotency result is not replayable")
	}
	var user businesscore.User
	if err := a.repo.DB().WithContext(ctx).Where("id = ?", parts[1]).First(&user).Error; err != nil {
		return AuthSessionDTO{}, err
	}
	var space businesscore.Space
	if err := a.repo.DB().WithContext(ctx).Where("id = ?", parts[0]).First(&space).Error; err != nil {
		return AuthSessionDTO{}, err
	}
	return a.authSessionDTO(user, space, nil, RoleOwner, token, "", a.now().Add(24*time.Hour)), nil
}

func (a *App) spaceContextFromSpace(ctx context.Context, userID string, space businesscore.Space, enterpriseID, enterpriseRole string) (SpaceContextDTO, error) {
	creditID := ""
	if space.CreditAccountID != nil {
		creditID = *space.CreditAccountID
	}
	scope := SpacePersonal
	keys := []string{"public", "user:" + userID}
	if space.SpaceType == SpaceEnterprise {
		scope = SpaceEnterprise
		keys = append(keys, "enterprise:"+enterpriseID)
	}
	return SpaceContextDTO{
		UserID: userID, SpaceID: space.ID, SpaceType: space.SpaceType, EnterpriseID: enterpriseID, EnterpriseRole: enterpriseRole,
		CreditAccountScope: scope, CreditAccountID: creditID, SkillScopeKeys: keys,
		PermissionSummary: map[string]string{
			"can_create_project":    "true",
			"can_manage_enterprise": boolString(enterpriseRole == RoleOwner),
			"can_use_agent":         "true",
		},
	}, nil
}

func (a *App) requireActiveEnterpriseMember(ctx context.Context, userID, enterpriseID string) (businesscore.EnterpriseMember, businesscore.Enterprise, error) {
	return a.requireActiveEnterpriseMemberTx(a.repo.DB().WithContext(ctx), userID, enterpriseID)
}

func (a *App) requireActiveEnterpriseMemberTx(tx *gorm.DB, userID, enterpriseID string) (businesscore.EnterpriseMember, businesscore.Enterprise, error) {
	if enterpriseID == "" {
		return businesscore.EnterpriseMember{}, businesscore.Enterprise{}, bizerrors.New(bizerrors.CodeInvalidArgument, "enterprise_id is required")
	}
	var ent businesscore.Enterprise
	if err := tx.Where("id = ? AND status = ?", enterpriseID, StatusActive).First(&ent).Error; err != nil {
		return businesscore.EnterpriseMember{}, businesscore.Enterprise{}, bizerrors.New(bizerrors.CodePermissionDenied, "enterprise is unavailable")
	}
	var member businesscore.EnterpriseMember
	if err := tx.Where("enterprise_id = ? AND user_id = ? AND status = ?", enterpriseID, userID, StatusActive).First(&member).Error; err != nil {
		return businesscore.EnterpriseMember{}, businesscore.Enterprise{}, bizerrors.New(bizerrors.CodePermissionDenied, "enterprise membership is unavailable")
	}
	return member, ent, nil
}

func (a *App) requireEnterpriseOwner(ctx context.Context, auth AuthContext) error {
	if auth.EnterpriseID == "" {
		return bizerrors.New(bizerrors.CodePermissionDenied, "enterprise identity is required")
	}
	member, _, err := a.requireActiveEnterpriseMember(ctx, auth.UserID, auth.EnterpriseID)
	if err != nil {
		return err
	}
	if member.Role != RoleOwner {
		return bizerrors.New(bizerrors.CodePermissionDenied, "enterprise owner permission is required")
	}
	return nil
}

func (a *App) writeLoginAudit(ctx context.Context, traceID, account, result, failure string) {
	accountHash := security.HashIdentifier(account)
	attempt := businesscore.AdminLoginAttempt{
		ID: security.RandomID("login_"), AdminAccount: account, AccountHash: &accountHash, Result: result,
		FailureReason: optionalString(failure), TraceID: traceID, CreatedAt: a.now(),
	}
	_ = a.repo.DB().WithContext(ctx).Create(&attempt).Error
}

func inviteDTO(invite businesscore.EnterpriseInvite, rawToken string) EnterpriseInviteDTO {
	masked := ""
	if invite.InviteeEmail != nil {
		masked = security.MaskEmail(*invite.InviteeEmail)
	}
	if masked == "" && invite.InviteePhone != nil {
		masked = "***" + last4(*invite.InviteePhone)
	}
	dto := EnterpriseInviteDTO{InviteID: invite.ID, EnterpriseID: invite.EnterpriseID, TargetAccountMasked: masked, Status: invite.Status, ExpiresAt: invite.ExpiresAt}
	if rawToken != "" {
		dto.TargetAccountMasked = dto.TargetAccountMasked + " invite_token=" + rawToken
	}
	return dto
}

func auditRecord(traceID, operatorType, operatorID, spaceID, action, resourceType, resourceID, result string) *auditlog.AuditRecord {
	return &auditlog.AuditRecord{
		AuditID: security.RandomID("audit_"), TraceID: traceID, OperatorType: operatorType, OperatorID: &operatorID,
		TenantID: "tenant:" + spaceID, SpaceID: &spaceID, BusinessAction: action, ResourceType: resourceType, ResourceID: &resourceID, Result: result,
		MetadataSummary: datatypes.JSON([]byte(`{}`)), CreatedAt: time.Now().UTC(),
	}
}

func optionalHash(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	hashed := security.HashIdentifier(value)
	return &hashed
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalEnterpriseID(ent *businesscore.Enterprise) string {
	if ent == nil {
		return ""
	}
	return ent.ID
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

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func authTenantID(actorKey string) string {
	return "auth:" + security.HashIdentifier(actorKey)[:59]
}

func businessIdempotencyKey(meta RequestMeta, scope, hash string) string {
	if key := strings.TrimSpace(meta.IdempotencyKey); key != "" {
		return key
	}
	if hash == "" {
		return ""
	}
	return scope + ":" + hash
}

func errorCode(err error) string {
	var businessErr *bizerrors.BusinessError
	if errors.As(err, &businessErr) {
		return string(businessErr.Code)
	}
	return string(bizerrors.CodeInternal)
}

func last4(value string) string {
	if len(value) <= 4 {
		return value
	}
	return value[len(value)-4:]
}
