namespace go dora.api.businessagent

// LoginIdentityType 表示 Agent 调用业务服务时的当前登录身份。
enum LoginIdentityType {
  PERSONAL = 1,
  ENTERPRISE_MEMBER = 2,
  ADMIN = 3,
}

// ProjectAccessPurpose 表示项目访问校验目的。
enum ProjectAccessPurpose {
  VIEW = 1,
  CONTINUE_CREATION = 2,
  ATTACH_ASSET = 3,
  COMMIT_ASSET = 4,
  CREATE_WORK = 5,
}

// AuthContext 是业务服务执行最终权限校验的身份上下文。
struct AuthContext {
  1: required string actor_user_id,
  2: required LoginIdentityType login_identity_type,
  3: optional string space_id,
  4: optional string enterprise_id,
  5: optional string enterprise_role,
  6: optional string admin_id,
}

// RequestMeta 是跨服务请求元信息；写操作必须携带 idempotency_key。
struct RequestMeta {
  1: required string request_id,
  2: required string trace_id,
  3: optional string idempotency_key,
  4: required string source,
}

// SafetyEvidenceDTO 是 Agent 传给业务服务的脱敏内容安全证据。
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
  3: optional string expected_space_id,
}

struct ResolveAuthContextFromTokenRequest {
  1: required string authorization,
  2: required RequestMeta request_meta,
  3: optional string expected_space_id,
}

struct ResolveAuthContextFromTokenResponse {
  1: required AuthContext auth_context,
  2: required ResolveCurrentSpaceContextResponse space_context,
  3: required string session_id,
  4: optional string expires_at,
}

struct ResolveCurrentSpaceContextResponse {
  1: required string space_id,
  2: required string space_type,
  3: optional string enterprise_id,
  4: optional string enterprise_role,
  5: required string credit_account_scope,
  6: required string credit_account_id,
  7: required list<string> skill_scope_keys,
  8: optional map<string,string> permission_summary,
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

// SkillOutputElementDTO 声明 Skill 一个输出元素结构(SKILL-2)。element_type 为平台内置 active 资产元素类型；
// use_draft/use_final 表达草稿/最终双态，editable/referable 不得超字典上限。
struct SkillOutputElementDTO {
  1: required string element_type,
  2: required string element_name,
  3: required bool required,
  4: required bool use_draft,
  5: required bool use_final,
  6: required bool editable,
  7: required bool referable,
  8: optional i32 display_order,
  9: optional string display_slot,
  10: optional string schema_json,
}

struct SkillSpecResponse {
  1: required string skill_id,
  2: required string version,
  3: required string skill_spec_json,
  4: required string output_schema_json,
  5: required list<string> tool_refs,
  6: optional string memory_policy_json,
  7: required string confirmation_policy_json,
  8: required string execution_policy_summary_json,
  9: optional list<SkillOutputElementDTO> output_elements,
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

struct ResolveGenerationModelSnapshotRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string resource_type,
  4: required string model_id,
  5: required string pricing_snapshot_id,
}

struct ModelRuntimeSnapshotDTO {
  1: required string model_id,
  2: required string display_name,
  3: required string resource_type,
  4: required string pricing_snapshot_id,
  5: required string provider_runtime_ref,
  6: required i32 timeout_ms,
  7: optional map<string,string> retry_policy,
  8: optional map<string,string> runtime_parameters,
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
  10: required SafetyEvidenceDTO safety_evidence,
}

struct EstimateGenerationCreditsResponse {
  1: required string estimate_id,
  2: required i64 estimate_points,
  3: required i64 available_points,
  4: required i64 expires_soon_points,
  5: required string credit_account_scope,
  6: required string credit_account_id,
  7: required list<CreditEstimateLineItemDTO> line_items,
  8: required string expires_at,
  9: required bool insufficient,
}

struct EstimateToolCreditsRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string project_id,
  4: required list<ToolUsageEstimateItemInput> tool_usage_items,
  5: required SafetyEvidenceDTO safety_evidence,
}

