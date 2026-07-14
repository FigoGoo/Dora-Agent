namespace go sessionv1

// Session RPC 总体版本和两个方法的独立 Schema 版本；方法升级不得复用已发布字段编号。
const string SESSION_RPC_SCHEMA_VERSION = "session.rpc.v1"
const string ENSURE_PROJECT_SESSION_SCHEMA_VERSION = "ensure_project_session.v1"
const string QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION = "query_project_session_command.v1"
const string AGENT_SESSION_SERVICE_NAME = "dora.agent.session.v1"

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
}
