package businesscore

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) DB() *gorm.DB {
	return r.db
}

type User struct {
	ID               string     `gorm:"column:id;primaryKey"`
	AccountNo        string     `gorm:"column:account_no"`
	Email            *string    `gorm:"column:email"`
	Phone            *string    `gorm:"column:phone"`
	EmailHash        *string    `gorm:"column:email_hash"`
	PhoneHash        *string    `gorm:"column:phone_hash"`
	PasswordHash     string     `gorm:"column:password_hash"`
	DisplayName      string     `gorm:"column:display_name"`
	AvatarAssetID    *string    `gorm:"column:avatar_asset_id"`
	Status           string     `gorm:"column:status"`
	DisabledReason   *string    `gorm:"column:disabled_reason"`
	DefaultSpaceID   *string    `gorm:"column:default_space_id"`
	RegisteredSource string     `gorm:"column:registered_source"`
	LastLoginAt      *time.Time `gorm:"column:last_login_at"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
}

func (User) TableName() string { return "business_users" }

type AuthSession struct {
	ID                    string     `gorm:"column:id;primaryKey"`
	UserID                string     `gorm:"column:user_id"`
	LoginIdentityType     string     `gorm:"column:login_identity_type"`
	CurrentSpaceID        string     `gorm:"column:current_space_id"`
	CurrentEnterpriseID   *string    `gorm:"column:current_enterprise_id"`
	CurrentEnterpriseRole *string    `gorm:"column:current_enterprise_role"`
	EnterpriseRole        *string    `gorm:"column:enterprise_role"`
	SessionTokenDigest    string     `gorm:"column:session_token_digest"`
	SessionTokenHash      string     `gorm:"column:session_token_hash"`
	RefreshTokenDigest    *string    `gorm:"column:refresh_token_digest"`
	CSRFTokenHash         *string    `gorm:"column:csrf_token_hash"`
	Status                string     `gorm:"column:status"`
	ClientIPDigest        *string    `gorm:"column:client_ip_digest"`
	UserAgentDigest       *string    `gorm:"column:user_agent_digest"`
	ExpiresAt             time.Time  `gorm:"column:expires_at"`
	LastSeenAt            *time.Time `gorm:"column:last_seen_at"`
	CreatedAt             time.Time  `gorm:"column:created_at"`
	UpdatedAt             time.Time  `gorm:"column:updated_at"`
}

func (AuthSession) TableName() string { return "auth_sessions" }

type Space struct {
	ID              string    `gorm:"column:id;primaryKey"`
	OwnerUserID     string    `gorm:"column:owner_user_id"`
	SpaceType       string    `gorm:"column:space_type"`
	EnterpriseID    *string   `gorm:"column:enterprise_id"`
	DisplayName     string    `gorm:"column:display_name"`
	Status          string    `gorm:"column:status"`
	CreditAccountID *string   `gorm:"column:credit_account_id"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

func (Space) TableName() string { return "business_spaces" }

type Enterprise struct {
	ID              string    `gorm:"column:id;primaryKey"`
	EnterpriseNo    string    `gorm:"column:enterprise_no"`
	Name            string    `gorm:"column:name"`
	OwnerUserID     string    `gorm:"column:owner_user_id"`
	DefaultSpaceID  *string   `gorm:"column:default_space_id"`
	CreditAccountID *string   `gorm:"column:credit_account_id"`
	Status          string    `gorm:"column:status"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

func (Enterprise) TableName() string { return "enterprises" }

type EnterpriseMember struct {
	ID              string     `gorm:"column:id;primaryKey"`
	EnterpriseID    string     `gorm:"column:enterprise_id"`
	UserID          string     `gorm:"column:user_id"`
	Role            string     `gorm:"column:role"`
	Status          string     `gorm:"column:status"`
	JoinedAt        *time.Time `gorm:"column:joined_at"`
	InvitedByUserID *string    `gorm:"column:invited_by_user_id"`
	RemovedAt       *time.Time `gorm:"column:removed_at"`
	RemovedBy       *string    `gorm:"column:removed_by"`
	RemoveReason    *string    `gorm:"column:remove_reason"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
}

func (EnterpriseMember) TableName() string { return "enterprise_members" }

type EnterpriseInvite struct {
	ID               string     `gorm:"column:id;primaryKey"`
	EnterpriseID     string     `gorm:"column:enterprise_id"`
	InviteeEmail     *string    `gorm:"column:invitee_email"`
	InviteePhone     *string    `gorm:"column:invitee_phone"`
	TargetEmailHash  *string    `gorm:"column:target_email_hash"`
	TargetPhoneHash  *string    `gorm:"column:target_phone_hash"`
	Role             string     `gorm:"column:role"`
	InviteCodeDigest string     `gorm:"column:invite_code_digest"`
	InviteTokenHash  *string    `gorm:"column:invite_token_hash"`
	Status           string     `gorm:"column:status"`
	InvitedByUserID  string     `gorm:"column:invited_by_user_id"`
	AcceptedByUserID *string    `gorm:"column:accepted_by_user_id"`
	ExpiresAt        time.Time  `gorm:"column:expires_at"`
	AcceptedAt       *time.Time `gorm:"column:accepted_at"`
	CancelledAt      *time.Time `gorm:"column:cancelled_at"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
}

func (EnterpriseInvite) TableName() string { return "enterprise_invites" }

type CreditAccount struct {
	ID                string    `gorm:"column:id;primaryKey"`
	AccountType       string    `gorm:"column:account_type"`
	OwnerUserID       *string   `gorm:"column:owner_user_id"`
	EnterpriseID      *string   `gorm:"column:enterprise_id"`
	Status            string    `gorm:"column:status"`
	AvailablePoints   int64     `gorm:"column:available_points"`
	FrozenPoints      int64     `gorm:"column:frozen_points"`
	ExpiresSoonPoints int64     `gorm:"column:expires_soon_points"`
	CreatedAt         time.Time `gorm:"column:created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
}

func (CreditAccount) TableName() string { return "credit_accounts" }

type PlatformAdmin struct {
	ID                 string     `gorm:"column:id;primaryKey"`
	AdminAccount       string     `gorm:"column:admin_account"`
	PasswordHash       string     `gorm:"column:password_hash"`
	DisplayName        string     `gorm:"column:display_name"`
	Role               string     `gorm:"column:role"`
	Status             string     `gorm:"column:status"`
	MustRotatePassword bool       `gorm:"column:must_rotate_password"`
	CreatedBy          *string    `gorm:"column:created_by"`
	DisabledBy         *string    `gorm:"column:disabled_by"`
	DisabledReason     *string    `gorm:"column:disabled_reason"`
	LastLoginAt        *time.Time `gorm:"column:last_login_at"`
	PasswordRotatedAt  *time.Time `gorm:"column:password_rotated_at"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
}

func (PlatformAdmin) TableName() string { return "platform_admins" }

type PlatformAdminBootstrap struct {
	ID                  string     `gorm:"column:id;primaryKey"`
	AdminID             string     `gorm:"column:admin_id"`
	BootstrapAccount    string     `gorm:"column:bootstrap_account"`
	InitializedBy       string     `gorm:"column:initialized_by"`
	CredentialSecretRef *string    `gorm:"column:credential_secret_ref"`
	Status              string     `gorm:"column:status"`
	InitializedAt       time.Time  `gorm:"column:initialized_at"`
	RotatedAt           *time.Time `gorm:"column:rotated_at"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (PlatformAdminBootstrap) TableName() string { return "platform_admin_bootstraps" }

type PlatformAdminSession struct {
	ID                 string     `gorm:"column:id;primaryKey"`
	AdminID            string     `gorm:"column:admin_id"`
	SessionTokenDigest string     `gorm:"column:session_token_digest"`
	CSRFTokenHash      *string    `gorm:"column:csrf_token_hash"`
	Status             string     `gorm:"column:status"`
	ClientIPDigest     *string    `gorm:"column:client_ip_digest"`
	UserAgentDigest    *string    `gorm:"column:user_agent_digest"`
	ExpiresAt          time.Time  `gorm:"column:expires_at"`
	LastSeenAt         *time.Time `gorm:"column:last_seen_at"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
}

func (PlatformAdminSession) TableName() string { return "platform_admin_sessions" }

type AdminLoginAttempt struct {
	ID              string    `gorm:"column:id;primaryKey"`
	AdminAccount    string    `gorm:"column:admin_account"`
	AccountHash     *string   `gorm:"column:account_hash"`
	Result          string    `gorm:"column:result"`
	FailureReason   *string   `gorm:"column:failure_reason"`
	ClientIPDigest  *string   `gorm:"column:client_ip_digest"`
	UserAgentDigest *string   `gorm:"column:user_agent_digest"`
	TraceID         string    `gorm:"column:trace_id"`
	CreatedAt       time.Time `gorm:"column:created_at"`
}

func (AdminLoginAttempt) TableName() string { return "admin_login_attempts" }

type Project struct {
	ID              string     `gorm:"column:id;primaryKey"`
	ProjectNo       string     `gorm:"column:project_no"`
	OwnerUserID     string     `gorm:"column:owner_user_id"`
	SpaceID         string     `gorm:"column:space_id"`
	EnterpriseID    *string    `gorm:"column:enterprise_id"`
	Title           string     `gorm:"column:title"`
	Description     *string    `gorm:"column:description"`
	Status          string     `gorm:"column:status"`
	CreativeStatus  string     `gorm:"column:creative_status"`
	CreativeAllowed bool       `gorm:"column:creative_allowed"`
	CoverAssetID    *string    `gorm:"column:cover_asset_id"`
	LastOpenedAt    *time.Time `gorm:"column:last_opened_at"`
	LastActivityAt  time.Time  `gorm:"column:last_activity_at"`
	ArchiveReason   *string    `gorm:"column:archive_reason"`
	ArchivedBy      *string    `gorm:"column:archived_by"`
	ArchivedAt      *time.Time `gorm:"column:archived_at"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
}

func (Project) TableName() string { return "projects" }

type ProjectAsset struct {
	ID               string    `gorm:"column:id;primaryKey"`
	ProjectID        string    `gorm:"column:project_id"`
	AssetID          string    `gorm:"column:asset_id"`
	AssetRole        string    `gorm:"column:asset_role"`
	AttachedByUserID string    `gorm:"column:attached_by_user_id"`
	AttachedBy       *string   `gorm:"column:attached_by"`
	Status           string    `gorm:"column:status"`
	SourceSessionID  *string   `gorm:"column:source_session_id"`
	SourceRunID      *string   `gorm:"column:source_run_id"`
	SourceArtifactID *string   `gorm:"column:source_artifact_id"`
	SourceType       string    `gorm:"column:source_type"`
	DisplayOrder     int       `gorm:"column:display_order"`
	CreatedAt        time.Time `gorm:"column:created_at"`
}

func (ProjectAsset) TableName() string { return "project_assets" }

type ProjectWork struct {
	ID                  string         `gorm:"column:id;primaryKey"`
	ProjectID           string         `gorm:"column:project_id"`
	WorkID              string         `gorm:"column:work_id"`
	Status              string         `gorm:"column:status"`
	CreatedFromAssetIDs datatypes.JSON `gorm:"column:created_from_asset_ids;type:jsonb"`
	CreatedBy           *string        `gorm:"column:created_by"`
	CreatedAt           time.Time      `gorm:"column:created_at"`
}

func (ProjectWork) TableName() string { return "project_works" }
