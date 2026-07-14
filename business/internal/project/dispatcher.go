package project

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrOutboxEmpty 表示当前没有达到可派发时间的 Session 初始化命令，调度循环可以正常等待下一次唤醒。
	ErrOutboxEmpty = errors.New("project session outbox is empty")
	// ErrOutboxLeaseLost 表示提交派发结果时短租约或 Fencing Version 已变化，当前执行者不得覆盖新 Owner。
	ErrOutboxLeaseLost = errors.New("project session outbox lease lost")
	// ErrAgentSessionConflict 表示 Agent 已冻结同 command_id 的另一业务语义，必须停止自动重试。
	ErrAgentSessionConflict = errors.New("agent session command conflict")
	// ErrAgentSessionInvalid 表示 Agent 拒绝了确定性契约输入，必须停止自动重试并进入治理。
	ErrAgentSessionInvalid = errors.New("agent session command invalid")
	// ErrAgentSessionUnavailable 表示 Agent RPC 未确认结果，必须先 Query 原 command_id 后才能决定是否重试。
	ErrAgentSessionUnavailable = errors.New("agent session service unavailable")
	// ErrInvalidAgentReceipt 表示 Agent 响应与原 Command/Digest 或 Prompt Presence 不一致，必须失败关闭。
	ErrInvalidAgentReceipt = errors.New("invalid agent session receipt")
)

const (
	dispatchErrorConflict         = "AGENT_SESSION_COMMAND_CONFLICT"
	dispatchErrorInvalid          = "AGENT_SESSION_COMMAND_INVALID"
	dispatchErrorReceipt          = "AGENT_SESSION_RECEIPT_INVALID"
	dispatchErrorPromptReveal     = "PROMPT_DECRYPT_FAILED"
	dispatchErrorAttemptsExceeded = "AGENT_SESSION_ATTEMPTS_EXCEEDED"
)

// QueryStatus 是 Agent 原 command_id 权威查询的冻结三态。
type QueryStatus string

const (
	// QueryStatusNotFound 表示 Agent 已确认不存在该 command_id，可以使用原命令重试一次。
	QueryStatusNotFound QueryStatus = "not_found"
	// QueryStatusCompleted 表示 Agent 已完成原命令，必须重放冻结 Receipt。
	QueryStatusCompleted QueryStatus = "completed"
	// QueryStatusConflict 表示 command_id 已绑定另一摘要，禁止覆盖或换键重试。
	QueryStatusConflict QueryStatus = "conflict"
)

// EnsureSessionRequest 是 Business 应用层传给 Agent Client Adapter 的显式跨服务 DTO。
type EnsureSessionRequest struct {
	// RequestID 本次 RPC 尝试的 UUIDv7，只用于关联，不进入业务摘要。
	RequestID string
	// CommandID 来自 Business Outbox 的稳定 UUIDv7，所有技术重试保持不变。
	CommandID string
	// RequestDigest 冻结的 ensure_project_session.v1 摘要。
	RequestDigest Digest
	// ProjectID Business Project UUIDv7。
	ProjectID string
	// OwnerUserID Project 创建时冻结的可信用户 UUIDv7。
	OwnerUserID string
	// InitialPrompt 解密后的规范化首提示词；空工作台时为空且 PromptPresent=false。
	InitialPrompt string
	// PromptPresent 表示是否包含非空首提示词。
	PromptPresent bool
	// PromptDigest 首提示词摘要；空工作台时为全零值，协议层映射为空字符串。
	PromptDigest Digest
	// RequestedAt 本次 RPC 尝试时间，不进入业务摘要。
	RequestedAt time.Time
}

// EnsureSessionReceipt 是 Agent 首次完成或重放时返回的安全冻结回执。
type EnsureSessionReceipt struct {
	// CommandID 必须等于请求的稳定 command_id。
	CommandID string
	// RequestDigest 必须等于请求摘要。
	RequestDigest Digest
	// SessionID Agent 权威 Session UUIDv7。
	SessionID string
	// InputID 非空首提示词对应的 Agent Input UUIDv7；空工作台时为空。
	InputID *string
	// Replayed 表示 Agent 是否重放既有 Receipt，不改变业务结果。
	Replayed bool
}