struct EstimateToolCreditsResponse {
  1: required string estimate_id,
  2: required i64 estimate_points,
  3: required i64 available_points,
  4: required i64 expires_soon_points,
  5: required string credit_account_scope,
  6: required string credit_account_id,
  7: required list<CreditEstimateLineItemDTO> line_items,
  8: required string expires_at,
  9: required bool insufficient,
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

struct GeneratedAssetObjectInput {
  1: required string artifact_id,
  2: required string resource_type,
  3: required string filename,
  4: required string content_type,
  5: required i64 size_bytes,
  6: optional string checksum,
  7: optional map<string,string> metadata_summary,
}

struct PrepareGeneratedAssetObjectsRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string project_id,
  4: required string session_id,
  5: required string run_id,
  6: required list<GeneratedAssetObjectInput> artifacts,
}

struct GeneratedAssetUploadSlot {
  1: required string artifact_id,
  2: required string bucket,
  3: required string object_key,
  4: required string upload_url,
  5: required map<string,string> upload_headers,
  6: required string expires_at,
  7: required i64 max_size_bytes,
}

struct PrepareGeneratedAssetObjectsResponse {
  1: required list<GeneratedAssetUploadSlot> upload_slots,
}

struct GeneratedStorageObjectRef {
  1: required string object_key,
  2: required string bucket,
  3: required string content_type,
  4: required i64 size_bytes,
  5: required string checksum,
  6: optional string etag,
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
  11: required GeneratedStorageObjectRef storage_object_ref,
}

struct GeneratedAssetElementInput {
  1: required string element_type,
  2: required string element_payload_json,
  3: required i32 display_order,
  4: optional string source_tool_call_id,
}

struct CommitGeneratedAssetAndChargeRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string project_id,
  4: required string session_id,
  5: required string run_id,
  6: required string freeze_id,
  7: required list<CommitArtifactDTO> artifacts,
  8: required list<GeneratedAssetElementInput> final_elements,
  9: required SafetyEvidenceDTO safety_evidence,
  10: optional string estimate_id,
}

struct CommittedAssetRefDTO {
  1: required string asset_id,
  2: required string source_artifact_id,
  3: required string resource_type,
  4: required string asset_type,
  5: required string status,
  6: optional string preview_url,
  7: optional string elements_summary_json,
}

struct CommitGeneratedAssetAndChargeResponse {
  1: required list<CommittedAssetRefDTO> asset_refs,
  2: required i64 charged_points,
  3: required i64 released_points,
  4: required string commit_status,
  5: optional string ledger_ref,
  6: optional list<ChargedLineItemDTO> charged_line_items,
}

struct ListAssetElementTypesRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: optional i32 page_size,
  4: optional string schema_version,
}

struct AssetElementTypeDTO {
  1: required string element_type,
  2: required string display_name,
  3: required string category,
  4: required string schema_version,
  5: required string schema_hint_json,
  6: optional string render_hint_json,
  7: required bool active,
  8: required i32 sort_order,
  9: required string resource_type,
  10: required string status,
  11: required string usage_stage,
  12: required bool draft_enabled,
  13: required bool final_enabled,
  14: required bool editable,
  15: required bool referable,
  16: optional string render_hint,
}

struct ListAssetElementTypesResponse {
  1: required list<AssetElementTypeDTO> element_types,
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
  8: required string confirmation_policy_json,
  9: optional string test_input_json,
  10: optional string expected_elements_json,
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

struct EnterpriseSummaryDTO {
  1: required string enterprise_id,
  2: required string space_id,
  3: required string name,
  4: required string owner_user_id,
  5: optional string current_user_role,
  6: required string status,
  7: required i64 member_count,
}

struct PreviewTransferOwnerRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string target_member_id,
  4: required string reason,
}

