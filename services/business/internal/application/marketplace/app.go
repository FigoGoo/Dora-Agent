package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr4"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"gorm.io/gorm"
)

type AuthContext = accountspace.AuthContext
type RequestMeta = accountspace.RequestMeta

type App struct {
	repo *businesscore.Repository
	now  func() time.Time
}

func New(repo *businesscore.Repository) *App {
	return &App{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

type MarketplaceListingDTO struct {
	ListingID           string     `json:"listing_id"`
	SkillID             string     `json:"skill_id"`
	SkillVersionID      string     `json:"skill_version_id"`
	SkillVersion        string     `json:"skill_version"`
	SkillName           string     `json:"skill_name"`
	SkillDescription    string     `json:"skill_description"`
	CreatorUserID       string     `json:"creator_user_id"`
	Status              string     `json:"status"`
	PricingModel        string     `json:"pricing_model"`
	UsageCredits        int        `json:"usage_credits"`
	ValueDeliveredStage string     `json:"value_delivered_stage"`
	PricingPolicyDigest string     `json:"pricing_policy_digest"`
	PublishedBy         string     `json:"published_by"`
	ListedAt            *time.Time `json:"listed_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type SkillInstallationDTO struct {
	InstallationID   string    `json:"installation_id"`
	AccountID        string    `json:"account_id"`
	AccountScope     string    `json:"account_scope"`
	ListingID        string    `json:"listing_id"`
	SkillID          string    `json:"skill_id"`
	SkillName        string    `json:"skill_name,omitempty"`
	InstalledVersion string    `json:"installed_version"`
	VersionStrategy  string    `json:"version_strategy"`
	Status           string    `json:"status"`
	UpgradeStatus    string    `json:"upgrade_status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type SkillUsageRecordDTO struct {
	UsageID             string     `json:"usage_id"`
	RunID               string     `json:"run_id"`
	ListingID           string     `json:"listing_id"`
	SkillID             string     `json:"skill_id"`
	SkillVersion        string     `json:"skill_version"`
	PricingPolicyDigest string     `json:"pricing_policy_digest"`
	SkillUsageDigest    string     `json:"skill_usage_digest"`
	UsageStatus         string     `json:"usage_status"`
	ChargeStatus        string     `json:"charge_status"`
	RefundStatus        string     `json:"refund_status"`
	SettlementStatus    string     `json:"settlement_status"`
	EstimatedCredits    int        `json:"estimated_credits"`
	CreditHoldID        *string    `json:"credit_hold_id,omitempty"`
	ValueDeliveredAt    *time.Time `json:"value_delivered_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type SkillSettlementDTO struct {
	SettlementID       string    `json:"settlement_id"`
	UsageID            string    `json:"usage_id"`
	CreatorUserID      string    `json:"creator_user_id"`
	Status             string    `json:"status"`
	GrossCredits       int       `json:"gross_credits"`
	PlatformFeeCredits int       `json:"platform_fee_credits"`
	CreatorCredits     int       `json:"creator_credits"`
	HoldUntil          time.Time `json:"hold_until"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ListMarketplaceSkillsInput struct {
	Auth   AuthContext
	Query  string
	Limit  int
	Cursor string
}

type ListMarketplaceSkillsOutput struct {
	Items      []MarketplaceListingDTO `json:"items"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

type GetMarketplaceSkillOutput struct {
	Listing MarketplaceListingDTO `json:"listing"`
}

type InstallSkillInput struct {
	Auth         AuthContext
	Meta         RequestMeta
	ListingID    string
	TargetScope  string
	EnterpriseID string
}

type InstallSkillOutput struct {
	Installation     SkillInstallationDTO `json:"installation"`
	IdempotentReplay bool                 `json:"idempotent_replay"`
}

type ListInstalledSkillsInput struct {
	Auth         AuthContext
	AccountScope string
	Limit        int
	Offset       int
}

type ListInstalledSkillsOutput struct {
	Items []SkillInstallationDTO `json:"items"`
}

type UpgradeSkillInstallationInput struct {
	Auth           AuthContext
	Meta           RequestMeta
	InstallationID string
	TargetVersion  string
	Confirmed      bool
}

type UpgradeSkillInstallationOutput struct {
	Installation SkillInstallationDTO `json:"installation"`
}

type EstimateSkillUsageCreditsInput struct {
	Auth                AuthContext
	Meta                RequestMeta
	RunID               string
	ListingID           string
	PricingPolicyDigest string
}

type EstimateSkillUsageCreditsOutput struct {
	EstimatedCredits    int       `json:"estimated_credits"`
	PricingPolicyDigest string    `json:"pricing_policy_digest"`
	SkillUsageDigest    string    `json:"skill_usage_digest"`
	ExpiresAt           time.Time `json:"expires_at"`
}

type CreateSkillUsageRecordInput struct {
	Auth                AuthContext
	Meta                RequestMeta
	RunID               string
	ListingID           string
	SkillID             string
	SkillVersion        string
	PricingPolicyDigest string
	SkillUsageDigest    string
	EstimatedCredits    int
}

type CreateSkillUsageRecordOutput struct {
	Usage            SkillUsageRecordDTO `json:"usage"`
	IdempotentReplay bool                `json:"idempotent_replay"`
}

type FreezeSkillUsageCreditsInput struct {
	Auth             AuthContext
	UsageID          string
	SkillUsageDigest string
	CreditHoldID     string
}

type FreezeSkillUsageCreditsOutput struct {
	Usage SkillUsageRecordDTO `json:"usage"`
}

type ReleaseSkillUsageFreezeInput struct {
	Auth          AuthContext
	UsageID       string
	ReleaseReason string
}

type ReleaseSkillUsageFreezeOutput struct {
	Usage SkillUsageRecordDTO `json:"usage"`
}

type CommitSkillUsageAndSettleInput struct {
	Auth         AuthContext
	UsageID      string
	CreditHoldID string
}

type CommitSkillUsageAndSettleOutput struct {
	Usage      SkillUsageRecordDTO `json:"usage"`
	Settlement SkillSettlementDTO  `json:"settlement"`
}

type listingRow struct {
	ListingID           string     `gorm:"column:listing_id"`
	SkillID             string     `gorm:"column:skill_id"`
	SkillVersionID      string     `gorm:"column:skill_version_id"`
	SkillVersion        string     `gorm:"column:skill_version"`
	SkillName           string     `gorm:"column:skill_name"`
	SkillDescription    string     `gorm:"column:skill_description"`
	CreatorUserID       string     `gorm:"column:creator_user_id"`
	Status              string     `gorm:"column:status"`
	PricingModel        string     `gorm:"column:pricing_model"`
	UsageCredits        int        `gorm:"column:usage_credits"`
	ValueDeliveredStage string     `gorm:"column:value_delivered_stage"`
	PricingPolicyDigest string     `gorm:"column:pricing_policy_digest"`
	PublishedBy         string     `gorm:"column:published_by"`
	ListedAt            *time.Time `gorm:"column:listed_at"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

type installationRow struct {
	InstallationID   string    `gorm:"column:installation_id"`
	AccountID        string    `gorm:"column:account_id"`
	AccountScope     string    `gorm:"column:account_scope"`
	ListingID        string    `gorm:"column:listing_id"`
	SkillID          string    `gorm:"column:skill_id"`
	SkillName        string    `gorm:"column:skill_name"`
	InstalledVersion string    `gorm:"column:installed_version"`
	VersionStrategy  string    `gorm:"column:version_strategy"`
	Status           string    `gorm:"column:status"`
	UpgradeStatus    string    `gorm:"column:upgrade_status"`
	CreatedAt        time.Time `gorm:"column:created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at"`
}

func (a *App) ListMarketplaceSkills(ctx context.Context, in ListMarketplaceSkillsInput) (ListMarketplaceSkillsOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return ListMarketplaceSkillsOutput{}, err
	}
	limit := normalizeLimit(in.Limit)
	offset := cursorOffset(in.Cursor)
	query := a.listingQuery(ctx).Where("ml.status = ?", "listed")
	if keyword := strings.TrimSpace(in.Query); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("sp.name ILIKE ? OR sp.description ILIKE ?", like, like)
	}
	var rows []listingRow
	if err := query.Order("ml.updated_at DESC, ml.listing_id ASC").Limit(limit + 1).Offset(offset).Scan(&rows).Error; err != nil {
		return ListMarketplaceSkillsOutput{}, err
	}
	nextCursor := ""
	if len(rows) > limit {
		rows = rows[:limit]
		nextCursor = strconv.Itoa(offset + limit)
	}
	items := make([]MarketplaceListingDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, listingDTO(row))
	}
	return ListMarketplaceSkillsOutput{Items: items, NextCursor: nextCursor}, nil
}

func (a *App) GetMarketplaceSkill(ctx context.Context, auth AuthContext, listingID string) (GetMarketplaceSkillOutput, error) {
	if err := requireAuth(auth); err != nil {
		return GetMarketplaceSkillOutput{}, err
	}
	row, err := a.getListingRow(ctx, listingID, true)
	if err != nil {
		return GetMarketplaceSkillOutput{}, err
	}
	return GetMarketplaceSkillOutput{Listing: listingDTO(row)}, nil
}

func (a *App) InstallSkill(ctx context.Context, in InstallSkillInput) (InstallSkillOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return InstallSkillOutput{}, err
	}
	scope := strings.TrimSpace(in.TargetScope)
	if scope == "" {
		scope = pr4.AccountScopePersonal
	}
	accountID, err := accountIDForScope(in.Auth, scope, in.EnterpriseID)
	if err != nil {
		return InstallSkillOutput{}, err
	}
	if strings.TrimSpace(in.Meta.IdempotencyKey) == "" {
		return InstallSkillOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "idempotency_key is required")
	}
	row, err := a.getListingRow(ctx, in.ListingID, true)
	if err != nil {
		return InstallSkillOutput{}, err
	}
	existing, found, err := a.findInstallation(ctx, accountID, scope, row.SkillID)
	if err != nil {
		return InstallSkillOutput{}, err
	}
	if found {
		return InstallSkillOutput{Installation: installationDTO(existing), IdempotentReplay: true}, nil
	}

	now := a.now().UTC()
	versionStrategy := pr4.VersionStrategyLatestPublished
	if scope == pr4.AccountScopeEnterprise {
		versionStrategy = pr4.VersionStrategyPinned
	}
	installation := pr4.SkillInstallation{
		SchemaVersion:    pr4.SchemaVersionSkillInstallation,
		InstallationID:   prefixedStableID("sinst_", accountID, scope, row.ListingID, row.SkillID),
		AccountID:        accountID,
		AccountScope:     scope,
		ListingID:        row.ListingID,
		SkillID:          row.SkillID,
		InstalledVersion: row.SkillVersion,
		VersionStrategy:  versionStrategy,
		Status:           "installed",
		UpgradeStatus:    "none",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	request := pr4.InstallSkillRequest{AccountID: accountID, AccountScope: scope, ListingID: row.ListingID, IdempotencyKey: in.Meta.IdempotencyKey}
	var saved pr4.SkillInstallation
	if scope == pr4.AccountScopePersonal {
		saved, err = a.repo.InstallPersonalLatestSkillV1(ctx, request, installation)
	} else {
		if err = a.repo.EnsureMarketplaceListingInstallableV1(ctx, row.ListingID); err == nil {
			saved, err = a.repo.SaveSkillInstallationSnapshotV1(ctx, installation, in.Meta.IdempotencyKey)
		}
	}
	if err != nil {
		return InstallSkillOutput{}, mapStoreError(err)
	}
	return InstallSkillOutput{Installation: installationDTOFromContract(saved)}, nil
}

