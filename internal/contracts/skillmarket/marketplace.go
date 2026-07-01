package skillmarket

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
)

const (
	SchemaVersionSkillPackage       = "skill_package.v1"
	SchemaVersionSkillVersion       = "skill_version.v1"
	SchemaVersionSkillPricingPolicy = "skill_pricing_policy.v1"
	SchemaVersionMarketplaceListing = "marketplace_listing.v1"
	SchemaVersionSkillInstallation  = "skill_installation.v1"
)

const (
	SkillVisibilityPrivate    = "private"
	SkillVisibilityReviewOnly = "review_only"
	SkillVisibilityPublic     = "public"
)

const (
	PricingModelFree        = "free"
	PricingModelFixedPerUse = "fixed_per_use"
)

const (
	VersionStrategyLatestPublished = "latest_published"
	VersionStrategyPinned          = "pinned"
	VersionStrategyManual          = "manual"
)

type SkillPackage struct {
	SchemaVersion  string    `json:"schema_version"`
	SkillID        string    `json:"skill_id"`
	CreatorUserID  string    `json:"creator_user_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Visibility     string    `json:"visibility"`
	CurrentVersion *string   `json:"current_version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SkillVersion struct {
	SchemaVersion       string     `json:"schema_version"`
	SkillVersionID      string     `json:"skill_version_id"`
	SkillID             string     `json:"skill_id"`
	Version             string     `json:"version"`
	Status              string     `json:"status"`
	RuntimeSpecDigest   string     `json:"runtime_spec_digest"`
	PricingPolicyDigest string     `json:"pricing_policy_digest"`
	SubmittedAt         *time.Time `json:"submitted_at"`
	PublishedAt         *time.Time `json:"published_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type SkillPricingPolicy struct {
	SchemaVersion       string    `json:"schema_version"`
	PricingPolicyID     string    `json:"pricing_policy_id"`
	SkillID             string    `json:"skill_id"`
	SkillVersion        string    `json:"skill_version"`
	PricingModel        string    `json:"pricing_model"`
	UsageCredits        int       `json:"usage_credits"`
	ValueDeliveredStage string    `json:"value_delivered_stage"`
	PricingPolicyDigest string    `json:"pricing_policy_digest"`
	CreatedAt           time.Time `json:"created_at"`
}

type MarketplaceListing struct {
	SchemaVersion       string     `json:"schema_version"`
	ListingID           string     `json:"listing_id"`
	SkillID             string     `json:"skill_id"`
	SkillVersionID      string     `json:"skill_version_id"`
	Status              string     `json:"status"`
	PricingPolicyDigest string     `json:"pricing_policy_digest"`
	PublishedBy         string     `json:"published_by"`
	ListedAt            *time.Time `json:"listed_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type SkillInstallation struct {
	SchemaVersion    string    `json:"schema_version"`
	InstallationID   string    `json:"installation_id"`
	AccountID        string    `json:"account_id"`
	AccountScope     string    `json:"account_scope"`
	ListingID        string    `json:"listing_id"`
	SkillID          string    `json:"skill_id"`
	InstalledVersion string    `json:"installed_version"`
	VersionStrategy  string    `json:"version_strategy"`
	Status           string    `json:"status"`
	UpgradeStatus    string    `json:"upgrade_status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type InstallSkillRequest struct {
	AccountID      string `json:"account_id"`
	AccountScope   string `json:"account_scope"`
	ListingID      string `json:"listing_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type UpgradeSkillInstallationRequest struct {
	InstallationID string `json:"installation_id"`
	TargetVersion  string `json:"target_version"`
	Confirmed      bool   `json:"confirmed"`
	IdempotencyKey string `json:"idempotency_key"`
}

type HistoricalRunRule struct {
	RunID                      string `json:"run_id"`
	MustResumeWithSkillVersion string `json:"must_resume_with_skill_version"`
}

