namespace go dora.api.businessagent

// LoginIdentityType 表示当前调用身份类型。
enum LoginIdentityType {
  PERSONAL = 1,
  ENTERPRISE_MEMBER = 2,
  ADMIN = 3,
}

// ProjectAccessPurpose 表示项目权限校验目的。
enum ProjectAccessPurpose {
  VIEW = 1,
  CONTINUE_CREATION = 2,
  ATTACH_ASSET = 3,
  COMMIT_ASSET = 4,
  CREATE_WORK = 5,
}

// AuthContext 是业务服务最终校验权限的身份上下文。
struct AuthContext {
  1: required string actor_user_id,
  2: required LoginIdentityType login_identity_type,
  3: optional string space_id,
  4: optional string enterprise_id,
  5: optional string enterprise_role,
  6: optional string admin_id,
}

// RequestMeta 是跨服务请求元信息。
struct RequestMeta {
  1: required string request_id,
  2: required string trace_id,
  3: optional string idempotency_key,
  4: required string source,
}

// SafetyEvidenceDTO 是 Agent 传给业务写入的脱敏内容安全证据。
struct SafetyEvidenceDTO {
  1: required string safety_evidence_id,
  2: required string scene,
  3: required string result,
  4: required string target_type,
  5: optional string target_ref_id,
  6: required string evaluated_object_digest,
  7: required string policy_version,
  8: required string evidence_version,
  9: required string evaluated_at,
  10: optional string expires_at,
  11: optional string source_session_id,
  12: optional string source_run_id,
  13: optional string source_artifact_id,
  14: required string trace_id,
  15: optional string user_visible_reason,
}

struct ResolveCurrentSpaceContextRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
}

struct ResolveCurrentSpaceContextResponse {
  1: required string space_id,
  2: required string space_type,
  3: optional string enterprise_id,
  4: optional string enterprise_role,
  5: required string credit_account_type,
  6: required string user_status,
}

struct CheckProjectAccessRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string project_id,
  4: required ProjectAccessPurpose access_purpose,
}

struct ProjectAccessResponse {
  1: required bool allowed,
  2: required string project_status,
  3: required bool creative_allowed,
  4: required list<string> allowed_actions,
  5: optional string user_message,
  6: optional map<string,string> project_summary,
}

struct BatchCheckAssetAccessRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string project_id,
  4: required list<string> asset_ids,
  5: required string purpose,
}

struct AssetAccessResult {
  1: required string asset_id,
  2: required bool allowed,
  3: required string reason,
  4: optional map<string,string> asset_summary,
}

struct BatchCheckAssetAccessResponse {
  1: required list<AssetAccessResult> results,
}

struct EstimateGenerationCreditsRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string project_id,
  4: required string resource_type,
  5: required string model_id,
  6: required string pricing_snapshot_id,
  7: optional i32 quantity,
  8: optional i32 duration_seconds,
  9: optional list<ToolUsageEstimateItemInput> tool_usage_items,
}

struct EstimateGenerationCreditsResponse {
  1: required string estimate_id,
  2: required i64 estimate_points,
  3: required i64 available_points,
  4: required i64 expires_soon_points,
  5: required string account_type,
  6: optional list<CreditEstimateLineItemDTO> line_items,
  7: optional string expires_at,
  8: optional bool insufficient,
}

struct ToolUsageEstimateItemInput {
  1: required string tool_name,
  2: required string tool_type,
  3: required string billing_unit,
  4: required double quantity,
  5: optional map<string,string> metadata_summary,
}

struct CreditEstimateLineItemDTO {
  1: required string estimate_item_id,
  2: required string item_type,
  3: optional string tool_name,
  4: optional string tool_type,
  5: optional string pricing_policy_id,
  6: optional string model_id,
  7: optional string resource_type,
  8: optional string billing_unit,
  9: optional double quantity,
  10: optional double unit_points,
  11: required i64 estimate_points,
  12: optional string free_reason,
  13: optional map<string,string> metadata_summary,
}

struct EstimateToolCreditsRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string project_id,
  4: required list<ToolUsageEstimateItemInput> tool_usage_items,
}

