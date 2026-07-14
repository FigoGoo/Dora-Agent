namespace go sessionv1

// Session RPC 总体版本和两个方法的独立 Schema 版本；方法升级不得复用已发布字段编号。
const string SESSION_RPC_SCHEMA_VERSION = "session.rpc.v1"
const string ENSURE_PROJECT_SESSION_SCHEMA_VERSION = "ensure_project_session.v1"
const string QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION = "query_project_session_command.v1"
const string AGENT_SESSION_SERVICE_NAME = "dora.agent.session.v1"
const string ENSURE_PROJECT_SESSION_SCHEMA_VERSION_V2 = "ensure_project_session.v2"
const string QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION_V2 = "query_project_session_command.v2"
const string SESSION_SKILL_SNAPSHOT_SCHEMA_VERSION_V1 = "session_skill_snapshot.v1"
const string SKILL_RUNTIME_CONTENT_SCHEMA_VERSION_V1 = "skill_runtime_content.v1"

enum CreationSourceV1 {
    QUICK_CREATE = 1
}

enum SkillSnapshotModeV1 {
    EMPTY = 1
}

enum EnsureDispositionV1 {
    CREATED = 1
    REPLAYED = 2
}

enum QueryProjectSessionCommandStatusV1 {
    NOT_FOUND = 1
    COMPLETED = 2
    CONFLICT = 3
}

// EnsureProjectSessionRequestV1 是 Business Outbox 派发的幂等 Session 建立命令。
// initial_prompt 是敏感可选正文，禁止进入日志；两个 Digest 都必须由 Agent 独立重算。
struct EnsureProjectSessionRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required string request_digest
    5: required string project_id
    6: required string owner_user_id
    7: required CreationSourceV1 creation_source
    8: optional string initial_prompt
    9: required string prompt_digest
    10: required SkillSnapshotModeV1 skill_snapshot_mode
    11: required i64 requested_at_unix_ms
}

// ProjectSessionReceiptV1 是 Agent PostgreSQL 冻结的安全回执，不包含 Prompt、Digest 或密文。
struct ProjectSessionReceiptV1 {
    1: required string command_id
    2: required string session_id
    3: optional string message_id
    4: optional string input_id
    5: required i32 result_version
    6: required i64 completed_at_unix_ms
}

// EnsureProjectSessionResponseV1 返回首次创建或同语义重放结果。
struct EnsureProjectSessionResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required EnsureDispositionV1 disposition
    4: required ProjectSessionReceiptV1 receipt
}

// QueryProjectSessionCommandRequestV1 用于 Unknown Outcome 后按原命令和预期摘要核对权威状态。
struct QueryProjectSessionCommandRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required string expected_request_digest
}

// QueryProjectSessionCommandResponseV1 返回严格三态；只有 completed 可以携带 Receipt。
struct QueryProjectSessionCommandResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required QueryProjectSessionCommandStatusV1 status
    4: optional ProjectSessionReceiptV1 receipt
}

// SessionSkillSnapshotKindV1 区分显式空集合和包含不可变发布引用的 Session Skill 快照。
enum SessionSkillSnapshotKindV1 {
    EMPTY = 1
    PUBLISHED_REFS = 2
}

// SkillNamespaceV1 是 Business 冻结的 Skill 命名空间；W1 Producer 只发送 USER。
enum SkillNamespaceV1 {
    SYSTEM = 1
    USER = 2
}

// SkillGuidanceApplicabilityV1 固定一个能力指导是否适用于当前 Skill。
enum SkillGuidanceApplicabilityV1 {
    ENABLED = 1
    NOT_APPLICABLE = 2
}

// CapabilityGuidanceV1 保存一个固定 Graph Tool 能力的互斥指导字段。
struct CapabilityGuidanceV1 {
    1: required SkillGuidanceApplicabilityV1 applicability
    2: required string guidance
    3: required string not_applicable_reason
}

// SkillExampleV1 保存已经由 Business 规范排序的示例输入与输出。
struct SkillExampleV1 {
    1: required string input
    2: required string output
}

// SkillRuntimeContentV1 是允许 Agent 内联加载的最小发布内容，不包含权限、价格或市场字段。
struct SkillRuntimeContentV1 {
    1: required string schema_version
    2: required string name
    3: required string input_description
    4: required string output_description
    5: required string invocation_rules
    6: required CapabilityGuidanceV1 plan_creation_spec
    7: required CapabilityGuidanceV1 analyze_materials
    8: required CapabilityGuidanceV1 plan_storyboard
    9: required CapabilityGuidanceV1 generate_media
    10: required CapabilityGuidanceV1 write_prompts
    11: required CapabilityGuidanceV1 assemble_output
    12: required list<SkillExampleV1> examples
    13: required list<string> starter_prompts
}

