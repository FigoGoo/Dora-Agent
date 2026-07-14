package session

import (
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
)

// QueryCommandSchemaVersionV2 是 W1 QueryProjectSessionCommandV2 的冻结契约版本。
const QueryCommandSchemaVersionV2 = "query_project_session_command.v2"

// EnsureCommandV2 是 Business→Agent 的 W1 领域边界 DTO，携带同一 Business 事务冻结的完整 Skill Snapshot。
// InitialPrompt 与 Snapshot Runtime Content 属于敏感数据，不得进入日志、Trace、Receipt 或 EventLog。
type EnsureCommandV2 struct {
	// SchemaVersion 固定为 ensure_project_session.v2。
	SchemaVersion string
	// RequestID 是本次传输追踪 UUIDv7，不进入命令语义摘要。
	RequestID string
	// CommandID 是 Business Outbox 稳定 UUIDv7，Unknown Outcome 重试必须复用。
	CommandID string
	// RequestDigest 是 Business 按 V2 Canonical Schema 冻结的语义摘要。
	RequestDigest string
	// ProjectID 是 Business Project UUIDv7 逻辑引用。
	ProjectID string
	// OwnerUserID 是 Business 已认证 Project Owner UUIDv7。
	OwnerUserID string
	// CreationSource 在 W1 固定 quick_create。
	CreationSource string
	// InitialPrompt 是可选首 Prompt；纯 Unicode 空白折叠为 absent。
	InitialPrompt string
	// PromptDigest 是 NFC Prompt 摘要；absent 时必须为空。
	PromptDigest string
	// SkillSnapshot 是 Business 冻结且 Agent 必须独立重算全部摘要的强类型集合。
	SkillSnapshot skill.SessionSkillSnapshotV1
	// RequestedAt 是 Business 接受命令的 UTC 时间，只用于审计，不进入幂等摘要。
	RequestedAt time.Time
}

// LoadedSkillSnapshotV1 是 Session Runtime 后续消费的不可变强类型 Snapshot。
// Runtime Content 已在受控应用层解密并完成 item/runtime/set digest 重验，不包含密文或持久化 Model。
type LoadedSkillSnapshotV1 struct {
	// SessionID 是快照所属 Agent Session UUIDv7。
	SessionID string
	// Snapshot 是通过 canonical 重验的不可变 Runtime Skill 集合。
	Snapshot skill.SessionSkillSnapshotV1
}
