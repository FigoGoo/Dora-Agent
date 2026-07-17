// Package runtime 实现持久化 Session Lane、Eino Runner Processor 与 Preview 终态投影。
package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
)

var (
	// ErrInvalidInput 表示 HTTP→Runtime DTO、ID 或状态违反冻结契约。
	ErrInvalidInput = errors.New("invalid creation spec preview runtime input")
	// ErrNotFound 表示 Session 不存在、已归档或不属于可信 User/Project。
	ErrNotFound = errors.New("creation spec preview session not found")
	// ErrIdempotencyConflict 表示同 Idempotency-Key 已绑定另一语义或身份。
	ErrIdempotencyConflict = errors.New("creation spec preview idempotency conflict")
	// ErrSessionLaneBlocked 表示 Preview 前存在不得由 Preview Processor 跳过或改写的非 Preview 未决 Input。
	ErrSessionLaneBlocked = errors.New("creation spec preview session lane is blocked")
	// ErrPersistence 表示 PostgreSQL 操作失败且内部 SQL 不得穿透边界。
	ErrPersistence = errors.New("creation spec preview persistence unavailable")
	// ErrFenceLost 表示过期 Processor 已失去提交权，不得投影结果。
	ErrFenceLost = errors.New("creation spec preview session fence lost")
)

// EnqueueCommand 是通过内部身份断言后进入 Runtime 的有类型 POST 命令。
type EnqueueCommand struct {
	// RequestID 是 Business 签发的内部请求 UUIDv7。
	RequestID string
	// IdempotencyKey 是浏览器生成并经 BFF 严格校验的 UUIDv7。
	IdempotencyKey string
	// UserID 是可信 Business Principal UUIDv7。
	UserID string
	// ProjectID 是可信 Business Project UUIDv7。
	ProjectID string
	// SessionID 是路由中的 Agent Session UUIDv7。
	SessionID string
	// Intent 是严格解码后的 Preview Intent。
	Intent plancreationspec.Intent
}

// EnqueueResult 是 HTTP 202 exact DTO 的领域值。
type EnqueueResult struct {
	// RequestID 是当前可信内部请求 UUIDv7。
	RequestID string
	// SessionID 是已持久化输入所属 Session UUIDv7。
	SessionID string
	// InputID 是首次写入或同义重放的稳定 Input UUIDv7。
	InputID string
	// Status 固定 pending，不能被解释为 Runner 已完成。
	Status string
}

// EnqueuePlan 是 Repository 单事务创建 Message/Input/Run/Receipt/Event 所需的完整计划。
type EnqueuePlan struct {
	// RequestID 是首次入队关联请求 UUIDv7。
	RequestID string
	// IdempotencyKey 是 first-write-wins HTTP 幂等键。
	IdempotencyKey string
	// RequestDigest 是严格 Intent canonical SHA-256。
	RequestDigest string
	// UserID 是可信用户逻辑引用。
	UserID string
	// ProjectID 是可信 Project 逻辑引用。
	ProjectID string
	// SessionID 是目标 Session。
	SessionID string
	// MessageID 是保存 Intent 密文的 Message UUIDv7。
	MessageID string
	// InputID 是 Session Lane Input UUIDv7。
	InputID string
	// TurnID 是技术重试复用的 Runner Turn UUIDv7。
	TurnID string
	// RunID 是技术重试复用的 Runner Run UUIDv7。
	RunID string
	// ToolCallID 是稳定 Graph Tool Call UUIDv7。
	ToolCallID string
	// BusinessCommandID 是 Save/Query 复用的 Business Command UUIDv7。
	BusinessCommandID string
	// EventID 是 session.input.accepted Event UUIDv7。
	EventID string
	// TerminalEventID 是入队时预分配的 completed/failed Event UUIDv7，投影重试必须复用。
	TerminalEventID string
	// PromptVersion 是入队时冻结的 Prompt 版本。
	PromptVersion string
	// ValidatorVersion 是入队时冻结的 Validator 版本。
	ValidatorVersion string
	// Content 是严格 Intent 的 DRAE v1 加密正文。
	Content session.ProtectedContent
	// CreatedAt 是事务外一次冻结的 UTC 时间。
	CreatedAt time.Time
}

// Claim 是 Processor 持有的 Session HOL、Lease/Fence 与稳定执行身份。
type Claim struct {
	// Owner 是本次领取 Preview Input 的 Processor 实例标识，所有后续 CAS 必须原样复用。
	Owner string
	// RequestID 是首次入队请求 UUIDv7。
	RequestID string
	// RequestDigest 是严格 Intent 摘要。
	RequestDigest string
	// SessionID 是当前串行 Lane。
	SessionID string
	// UserID 是可信用户。
	UserID string
	// ProjectID 是可信 Project。
	ProjectID string
	// InputID 是 HOL Input。
	InputID string
	// MessageID 是 Intent Message。
	MessageID string
	// TurnID 是稳定 Runner Turn。
	TurnID string
	// RunID 是稳定 Runner Run。
	RunID string
	// ToolCallID 是稳定 Tool Call。
	ToolCallID string
	// BusinessCommandID 是稳定 Business Command。
	BusinessCommandID string
	// TerminalEventID 是接受 202 前已持久化的稳定终态 Event ID。
	TerminalEventID string
	// PromptVersion 是入队时冻结的 Graph Prompt pin，恢复不得静默升级。
	PromptVersion string
	// ValidatorVersion 是入队时冻结的 Validator pin，恢复不得静默升级。
	ValidatorVersion string
	// FenceToken 是本次 Claim 同时写入 Session Lease 与 Input 的 Fence。
	FenceToken int64
	// Attempts 是包含本次在内的已开始尝试数。
	Attempts int
	// Intent 是认证解密并重验摘要后的 strict JSON。
	Intent []byte
	// Poisoned 表示已成功领取但持久密文/摘要/Intent 损坏，Processor 必须走有界 dead 处置而非执行 Runner。
	Poisoned bool
}

