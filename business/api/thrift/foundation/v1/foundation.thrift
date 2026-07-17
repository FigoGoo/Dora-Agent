namespace go foundationv1

const string FOUNDATION_SCHEMA_VERSION = "foundation.rpc.v1"
const string BUSINESS_FOUNDATION_SERVICE_NAME = "dora.business.foundation.v1"
const string CREATION_SPEC_PREVIEW_SCHEMA_VERSION = "creation_spec.preview.rpc.v1"
const string ASSET_ANALYSIS_INPUTS_PREVIEW_RPC_SCHEMA_VERSION = "asset_analysis_inputs.preview.rpc.v1"
const string STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION = "storyboard.preview.rpc.v1"
const string PLAN_STORYBOARD_RUNTIME_PROFILE = "plan_storyboard.runtime.v2preview1"
const string PROMPT_PREVIEW_RPC_SCHEMA_VERSION = "prompt.preview.rpc.v1"
const string WRITE_PROMPTS_RUNTIME_PROFILE = "write_prompts.runtime.v2preview1"

enum CreationSpecPreviewDeliverableTypeV1 {
    VIDEO = 1,
    IMAGE_SET = 2,
    AUDIO = 3,
    MIXED = 4,
}

enum CreationSpecPreviewCommandDispositionV1 {
    CREATED = 1,
    REPLAYED = 2,
}

enum CreationSpecPreviewQueryStatusV1 {
    NOT_FOUND = 1,
    COMPLETED = 2,
    CONFLICT = 3,
}

enum AssetAnalysisPreviewMediaTypeV1 {
    TEXT = 1,
    IMAGE = 2,
}

enum AssetAnalysisPreviewEvidenceKindV1 {
    TEXT_SEGMENT = 1,
    VISUAL_DESCRIPTION = 2,
    SAFETY_LABEL = 3,
}

enum AssetAnalysisPreviewAvailabilityV1 {
    READY = 1,
    MISSING = 2,
    FAILED = 3,
    REDACTED = 4,
    UNSUPPORTED = 5,
}

enum AssetAnalysisPreviewLocatorKindV1 {
    TEXT_RANGE = 1,
    IMAGE_WHOLE = 2,
    IMAGE_REGION = 3,
}

enum StoryboardPreviewElementTypeV1 {
    SCENE = 1,
    SHOT = 2,
    NARRATION = 3,
    CAPTION = 4,
    AUDIO = 5,
}

enum StoryboardPreviewSlotTypeV1 {
    IMAGE = 1,
    VIDEO = 2,
    AUDIO = 3,
    VOICEOVER = 4,
    CAPTION = 5,
}

enum StoryboardPreviewCommandDispositionV1 {
    CREATED = 1,
    REPLAYED = 2,
}

enum StoryboardPreviewQueryStatusV1 {
    NOT_FOUND = 1,
    COMPLETED = 2,
    CONFLICT = 3,
}

enum PromptPreviewMediaKindV1 {
    IMAGE = 1,
    VIDEO = 2,
    AUDIO = 3,
    TEXT = 4,
}

enum PromptPreviewCommandDispositionV1 {
    CREATED = 1,
    REPLAYED = 2,
}

enum PromptPreviewQueryStatusV1 {
    NOT_FOUND = 1,
    COMPLETED = 2,
    CONFLICT = 3,
}

struct FoundationProbeRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string caller_service
    4: required string caller_version
    5: required i64 sent_at_unix_ms
}

struct FoundationProbeResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required string service_name
    4: required string service_version
    5: required string environment
    6: required string instance_id
    7: required i64 received_at_unix_ms
    8: optional bool plan_storyboard_runtime_enabled
    9: optional string plan_storyboard_runtime_profile
    10: optional bool write_prompts_runtime_enabled
    11: optional string write_prompts_runtime_profile
}

struct CreationSpecPreviewPhaseV1 {
    1: required string key
    2: required string title
    3: required string objective
    4: required string output
}

struct CreationSpecPreviewContentV1 {
    1: required string title
    2: required string goal
    3: required CreationSpecPreviewDeliverableTypeV1 deliverable_type
    4: required string audience
    5: required string locale
    6: required list<CreationSpecPreviewPhaseV1> phases
    7: required list<string> constraints
    8: required list<string> acceptance_criteria
}

struct GetCreationSpecContextPreviewRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string user_id
    4: required string project_id
}

struct GetCreationSpecContextPreviewResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required string project_id
    4: required i64 project_version
    5: required string project_title
}

struct CreationSpecDraftPreviewResourceV1 {
    1: required string creation_spec_id
    2: required string project_id
    3: required i64 version
    4: required string status
    5: required string content_digest
    6: required CreationSpecPreviewContentV1 content
}

struct SaveCreationSpecDraftPreviewRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required string request_digest
    5: required string user_id
    6: required string project_id
    7: required i64 expected_project_version
    8: required string tool_call_id
    9: required string prompt_version
    10: required string validator_version
    11: required CreationSpecPreviewContentV1 content
}

struct SaveCreationSpecDraftPreviewResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required CreationSpecPreviewCommandDispositionV1 disposition
    5: required CreationSpecDraftPreviewResourceV1 resource
}

struct QueryCreationSpecDraftCommandPreviewRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required string request_digest
    5: required string user_id
    6: required string project_id
}

struct QueryCreationSpecDraftCommandPreviewResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required CreationSpecPreviewQueryStatusV1 status
    5: optional CreationSpecDraftPreviewResourceV1 resource
}

struct AssetAnalysisPreviewTargetV1 {
    1: required string asset_id
    2: optional i64 expected_asset_version
}

struct AssetAnalysisPreviewLocatorV1 {
    1: required AssetAnalysisPreviewLocatorKindV1 kind
    2: optional i64 text_start
    3: optional i64 text_end
    4: optional i64 text_source_length
    5: optional i32 image_x
    6: optional i32 image_y
    7: optional i32 image_width
    8: optional i32 image_height
}

struct AssetAnalysisPreviewEvidenceV1 {
    1: required string evidence_id
    2: required string asset_id
    3: required i64 asset_version
    4: required AssetAnalysisPreviewMediaTypeV1 media_type
    5: required AssetAnalysisPreviewEvidenceKindV1 evidence_kind
    6: required AssetAnalysisPreviewAvailabilityV1 availability
    7: optional string reason_code
    8: optional string content_digest
    9: optional string extractor_schema_version
    10: optional string extractor_version
    11: optional AssetAnalysisPreviewLocatorV1 locator
    12: optional string content
}

struct AssetAnalysisPreviewAssetV1 {
    1: required string asset_id
    2: required i64 asset_version
    3: required AssetAnalysisPreviewMediaTypeV1 media_type
    4: required list<AssetAnalysisPreviewEvidenceV1> evidence
}

struct BatchGetAssetAnalysisInputsPreviewRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string user_id
    4: required string project_id
    5: required list<AssetAnalysisPreviewTargetV1> targets
}

struct BatchGetAssetAnalysisInputsPreviewResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required string snapshot_token
    4: required bool response_complete
    5: required list<AssetAnalysisPreviewAssetV1> assets
}

struct StoryboardPreviewCreationSpecRefV1 {
    1: required string creation_spec_id
    2: required i64 version
    3: required string content_digest
}

struct GetStoryboardPlanningContextPreviewRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string user_id
    4: required string project_id
    5: required StoryboardPreviewCreationSpecRefV1 creation_spec_ref
}

struct GetStoryboardPlanningContextPreviewResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required string project_id
    4: required i64 project_version
    5: required string project_title
    6: required CreationSpecDraftPreviewResourceV1 creation_spec
}

struct StoryboardPreviewSectionV1 {
    1: required string key
    2: required string title
    3: required string objective
}

struct StoryboardPreviewElementV1 {
    1: required string key
    2: required string section_key
    3: required i32 order
    4: required StoryboardPreviewElementTypeV1 element_type
    5: required string title
    6: required string narrative_purpose
    7: required i32 duration_seconds
    8: required string source_phase_key
    9: required list<string> dependency_keys
}