func (a *App) ListInstalledSkills(ctx context.Context, in ListInstalledSkillsInput) (ListInstalledSkillsOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return ListInstalledSkillsOutput{}, err
	}
	scope := strings.TrimSpace(in.AccountScope)
	if scope == "" {
		scope = currentAccountScope(in.Auth)
	}
	accountID, err := accountIDForScope(in.Auth, scope, "")
	if err != nil {
		return ListInstalledSkillsOutput{}, err
	}
	limit := normalizeLimit(in.Limit)
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}
	var rows []installationRow
	err = a.installationQuery(ctx).
		Where("si.account_id = ? AND si.account_scope = ?", accountID, scope).
		Order("si.updated_at DESC, si.installation_id ASC").
		Limit(limit).
		Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return ListInstalledSkillsOutput{}, err
	}
	items := make([]SkillInstallationDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, installationDTO(row))
	}
	return ListInstalledSkillsOutput{Items: items}, nil
}

func (a *App) UpgradeSkillInstallation(ctx context.Context, in UpgradeSkillInstallationInput) (UpgradeSkillInstallationOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return UpgradeSkillInstallationOutput{}, err
	}
	if strings.TrimSpace(in.Meta.IdempotencyKey) == "" {
		return UpgradeSkillInstallationOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "idempotency_key is required")
	}
	if !in.Confirmed {
		return UpgradeSkillInstallationOutput{}, bizerrors.New(bizerrors.CodeStateConflict, "enterprise upgrade requires confirmation")
	}
	initial, err := a.getInstallationContract(ctx, in.InstallationID)
	if err != nil {
		return UpgradeSkillInstallationOutput{}, err
	}
	accountID, err := accountIDForScope(in.Auth, initial.AccountScope, "")
	if err != nil {
		return UpgradeSkillInstallationOutput{}, err
	}
	if initial.AccountID != accountID {
		return UpgradeSkillInstallationOutput{}, bizerrors.New(bizerrors.CodePermissionDenied, "installation is not visible to current account")
	}
	now := a.now().UTC()
	after := initial
	after.InstalledVersion = strings.TrimSpace(in.TargetVersion)
	after.UpgradeStatus = "confirmed"
	after.UpdatedAt = now
	request := pr4.UpgradeSkillInstallationRequest{
		InstallationID: initial.InstallationID,
		TargetVersion:  after.InstalledVersion,
		Confirmed:      in.Confirmed,
		IdempotencyKey: in.Meta.IdempotencyKey,
	}
	rule := pr4.HistoricalRunRule{
		RunID:                      prefixedStableID("run_upgrade_", initial.InstallationID, initial.InstalledVersion),
		MustResumeWithSkillVersion: initial.InstalledVersion,
	}
	upgraded, err := a.repo.UpgradeSkillInstallationV1(ctx, request, after, rule)
	if err != nil {
		return UpgradeSkillInstallationOutput{}, mapStoreError(err)
	}
	return UpgradeSkillInstallationOutput{Installation: installationDTOFromContract(upgraded)}, nil
}