// QuerySessionResult 是 QueryProjectSessionCommandV1 的应用层安全结果。
type QuerySessionResult struct {
	// Status 为 not_found、completed 或 conflict。
	Status QueryStatus
	// Receipt 只在 completed 状态存在。
	Receipt *EnsureSessionReceipt
}

// AgentSessionClient 定义 Business Dispatcher 消费 Agent Session RPC 的最小接口。
type AgentSessionClient interface {
	// Ensure 使用原 command_id 创建或重放 Agent Session；Adapter 禁止为写操作启用框架自动重试。
	Ensure(ctx context.Context, request EnsureSessionRequest) (EnsureSessionReceipt, error)
	// Query 按 command_id 与 expected digest 核对 Unknown Outcome，不产生业务副作用。
	Query(ctx context.Context, commandID string, expectedDigest Digest) (QuerySessionResult, error)
}

// PromptRevealer 只在 Outbox 派发时解密首提示词并执行认证标签校验。
type PromptRevealer interface {
	// Reveal 返回规范化明文；实现不得记录或缓存正文，认证失败必须返回错误。
	Reveal(ctx context.Context, payload EncryptedPayload) (string, error)
}

// DispatchRepository 定义单命令 Claim 和带 Fence 的终态提交，避免批处理循环执行同构 SQL。
type DispatchRepository interface {
	// ClaimNext 领取一个到期命令并递增 LeaseVersion/AttemptCount；没有工作返回 ErrOutboxEmpty。
	ClaimNext(ctx context.Context, leaseOwner string, now time.Time, leaseUntil time.Time) (SessionOutbox, error)
	// MarkDelivered 原子更新 Binding、Project 与 Outbox，并清除已交付 Prompt 密文。
	MarkDelivered(ctx context.Context, outbox SessionOutbox, receipt EnsureSessionReceipt, deliveredAt time.Time) error
	// MarkRetry 释放当前 Lease 并以原 command_id 安排下一次有限重试。
	MarkRetry(ctx context.Context, outbox SessionOutbox, availableAt time.Time, updatedAt time.Time) error
	// MarkDead 释放当前 Lease并把 Binding/Project 收敛为需治理状态。
	MarkDead(ctx context.Context, outbox SessionOutbox, stableErrorCode string, updatedAt time.Time) error
}

// DispatcherConfig 冻结单实例派发 Owner、短租约和退避参数。
type DispatcherConfig struct {
	// LeaseOwner 是可审计且不含 Secret 的实例 Owner。
	LeaseOwner string
	// LeaseDuration 是一次 RPC 与 Unknown Outcome 核对共享的短租约预算。
	LeaseDuration time.Duration
	// RetryDelay 是本批固定的有界重试间隔，后续可升级为版本化退避策略。
	RetryDelay time.Duration
}

// Dispatcher 每次只处理一个 Outbox 命令，遵循 Ensure Unknown Outcome 后先 Query 再原命令重试的顺序。
type Dispatcher struct {
	repository DispatchRepository
	client     AgentSessionClient
	revealer   PromptRevealer
	clock      Clock
	idgen      IDGenerator
	config     DispatcherConfig
}

// NewDispatcher 校验派发依赖和所有有界时间参数，缺失时阻止启动。
func NewDispatcher(repository DispatchRepository, client AgentSessionClient, revealer PromptRevealer, clock Clock, idgen IDGenerator, config DispatcherConfig) (*Dispatcher, error) {
	if repository == nil || client == nil || revealer == nil || clock == nil || idgen == nil || config.LeaseOwner == "" || config.LeaseDuration <= 0 || config.RetryDelay <= 0 {
		return nil, errors.New("create project session dispatcher: required dependency is missing")
	}
	return &Dispatcher{repository: repository, client: client, revealer: revealer, clock: clock, idgen: idgen, config: config}, nil
}