struct StoryboardPreviewSlotV1 {
    1: required string key
    2: required string element_key
    3: required StoryboardPreviewSlotTypeV1 slot_type
    4: required string purpose
    5: required bool required
}

struct StoryboardPreviewContentV1 {
    1: required string title
    2: required string summary
    3: required list<StoryboardPreviewSectionV1> sections
    4: required list<StoryboardPreviewElementV1> elements
    5: required list<StoryboardPreviewSlotV1> slots
}

struct StoryboardDraftPreviewResourceV1 {
    1: required string storyboard_preview_id
    2: required string project_id
    3: required StoryboardPreviewCreationSpecRefV1 creation_spec_ref
    4: required i64 version
    5: required string status
    6: required string content_digest
    7: required StoryboardPreviewContentV1 content
}

struct SaveStoryboardDraftPreviewRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required string request_digest
    5: required string user_id
    6: required string project_id
    7: required i64 expected_project_version
    8: required StoryboardPreviewCreationSpecRefV1 creation_spec_ref
    9: required string tool_call_id
    10: required string prompt_version
    11: required string validator_version
    12: required StoryboardPreviewContentV1 content
}

struct SaveStoryboardDraftPreviewResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required StoryboardPreviewCommandDispositionV1 disposition
    5: required StoryboardDraftPreviewResourceV1 resource
}

struct QueryStoryboardDraftCommandPreviewRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required string request_digest
    5: required string user_id
    6: required string project_id
}

struct QueryStoryboardDraftCommandPreviewResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required StoryboardPreviewQueryStatusV1 status
    5: optional StoryboardDraftPreviewResourceV1 resource
}

// PromptPreviewStoryboardRefV1 是 Prompt Draft 绑定的精确 Storyboard Preview 引用。
struct PromptPreviewStoryboardRefV1 {
    1: required string storyboard_preview_id
    2: required i64 version
    3: required string content_digest
}

// PromptGenerationStoryboardElementPreviewV1 只投影 Prompt 写作需要的安全 Element 字段。
struct PromptGenerationStoryboardElementPreviewV1 {
    1: required string element_local_key
    2: required i32 order
    3: required string title
    4: required string narrative_purpose
}

// PromptGenerationStoryboardSlotPreviewV1 只投影全部 Prompt 目标需要的安全 Slot 字段。
struct PromptGenerationStoryboardSlotPreviewV1 {
    1: required string target_local_key
    2: required string element_local_key
    3: required StoryboardPreviewSlotTypeV1 slot_type
    4: required string purpose
    5: required bool required
}

// PromptGenerationStoryboardContentPreviewV1 是 Source 完整摘要的最小安全投影；content_digest 仍属于完整 Business Aggregate。
struct PromptGenerationStoryboardContentPreviewV1 {
    1: required string title
    2: required string summary
    3: required list<PromptGenerationStoryboardElementPreviewV1> elements
    4: required list<PromptGenerationStoryboardSlotPreviewV1> slots
}

struct PromptGenerationStoryboardResourcePreviewV1 {
    1: required string storyboard_preview_id
    2: required string project_id
    3: required i64 version
    4: required string status
    5: required string schema_version
    6: required string content_digest
    7: required PromptGenerationStoryboardContentPreviewV1 content
}

struct GetPromptGenerationContextPreviewRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string user_id
    4: required string project_id
    5: required PromptPreviewStoryboardRefV1 storyboard_preview_ref
}

struct GetPromptGenerationContextPreviewResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required string project_id
    4: required i64 project_version
    5: required string project_title
    6: required PromptGenerationStoryboardResourcePreviewV1 storyboard_preview
}

// PromptPreviewDraftEntryV1 保存双 Validator 通过且已回填可信目标字段的单项 Prompt。
struct PromptPreviewDraftEntryV1 {
    1: required string target_local_key
    2: required string element_local_key
    3: required StoryboardPreviewSlotTypeV1 slot_type
    4: required PromptPreviewMediaKindV1 media_kind
    5: required string purpose
    6: required bool required
    7: required string positive_prompt
    8: required list<string> negative_constraints
    9: required string output_language
}