func (a *App) EstimateSkillUsageCredits(ctx context.Context, in EstimateSkillUsageCreditsInput) (EstimateSkillUsageCreditsOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return EstimateSkillUsageCreditsOutput{}, err
	}
	row, err := a.getListingRow(ctx, in.ListingID, true)
	if err != nil {
		return EstimateSkillUsageCreditsOutput{}, err
	}
	if strings.TrimSpace(in.RunID) == "" {
		return EstimateSkillUsageCreditsOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "run_id is required")
	}
	if in.PricingPolicyDigest != "" && in.PricingPolicyDigest != row.PricingPolicyDigest {
		return EstimateSkillUsageCreditsOutput{}, bizerrors.New(bizerrors.CodeStateConflict, "pricing_policy_digest does not match listing")
	}
	digest, err := skillUsageDigest(in.RunID, row)
	if err != nil {
		return EstimateSkillUsageCreditsOutput{}, err
	}
	return EstimateSkillUsageCreditsOutput{
		EstimatedCredits:    row.UsageCredits,
		PricingPolicyDigest: row.PricingPolicyDigest,
		SkillUsageDigest:    digest,
		ExpiresAt:           a.now().UTC().Add(15 * time.Minute),
	}, nil
}

func (a *App) CreateSkillUsageRecord(ctx context.Context, in CreateSkillUsageRecordInput) (CreateSkillUsageRecordOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return CreateSkillUsageRecordOutput{}, err
	}
	row, err := a.getListingRow(ctx, in.ListingID, true)
	if err != nil {
		return CreateSkillUsageRecordOutput{}, err
	}
	if strings.TrimSpace(in.RunID) == "" {
		return CreateSkillUsageRecordOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "run_id is required")
	}
	if err := validateUsageEstimateInput(in, row); err != nil {
		return CreateSkillUsageRecordOutput{}, err
	}
	expectedDigest, err := skillUsageDigest(in.RunID, row)
	if err != nil {
		return CreateSkillUsageRecordOutput{}, err
	}
	digest := strings.TrimSpace(in.SkillUsageDigest)
	if digest == "" {
		digest = expectedDigest
	} else if digest != expectedDigest {
		return CreateSkillUsageRecordOutput{}, bizerrors.New(bizerrors.CodeStateConflict, "skill_usage_digest does not match listing estimate")
	}
	credits := in.EstimatedCredits
	if credits == 0 {
		credits = row.UsageCredits
	}
	now := a.now().UTC()
	usage := pr4.SkillUsageRecord{
		SchemaVersion:       pr4.SchemaVersionSkillUsageRecord,
		UsageID:             prefixedStableID("susage_", in.RunID, row.ListingID, row.SkillVersion, row.PricingPolicyDigest),
		RunID:               in.RunID,
		ListingID:           row.ListingID,
		SkillID:             row.SkillID,
		SkillVersion:        row.SkillVersion,
		PricingPolicyDigest: row.PricingPolicyDigest,
		SkillUsageDigest:    digest,
		UsageStatus:         "confirmation_required",
		ChargeStatus:        "not_frozen",
		RefundStatus:        "none",
		SettlementStatus:    "pending_hold",
		EstimatedCredits:    credits,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	idempotencyKey := strings.TrimSpace(in.Meta.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = strings.Join([]string{in.RunID, row.ListingID, row.SkillVersion, row.PricingPolicyDigest}, ":")
	}
	created, err := a.repo.CreateSkillUsageRecordV1(ctx, usage, idempotencyKey)
	if err != nil {
		return CreateSkillUsageRecordOutput{}, mapStoreError(err)
	}
	return CreateSkillUsageRecordOutput{Usage: usageDTO(created)}, nil
}