func ValidateSkillPackage(pkg SkillPackage) error {
	if pkg.SchemaVersion != SchemaVersionSkillPackage {
		return fmt.Errorf("schema_version must be %s", SchemaVersionSkillPackage)
	}
	if err := validatePrefixID(pkg.SkillID, "skill_"); err != nil {
		return fmt.Errorf("skill_id: %w", err)
	}
	if strings.TrimSpace(pkg.CreatorUserID) == "" {
		return errors.New("creator_user_id is required")
	}
	if name := strings.TrimSpace(pkg.Name); name == "" || len([]rune(name)) > 80 {
		return fmt.Errorf("invalid name %q", pkg.Name)
	}
	if len([]rune(pkg.Description)) > 1000 {
		return errors.New("description must be <= 1000 characters")
	}
	if !isAllowed(pkg.Visibility, []string{SkillVisibilityPrivate, SkillVisibilityReviewOnly, SkillVisibilityPublic}) {
		return fmt.Errorf("invalid visibility %q", pkg.Visibility)
	}
	if pkg.CurrentVersion != nil && strings.TrimSpace(*pkg.CurrentVersion) == "" {
		return errors.New("current_version cannot be empty")
	}
	return validateTimeRange(pkg.CreatedAt, pkg.UpdatedAt)
}

func ValidateSkillVersion(version SkillVersion) error {
	if version.SchemaVersion != SchemaVersionSkillVersion {
		return fmt.Errorf("schema_version must be %s", SchemaVersionSkillVersion)
	}
	if err := validatePrefixID(version.SkillVersionID, "skv_"); err != nil {
		return fmt.Errorf("skill_version_id: %w", err)
	}
	if err := validatePrefixID(version.SkillID, "skill_"); err != nil {
		return fmt.Errorf("skill_id: %w", err)
	}
	if !strings.HasPrefix(version.Version, "v") {
		return fmt.Errorf("invalid version %q", version.Version)
	}
	if !foundation.IsValidState(foundation.StateSkillVersionStatus, version.Status) {
		return fmt.Errorf("invalid skill version status %q", version.Status)
	}
	if err := foundation.ValidateDigest(version.RuntimeSpecDigest); err != nil {
		return fmt.Errorf("runtime_spec_digest: %w", err)
	}
	if err := foundation.ValidateDigest(version.PricingPolicyDigest); err != nil {
		return fmt.Errorf("pricing_policy_digest: %w", err)
	}
	if version.Status == "published" && version.PublishedAt == nil {
		return errors.New("published version requires published_at")
	}
	return validateTimeRange(version.CreatedAt, version.UpdatedAt)
}

func ValidateSkillPricingPolicy(policy SkillPricingPolicy) error {
	if policy.SchemaVersion != SchemaVersionSkillPricingPolicy {
		return fmt.Errorf("schema_version must be %s", SchemaVersionSkillPricingPolicy)
	}
	if err := validatePrefixID(policy.PricingPolicyID, "spp_"); err != nil {
		return fmt.Errorf("pricing_policy_id: %w", err)
	}
	if err := validatePrefixID(policy.SkillID, "skill_"); err != nil {
		return fmt.Errorf("skill_id: %w", err)
	}
	if strings.TrimSpace(policy.SkillVersion) == "" {
		return errors.New("skill_version is required")
	}
	if !isAllowed(policy.PricingModel, []string{PricingModelFree, PricingModelFixedPerUse}) {
		return fmt.Errorf("invalid pricing_model %q", policy.PricingModel)
	}
	if policy.UsageCredits < 0 {
		return errors.New("usage_credits must be >= 0")
	}
	if policy.PricingModel == PricingModelFree && policy.UsageCredits != 0 {
		return errors.New("free pricing policy must have usage_credits=0")
	}
	if !isAllowed(policy.ValueDeliveredStage, []string{"board_ready", "storyboard_ready", "asset_ready"}) {
		return fmt.Errorf("invalid value_delivered_stage %q", policy.ValueDeliveredStage)
	}
	if err := foundation.ValidateDigest(policy.PricingPolicyDigest); err != nil {
		return fmt.Errorf("pricing_policy_digest: %w", err)
	}
	if policy.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	return nil
}