// CompletionDisposition 区分确定业务终态与 poison/协议/执行耗尽的 dead-letter 终态。
type CompletionDisposition string

const (
	// CompletionResolved 表示 completed 或确定业务 failed 已正常投影。
	CompletionResolved CompletionDisposition = "resolved"
	// CompletionDead 表示 poison、协议不变量或执行技术重试耗尽，但仍已投影 failed Event。
	CompletionDead CompletionDisposition = "dead"
)

// Terminal 是已冻结 Tool Result 与其 Session Input 终态处置的持久组合。
type Terminal struct {
	Result      plancreationspec.Result
	Disposition CompletionDisposition
}

// ReceiptStage 是 Processor 可见的 Tool Receipt 执行阶段。
type ReceiptStage string

const (
	ReceiptStagePending          ReceiptStage = "pending"
	ReceiptStageBusinessPrepared ReceiptStage = "business_prepared"
	ReceiptStageBusinessUnknown  ReceiptStage = "business_unknown"
	// ReceiptStageBusinessResendExhausted 表示最终查询仍未收敛且自动同键重发预算已经耗尽。
	ReceiptStageBusinessResendExhausted ReceiptStage = "business_resend_exhausted"
	ReceiptStageCompleted               ReceiptStage = "completed"
	ReceiptStageFailed                  ReceiptStage = "failed"
)

// ReceiptSnapshot 以一次真源读区分开放、recovery-only 与已冻结终态。
type ReceiptSnapshot struct {
	Stage    ReceiptStage
	Terminal *Terminal
}

// RecoveryOnly 表示 Save 前命令已冻结，任何后续故障都只能 Query 原命令。
func (snapshot ReceiptSnapshot) RecoveryOnly() bool {
	return snapshot.Stage == ReceiptStageBusinessPrepared || snapshot.Stage == ReceiptStageBusinessUnknown ||
		snapshot.Stage == ReceiptStageBusinessResendExhausted
}

// EnqueueRepository 是 HTTP persist-only 路径消费的 first-write-wins 最小端口。
type EnqueueRepository interface {
	// LookupEnqueue 在 KMS/随机源/时钟前查询已接受幂等键；miss 返回 nil，异义返回 conflict。
	LookupEnqueue(
		ctx context.Context,
		idempotencyKey string,
		requestDigest string,
		userID string,
		projectID string,
		sessionID string,
	) (*EnqueueResult, error)
	// Enqueue 在一个短事务内 first-write-wins 创建全部入队事实与既有 accepted Event。
	Enqueue(ctx context.Context, plan EnqueuePlan) (EnqueueResult, error)
}

// Repository 是 Processor 消费的 PostgreSQL Session Lane 最小接口。
type Repository interface {
	// ClaimNext 只领取每个 Session 最小未决 enqueue_seq，并同时更新 Session/Input Fence。
	ClaimNext(ctx context.Context, owner string, now time.Time, leaseDuration time.Duration) (*Claim, error)
	// MarkRunning 使用 owner+fence CAS 把 claimed 输入推进到 running。
	MarkRunning(ctx context.Context, claim Claim, now time.Time) error
	// RenewLease 使用同一 Fence 延长 Session/Input Lease；RowsAffected 不匹配即失去提交权。
	RenewLease(ctx context.Context, claim Claim, now time.Time, leaseDuration time.Duration) error
	// LoadReceipt 以一次真源读恢复开放/recovery-only/已冻结 Result 状态。
	LoadReceipt(ctx context.Context, claim Claim) (ReceiptSnapshot, error)
	// Complete 以 Fence CAS 原子写 Projection/Event、resolve Input 并释放 Session Lease。
	Complete(ctx context.Context, claim Claim, terminal Terminal, now time.Time) error
	// FreezeExecutionFailure 在当前 fence 下把 poison/协议/执行耗尽冻结为 dead-letter failed Result。
	FreezeExecutionFailure(ctx context.Context, claim Claim, result plancreationspec.Result) error
	// DeferRecovery 保持 HOL 未决并把 business_unknown 输入转为 recovery_pending 后释放本次 Lease。
	DeferRecovery(ctx context.Context, claim Claim, availableAt time.Time) error
	// DeferProjection 对回执读取或终态投影故障无限有界退避，不消耗 execution max。
	DeferProjection(ctx context.Context, claim Claim, availableAt time.Time) error
	// RetryExecution 仅对未冻结终态的执行技术失败进入 retry_wait。
	RetryExecution(ctx context.Context, claim Claim, availableAt time.Time) error
}

// ContentProtector 为 Runtime Enqueue 提供 Message 加密；Repository 负责回读时认证解密。
type ContentProtector interface {
	// Protect 把严格 Intent JSON 加密为自描述 DRAE v1 Envelope。
	Protect(ctx context.Context, plaintext []byte) (session.ProtectedContent, error)
}

// IDGenerator 生成应用侧 UUIDv7。
type IDGenerator interface {
	// New 返回规范小写 UUIDv7。
	New() (string, error)
}

// Clock 为一次入队或 Processor 状态迁移冻结 UTC 时间。
type Clock interface {
	// Now 返回当前时间。
	Now() time.Time
}

// Runner 是 Processor 消费的真实 Eino Runner 适配器。
type Runner interface {
	// Run 使用 Claim 可信上下文执行唯一主 Agent，并完整消费 AgentEvent 流。
	Run(ctx context.Context, claim Claim) error
}