func (a *App) FreezeSkillUsageCredits(ctx context.Context, in FreezeSkillUsageCreditsInput) (FreezeSkillUsageCreditsOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return FreezeSkillUsageCreditsOutput{}, err
	}
	creditHoldID := strings.TrimSpace(in.CreditHoldID)
	if creditHoldID == "" {
		creditHoldID = prefixedStableID("chold_", in.UsageID, in.SkillUsageDigest)
	}
	frozen, err := a.repo.FreezeSkillUsageRecordV1(ctx, in.UsageID, in.SkillUsageDigest, creditHoldID, a.now().UTC())
	if err != nil {
		return FreezeSkillUsageCreditsOutput{}, mapStoreError(err)
	}
	return FreezeSkillUsageCreditsOutput{Usage: usageDTO(frozen)}, nil
}

func (a *App) ReleaseSkillUsageFreeze(ctx context.Context, in ReleaseSkillUsageFreezeInput) (ReleaseSkillUsageFreezeOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return ReleaseSkillUsageFreezeOutput{}, err
	}
	released, err := a.repo.ReleaseSkillUsageFreezeV1(ctx, in.UsageID, in.ReleaseReason, a.now().UTC())
	if err != nil {
		return ReleaseSkillUsageFreezeOutput{}, mapStoreError(err)
	}
	return ReleaseSkillUsageFreezeOutput{Usage: usageDTO(released)}, nil
}

