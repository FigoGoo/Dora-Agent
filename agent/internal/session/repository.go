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