func ValidateMarketplaceListing(listing MarketplaceListing) error {
	if listing.SchemaVersion != SchemaVersionMarketplaceListing {
		return fmt.Errorf("schema_version must be %s", SchemaVersionMarketplaceListing)
	}
	if err := validatePrefixID(listing.ListingID, "listing_"); err != nil {
		return fmt.Errorf("listing_id: %w", err)
	}
	if err := validatePrefixID(listing.SkillID, "skill_"); err != nil {
		return fmt.Errorf("skill_id: %w", err)
	}
	if err := validatePrefixID(listing.SkillVersionID, "skv_"); err != nil {
		return fmt.Errorf("skill_version_id: %w", err)
	}
	if !foundation.IsValidState(foundation.StateMarketplaceListingStatus, listing.Status) {
		return fmt.Errorf("invalid listing status %q", listing.Status)
	}
	if err := foundation.ValidateDigest(listing.PricingPolicyDigest); err != nil {
		return fmt.Errorf("pricing_policy_digest: %w", err)
	}
	if strings.TrimSpace(listing.PublishedBy) == "" {
		return errors.New("published_by is required")
	}
	if listing.Status == "listed" && listing.ListedAt == nil {
		return errors.New("listed listing requires listed_at")
	}
	return validateTimeRange(listing.CreatedAt, listing.UpdatedAt)
}

func ValidateCreatorPublishFlow(pkg SkillPackage, version SkillVersion, policy SkillPricingPolicy, listing MarketplaceListing) error {
	if err := ValidateSkillPackage(pkg); err != nil {
		return fmt.Errorf("skill_package: %w", err)
	}
	if err := ValidateSkillVersion(version); err != nil {
		return fmt.Errorf("skill_version: %w", err)
	}
	if err := ValidateSkillPricingPolicy(policy); err != nil {
		return fmt.Errorf("pricing_policy: %w", err)
	}
	if err := ValidateMarketplaceListing(listing); err != nil {
		return fmt.Errorf("listing: %w", err)
	}
	if pkg.SkillID != version.SkillID || pkg.SkillID != policy.SkillID || pkg.SkillID != listing.SkillID {
		return errors.New("skill identity must be stable across package, version, policy and listing")
	}
	if pkg.CurrentVersion == nil || *pkg.CurrentVersion != version.Version {
		return errors.New("package current_version must match published version")
	}
	if version.Status != "published" || listing.Status != "listed" {
		return errors.New("creator publish flow requires published version and listed listing")
	}
	if version.PricingPolicyDigest != policy.PricingPolicyDigest || listing.PricingPolicyDigest != policy.PricingPolicyDigest {
		return errors.New("pricing digest must match version, policy and listing")
	}
	if listing.SkillVersionID != version.SkillVersionID {
		return errors.New("listing must reference published skill_version_id")
	}
	return nil
}

func ValidateSkillInstallation(installation SkillInstallation) error {
	if installation.SchemaVersion != SchemaVersionSkillInstallation {
		return fmt.Errorf("schema_version must be %s", SchemaVersionSkillInstallation)
	}
	if err := validatePrefixID(installation.InstallationID, "sinst_"); err != nil {
		return fmt.Errorf("installation_id: %w", err)
	}
	if strings.TrimSpace(installation.AccountID) == "" {
		return errors.New("account_id is required")
	}
	if !isAllowed(installation.AccountScope, []string{AccountScopePersonal, AccountScopeEnterprise}) {
		return fmt.Errorf("invalid account_scope %q", installation.AccountScope)
	}
	if err := validatePrefixID(installation.ListingID, "listing_"); err != nil {
		return fmt.Errorf("listing_id: %w", err)
	}
	if err := validatePrefixID(installation.SkillID, "skill_"); err != nil {
		return fmt.Errorf("skill_id: %w", err)
	}
	if strings.TrimSpace(installation.InstalledVersion) == "" {
		return errors.New("installed_version is required")
	}
	if !isAllowed(installation.VersionStrategy, []string{VersionStrategyLatestPublished, VersionStrategyPinned, VersionStrategyManual}) {
		return fmt.Errorf("invalid version_strategy %q", installation.VersionStrategy)
	}
	if !foundation.IsValidState(foundation.StateInstallationStatus, installation.Status) {
		return fmt.Errorf("invalid installation status %q", installation.Status)
	}
	if !foundation.IsValidState(foundation.StateInstallationUpgradeStatus, installation.UpgradeStatus) {
		return fmt.Errorf("invalid upgrade_status %q", installation.UpgradeStatus)
	}
	return validateTimeRange(installation.CreatedAt, installation.UpdatedAt)
}