struct TransferOwnerPreviewDTO {
  1: required string preview_token,
  2: required list<string> impact_items,
  3: required string expires_at,
}

struct ConfirmTransferOwnerRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string target_member_id,
  4: required string reason,
  5: required string preview_token,
}

struct PlatformAdminDTO {
  1: required string admin_id,
  2: required string account,
  3: required string status,
  4: required bool must_rotate_password,
  5: required string created_at,
}

struct CreateAdminRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string account,
  4: required string initial_password,
  5: required string reason,
}

struct DisableAdminRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string admin_id,
  4: required string reason,
}

struct PreviewSetUserStatusRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string target_user_id,
  4: required string target_status,
  5: required string reason,
}

struct UserStatusPreviewDTO {
  1: required string preview_token,
  2: required string current_status,
  3: required string target_status,
  4: required list<string> impact_summary,
  5: required bool public_content_retained,
  6: required bool private_content_not_exposed,
  7: required string expires_at,
}

struct ConfirmSetUserStatusRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string target_user_id,
  4: required string target_status,
  5: required string reason,
  6: required string preview_token,
}

struct AdminUserSummaryDTO {
  1: required string user_id,
  2: required string status,
  3: required string public_nickname,
  4: optional string email_masked,
  5: optional string phone_masked,
  6: optional string personal_space_id,
  7: required string registered_at,
  8: optional string last_login_at,
}

struct CreateProjectRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string title,
  4: optional string initial_prompt_digest,
  5: optional string source,
  6: optional string space_id,
}

struct ProjectDetailDTO {
  1: required string project_id,
  2: required string title,
  3: optional string description,
  4: optional string cover_asset_id,
  5: required string status,
  6: required bool creative_allowed,
  7: required list<string> allowed_actions,
  8: optional string agent_session_query_ref,
  9: required string updated_at,
}

struct UpdateProjectTitleRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string project_id,
  4: required string title,
  5: optional string base_updated_at,
}

struct AttachAssetToProjectRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string project_id,
  4: required string asset_id,
  5: optional string asset_role,
  6: optional string source_session_id,
  7: optional string source_run_id,
  8: optional string source_artifact_id,
  9: optional string source_type,
  10: optional i32 display_order,
}

struct ProjectAssetDTO {
  1: required string asset_id,
  2: required string source_type,
  3: optional string source_session_id,
  4: optional string source_run_id,
  5: required string created_at,
}

struct CreateUploadIntentRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string project_id,
  4: required string asset_type,
  5: required string filename,
  6: required string content_type,
  7: required i64 size_bytes,
  8: optional string checksum,
  9: optional string metadata_text,
  10: required SafetyEvidenceDTO safety_evidence,
}

struct UploadIntentDTO {
  1: required string upload_intent_id,
  2: required string asset_id,
  3: required string bucket,
  4: required string object_key,
  5: required string upload_url,
  6: required map<string,string> upload_headers,
  7: required string expires_at,
  8: required i64 max_size_bytes,
  9: required string content_type,
}

struct ConfirmUploadedAssetRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string upload_intent_id,
  4: required string etag,
  5: required i64 size_bytes,
  6: required string content_type,
  7: required string checksum,
}

struct AssetDetailDTO {
  1: required string asset_id,
  2: required string asset_type,
  3: required string status,
  4: optional string project_id,
  5: optional string preview_url,
  6: required list<string> access_actions,
}

struct CreateWorkRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string project_id,
  4: required string title,
  5: optional string description,
  6: required list<string> asset_ids,
  7: optional string cover_asset_id,
  8: optional string category,
  9: optional list<string> tags,
}

struct WorkDetailDTO {
  1: required string work_id,
  2: required string project_id,
  3: required string title,
  4: optional string description,
  5: required string share_status,
  6: optional string cover_asset_id,
  7: optional string category,
  8: required list<string> tags,
  9: required list<string> asset_ids,
  10: required list<string> allowed_actions,
  11: required string updated_at,
}

