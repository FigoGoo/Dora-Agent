package session

import (
	"context"
	"time"
)

// Repository 定义 Session 基础聚合的最小持久化消费契约。
// Ensure 必须在单个 Agent PostgreSQL 事务内完成回执判定、全部领域写入和事件批量追加。
type Repository interface {
	// Ensure 以 CommandID 串行化 first-write-wins；同键同摘要重放，同键异摘要返回 ErrCommandConflict。
	// 不同 Command 竞争同一 Project 时必须返回 ErrProjectSessionConflict，事务失败不得留下部分数据。
	Ensure(ctx context.Context, plan EnsurePlan) (EnsureResult, error)
	// Query 按 CommandID 只读核对冻结 Receipt；摘要不同返回 conflict 且不返回 Receipt。
	Query(ctx context.Context, command QueryCommand) (QueryCommandResult, error)
	// LoadSkillSnapshot 使用至多两条 SQL 读取 Header 和有界 Item 集合；maxItems 必须来自已校验配置。
	LoadSkillSnapshot(ctx context.Context, sessionID string, maxItems int) (StoredSkillSnapshot, error)
}

// IDGenerator 生成应用侧 UUIDv7；失败时用例必须停止且不能提交降级标识。
type IDGenerator interface {
	// New 返回一个新的规范 UUIDv7 字符串。
	New() (string, error)
}

// Clock 为用例提供可注入 UTC 时间，单次 Ensure 只读取一次并冻结复用。
type Clock interface {
	// Now 返回当前时间；实现可以返回任意时区，用例负责转换为 UTC。
	Now() time.Time
}

// ContentProtector 将非空规范化 Prompt 转换为可持久化密文。
// 实现不得记录明文；返回的 Ciphertext 必须是包含 algorithm、nonce 和 auth tag 的版本化自描述 AEAD Envelope，禁止返回裸密文。
// 加密失败或无法形成完整 Envelope 时 Ensure 必须在数据库事务前停止。
type ContentProtector interface {
	// Protect 加密敏感正文并返回非空自描述 AEAD Envelope 与独立密钥版本引用。
	Protect(ctx context.Context, plaintext []byte) (ProtectedContent, error)
}

// SkillSnapshotContentIdentity 是 Runtime Content AEAD AAD 的稳定身份字段，任何字段变化都必须导致认证失败。
type SkillSnapshotContentIdentity struct {
	// SessionID 是冻结快照所属 Session UUIDv7。
	SessionID string
	// SkillID 是 Business Skill UUIDv7。
	SkillID string
	// PublishedSnapshotID 是不可变 Published Snapshot UUIDv7。
	PublishedSnapshotID string
	// RuntimeContentDigest 是 canonical Runtime Content SHA-256 摘要。
	RuntimeContentDigest string
}

// SkillSnapshotPlaintext 是一次批量保护所需的有界 Runtime Content 明文和 AAD 身份。
type SkillSnapshotPlaintext struct {
	// Identity 是该 Item 唯一且不可互换的 AAD 身份。
	Identity SkillSnapshotContentIdentity
	// CanonicalBytes 是通过 skill canonical 验证后的 Runtime Content UTF-8 JSON。
	CanonicalBytes []byte
}

// SkillSnapshotCiphertext 是批量保护结果，顺序必须与输入保持一致。
type SkillSnapshotCiphertext struct {
	// Identity 是输入 AAD 身份副本，Service 用于防止实现重排或串线。
	Identity SkillSnapshotContentIdentity
	// Protected 是专用 AAD 认证后的 DRAE v1 Envelope 与 KeyVersion。
	Protected ProtectedContent
}

// SkillSnapshotContentProtector 使用 Agent Skill Snapshot 专用 key purpose/AAD 批量保护和读取 Runtime Content。
// 实现不得记录明文、AAD 全量、密钥或密文；任一 Item 失败不得返回部分成功结果。
type SkillSnapshotContentProtector interface {
	// ProtectBatch 在数据库事务前一次性保护全部有界明文，返回与输入一一对应的完整结果。
	ProtectBatch(ctx context.Context, plaintexts []SkillSnapshotPlaintext) ([]SkillSnapshotCiphertext, error)
	// OpenBatch 在数据库读取完成后认证解密全部 Item；任一损坏、AAD 或 key 错误时不返回部分明文。
	OpenBatch(ctx context.Context, ciphertexts []SkillSnapshotCiphertext) ([][]byte, error)
}