func (a *App) CommitSkillUsageAndSettle(ctx context.Context, in CommitSkillUsageAndSettleInput) (CommitSkillUsageAndSettleOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return CommitSkillUsageAndSettleOutput{}, err
	}
	current, err := a.getUsageContract(ctx, in.UsageID)
	if err != nil {
		return CommitSkillUsageAndSettleOutput{}, err
	}
	creatorUserID, err := a.getSkillCreatorUserID(ctx, current.SkillID)
	if err != nil {
		return CommitSkillUsageAndSettleOutput{}, err
	}
	now := a.now().UTC()
	creditHoldID := strings.TrimSpace(in.CreditHoldID)
	if current.CreditHoldID != nil && *current.CreditHoldID != "" {
		creditHoldID = *current.CreditHoldID
	}
	if creditHoldID == "" {
		creditHoldID = prefixedStableID("chold_", current.UsageID, current.SkillUsageDigest)
	}
	afterCharge := current
	afterCharge.UsageStatus = "value_delivered"
	afterCharge.ChargeStatus = "charged"
	afterCharge.RefundStatus = "none"
	afterCharge.SettlementStatus = "pending_hold"
	afterCharge.CreditHoldID = &creditHoldID
	afterCharge.ValueDeliveredAt = &now
	afterCharge.UpdatedAt = now
	platformFee := current.EstimatedCredits / 5
	settlement := pr4.SkillSettlement{
		SchemaVersion:      pr4.SchemaVersionSkillSettlement,
		SettlementID:       prefixedStableID("settle_", current.UsageID),
		UsageID:            current.UsageID,
		CreatorUserID:      creatorUserID,
		Status:             "pending_hold",
		GrossCredits:       current.EstimatedCredits,
		PlatformFeeCredits: platformFee,
		CreatorCredits:     current.EstimatedCredits - platformFee,
		HoldUntil:          now.Add(7 * 24 * time.Hour),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	committed, settled, err := a.repo.CommitSkillUsageAndSettleV1(ctx, afterCharge, settlement)
	if err != nil {
		return CommitSkillUsageAndSettleOutput{}, mapStoreError(err)
	}
	return CommitSkillUsageAndSettleOutput{Usage: usageDTO(committed), Settlement: settlementDTO(settled)}, nil
}