// DispatchNext Claim 一个命令并完成 Ensure/Query/重试决策；ErrOutboxEmpty 是正常空闲结果。
func (d *Dispatcher) DispatchNext(ctx context.Context) error {
	now := d.clock.Now().UTC()
	outbox, err := d.repository.ClaimNext(ctx, d.config.LeaseOwner, now, now.Add(d.config.LeaseDuration))
	if err != nil {
		return err
	}
	if err := outbox.Validate(); err != nil || outbox.Status != OutboxStatusProcessing {
		return d.repository.MarkDead(ctx, outbox, dispatchErrorInvalid, now)
	}
	if outbox.AttemptCount > outbox.MaxAttempts {
		return d.repository.MarkDead(ctx, outbox, dispatchErrorAttemptsExceeded, now)
	}
	if outbox.RecoveryRequired {
		// retry 或过期 processing 代表上一次 Ensure 结果未知；禁止跨 Attempt 再次先写。
		return d.recoverUnknownOutcome(ctx, outbox, nil, now)
	}

	request, err := d.ensureRequest(ctx, outbox, now)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return d.repository.MarkDead(ctx, outbox, dispatchErrorPromptReveal, now)
	}
	receipt, ensureErr := d.client.Ensure(ctx, request)
	if ensureErr == nil {
		return d.commitReceipt(ctx, outbox, request, receipt, now)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(ensureErr, ErrAgentSessionConflict) {
		return d.repository.MarkDead(ctx, outbox, dispatchErrorConflict, now)
	}
	if errors.Is(ensureErr, ErrAgentSessionInvalid) {
		return d.repository.MarkDead(ctx, outbox, dispatchErrorInvalid, now)
	}
	return d.recoverUnknownOutcome(ctx, outbox, &request, now)
}

// recoverUnknownOutcome 查询原 command_id；只有 confirmed not_found 才能以原命令重试。
// request 为空表示跨 Attempt 恢复，completed Receipt 可仅凭冻结 Command/Digest/Prompt Presence 校验；
// 只有 not_found 才解密 Prompt 并生成新的关联 Request ID。
func (d *Dispatcher) recoverUnknownOutcome(ctx context.Context, outbox SessionOutbox, request *EnsureSessionRequest, now time.Time) error {
	query, queryErr := d.client.Query(ctx, outbox.ID, outbox.RequestDigest)
	if queryErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return d.retryOrDead(ctx, outbox, now)
	}
	switch query.Status {
	case QueryStatusCompleted:
		if query.Receipt == nil {
			return d.repository.MarkDead(ctx, outbox, dispatchErrorReceipt, now)
		}
		receiptRequest := EnsureSessionRequest{
			CommandID: outbox.ID, RequestDigest: outbox.RequestDigest, PromptPresent: outbox.HasInitialPrompt,
		}
		if request != nil {
			receiptRequest = *request
		}
		return d.commitReceipt(ctx, outbox, receiptRequest, *query.Receipt, now)
	case QueryStatusConflict:
		return d.repository.MarkDead(ctx, outbox, dispatchErrorConflict, now)
	case QueryStatusNotFound:
		if request == nil {
			prepared, err := d.ensureRequest(ctx, outbox, now)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return d.repository.MarkDead(ctx, outbox, dispatchErrorPromptReveal, now)
			}
			request = &prepared
		}
		receipt, retryErr := d.client.Ensure(ctx, *request)
		if retryErr == nil {
			return d.commitReceipt(ctx, outbox, *request, receipt, now)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(retryErr, ErrAgentSessionConflict) || errors.Is(retryErr, ErrAgentSessionInvalid) {
			stableCode := dispatchErrorInvalid
			if errors.Is(retryErr, ErrAgentSessionConflict) {
				stableCode = dispatchErrorConflict
			}
			return d.repository.MarkDead(ctx, outbox, stableCode, now)
		}
		return d.retryOrDead(ctx, outbox, now)
	default:
		return d.repository.MarkDead(ctx, outbox, dispatchErrorReceipt, now)
	}
}