struct PromptPreviewDraftContentV1 {
    1: required string schema_version
    2: required string mode
    3: required PromptPreviewStoryboardRefV1 source_storyboard_preview_ref
    4: required list<PromptPreviewDraftEntryV1> prompts
}

struct PromptPreviewDraftResourceV1 {
    1: required string prompt_preview_id
    2: required string project_id
    3: required PromptPreviewStoryboardRefV1 storyboard_preview_ref
    4: required i64 version
    5: required string status
    6: required string content_digest
    7: required string exact_target_set_digest
    8: required PromptPreviewDraftContentV1 content
}

struct SavePromptDraftPreviewRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required string request_digest
    5: required string user_id
    6: required string project_id
    7: required i64 expected_project_version
    8: required PromptPreviewStoryboardRefV1 storyboard_preview_ref
    9: required string tool_call_id
    10: required string prompt_version
    11: required string validator_version
    12: required string exact_set_validator_version
    13: required string exact_target_set_digest
    14: required PromptPreviewDraftContentV1 content
}

struct SavePromptDraftPreviewResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required PromptPreviewCommandDispositionV1 disposition
    5: required PromptPreviewDraftResourceV1 resource
}

struct QueryPromptDraftCommandPreviewRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required string request_digest
    5: required string user_id
    6: required string project_id
}

struct QueryPromptDraftCommandPreviewResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required PromptPreviewQueryStatusV1 status
    5: optional PromptPreviewDraftResourceV1 resource
}

exception FoundationServiceExceptionV1 {
    1: required string code
    2: required string message
    3: required bool retryable
}

service BusinessFoundationServiceV1 {
    FoundationProbeResponseV1 Probe(1: FoundationProbeRequestV1 request)
        throws (1: FoundationServiceExceptionV1 service_error)

    GetCreationSpecContextPreviewResponseV1 GetCreationSpecContextPreviewV1(
        1: GetCreationSpecContextPreviewRequestV1 request
    ) throws (1: FoundationServiceExceptionV1 service_error)

    SaveCreationSpecDraftPreviewResponseV1 SaveCreationSpecDraftPreviewV1(
        1: SaveCreationSpecDraftPreviewRequestV1 request
    ) throws (1: FoundationServiceExceptionV1 service_error)

    QueryCreationSpecDraftCommandPreviewResponseV1 QueryCreationSpecDraftCommandPreviewV1(
        1: QueryCreationSpecDraftCommandPreviewRequestV1 request
    ) throws (1: FoundationServiceExceptionV1 service_error)

    BatchGetAssetAnalysisInputsPreviewResponseV1 BatchGetAssetAnalysisInputsPreviewV1(
        1: BatchGetAssetAnalysisInputsPreviewRequestV1 request
    ) throws (1: FoundationServiceExceptionV1 service_error)

    GetStoryboardPlanningContextPreviewResponseV1 GetStoryboardPlanningContextPreviewV1(
        1: GetStoryboardPlanningContextPreviewRequestV1 request
    ) throws (1: FoundationServiceExceptionV1 service_error)

    SaveStoryboardDraftPreviewResponseV1 SaveStoryboardDraftPreviewV1(
        1: SaveStoryboardDraftPreviewRequestV1 request
    ) throws (1: FoundationServiceExceptionV1 service_error)

    QueryStoryboardDraftCommandPreviewResponseV1 QueryStoryboardDraftCommandPreviewV1(
        1: QueryStoryboardDraftCommandPreviewRequestV1 request
    ) throws (1: FoundationServiceExceptionV1 service_error)

    GetPromptGenerationContextPreviewResponseV1 GetPromptGenerationContextPreviewV1(
        1: GetPromptGenerationContextPreviewRequestV1 request
    ) throws (1: FoundationServiceExceptionV1 service_error)

    SavePromptDraftPreviewResponseV1 SavePromptDraftPreviewV1(
        1: SavePromptDraftPreviewRequestV1 request
    ) throws (1: FoundationServiceExceptionV1 service_error)

    QueryPromptDraftCommandPreviewResponseV1 QueryPromptDraftCommandPreviewV1(
        1: QueryPromptDraftCommandPreviewRequestV1 request
    ) throws (1: FoundationServiceExceptionV1 service_error)
}