struct EstimateToolCreditsResponse {
  1: required string estimate_id,
  2: required i64 estimate_points,
  3: required i64 available_points,
  4: required i64 expires_soon_points,
  5: required string account_type,
  6: required list<CreditEstimateLineItemDTO> line_items,
  7: required string expires_at,
  8: required bool insufficient,
}

struct FreezeCreditsRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string estimate_id,
  4: required i64 points,
  5: required string run_id,
  6: optional string confirmation_id,
  7: optional string account_id,
}

struct FreezeCreditsResponse {
  1: required string freeze_id,
  2: required i64 frozen_points,
  3: required string expires_at,
}

struct ReleaseFrozenCreditsRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string freeze_id,
  4: required i64 release_points,
  5: required string reason,
  6: required string run_id,
}

struct ReleaseFrozenCreditsResponse {
  1: required i64 released_points,
  2: required string release_status,
}

struct ToolChargeItemInput {
  1: required string estimate_item_id,
  2: required string tool_call_id,
  3: required string tool_name,
  4: required string tool_type,
  5: required string billing_unit,
  6: required double actual_quantity,
  7: required string execution_status,
  8: optional map<string,string> metadata_summary,
}

struct ChargedLineItemDTO {
  1: required string estimate_item_id,
  2: required i64 charged_points,
  3: required string status,
  4: optional string asset_id,
  5: optional string tool_call_id,
  6: optional string artifact_id,
}

struct ChargeToolUsageCreditsRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string project_id,
  4: required string estimate_id,
  5: required string freeze_id,
  6: required string session_id,
  7: required string run_id,
  8: required list<ToolChargeItemInput> charge_items,
}

struct ChargeToolUsageCreditsResponse {
  1: required string tool_charge_id,
  2: required i64 charged_points,
  3: required i64 released_points,
  4: required string freeze_status,
  5: required list<string> ledger_entry_ids,
  6: required list<ChargedLineItemDTO> charged_line_items,
}

struct CommitArtifactDTO {
  1: required string artifact_id,
  2: required string resource_type,
  3: required string element_type,
  4: required map<string,string> artifact_summary,
  5: optional string content_uri_digest,
  6: optional string estimate_item_id,
  7: optional string tool_name,
  8: optional string tool_type,
  9: optional i64 charge_quantity,
  10: optional map<string,string> metadata_summary,
}

struct CommitGeneratedAssetAndChargeRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string project_id,
  4: required string session_id,
  5: required string run_id,
  6: required string freeze_id,
  7: required list<CommitArtifactDTO> artifacts,
  8: required list<map<string,string>> final_elements,
  9: required SafetyEvidenceDTO safety_evidence,
  10: optional string estimate_id,
}

struct CommitGeneratedAssetAndChargeResponse {
  1: required list<map<string,string>> asset_refs,
  2: required i64 charged_points,
  3: required i64 released_points,
  4: required string commit_status,
  5: optional string ledger_ref,
  6: optional list<ChargedLineItemDTO> charged_line_items,
}

struct ListRoutableSkillsRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: optional string skill_scope_filter,
  4: optional i32 page_size,
  5: optional string cursor,
}

struct SkillSummaryDTO {
  1: required string skill_id,
  2: required string skill_name,
  3: required string skill_scope,
  4: required string version,
  5: required string status,
  6: optional map<string,string> route_hints,
}

struct ListRoutableSkillsResponse {
  1: required list<SkillSummaryDTO> skills,
  2: optional string next_cursor,
}

struct GetPublishedSkillSpecRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string skill_id,
  4: optional string version,
}

struct SkillSpecResponse {
  1: required string skill_id,
  2: required string version,
  3: required string skill_spec_json,
  4: required string output_schema_json,
  5: required list<string> tool_refs,
  6: optional string memory_policy_json,
}

struct CheckToolExecutionPolicyRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string tool_name,
  4: required string tool_type,
  5: required string project_id,
  6: optional map<string,string> risk_context,
}

struct ToolExecutionPolicyResponse {
  1: required bool allowed,
  2: required string risk_level,
  3: required bool requires_confirmation,
  4: required i32 timeout_ms,
  5: optional map<string,string> retry_policy,
  6: optional map<string,string> cancel_policy,
}

struct ListAvailableGenerationModelsRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string resource_type,
  4: optional i32 page_size,
  5: optional string cursor,
}

struct ModelSummaryDTO {
  1: required string model_id,
  2: required string display_name,
  3: required bool is_default,
  4: required string pricing_snapshot_id,
  5: required string resource_type,
}

struct ListAvailableGenerationModelsResponse {
  1: required list<ModelSummaryDTO> models,
  2: optional string next_cursor,
}

struct ResolveDefaultModelRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string resource_type,
}

struct ListAssetElementTypesRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: optional i32 page_size,
  4: optional string schema_version,
}

struct ListAssetElementTypesResponse {
  1: required list<map<string,string>> element_types,
  2: required string schema_version,
}

struct GetReviewCandidateSkillSpecRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string skill_id,
  4: required string version_id,
  5: optional string test_case_id,
  6: optional string test_run_id,
}

struct ReviewCandidateSkillSpecResponse {
  1: required string skill_id,
  2: required string version_id,
  3: required string skill_spec_json,
  4: required string input_schema_json,
  5: required string output_schema_json,
  6: required list<string> tool_refs,
  7: required string memory_policy_json,
  8: optional string test_input_json,
  9: optional string expected_elements_json,
}

struct SaveSkillTestResultRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string skill_id,
  4: required string version_id,
  5: required string test_run_id,
  6: optional string test_case_id,
  7: required string status,
  8: required string actual_elements_json,
  9: optional string error_code,
  10: optional string error_summary,
  11: optional string safety_evidence_json,
  12: required string agent_trace_id,
}

struct SaveSkillTestResultResponse {
  1: required string test_run_id,
  2: required string status,
  3: required bool saved,
}

service AccountSpaceService {
  ResolveCurrentSpaceContextResponse ResolveCurrentSpaceContext(1: ResolveCurrentSpaceContextRequest req)
}

service ProjectService {
  ProjectAccessResponse CheckProjectAccess(1: CheckProjectAccessRequest req)
}

service AssetService {
  BatchCheckAssetAccessResponse BatchCheckAssetAccess(1: BatchCheckAssetAccessRequest req)
}

service CreditService {
  EstimateGenerationCreditsResponse EstimateGenerationCredits(1: EstimateGenerationCreditsRequest req)
  EstimateToolCreditsResponse EstimateToolCredits(1: EstimateToolCreditsRequest req)
  FreezeCreditsResponse FreezeCredits(1: FreezeCreditsRequest req)
  ChargeToolUsageCreditsResponse ChargeToolUsageCredits(1: ChargeToolUsageCreditsRequest req)
  ReleaseFrozenCreditsResponse ReleaseFrozenCredits(1: ReleaseFrozenCreditsRequest req)
}

service AssetCreditCommitService {
  CommitGeneratedAssetAndChargeResponse CommitGeneratedAssetAndCharge(1: CommitGeneratedAssetAndChargeRequest req)
}

service SkillCatalogService {
  ListRoutableSkillsResponse ListRoutableSkills(1: ListRoutableSkillsRequest req)
  SkillSpecResponse GetPublishedSkillSpec(1: GetPublishedSkillSpecRequest req)
  ReviewCandidateSkillSpecResponse GetReviewCandidateSkillSpec(1: GetReviewCandidateSkillSpecRequest req)
  SaveSkillTestResultResponse SaveSkillTestResult(1: SaveSkillTestResultRequest req)
}

service ToolCapabilityService {
  ToolExecutionPolicyResponse CheckToolExecutionPolicy(1: CheckToolExecutionPolicyRequest req)
}

service ModelConfigService {
  ListAvailableGenerationModelsResponse ListAvailableGenerationModels(1: ListAvailableGenerationModelsRequest req)
  ModelSummaryDTO ResolveDefaultModel(1: ResolveDefaultModelRequest req)
}

service PlatformDictionaryService {
  ListAssetElementTypesResponse ListAssetElementTypes(1: ListAssetElementTypesRequest req)
}
