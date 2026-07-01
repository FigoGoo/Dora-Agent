namespace go dora.api.businesstool

include "business_agent_service.thrift"

struct GetToolPricingRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string tool_id,
  4: required string tool_version,
  5: required string resource_type,
}

struct ToolPricingDTO {
  1: required string tool_id,
  2: required string tool_version,
  3: required string resource_type,
  4: required i64 unit_credits,
  5: required string pricing_digest,
  6: required string effective_at,
  7: optional string expires_at,
}

struct GetToolPricingResponse {
  1: required ToolPricingDTO pricing,
}

service BusinessToolService {
  GetToolPricingResponse GetToolPricing(1: GetToolPricingRequest request)
}