// ensureRequest 解密并再次核对 Prompt Digest，随后生成不进入业务语义的 RPC request_id。
func (d *Dispatcher) ensureRequest(ctx context.Context, outbox SessionOutbox, requestedAt time.Time) (EnsureSessionRequest, error) {
	request := EnsureSessionRequest{
		CommandID: outbox.ID, RequestDigest: outbox.RequestDigest, ProjectID: outbox.AggregateID,
		OwnerUserID: outbox.OwnerUserID, PromptPresent: outbox.HasInitialPrompt, RequestedAt: requestedAt,
	}
	requestID, err := d.idgen.New()
	if err != nil {
		return EnsureSessionRequest{}, err
	}
	request.RequestID = requestID
	if !outbox.HasInitialPrompt {
		return request, nil
	}
	if outbox.EncryptedPayload == nil {
		return EnsureSessionRequest{}, ErrPromptProtection
	}
	prompt, err := d.revealer.Reveal(ctx, *outbox.EncryptedPayload)
	if err != nil {
		return EnsureSessionRequest{}, ErrPromptProtection
	}
	normalized, digest, present, err := NormalizeEnsureSessionPrompt(prompt)
	if err != nil || !present || normalized != prompt || digest != outbox.EncryptedPayload.PayloadDigest {
		return EnsureSessionRequest{}, ErrPromptProtection
	}
	expectedRequestDigest, err := CalculateEnsureSessionRequestDigest(outbox.AggregateID, outbox.OwnerUserID, true, digest)
	if err != nil || expectedRequestDigest != outbox.RequestDigest {
		return EnsureSessionRequest{}, ErrPromptProtection
	}
	request.InitialPrompt = prompt
	request.PromptDigest = digest
	return request, nil
}

// commitReceipt 校验 Agent 冻结回执与原请求完全一致后，才允许 Repository 清除 Business Prompt 密文。
func (d *Dispatcher) commitReceipt(ctx context.Context, outbox SessionOutbox, request EnsureSessionRequest, receipt EnsureSessionReceipt, deliveredAt time.Time) error {
	if err := validateEnsureReceipt(request, receipt); err != nil {
		return d.repository.MarkDead(ctx, outbox, dispatchErrorReceipt, deliveredAt)
	}
	return d.repository.MarkDelivered(ctx, outbox, receipt, deliveredAt)
}

// retryOrDead 按已开始的 AttemptCount 决定释放重试或进入终止，所有技术重试仍使用原 command_id。
func (d *Dispatcher) retryOrDead(ctx context.Context, outbox SessionOutbox, now time.Time) error {
	if outbox.AttemptCount >= outbox.MaxAttempts {
		return d.repository.MarkDead(ctx, outbox, dispatchErrorAttemptsExceeded, now)
	}
	return d.repository.MarkRetry(ctx, outbox, now.Add(d.config.RetryDelay), now)
}

// validateEnsureReceipt 校验 Receipt 不会把另一 Command、Digest 或空 Prompt 的 Input 投影进当前 Project。
func validateEnsureReceipt(request EnsureSessionRequest, receipt EnsureSessionReceipt) error {
	if receipt.CommandID != request.CommandID || receipt.RequestDigest != request.RequestDigest || !isUUIDv7(receipt.SessionID) {
		return ErrInvalidAgentReceipt
	}
	if request.PromptPresent {
		if receipt.InputID == nil || !isUUIDv7(*receipt.InputID) {
			return ErrInvalidAgentReceipt
		}
	} else if receipt.InputID != nil {
		return ErrInvalidAgentReceipt
	}
	return nil
}