func ValidatePersonalLatestInstall(request InstallSkillRequest, installation SkillInstallation) error {
	if err := validateInstallRequest(request); err != nil {
		return err
	}
	if err := ValidateSkillInstallation(installation); err != nil {
		return err
	}
	if request.AccountScope != AccountScopePersonal || installation.AccountScope != AccountScopePersonal {
		return errors.New("personal install must use personal scope")
	}
	if request.AccountID != installation.AccountID || request.ListingID != installation.ListingID {
		return errors.New("installation must match install request account and listing")
	}
	if installation.VersionStrategy != VersionStrategyLatestPublished || installation.UpgradeStatus != "none" {
		return errors.New("personal install must default latest_published with upgrade_status=none")
	}
	return nil
}

func ValidateEnterprisePinnedUpgrade(initial SkillInstallation, request UpgradeSkillInstallationRequest, after SkillInstallation, rule HistoricalRunRule) error {
	if err := ValidateSkillInstallation(initial); err != nil {
		return fmt.Errorf("initial_installation: %w", err)
	}
	if err := ValidateSkillInstallation(after); err != nil {
		return fmt.Errorf("installation_after_upgrade: %w", err)
	}
	if initial.AccountScope != AccountScopeEnterprise || initial.VersionStrategy != VersionStrategyPinned {
		return errors.New("enterprise initial installation must be pinned")
	}
	if err := validatePrefixID(request.InstallationID, "sinst_"); err != nil {
		return fmt.Errorf("upgrade installation_id: %w", err)
	}
	if !request.Confirmed {
		return errors.New("enterprise upgrade requires confirmation")
	}
	if strings.TrimSpace(request.TargetVersion) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		return errors.New("target_version and idempotency_key are required")
	}
	if initial.InstallationID != request.InstallationID || after.InstallationID != initial.InstallationID {
		return errors.New("installation_id must be stable")
	}
	if after.InstalledVersion != request.TargetVersion || after.UpgradeStatus != "confirmed" {
		return errors.New("installation_after_upgrade must install confirmed target version")
	}
	if after.AccountID != initial.AccountID ||
		after.AccountScope != initial.AccountScope ||
		after.ListingID != initial.ListingID ||
		after.SkillID != initial.SkillID ||
		after.VersionStrategy != initial.VersionStrategy {
		return errors.New("installation identity and strategy must be stable across upgrade")
	}
	if strings.TrimSpace(rule.RunID) == "" || rule.MustResumeWithSkillVersion != initial.InstalledVersion {
		return errors.New("historical runs must resume with old skill version snapshot")
	}
	return nil
}

func validateInstallRequest(request InstallSkillRequest) error {
	if strings.TrimSpace(request.AccountID) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		return errors.New("account_id and idempotency_key are required")
	}
	if !isAllowed(request.AccountScope, []string{AccountScopePersonal, AccountScopeEnterprise}) {
		return fmt.Errorf("invalid account_scope %q", request.AccountScope)
	}
	if err := validatePrefixID(request.ListingID, "listing_"); err != nil {
		return fmt.Errorf("listing_id: %w", err)
	}
	return nil
}

func validateTimeRange(createdAt, updatedAt time.Time) error {
	if createdAt.IsZero() || updatedAt.IsZero() {
		return errors.New("created_at and updated_at are required")
	}
	if updatedAt.Before(createdAt) {
		return errors.New("updated_at must not be before created_at")
	}
	return nil
}
