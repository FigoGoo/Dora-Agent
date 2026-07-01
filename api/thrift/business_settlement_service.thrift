namespace go dora.api.businesssettlement

include "business_agent_service.thrift"

enum SettlementStatus {
  PENDING_HOLD = 1,
  ELIGIBLE = 2,
  SETTLING = 3,
  SETTLED = 4,
  REVERSED = 5,
  FROZEN = 6,
  FAILED = 7,
}

struct SkillSettlementDTO {
  1: required string settlement_id,
  2: required string usage_id,
  3: required string creator_user_id,
  4: required SettlementStatus status,
  5: required i64 gross_credits,
  6: required i64 platform_fee_credits,
  7: required i64 creator_credits,
  8: required string hold_until,
}

struct CreateSkillSettlementHoldRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string usage_id,
  4: required string creator_user_id,
  5: required i64 gross_credits,
  6: required string settlement_digest,
}

struct CreateSkillSettlementHoldResponse {
  1: required SkillSettlementDTO settlement,
}

struct ReverseSkillSettlementRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string settlement_id,
  4: required string usage_id,
  5: required string refund_case_id,
  6: required string reverse_digest,
}

struct ReverseSkillSettlementResponse {
  1: required SkillSettlementDTO settlement,
}

service BusinessSettlementService {
  CreateSkillSettlementHoldResponse CreateSkillSettlementHold(1: CreateSkillSettlementHoldRequest request)
  ReverseSkillSettlementResponse ReverseSkillSettlement(1: ReverseSkillSettlementRequest request)
}