struct PreviewShareWorkRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string work_id,
  4: required string public_title,
  5: optional string public_description,
  6: optional list<string> tags,
  7: required SafetyEvidenceDTO safety_evidence,
}

struct ShareWorkPreviewDTO {
  1: required string preview_token,
  2: required string work_id,
  3: required string public_title,
  4: required string public_description_digest,
  5: required list<string> tags,
  6: required list<string> privacy_redaction_summary,
  7: required list<string> public_media_summary,
  8: required string expires_at,
}

struct ConfirmShareWorkRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string work_id,
  4: required string preview_token,
}

struct WorkShareResultDTO {
  1: required string work_id,
  2: required string public_work_id,
  3: required string share_url,
  4: required string share_status,
  5: required string snapshot_id,
}

struct PreviewTakeDownPublicWorkRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string public_work_id,
  4: required string reason,
  5: required bool notify_author,
}

struct TakeDownPublicWorkPreviewDTO {
  1: required string preview_token,
  2: required string public_work_id,
  3: required string work_id,
  4: required string current_status,
  5: required list<string> impact_items,
  6: required bool public_link_will_be_inaccessible,
  7: required bool source_asset_retained,
  8: required bool notify_author,
  9: required string expires_at,
}

struct ConfirmTakeDownPublicWorkRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string public_work_id,
  4: required string preview_token,
  5: required string reason,
  6: required bool notify_author,
}

struct AdminPublicWorkDTO {
  1: required string public_work_id,
  2: required string work_id,
  3: required string title,
  4: optional map<string,string> author_summary,
  5: required string status,
  6: required string published_at,
  7: optional string taken_down_at,
  8: optional string notification_status,
}

struct ListPublicWorksRequest {
  1: optional string category,
  2: optional string tag,
  3: optional string resource_type,
  4: optional string sort_by,
  5: optional i32 page_size,
  6: optional i32 offset,
}

struct PublicWorkCardDTO {
  1: required string public_work_id,
  2: required string title,
  3: optional string cover_url,
  4: required string share_url,
  5: optional string category,
  6: required list<string> tags,
  7: optional string resource_type,
  8: required i64 like_count,
  9: required string published_at,
}

struct ListPublicWorksResponse {
  1: required list<PublicWorkCardDTO> items,
  2: required i32 limit,
  3: required i32 offset,
  4: required i64 total,
}

struct GetPublicWorkRequest {
  1: required string public_work_id,
  2: optional AuthContext auth_context,
  3: required RequestMeta request_meta,
}

struct PublicWorkDetailDTO {
  1: required string public_work_id,
  2: required string title,
  3: optional string description,
  4: required string share_url,
  5: required list<map<string,string>> public_media_refs,
  6: required string author_display_name,
  7: optional string category,
  8: required list<string> tags,
  9: required i64 like_count,
  10: required bool liked_by_current_user,
}

struct CreateNotificationRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string recipient_user_id,
  4: optional string recipient_space_id,
  5: optional string recipient_enterprise_id,
  6: required string type,
  7: required string title,
  8: required string summary,
  9: optional string body,
  10: required map<string,string> navigation_hint,
  11: optional string related_resource_type,
  12: optional string related_resource_id,
}

struct NotificationDTO {
  1: required string notification_id,
  2: required string type,
  3: required string title,
  4: required string summary,
  5: optional string body,
  6: required map<string,string> navigation_hint,
  7: optional string read_at,
  8: required string created_at,
  9: optional string related_resource_type,
  10: optional string related_resource_id,
}

struct ListNotificationsRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: optional string type,
  4: optional string read_state,
  5: optional i32 page_size,
  6: optional i32 offset,
}

struct ListNotificationsResponse {
  1: required list<NotificationDTO> items,
  2: required i32 limit,
  3: required i32 offset,
  4: required i64 total,
}

