namespace go dora.api.businessasset

include "business_agent_service.thrift"

enum AssetCommitStatus {
  COMMITTED = 1,
  PARTIALLY_COMMITTED = 2,
  FAILED = 3,
}

struct GeneratedAssetDTO {
  1: required string asset_id,
  2: required string project_id,
  3: required string run_id,
  4: required string tool_task_id,
  5: required string resource_type,
  6: required string status,
  7: required string tos_object_key,
  8: required string asset_digest,
  9: required string created_at,
  10: optional string preview_url,
}

struct CommitGeneratedAssetsRequest {
  1: required business_agent_service.AuthContext auth_context,
  2: required business_agent_service.RequestMeta request_meta,
  3: required string project_id,
  4: required string run_id,
  5: required string tool_task_id,
  6: required string tool_result_digest,
  7: required list<GeneratedAssetDTO> assets,
}

struct CommitGeneratedAssetsResponse {
  1: required string commit_record_id,
  2: required AssetCommitStatus status,
  3: required list<GeneratedAssetDTO> committed_assets,
  4: required i32 failed_asset_count,
  5: required string commit_digest,
}

service BusinessAssetService {
  CommitGeneratedAssetsResponse CommitGeneratedAssets(1: CommitGeneratedAssetsRequest request)
}