func (a *App) listingQuery(ctx context.Context) *gorm.DB {
	return a.repo.DB().WithContext(ctx).Table("marketplace_listings AS ml").
		Select(`ml.listing_id,
			ml.skill_id,
			ml.skill_version_id,
			sv.version AS skill_version,
			sp.name AS skill_name,
			sp.description AS skill_description,
			sp.creator_user_id,
			ml.status,
			pp.pricing_model,
			pp.usage_credits,
			pp.value_delivered_stage,
			ml.pricing_policy_digest,
			ml.published_by,
			ml.listed_at,
			ml.created_at,
			ml.updated_at`).
		Joins("JOIN skill_packages AS sp ON sp.skill_id = ml.skill_id").
		Joins("JOIN skill_versions AS sv ON sv.skill_version_id = ml.skill_version_id").
		Joins("JOIN skill_pricing_policies AS pp ON pp.skill_id = ml.skill_id AND pp.skill_version = sv.version AND pp.pricing_policy_digest = ml.pricing_policy_digest")
}

func (a *App) installationQuery(ctx context.Context) *gorm.DB {
	return a.repo.DB().WithContext(ctx).Table("skill_installations AS si").
		Select(`si.installation_id,
			si.account_id,
			si.account_scope,
			si.listing_id,
			si.skill_id,
			sp.name AS skill_name,
			si.installed_version,
			si.version_strategy,
			si.status,
			si.upgrade_status,
			si.created_at,
			si.updated_at`).
		Joins("LEFT JOIN skill_packages AS sp ON sp.skill_id = si.skill_id")
}

func (a *App) getListingRow(ctx context.Context, listingID string, requireListed bool) (listingRow, error) {
	if strings.TrimSpace(listingID) == "" {
		return listingRow{}, bizerrors.New(bizerrors.CodeInvalidArgument, "listing_id is required")
	}
	query := a.listingQuery(ctx).Where("ml.listing_id = ?", listingID)
	if requireListed {
		query = query.Where("ml.status = ?", "listed")
	}
	var row listingRow
	if err := query.First(&row).Error; err != nil {
		return listingRow{}, mapStoreError(err)
	}
	return row, nil
}

func (a *App) findInstallation(ctx context.Context, accountID string, scope string, skillID string) (installationRow, bool, error) {
	var row installationRow
	err := a.installationQuery(ctx).
		Where("si.account_id = ? AND si.account_scope = ? AND si.skill_id = ?", accountID, scope, skillID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return installationRow{}, false, nil
	}
	if err != nil {
		return installationRow{}, false, err
	}
	return row, true, nil
}

func (a *App) getInstallationContract(ctx context.Context, installationID string) (pr4.SkillInstallation, error) {
	if strings.TrimSpace(installationID) == "" {
		return pr4.SkillInstallation{}, bizerrors.New(bizerrors.CodeInvalidArgument, "installation_id is required")
	}
	var record businesscore.PR4SkillInstallationRecord
	if err := a.repo.DB().WithContext(ctx).Where("installation_id = ?", installationID).First(&record).Error; err != nil {
		return pr4.SkillInstallation{}, mapStoreError(err)
	}
	installation := pr4.SkillInstallation{
		SchemaVersion:    pr4.SchemaVersionSkillInstallation,
		InstallationID:   record.InstallationID,
		AccountID:        record.AccountID,
		AccountScope:     record.AccountScope,
		ListingID:        record.ListingID,
		SkillID:          record.SkillID,
		InstalledVersion: record.InstalledVersion,
		VersionStrategy:  record.VersionStrategy,
		Status:           record.Status,
		UpgradeStatus:    record.UpgradeStatus,
		CreatedAt:        record.CreatedAt.UTC(),
		UpdatedAt:        record.UpdatedAt.UTC(),
	}
	if err := pr4.ValidateSkillInstallation(installation); err != nil {
		return pr4.SkillInstallation{}, err
	}
	return installation, nil
}