struct GetUnreadCountRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
}

struct UnreadCountDTO {
  1: required i64 unread_count,
}

struct MarkNotificationReadRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: required string notification_id,
}

struct MarkAllNotificationsReadRequest {
  1: required AuthContext auth_context,
  2: required RequestMeta request_meta,
  3: optional string type,
}

service AccountSpaceService {
  ResolveCurrentSpaceContextResponse ResolveCurrentSpaceContext(1: ResolveCurrentSpaceContextRequest req)
  ResolveAuthContextFromTokenResponse ResolveAuthContextFromToken(1: ResolveAuthContextFromTokenRequest req)
}

service EnterpriseService {
  TransferOwnerPreviewDTO PreviewTransferOwner(1: PreviewTransferOwnerRequest req)
  EnterpriseSummaryDTO ConfirmTransferOwner(1: ConfirmTransferOwnerRequest req)
}

service AdminService {
  PlatformAdminDTO CreateAdmin(1: CreateAdminRequest req)
  PlatformAdminDTO DisableAdmin(1: DisableAdminRequest req)
}

service UserAdminService {
  UserStatusPreviewDTO PreviewSetUserStatus(1: PreviewSetUserStatusRequest req)
  AdminUserSummaryDTO ConfirmSetUserStatus(1: ConfirmSetUserStatusRequest req)
}

service ProjectService {
  ProjectAccessResponse CheckProjectAccess(1: CheckProjectAccessRequest req)
  ProjectDetailDTO CreateProject(1: CreateProjectRequest req)
  ProjectDetailDTO UpdateProjectTitle(1: UpdateProjectTitleRequest req)
}

service ProjectAssetService {
  ProjectAssetDTO AttachAssetToProject(1: AttachAssetToProjectRequest req)
}

service AssetService {
  UploadIntentDTO CreateUploadIntent(1: CreateUploadIntentRequest req)
  AssetDetailDTO ConfirmUploadedAsset(1: ConfirmUploadedAssetRequest req)
  BatchCheckAssetAccessResponse BatchCheckAssetAccess(1: BatchCheckAssetAccessRequest req)
  PrepareGeneratedAssetObjectsResponse PrepareGeneratedAssetObjects(1: PrepareGeneratedAssetObjectsRequest req)
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
  ModelRuntimeSnapshotDTO ResolveGenerationModelSnapshot(1: ResolveGenerationModelSnapshotRequest req)
}

service PlatformDictionaryService {
  ListAssetElementTypesResponse ListAssetElementTypes(1: ListAssetElementTypesRequest req)
}

service WorkService {
  WorkDetailDTO CreateWork(1: CreateWorkRequest req)
}

service WorkShareService {
  ShareWorkPreviewDTO PreviewShareWork(1: PreviewShareWorkRequest req)
  WorkShareResultDTO ConfirmShareWork(1: ConfirmShareWorkRequest req)
}

service FeaturedWorkAdminService {
  TakeDownPublicWorkPreviewDTO PreviewTakeDownWork(1: PreviewTakeDownPublicWorkRequest req)
  AdminPublicWorkDTO ConfirmTakeDownWork(1: ConfirmTakeDownPublicWorkRequest req)
}

service PublicContentService {
  ListPublicWorksResponse ListPublicWorks(1: ListPublicWorksRequest req)
  PublicWorkDetailDTO GetPublicWork(1: GetPublicWorkRequest req)
}

service NotificationService {
  NotificationDTO CreateNotification(1: CreateNotificationRequest req)
  ListNotificationsResponse ListNotifications(1: ListNotificationsRequest req)
  UnreadCountDTO GetUnreadCount(1: GetUnreadCountRequest req)
  NotificationDTO MarkNotificationRead(1: MarkNotificationReadRequest req)
  UnreadCountDTO MarkAllNotificationsRead(1: MarkAllNotificationsReadRequest req)
}
