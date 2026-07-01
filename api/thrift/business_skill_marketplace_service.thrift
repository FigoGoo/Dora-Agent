namespace go dora.api.businessskillmarketplace

include "business_agent_service.thrift"

enum SkillVersionStatus {
  DRAFT = 1,
  SUBMITTED = 2,
  REVIEWING = 3,
  REJECTED = 4,
  PUBLISHED = 5,
  DEPRECATED = 6,
  REMOVED = 7,
}

enum MarketplaceListingStatus {
  DRAFT = 1,
  PENDING_LISTING_REVIEW = 2,
  LISTED = 3,
  UNLISTED = 4,
  SUSPENDED = 5,
  REMOVED = 6,
}

enum InstallationScope {
  PERSONAL = 1,
  ENTERPRISE = 2,
}

struct MarketplaceListingDTO {
  1: required string listing_id,
  2: required string skill_id,
  3: required string skill_version_id,
  4: required MarketplaceListingStatus status,
  5: required string pricing_policy_digest,
  6: required string created_at,
  7: required string updated_at,
}

struct SkillInstallationDTO {
  1: required string installation_id,
  2: required string account_id,
  3: required InstallationScope account_scope,
  4: required string listing_id,
  5: required string skill_id,
  6: required string installed_version,
  7: required string version_strategy,
  8: required string status,
  9: required string upgrade_status,
  10: required string created_at,
  11: required string updated_at,
}

struct SkillUsageRecordDTO {
  1: required string usage_id,
  2: required string run_id,
  3: required string listing_id,
  4: required string skill_id,
  5: required string skill_version,
  6: required string pricing_policy_digest,
  7: required string skill_usage_digest,
  8: required string usage_status,
  9: required string charge_status,
  10: required string refund_status,
  11: required string settlement_status,
  12: required i64 estimated_credits,
  13: optional string credit_hold_id,
}

struct ListMarketplaceSkillsRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: optional string query,
  4: optional i32 page_size,
  5: optional string cursor,
}

struct ListMarketplaceSkillsResponse {
  1: required list<MarketplaceListingDTO> listings,
  2: optional string next_cursor,
}

struct GetMarketplaceSkillRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string listing_id,
}

struct GetMarketplaceSkillResponse {
  1: required MarketplaceListingDTO listing,
}

struct InstallSkillRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string listing_id,
  4: required InstallationScope target_scope,
  5: optional string enterprise_id,
}

struct InstallSkillResponse {
  1: required SkillInstallationDTO installation,
  2: required bool idempotent_replay,
}

struct UpgradeSkillInstallationRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string installation_id,
  4: required string target_version,
  5: required bool confirmed,
}

struct UpgradeSkillInstallationResponse {
  1: required SkillInstallationDTO installation,
}

struct EstimateSkillUsageCreditsRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string run_id,
  4: required string listing_id,
  5: required string pricing_policy_digest,
}

struct EstimateSkillUsageCreditsResponse {
  1: required i64 estimated_credits,
  2: required string pricing_policy_digest,
  3: required string skill_usage_digest,
  4: required string expires_at,
}

struct CreateSkillUsageRecordRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string run_id,
  4: required string listing_id,
  5: required string skill_id,
  6: required string skill_version,
  7: required string pricing_policy_digest,
  8: required string skill_usage_digest,
  9: required i64 estimated_credits,
}

struct CreateSkillUsageRecordResponse {
  1: required SkillUsageRecordDTO usage,
  2: required bool idempotent_replay,
}

struct FreezeSkillUsageCreditsRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string usage_id,
  4: required string skill_usage_digest,
}

struct FreezeSkillUsageCreditsResponse {
  1: required SkillUsageRecordDTO usage,
}

struct CommitSkillUsageAndSettleRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string usage_id,
  4: required string value_delivered_digest,
}

struct CommitSkillUsageAndSettleResponse {
  1: required SkillUsageRecordDTO usage,
  2: required string settlement_id,
}

struct ReleaseSkillUsageFreezeRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string usage_id,
  4: required string release_reason,
}

struct ReleaseSkillUsageFreezeResponse {
  1: required SkillUsageRecordDTO usage,
}

service BusinessSkillMarketplaceService {
  ListMarketplaceSkillsResponse ListMarketplaceSkills(1: ListMarketplaceSkillsRequest request)
  GetMarketplaceSkillResponse GetMarketplaceSkill(1: GetMarketplaceSkillRequest request)
  InstallSkillResponse InstallSkill(1: InstallSkillRequest request)
  UpgradeSkillInstallationResponse UpgradeSkillInstallation(1: UpgradeSkillInstallationRequest request)
  EstimateSkillUsageCreditsResponse EstimateSkillUsageCredits(1: EstimateSkillUsageCreditsRequest request)
  CreateSkillUsageRecordResponse CreateSkillUsageRecord(1: CreateSkillUsageRecordRequest request)
  FreezeSkillUsageCreditsResponse FreezeSkillUsageCredits(1: FreezeSkillUsageCreditsRequest request)
  CommitSkillUsageAndSettleResponse CommitSkillUsageAndSettle(1: CommitSkillUsageAndSettleRequest request)
  ReleaseSkillUsageFreezeResponse ReleaseSkillUsageFreeze(1: ReleaseSkillUsageFreezeRequest request)
}