func (a *App) getUsageContract(ctx context.Context, usageID string) (pr4.SkillUsageRecord, error) {
	if strings.TrimSpace(usageID) == "" {
		return pr4.SkillUsageRecord{}, bizerrors.New(bizerrors.CodeInvalidArgument, "usage_id is required")
	}
	var record businesscore.PR4SkillUsageRecord
	if err := a.repo.DB().WithContext(ctx).Where("usage_id = ?", usageID).First(&record).Error; err != nil {
		return pr4.SkillUsageRecord{}, mapStoreError(err)
	}
	usage := pr4.SkillUsageRecord{
		SchemaVersion:       pr4.SchemaVersionSkillUsageRecord,
		UsageID:             record.UsageID,
		RunID:               record.RunID,
		ListingID:           record.ListingID,
		SkillID:             record.SkillID,
		SkillVersion:        record.SkillVersion,
		PricingPolicyDigest: record.PricingPolicyDigest,
		SkillUsageDigest:    record.SkillUsageDigest,
		UsageStatus:         record.UsageStatus,
		ChargeStatus:        record.ChargeStatus,
		RefundStatus:        record.RefundStatus,
		SettlementStatus:    record.SettlementStatus,
		EstimatedCredits:    record.EstimatedCredits,
		CreditHoldID:        record.CreditHoldID,
		ValueDeliveredAt:    utcTimePointer(record.ValueDeliveredAt),
		CreatedAt:           record.CreatedAt.UTC(),
		UpdatedAt:           record.UpdatedAt.UTC(),
	}
	if err := pr4.ValidateSkillUsageRecord(usage); err != nil {
		return pr4.SkillUsageRecord{}, err
	}
	return usage, nil
}

func (a *App) getSkillCreatorUserID(ctx context.Context, skillID string) (string, error) {
	var record businesscore.PR4SkillPackageRecord
	if err := a.repo.DB().WithContext(ctx).Where("skill_id = ?", skillID).First(&record).Error; err != nil {
		return "", mapStoreError(err)
	}
	return record.CreatorUserID, nil
}

func validateUsageEstimateInput(in CreateSkillUsageRecordInput, row listingRow) error {
	if in.SkillID != "" && in.SkillID != row.SkillID {
		return bizerrors.New(bizerrors.CodeStateConflict, "skill_id does not match listing")
	}
	if in.SkillVersion != "" && in.SkillVersion != row.SkillVersion {
		return bizerrors.New(bizerrors.CodeStateConflict, "skill_version does not match listing")
	}
	if in.PricingPolicyDigest != "" && in.PricingPolicyDigest != row.PricingPolicyDigest {
		return bizerrors.New(bizerrors.CodeStateConflict, "pricing_policy_digest does not match listing")
	}
	if in.EstimatedCredits != 0 && in.EstimatedCredits != row.UsageCredits {
		return bizerrors.New(bizerrors.CodeStateConflict, "estimated_credits does not match listing")
	}
	return nil
}

func requireAuth(auth AuthContext) error {
	if strings.TrimSpace(auth.UserID) == "" {
		return bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	return nil
}

func accountIDForScope(auth AuthContext, scope string, enterpriseID string) (string, error) {
	switch scope {
	case pr4.AccountScopePersonal:
		if auth.SpaceID != "" {
			return auth.SpaceID, nil
		}
		if auth.UserID != "" {
			return auth.UserID, nil
		}
	case pr4.AccountScopeEnterprise:
		id := strings.TrimSpace(enterpriseID)
		if id == "" {
			id = auth.EnterpriseID
		}
		if id != "" {
			return id, nil
		}
		return "", bizerrors.New(bizerrors.CodePermissionDenied, "enterprise context is required")
	}
	return "", bizerrors.New(bizerrors.CodeInvalidArgument, "target_scope must be personal or enterprise")
}

func currentAccountScope(auth AuthContext) string {
	if auth.LoginIdentityType == accountspace.IdentityEnterprise || auth.EnterpriseID != "" {
		return pr4.AccountScopeEnterprise
	}
	return pr4.AccountScopePersonal
}

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return bizerrors.New(bizerrors.CodeResourceNotFound, "marketplace resource not found")
	}
	if errors.Is(err, businesscore.ErrMarketplaceListingSuspended) {
		return bizerrors.New(bizerrors.CodeStateConflict, "marketplace listing is suspended")
	}
	if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "SQLSTATE 23505") {
		return bizerrors.New(bizerrors.CodeStateConflict, "marketplace resource already exists")
	}
	return err
}

func skillUsageDigest(runID string, row listingRow) (string, error) {
	return pr1.CanonicalDigest(map[string]any{
		"schema_version":         "skill_usage_preflight.v1",
		"run_id":                 runID,
		"listing_id":             row.ListingID,
		"skill_id":               row.SkillID,
		"skill_version":          row.SkillVersion,
		"pricing_policy_digest":  row.PricingPolicyDigest,
		"estimated_credits":      row.UsageCredits,
		"value_delivered_stage":  row.ValueDeliveredStage,
		"pricing_model":          row.PricingModel,
		"two_stage_confirmation": true,
	})
}

