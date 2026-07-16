package session

import "time"

// MaxInitialPromptBytes 是 W0 RPC DTO 规范化后允许的 Prompt UTF-8 最大字节数。
const MaxInitialPromptBytes = 64 * 1024

// QueryCommandSchemaVersionV1 是 W0 QueryProjectSessionCommand RPC 请求的冻结契约版本。
const QueryCommandSchemaVersionV1 = "query_project_session_command.v1"

// EnsureCommand 是 Business→Agent 的领域边界 DTO，调用方身份必须已由服务间认证确认。
// InitialPrompt 属于敏感数据，不得进入日志、Trace、Receipt 或 EventLog。
type EnsureCommand struct {
	// SchemaVersion 是冻结请求契约版本，W0 必须为 ensure_project_session.v1。
	SchemaVersion string
	// RequestID 是跨服务调用关联 UUIDv7，不进入命令语义摘要。
	RequestID string
	// CommandID 是 Business Outbox 稳定 UUIDv7，同一技术重试不变。
	CommandID string
	// RequestDigest 是调用方按 ensure_project_session.v1 Canonical Schema 计算的语义摘要。
	RequestDigest string
	// ProjectID 是 Business Project UUIDv7，只作逻辑引用。
	ProjectID string
	// OwnerUserID 是 Business 已认证的 Project Owner UUIDv7，禁止浏览器覆盖。
	OwnerUserID string
	// CreationSource 是创建来源，W0 固定 quick_create。
	CreationSource string
	// InitialPrompt 是可选首 Prompt；空字符串或 Unicode 空白视为缺失。
	InitialPrompt string
	// PromptDigest 是 NFC 规范化 Prompt 的 SHA-256 摘要；空 Prompt 时必须为空。
	PromptDigest string
	// SkillSnapshotMode 是 W0 必填 empty，防止省略 Session Skill 冻结事实。
	SkillSnapshotMode SkillSnapshotKind
	// RequestedAt 是 Business 接受命令的 UTC 时间，仅用于审计，不进入幂等语义摘要。
	RequestedAt time.Time
}

// EnsureDisposition 表示 Ensure 命令创建了新事实或重放既有冻结回执。
type EnsureDisposition string

const (
	// EnsureDispositionCreated 表示本次事务首次提交 Session 事实。
	EnsureDispositionCreated EnsureDisposition = "created"
	// EnsureDispositionReplayed 表示同键同语义命中并重放既有 Receipt。
	EnsureDispositionReplayed EnsureDisposition = "replayed"
)

// EnsureResult 是 Session 创建用例返回给 RPC Mapper 的安全领域 DTO。
type EnsureResult struct {
	// CommandID 是原 Business 命令 UUIDv7。
	CommandID string
	// SessionID 是冻结的 Session UUIDv7。
	SessionID string
	// MessageID 是可选首 Message UUIDv7。
	MessageID *string
	// InputID 是可选首 Input UUIDv7。
	InputID *string
	// Disposition 表示 created 或 replayed。
	Disposition EnsureDisposition
	// ResultVersion 是冻结结果结构版本。
	ResultVersion int
	// SkillSnapshotDigest 是命令冻结的 Session Skill Snapshot set digest；V1 为规范空集合摘要。
	SkillSnapshotDigest string
	// SkillCount 是冻结的 Snapshot Item 数量；V1 固定为 0。
	SkillCount int
	// AcceptedAt 是 Agent 本地事务冻结 UTC 时间。
	AcceptedAt time.Time
}

// QueryCommand 查询原命令的权威冻结回执，用于 RPC Unknown Outcome 后先核对再决定是否重试。
type QueryCommand struct {
	// SchemaVersion 是冻结查询契约版本，W0 必须为 query_project_session_command.v1。
	SchemaVersion string
	// RequestID 是本次只读查询的关联 UUIDv7，不进入命令语义。
	RequestID string
	// CommandID 是原 Ensure 命令 UUIDv7。
	CommandID string
	// ExpectedRequestDigest 是调用方原命令的预期语义摘要。
	ExpectedRequestDigest string
	// ExpectedCommandType 是调用方所查询的命令版本 token；跨 V1/V2 命中必须返回版本冲突。
	ExpectedCommandType string
}

// QueryCommandStatus 表示原命令不存在、已完成或已被不同语义占用。
type QueryCommandStatus string

const (
	// QueryCommandStatusNotFound 表示权威 Receipt 尚不存在，调用方可使用原命令执行有界重试。
	QueryCommandStatusNotFound QueryCommandStatus = "not_found"
	// QueryCommandStatusCompleted 表示同命令同摘要已经完成，应直接重放冻结 Receipt。
	QueryCommandStatusCompleted QueryCommandStatus = "completed"
	// QueryCommandStatusConflict 表示同一 CommandID 已绑定不同摘要，禁止重试覆盖。
	QueryCommandStatusConflict QueryCommandStatus = "conflict"
)

// QueryCommandResult 是 Query 用例返回给 RPC Mapper 的安全结果，不包含 Prompt、密文或持久化错误。
type QueryCommandResult struct {
	// Status 是 not_found、completed 或 conflict。
	Status QueryCommandStatus
	// Receipt 只在 completed 时存在；not_found/conflict 不泄漏其他命令结果。
	Receipt *EnsureResult
}
