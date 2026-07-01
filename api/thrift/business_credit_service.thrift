namespace go dora.api.businesscredit

include "business_agent_service.thrift"

enum CreditHoldStatus {
  FROZEN = 1,
  COMMITTED = 2,
  RELEASED = 3,
  FAILED = 4,
}

struct ToolCreditItemDTO {
  1: required string tool_plan_item_id,
  2: required string tool_id,
  3: required string tool_version,
  4: required string resource_type,
  5: required i32 quantity,
  6: required i64 estimated_credits,
  7: required string input_digest,
}

struct EstimateToolCreditsRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string run_id,
  4: required string project_id,
  5: required string tool_plan_id,
  6: required string tool_plan_digest,
  7: required list<ToolCreditItemDTO> items,
}

struct EstimateToolCreditsResponse {
  1: required string tool_plan_id,
  2: required string tool_plan_digest,
  3: required i64 estimated_credits,
  4: required string currency,
  5: required string estimate_digest,
  6: required string expires_at,
}

struct FreezeCreditsRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string credit_account_id,
  4: required string credit_account_scope,
  5: required string run_id,
  6: required string project_id,
  7: required string tool_plan_id,
  8: required string tool_plan_digest,
  9: required i64 credits,
}

struct CreditHoldDTO {
  1: required string credit_hold_id,
  2: required string credit_account_id,
  3: required string credit_account_scope,
  4: required string run_id,
  5: required string tool_plan_id,
  6: required string tool_plan_digest,
  7: required CreditHoldStatus status,
  8: required i64 frozen_credits,
  9: required i64 committed_credits,
  10: required i64 released_credits,
  11: required string created_at,
  12: optional string updated_at,
}

struct FreezeCreditsResponse {
  1: required CreditHoldDTO hold,
  2: required bool idempotent_replay,
}

struct CommitCreditsRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string credit_hold_id,
  4: required string tool_plan_id,
  5: required string commit_digest,
  6: required i64 committed_credits,
}

struct CommitCreditsResponse {
  1: required CreditHoldDTO hold,
  2: required string ledger_entry_id,
}

struct ReleaseCreditsRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string credit_hold_id,
  4: required string release_reason,
  5: required string release_digest,
}

struct ReleaseCreditsResponse {
  1: required CreditHoldDTO hold,
  2: required string ledger_entry_id,
}

service BusinessCreditService {
  EstimateToolCreditsResponse EstimateToolCredits(1: EstimateToolCreditsRequest request)
  FreezeCreditsResponse FreezeCredits(1: FreezeCreditsRequest request)
  CommitCreditsResponse CommitCredits(1: CommitCreditsRequest request)
  ReleaseCreditsResponse ReleaseCredits(1: ReleaseCreditsRequest request)
}