func prefixedStableID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + hex.EncodeToString(sum[:])[:24]
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func cursorOffset(cursor string) int {
	offset, err := strconv.Atoi(strings.TrimSpace(cursor))
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func listingDTO(row listingRow) MarketplaceListingDTO {
	return MarketplaceListingDTO{
		ListingID:           row.ListingID,
		SkillID:             row.SkillID,
		SkillVersionID:      row.SkillVersionID,
		SkillVersion:        row.SkillVersion,
		SkillName:           row.SkillName,
		SkillDescription:    row.SkillDescription,
		CreatorUserID:       row.CreatorUserID,
		Status:              row.Status,
		PricingModel:        row.PricingModel,
		UsageCredits:        row.UsageCredits,
		ValueDeliveredStage: row.ValueDeliveredStage,
		PricingPolicyDigest: row.PricingPolicyDigest,
		PublishedBy:         row.PublishedBy,
		ListedAt:            utcTimePointer(row.ListedAt),
		CreatedAt:           row.CreatedAt.UTC(),
		UpdatedAt:           row.UpdatedAt.UTC(),
	}
}

func installationDTO(row installationRow) SkillInstallationDTO {
	return SkillInstallationDTO{
		InstallationID:   row.InstallationID,
		AccountID:        row.AccountID,
		AccountScope:     row.AccountScope,
		ListingID:        row.ListingID,
		SkillID:          row.SkillID,
		SkillName:        row.SkillName,
		InstalledVersion: row.InstalledVersion,
		VersionStrategy:  row.VersionStrategy,
		Status:           row.Status,
		UpgradeStatus:    row.UpgradeStatus,
		CreatedAt:        row.CreatedAt.UTC(),
		UpdatedAt:        row.UpdatedAt.UTC(),
	}
}

func installationDTOFromContract(installation pr4.SkillInstallation) SkillInstallationDTO {
	return SkillInstallationDTO{
		InstallationID:   installation.InstallationID,
		AccountID:        installation.AccountID,
		AccountScope:     installation.AccountScope,
		ListingID:        installation.ListingID,
		SkillID:          installation.SkillID,
		InstalledVersion: installation.InstalledVersion,
		VersionStrategy:  installation.VersionStrategy,
		Status:           installation.Status,
		UpgradeStatus:    installation.UpgradeStatus,
		CreatedAt:        installation.CreatedAt.UTC(),
		UpdatedAt:        installation.UpdatedAt.UTC(),
	}
}

func usageDTO(usage pr4.SkillUsageRecord) SkillUsageRecordDTO {
	return SkillUsageRecordDTO{
		UsageID:             usage.UsageID,
		RunID:               usage.RunID,
		ListingID:           usage.ListingID,
		SkillID:             usage.SkillID,
		SkillVersion:        usage.SkillVersion,
		PricingPolicyDigest: usage.PricingPolicyDigest,
		SkillUsageDigest:    usage.SkillUsageDigest,
		UsageStatus:         usage.UsageStatus,
		ChargeStatus:        usage.ChargeStatus,
		RefundStatus:        usage.RefundStatus,
		SettlementStatus:    usage.SettlementStatus,
		EstimatedCredits:    usage.EstimatedCredits,
		CreditHoldID:        usage.CreditHoldID,
		ValueDeliveredAt:    utcTimePointer(usage.ValueDeliveredAt),
		CreatedAt:           usage.CreatedAt.UTC(),
		UpdatedAt:           usage.UpdatedAt.UTC(),
	}
}

func settlementDTO(settlement pr4.SkillSettlement) SkillSettlementDTO {
	return SkillSettlementDTO{
		SettlementID:       settlement.SettlementID,
		UsageID:            settlement.UsageID,
		CreatorUserID:      settlement.CreatorUserID,
		Status:             settlement.Status,
		GrossCredits:       settlement.GrossCredits,
		PlatformFeeCredits: settlement.PlatformFeeCredits,
		CreatorCredits:     settlement.CreatorCredits,
		HoldUntil:          settlement.HoldUntil.UTC(),
		CreatedAt:          settlement.CreatedAt.UTC(),
		UpdatedAt:          settlement.UpdatedAt.UTC(),
	}
}