// PublicToolSnapshotRefV1 为未来公共 Tool 契约保留强类型引用；W1 必须发送非 nil 空列表。
struct PublicToolSnapshotRefV1 {
    1: required string ref_id
    2: required string ref_digest
}

// PublishedSkillSnapshotRefV1 冻结一次发布、Runtime 内容、权限证明和能力声明。
struct PublishedSkillSnapshotRefV1 {
    1: required i32 load_order
    2: required i32 priority
    3: required SkillNamespaceV1 namespace
    4: required string skill_id
    5: required string publisher_user_id
    6: required string published_snapshot_id
    7: required i64 publication_revision
    8: required string definition_schema_version
    9: required string content_digest
    10: required string runtime_content_schema_version
    11: required string runtime_content_digest
    12: required SkillRuntimeContentV1 runtime_content
    13: required list<string> allowed_graph_tool_keys
    14: required list<PublicToolSnapshotRefV1> public_tool_refs
    15: required string permission_snapshot_digest
    16: required string runtime_policy_ref
    17: required i64 governance_epoch
    18: required i64 published_at_unix_ms
}

// SessionSkillSnapshotV1 是 Business 在 QuickCreate v2 事务中冻结的完整 Skill 集合。
struct SessionSkillSnapshotV1 {
    1: required string schema_version
    2: required SessionSkillSnapshotKindV1 snapshot_kind
    3: required i32 skill_count
    4: required string snapshot_set_digest
    5: required list<PublishedSkillSnapshotRefV1> skills
}

// EnsureProjectSessionRequestV2 追加不可变 Skill Snapshot，且不改变 V1 任一字段的字节语义。
struct EnsureProjectSessionRequestV2 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required string request_digest
    5: required string project_id
    6: required string owner_user_id
    7: required CreationSourceV1 creation_source
    8: optional string initial_prompt
    9: required string prompt_digest
    10: required SessionSkillSnapshotV1 skill_snapshot
    11: required i64 requested_at_unix_ms
}

// ProjectSessionReceiptV2 冻结 V2 Session 结果及其 Skill Snapshot 摘要，不包含 Runtime 明文。
struct ProjectSessionReceiptV2 {
    1: required string command_id
    2: required string session_id
    3: optional string message_id
    4: optional string input_id
    5: required i32 result_version
    6: required i64 completed_at_unix_ms
    7: required string skill_snapshot_digest
    8: required i32 skill_count
}

// EnsureProjectSessionResponseV2 返回 V2 首次创建或同语义重放的冻结回执。
struct EnsureProjectSessionResponseV2 {
    1: required string schema_version
    2: required string request_id
    3: required EnsureDispositionV1 disposition
    4: required ProjectSessionReceiptV2 receipt
}

// QueryProjectSessionCommandRequestV2 用于 V2 Unknown Outcome 后核对原命令摘要。
struct QueryProjectSessionCommandRequestV2 {
    1: required string schema_version
    2: required string request_id
    3: required string command_id
    4: required string expected_request_digest
}

// QueryProjectSessionCommandResponseV2 返回 V2 严格三态；只有 completed 可以携带 Receipt。
struct QueryProjectSessionCommandResponseV2 {
    1: required string schema_version
    2: required string request_id
    3: required QueryProjectSessionCommandStatusV1 status
    4: optional ProjectSessionReceiptV2 receipt
}

// SessionServiceExceptionV1 只携带稳定错误码与安全文案，不返回 SQL、Prompt、密钥或内部地址。
exception SessionServiceExceptionV1 {
    1: required string code
    2: required string message
    3: required bool retryable
    4: required string request_id
}

// AgentSessionServiceV1 是 Agent-owned W0 Session RPC 服务；Ensure 写操作无框架自动重试，Query 只读。
service AgentSessionServiceV1 {
    EnsureProjectSessionResponseV1 EnsureProjectSessionV1(1: EnsureProjectSessionRequestV1 request)
        throws (1: SessionServiceExceptionV1 service_error)

    QueryProjectSessionCommandResponseV1 QueryProjectSessionCommandV1(1: QueryProjectSessionCommandRequestV1 request)
        throws (1: SessionServiceExceptionV1 service_error)

    EnsureProjectSessionResponseV2 EnsureProjectSessionV2(1: EnsureProjectSessionRequestV2 request)
        throws (1: SessionServiceExceptionV1 service_error)

    QueryProjectSessionCommandResponseV2 QueryProjectSessionCommandV2(1: QueryProjectSessionCommandRequestV2 request)
        throws (1: SessionServiceExceptionV1 service_error)
}
