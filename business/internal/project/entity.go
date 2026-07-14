// Package project 定义 Business Project、快速创建幂等回执和 Session 初始化 Outbox 的领域边界。
package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

var (
	// ErrInvalidQuickCreate 表示快速创建聚合不满足身份、状态、摘要、加密或跨记录一致性不变量。
	ErrInvalidQuickCreate = errors.New("invalid project quick create aggregate")
	// ErrIdempotencyConflict 表示同一用户和幂等键摘要已经绑定到不同业务语义。
	ErrIdempotencyConflict = errors.New("project creation idempotency conflict")
	// ErrProjectNotFound 表示项目不存在或对当前可信用户不可见，避免泄露他人项目存在性。
	ErrProjectNotFound = errors.New("project not found")
	// ErrPersistence 表示 Project 持久化暂不可用，禁止向外暴露 GORM、SQL、连接地址或 Secret。
	ErrPersistence = errors.New("project persistence unavailable")
	// ErrInvalidIdempotencyKey 表示快速创建幂等键为空、过长或包含代理不应转发的字符。
	ErrInvalidIdempotencyKey = errors.New("invalid project idempotency key")
	// ErrPromptProtection 表示首提示词认证加密失败，外层不得返回密钥、算法实现或原始正文。
	ErrPromptProtection = errors.New("project prompt protection unavailable")
)

const (
	// QuickCreateCommandType 快速创建回执使用的稳定命令类型。
	QuickCreateCommandType = "quick_create"
	// EnsureSessionEventType 默认 Session 初始化 Outbox 使用的稳定事件类型。
	EnsureSessionEventType = "agent.session.ensure"
	// EnsureSessionSchemaVersion 默认 Session 初始化 Outbox 使用的契约版本。
	EnsureSessionSchemaVersion = "agent.session-bootstrap.v1"
	// EnsureSessionSchemaVersionV2 是完整加密 Session Bootstrap v2 Outbox plaintext 版本。
	EnsureSessionSchemaVersionV2 = "session_bootstrap_outbox_payload.v2"
	// PromptEncryptionAlgorithm 首提示词密文当前允许使用的认证加密算法。
	PromptEncryptionAlgorithm = "aes-256-gcm"
	// EnsureSessionCanonicalSchemaV1 Agent Session 初始化请求摘要使用的冻结 Canonical Schema。
	EnsureSessionCanonicalSchemaV1 = "ensure_project_session.v1"
	// EmptySkillSnapshotMode W0 Session 创建时必须显式冻结的空 Skill Snapshot 模式。
	EmptySkillSnapshotMode = "empty"
	// QuickCreateCreationSource W0 默认 Session 初始化请求的稳定业务来源。
	QuickCreateCreationSource = "quick_create"
	// QuickCreateCanonicalSchemaV1 QuickCreate HTTP 幂等语义摘要使用的冻结 Canonical Schema。
	QuickCreateCanonicalSchemaV1 = "project_quick_create.v1"
	// DefaultProjectTitle W0 QuickCreate 使用的安全默认标题，禁止从首提示词派生或由请求注入。
	DefaultProjectTitle = "未命名项目"
	// MaxInitialPromptBytes W0 NFC 规范化后允许的首提示词 UTF-8 最大字节数。
	MaxInitialPromptBytes = 64 * 1024
)

// LifecycleStatus 项目生命周期状态，独立于最近一次运行摘要。
type LifecycleStatus string

const (
	// LifecycleStatusActive 表示项目处于可使用状态。
	LifecycleStatusActive LifecycleStatus = "active"
	// LifecycleStatusArchived 表示项目已归档但仍可恢复。
	LifecycleStatusArchived LifecycleStatus = "archived"
	// LifecycleStatusTrash 表示项目位于回收站。
	LifecycleStatusTrash LifecycleStatus = "trash"
	// LifecycleStatusDeleted 表示项目已经进入删除终态。
	LifecycleStatusDeleted LifecycleStatus = "deleted"
)

// RecentRunStatus 项目最近运行摘要，不作为 Agent 内部运行状态的替代事实。
type RecentRunStatus string

const (
	// RecentRunStatusIdle 表示项目当前没有运行或首 Input。
	RecentRunStatusIdle RecentRunStatus = "idle"
	// RecentRunStatusQueued 表示项目首 Input 已被可靠接受并等待 Agent 后续处理。
	RecentRunStatusQueued RecentRunStatus = "queued"
	// RecentRunStatusRunning 表示项目最近运行正在执行。
	RecentRunStatusRunning RecentRunStatus = "running"
	// RecentRunStatusWaitingUser 表示项目最近运行等待用户操作。
	RecentRunStatusWaitingUser RecentRunStatus = "waiting_user"
	// RecentRunStatusWaitingAsync 表示项目最近运行等待异步任务。
	RecentRunStatusWaitingAsync RecentRunStatus = "waiting_async"
	// RecentRunStatusSucceeded 表示项目最近运行成功完成。
	RecentRunStatusSucceeded RecentRunStatus = "succeeded"
	// RecentRunStatusPartialFailed 表示项目最近运行部分失败。
	RecentRunStatusPartialFailed RecentRunStatus = "partial_failed"
	// RecentRunStatusFailed 表示项目最近运行失败。
	RecentRunStatusFailed RecentRunStatus = "failed"
	// RecentRunStatusCancelled 表示项目最近运行已取消。
	RecentRunStatusCancelled RecentRunStatus = "cancelled"
)

// InitialPromptStatus 首提示词状态，区分空工作台和等待 Agent 接受的初始输入。
type InitialPromptStatus string

const (
	// InitialPromptStatusAbsent 表示快速创建未包含非空首提示词。
	InitialPromptStatusAbsent InitialPromptStatus = "absent"
	// InitialPromptStatusPending 表示加密首提示词等待 Agent 接受。
	InitialPromptStatusPending InitialPromptStatus = "pending"
	// InitialPromptStatusAccepted 表示 Agent 已按原命令接受首提示词。
	InitialPromptStatusAccepted InitialPromptStatus = "accepted"
	// InitialPromptStatusFailed 表示首提示词初始化进入不可自动恢复失败。
	InitialPromptStatusFailed InitialPromptStatus = "failed"
)

// ProvisioningStatus 默认 Session 初始化状态，表达 Business 与 Agent 之间的最终一致进度。
type ProvisioningStatus string

const (
	// ProvisioningStatusPending 表示命令已经和 Project 同事务提交但尚未确认 Agent 结果。
	ProvisioningStatusPending ProvisioningStatus = "pending"
	// ProvisioningStatusReconciling 表示 RPC 结果未知且必须查询原 command_id。
	ProvisioningStatusReconciling ProvisioningStatus = "reconciling"
	// ProvisioningStatusReady 表示 Agent 已返回匹配的 Session 回执。
	ProvisioningStatusReady ProvisioningStatus = "ready"
	// ProvisioningStatusBlocked 表示稳定冲突或不可重试错误需要受控恢复。
	ProvisioningStatusBlocked ProvisioningStatus = "blocked"
)

// OutboxStatus Session 初始化 Outbox 的权威派发状态。
type OutboxStatus string

const (
	// OutboxStatusPending 表示命令等待首次派发。
	OutboxStatusPending OutboxStatus = "pending"
	// OutboxStatusProcessing 表示命令正在受短租约保护地派发。
	OutboxStatusProcessing OutboxStatus = "processing"
	// OutboxStatusRetry 表示命令将在有限退避后使用原 command_id 重试。
	OutboxStatusRetry OutboxStatus = "retry"
	// OutboxStatusDelivered 表示 Agent 已确认原命令回执。
	OutboxStatusDelivered OutboxStatus = "delivered"
	// OutboxStatusDead 表示命令已停止自动尝试并等待治理。
	OutboxStatusDead OutboxStatus = "dead"
)

// Digest SHA-256 摘要，用于幂等键、业务语义和加密负载完整性核对。
type Digest [sha256.Size]byte

// SHA256Digest 对给定稳定字节序列计算 SHA-256 摘要；调用方仍负责先完成契约规定的规范化。
func SHA256Digest(value []byte) Digest {
	return sha256.Sum256(value)
}

// Hex 将二进制 SHA-256 摘要编码为 64 位小写十六进制，供跨 Module DTO 使用。
func (d Digest) Hex() string {
	return hex.EncodeToString(d[:])
}

// quickCreateCanonicalV1 固定 QuickCreate HTTP 幂等语义摘要的 JSON 字段及顺序。
type quickCreateCanonicalV1 struct {
	// SchemaVersion 摘要结构版本，W0 固定为 project_quick_create.v1。
	SchemaVersion string `json:"schema_version"`
	// PromptPresent 表示规范化后是否存在非空首提示词。
	PromptPresent bool `json:"prompt_present"`
	// PromptDigest 非空首提示词的 SHA-256 小写十六进制摘要，空提示词固定为空字符串。
	PromptDigest string `json:"prompt_digest"`
}

// CalculateQuickCreateSemanticDigest 按冻结 project_quick_create.v1 Canonical JSON 计算 HTTP 幂等语义摘要。
// 用户身份和幂等键另由数据库唯一作用域约束；客户端不可注入摘要或改变 Canonical 字段。
func CalculateQuickCreateSemanticDigest(promptPresent bool, promptDigest Digest) (Digest, error) {
	if promptPresent == isZeroDigest(promptDigest) {
		return Digest{}, ErrInvalidQuickCreate
	}
	promptDigestHex := ""
	if promptPresent {
		promptDigestHex = promptDigest.Hex()
	}
	encoded, err := json.Marshal(quickCreateCanonicalV1{
		SchemaVersion: QuickCreateCanonicalSchemaV1,
		PromptPresent: promptPresent,
		PromptDigest:  promptDigestHex,
	})
	if err != nil {
		return Digest{}, fmt.Errorf("encode quick create canonical request: %w", err)
	}
	return SHA256Digest(encoded), nil
}

// ensureSessionCanonicalV1 固定 Business 与 Agent 独立重算请求摘要时的 JSON 字段及顺序。
type ensureSessionCanonicalV1 struct {
	// SchemaVersion 摘要结构版本，W0 固定为 ensure_project_session.v1。
	SchemaVersion string `json:"schema_version"`
	// ProjectID Business Project 的规范 UUIDv7。
	ProjectID string `json:"project_id"`
	// OwnerUserID Business 可信所有者的规范 UUIDv7。
	OwnerUserID string `json:"owner_user_id"`
	// CreationSource Session 创建业务来源，W0 固定为 quick_create。
	CreationSource string `json:"creation_source"`
	// PromptPresent 表示规范化后是否存在非空首提示词。
	PromptPresent bool `json:"prompt_present"`
	// PromptDigest 非空首提示词的 SHA-256 小写十六进制摘要，空提示词固定为空字符串。
	PromptDigest string `json:"prompt_digest"`
	// SkillSnapshotMode W0 显式空 Skill Snapshot 模式。
	SkillSnapshotMode string `json:"skill_snapshot_mode"`
}

// CalculateEnsureSessionRequestDigest 按冻结 ensure_project_session.v1 Canonical JSON 计算 Agent 请求摘要。
// Project、Owner、Prompt Presence、Prompt Digest 和 empty Skill Snapshot 进入摘要；CommandID、RequestedAt、RequestID 和 TraceID 不进入摘要。
func CalculateEnsureSessionRequestDigest(projectID string, ownerUserID string, promptPresent bool, promptDigest Digest) (Digest, error) {
	normalizedProjectID, err := normalizeUUIDv7(projectID)
	if err != nil {
		return Digest{}, ErrInvalidQuickCreate
	}
	normalizedOwnerUserID, err := normalizeUUIDv7(ownerUserID)
	if err != nil {
		return Digest{}, ErrInvalidQuickCreate
	}
	if promptPresent == isZeroDigest(promptDigest) {
		return Digest{}, ErrInvalidQuickCreate
	}
	promptDigestHex := ""
	if promptPresent {
		promptDigestHex = promptDigest.Hex()
	}
	canonical := ensureSessionCanonicalV1{
		SchemaVersion: EnsureSessionCanonicalSchemaV1, ProjectID: normalizedProjectID, OwnerUserID: normalizedOwnerUserID,
		CreationSource: QuickCreateCreationSource, PromptPresent: promptPresent, PromptDigest: promptDigestHex,
		SkillSnapshotMode: EmptySkillSnapshotMode,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return Digest{}, fmt.Errorf("encode ensure session canonical request: %w", err)
	}
	return SHA256Digest(encoded), nil
}

// NormalizeEnsureSessionPrompt 按冻结跨 Module 规则处理首提示词并计算摘要。
// 输入必须是合法 UTF-8，先执行 NFC；纯 Unicode 空白折叠为 absent，其他正文保留规范化后的首尾空白。
func NormalizeEnsureSessionPrompt(initialPrompt string) (normalized string, promptDigest Digest, present bool, err error) {
	if !utf8.ValidString(initialPrompt) {
		return "", Digest{}, false, ErrInvalidQuickCreate
	}
	normalized = norm.NFC.String(initialPrompt)
	if len(normalized) > MaxInitialPromptBytes {
		return "", Digest{}, false, ErrInvalidQuickCreate
	}
	if strings.TrimFunc(normalized, unicode.IsSpace) == "" {
		return "", Digest{}, false, nil
	}
	return normalized, SHA256Digest([]byte(normalized)), true, nil
}

// Project 项目实体，保存所有权、生命周期、最近运行摘要和并发版本。
type Project struct {
	// ID 项目唯一标识，必须由 Business 应用生成 UUIDv7。
	ID string
	// OwnerUserID 项目所有者标识，只能来自可信鉴权 Principal。
	OwnerUserID string
	// Title 项目标题，不应保存完整首提示词。
	Title string
	// LifecycleStatus 项目生命周期状态。
	LifecycleStatus LifecycleStatus
	// RecentRunStatus 最近运行摘要，和生命周期独立演进。
	RecentRunStatus RecentRunStatus
	// InitialPromptStatus 首提示词初始化状态。
	InitialPromptStatus InitialPromptStatus
	// Version 项目聚合乐观并发版本，从 1 开始。
	Version int64
	// CreatedAt 项目创建的 UTC 时间。
	CreatedAt time.Time
	// UpdatedAt 项目最近更新的 UTC 时间。
	UpdatedAt time.Time
}

// CreationReceipt 项目快速创建幂等回执，冻结首次安全响应以支持同键重放。
type CreationReceipt struct {
	// ID 创建回执唯一标识，必须由 Business 应用生成 UUIDv7。
	ID string
	// OwnerUserID 发起命令的可信用户标识。
	OwnerUserID string
	// CommandType 幂等命令类型，W0 固定为 quick_create。
	CommandType string
	// KeyDigest 客户端幂等键摘要，数据库不保存原始键。
	KeyDigest Digest
	// SemanticDigest 规范化快速创建语义摘要，用于识别同键异义。
	SemanticDigest Digest
	// ProjectID 首次命令创建的项目标识。
	ProjectID string
	// LifecycleStatus 首次响应中的项目生命周期快照。
	LifecycleStatus LifecycleStatus
	// RecentRunStatus 首次响应中的最近运行摘要。
	RecentRunStatus RecentRunStatus
	// SessionProvisioningStatus 首次响应中的默认 Session 初始化状态。
	SessionProvisioningStatus ProvisioningStatus
	// InitialPromptStatus 首次响应中的首提示词状态。
	InitialPromptStatus InitialPromptStatus
	// CreatedAt 首次命令提交的 UTC 时间。
	CreatedAt time.Time
}

// SessionBinding Project 与 Agent 默认 Session 的跨数据库逻辑绑定。
type SessionBinding struct {
	// ID 绑定唯一标识，必须由 Business 应用生成 UUIDv7。
	ID string
	// ProjectID 所属 Business Project 标识。
	ProjectID string
	// CommandID Agent Session 初始化命令标识，必须和 Outbox ID 一致。
	CommandID string
	// RequestDigest 按 ensure_project_session.v1 Canonical Schema 计算的 Agent 请求摘要。
	RequestDigest Digest
	// AgentSessionID Agent 权威 Session 标识，尚未就绪时为空。
	AgentSessionID *string
	// AgentInputID Agent 权威首 Input 标识，空提示词或尚未就绪时为空。
	AgentInputID *string
	// ProvisioningStatus Session 初始化状态。
	ProvisioningStatus ProvisioningStatus
	// LastErrorCode 最近一次稳定错误码，不包含内部堆栈或用户内容。
	LastErrorCode *string
	// Version 绑定并发版本，从 1 开始并用于条件更新。
	Version int64
	// CreatedAt 绑定创建的 UTC 时间。
	CreatedAt time.Time
	// UpdatedAt 绑定最近更新的 UTC 时间。
	UpdatedAt time.Time
}

// EncryptedPayload 首提示词的受保护负载；交付前承载密文元数据，清理后只保留 PayloadDigest。
type EncryptedPayload struct {
	// Algorithm 认证加密算法，W0 固定为 aes-256-gcm。
	Algorithm string
	// KeyVersion 外部密钥的版本引用，不包含密钥材料。
	KeyVersion string
	// Nonce 单次认证加密随机数，AES-GCM 固定为 12 字节且不得复用。
	Nonce []byte
	// Ciphertext 首提示词密文及认证标签，不得包含或回退为明文。
	Ciphertext []byte
	// PayloadDigest 规范化明文摘要，用于 Agent 解密后的完整性核对。
	PayloadDigest Digest
}

// SessionOutbox Agent 默认 Session 初始化命令，必须和 Project 创建事实处于同一事务。
type SessionOutbox struct {
	// ID Outbox 事件标识，同时作为跨服务稳定 command_id。
	ID string
	// EventType 事件类型，W0 固定为 agent.session.ensure。
	EventType string
	// SchemaVersion 事件负载契约版本，W0 固定为 agent.session-bootstrap.v1。
	SchemaVersion string
	// AggregateID 所属 Business Project 标识。
	AggregateID string
	// OwnerUserID 命令冻结的可信项目所有者标识。
	OwnerUserID string
	// RequestDigest Agent 必须按 ensure_project_session.v1 Canonical Schema 独立重算并核对的请求摘要。
	RequestDigest Digest
	// HasInitialPrompt 表示命令是否携带非空首提示词。
	HasInitialPrompt bool
	// EncryptedPayload 首提示词密文元数据；空提示词时必须为空。
	EncryptedPayload *EncryptedPayload
	// SkillSnapshotDigest 是 v2 冻结的 Snapshot set 摘要；v1 为零值且由数据库兼容列审计 empty digest。
	SkillSnapshotDigest Digest
	// SkillCount 是 v2 冻结的 Skill 数量；v1 固定零。
	SkillCount int32
	// BindingSetVersion 是 v2 冻结的 Project Skill Binding Set 版本；v1 为空。
	BindingSetVersion *int64
	// ResolutionID 是 v2 不可变解析头逻辑引用；v1 为空。
	ResolutionID *string
	// PayloadClearedAt Agent Receipt 确认后清除密文的 UTC 时间；仅 delivered 状态可存在。
	PayloadClearedAt *time.Time
	// Status Outbox 权威派发状态。
	Status OutboxStatus
	// AvailableAt 命令允许被 Dispatcher 领取的最早 UTC 时间。
	AvailableAt time.Time
	// LeaseOwner 当前短租约 Owner，仅 processing 状态存在。
	LeaseOwner *string
	// LeaseVersion 当前 Fencing 版本，每次成功领取后递增。
	LeaseVersion int64
	// LeaseExpiresAt 当前短租约过期时间，仅 processing 状态存在。
	LeaseExpiresAt *time.Time
	// AttemptCount 已经开始的派发尝试次数。
	AttemptCount int32
	// RecoveryRequired 只由 Repository Claim 投影，表示上一次写结果未知，本次必须先 Query 原 command_id。
	// 该字段不是持久化业务事实；retry 或过期 processing 被重新领取时为 true。
	RecoveryRequired bool
	// MaxAttempts 版本化配置允许的最大派发尝试次数。
	MaxAttempts int32
	// DeliveredAt Agent 确认原命令回执的 UTC 时间，仅 delivered 状态存在。
	DeliveredAt *time.Time
	// CreatedAt Outbox 创建的 UTC 时间。
	CreatedAt time.Time
	// UpdatedAt Outbox 最近更新的 UTC 时间。
	UpdatedAt time.Time
}

// QuickCreateSeed 构造快速创建聚合所需的应用输入，所有 ID 和加密结果都由调用方注入。
type QuickCreateSeed struct {
	// ProjectID 待创建 Project 的 UUIDv7。
	ProjectID string
	// ReceiptID 待创建幂等回执的 UUIDv7。
	ReceiptID string
	// BindingID 待创建 Session 绑定的 UUIDv7。
	BindingID string
	// CommandID 待创建 Outbox 的 UUIDv7，也是跨服务 command_id。
	CommandID string
	// OwnerUserID 来自可信鉴权 Principal 的用户 UUIDv7。
	OwnerUserID string
	// InitialPrompt 仅供构造期完成 NFC、空白和摘要核对；不得进入 Entity、Model、日志、Trace 或错误。
	InitialPrompt string
	// KeyDigest 原始 Idempotency-Key 的 SHA-256 摘要。
	KeyDigest Digest
	// EncryptedPayload 非空首提示词的认证加密结果，空提示词时为空。
	EncryptedPayload *EncryptedPayload
	// MaxAttempts Outbox 版本化配置允许的最大尝试次数。
	MaxAttempts int32
	// OccurredAt 快速创建命令冻结并提交的 UTC 时间。
	OccurredAt time.Time
}

// QuickCreateAggregate 快速创建本地事务聚合，确保 Project、Receipt、Binding 和 Outbox 始终成套提交。
type QuickCreateAggregate struct {
	// Project 待创建的 Business Project。
	Project Project
	// Receipt 待创建的公开幂等回执。
	Receipt CreationReceipt
	// Binding 待创建的 Project 与 Agent Session 逻辑绑定。
	Binding SessionBinding
	// Outbox 待创建的 Agent Session 初始化命令。
	Outbox SessionOutbox
}

// NewQuickCreateAggregate 按首提示词是否存在建立 W0 初始状态；成功返回可原子持久化的聚合，失败返回 ErrInvalidQuickCreate。
func NewQuickCreateAggregate(seed QuickCreateSeed) (QuickCreateAggregate, error) {
	_, promptDigest, promptPresent, err := NormalizeEnsureSessionPrompt(seed.InitialPrompt)
	if err != nil {
		return QuickCreateAggregate{}, err
	}
	// Prompt Presence 和 Digest 只来自构造期真实正文；密文元数据必须与内部摘要完全一致，不能自行声明另一语义。
	if promptPresent {
		if seed.EncryptedPayload == nil || seed.EncryptedPayload.PayloadDigest != promptDigest {
			return QuickCreateAggregate{}, ErrInvalidQuickCreate
		}
	} else if seed.EncryptedPayload != nil {
		return QuickCreateAggregate{}, ErrInvalidQuickCreate
	}

	recentRunStatus := RecentRunStatusIdle
	initialPromptStatus := InitialPromptStatusAbsent
	if promptPresent {
		recentRunStatus = RecentRunStatusQueued
		initialPromptStatus = InitialPromptStatusPending
	}
	quickCreateSemanticDigest, err := CalculateQuickCreateSemanticDigest(promptPresent, promptDigest)
	if err != nil {
		return QuickCreateAggregate{}, err
	}
	requestDigest, err := CalculateEnsureSessionRequestDigest(seed.ProjectID, seed.OwnerUserID, promptPresent, promptDigest)
	if err != nil {
		return QuickCreateAggregate{}, err
	}

	aggregate := QuickCreateAggregate{
		Project: Project{
			ID:                  seed.ProjectID,
			OwnerUserID:         seed.OwnerUserID,
			Title:               DefaultProjectTitle,
			LifecycleStatus:     LifecycleStatusActive,
			RecentRunStatus:     recentRunStatus,
			InitialPromptStatus: initialPromptStatus,
			Version:             1,
			CreatedAt:           seed.OccurredAt,
			UpdatedAt:           seed.OccurredAt,
		},
		Receipt: CreationReceipt{
			ID:                        seed.ReceiptID,
			OwnerUserID:               seed.OwnerUserID,
			CommandType:               QuickCreateCommandType,
			KeyDigest:                 seed.KeyDigest,
			SemanticDigest:            quickCreateSemanticDigest,
			ProjectID:                 seed.ProjectID,
			LifecycleStatus:           LifecycleStatusActive,
			RecentRunStatus:           recentRunStatus,
			SessionProvisioningStatus: ProvisioningStatusPending,
			InitialPromptStatus:       initialPromptStatus,
			CreatedAt:                 seed.OccurredAt,
		},
		Binding: SessionBinding{
			ID:                 seed.BindingID,
			ProjectID:          seed.ProjectID,
			CommandID:          seed.CommandID,
			RequestDigest:      requestDigest,
			ProvisioningStatus: ProvisioningStatusPending,
			Version:            1,
			CreatedAt:          seed.OccurredAt,
			UpdatedAt:          seed.OccurredAt,
		},
		Outbox: SessionOutbox{
			ID:               seed.CommandID,
			EventType:        EnsureSessionEventType,
			SchemaVersion:    EnsureSessionSchemaVersion,
			AggregateID:      seed.ProjectID,
			OwnerUserID:      seed.OwnerUserID,
			RequestDigest:    requestDigest,
			HasInitialPrompt: promptPresent,
			EncryptedPayload: cloneEncryptedPayload(seed.EncryptedPayload),
			Status:           OutboxStatusPending,
			AvailableAt:      seed.OccurredAt,
			LeaseVersion:     0,
			AttemptCount:     0,
			MaxAttempts:      seed.MaxAttempts,
			CreatedAt:        seed.OccurredAt,
			UpdatedAt:        seed.OccurredAt,
		},
	}
	if err := aggregate.Validate(); err != nil {
		return QuickCreateAggregate{}, err
	}
	return aggregate, nil
}

// Validate 校验快速创建四类事实的字段与交叉引用；成功后 Repository 才能开始事务写入。
func (a QuickCreateAggregate) Validate() error {
	if !isUUIDv7(a.Project.ID) || !isUUIDv7(a.Project.OwnerUserID) || !isUUIDv7(a.Receipt.ID) || !isUUIDv7(a.Binding.ID) || !isUUIDv7(a.Outbox.ID) {
		return ErrInvalidQuickCreate
	}
	if a.Project.Title != DefaultProjectTitle {
		return ErrInvalidQuickCreate
	}
	if a.Project.LifecycleStatus != LifecycleStatusActive || a.Project.Version != 1 || a.Project.CreatedAt.IsZero() || !a.Project.CreatedAt.Equal(a.Project.UpdatedAt) {
		return ErrInvalidQuickCreate
	}
	if a.Receipt.OwnerUserID != a.Project.OwnerUserID || a.Receipt.ProjectID != a.Project.ID || a.Receipt.CommandType != QuickCreateCommandType {
		return ErrInvalidQuickCreate
	}
	if isZeroDigest(a.Receipt.KeyDigest) || isZeroDigest(a.Receipt.SemanticDigest) || isZeroDigest(a.Binding.RequestDigest) || a.Binding.RequestDigest != a.Outbox.RequestDigest {
		return ErrInvalidQuickCreate
	}
	if a.Receipt.LifecycleStatus != a.Project.LifecycleStatus || a.Receipt.RecentRunStatus != a.Project.RecentRunStatus || a.Receipt.InitialPromptStatus != a.Project.InitialPromptStatus {
		return ErrInvalidQuickCreate
	}
	if a.Receipt.SessionProvisioningStatus != ProvisioningStatusPending || !a.Receipt.CreatedAt.Equal(a.Project.CreatedAt) {
		return ErrInvalidQuickCreate
	}
	if a.Binding.ProjectID != a.Project.ID || a.Binding.CommandID != a.Outbox.ID || a.Binding.ProvisioningStatus != ProvisioningStatusPending || a.Binding.Version != 1 {
		return ErrInvalidQuickCreate
	}
	if a.Binding.AgentSessionID != nil || a.Binding.AgentInputID != nil || a.Binding.LastErrorCode != nil || !a.Binding.CreatedAt.Equal(a.Project.CreatedAt) || !a.Binding.UpdatedAt.Equal(a.Project.CreatedAt) {
		return ErrInvalidQuickCreate
	}
	if a.Outbox.EventType != EnsureSessionEventType || a.Outbox.SchemaVersion != EnsureSessionSchemaVersion || a.Outbox.AggregateID != a.Project.ID || a.Outbox.OwnerUserID != a.Project.OwnerUserID {
		return ErrInvalidQuickCreate
	}
	if a.Outbox.Status != OutboxStatusPending || a.Outbox.LeaseOwner != nil || a.Outbox.LeaseExpiresAt != nil || a.Outbox.DeliveredAt != nil || a.Outbox.LeaseVersion != 0 || a.Outbox.AttemptCount != 0 || a.Outbox.MaxAttempts <= 0 {
		return ErrInvalidQuickCreate
	}
	if a.Outbox.AvailableAt.IsZero() || !a.Outbox.CreatedAt.Equal(a.Project.CreatedAt) || !a.Outbox.UpdatedAt.Equal(a.Project.CreatedAt) {
		return ErrInvalidQuickCreate
	}
	if err := a.Outbox.Validate(); err != nil {
		return err
	}
	promptDigest := Digest{}
	if a.Outbox.EncryptedPayload != nil {
		promptDigest = a.Outbox.EncryptedPayload.PayloadDigest
	}
	expectedQuickCreateSemanticDigest, err := CalculateQuickCreateSemanticDigest(a.Outbox.HasInitialPrompt, promptDigest)
	if err != nil || a.Receipt.SemanticDigest != expectedQuickCreateSemanticDigest {
		return ErrInvalidQuickCreate
	}
	expectedRequestDigest, err := CalculateEnsureSessionRequestDigest(a.Project.ID, a.Project.OwnerUserID, a.Outbox.HasInitialPrompt, promptDigest)
	if err != nil || a.Binding.RequestDigest != expectedRequestDigest || a.Outbox.RequestDigest != expectedRequestDigest {
		return ErrInvalidQuickCreate
	}

	// 空提示词绝不能留下密文、Input 等执行前兆；非空提示词则必须提供完整认证加密结果。
	if a.Outbox.HasInitialPrompt {
		if a.Project.RecentRunStatus != RecentRunStatusQueued || a.Project.InitialPromptStatus != InitialPromptStatusPending || !validEncryptedPayload(a.Outbox.EncryptedPayload) {
			return ErrInvalidQuickCreate
		}
	} else if a.Project.RecentRunStatus != RecentRunStatusIdle || a.Project.InitialPromptStatus != InitialPromptStatusAbsent || a.Outbox.EncryptedPayload != nil {
		return ErrInvalidQuickCreate
	}
	return nil
}

// Validate 校验 Session Outbox 的状态、租约、交付和 Prompt 密文保留三态；失败返回 ErrInvalidQuickCreate。
func (o SessionOutbox) Validate() error {
	if !isUUIDv7(o.ID) || !isUUIDv7(o.AggregateID) || !isUUIDv7(o.OwnerUserID) || isZeroDigest(o.RequestDigest) {
		return ErrInvalidQuickCreate
	}
	if o.EventType != EnsureSessionEventType ||
		(o.SchemaVersion != EnsureSessionSchemaVersion && o.SchemaVersion != EnsureSessionSchemaVersionV2) ||
		o.MaxAttempts <= 0 || o.AttemptCount < 0 || o.LeaseVersion < 0 {
		return ErrInvalidQuickCreate
	}
	if o.Status != OutboxStatusPending && o.Status != OutboxStatusProcessing && o.Status != OutboxStatusRetry && o.Status != OutboxStatusDelivered && o.Status != OutboxStatusDead {
		return ErrInvalidQuickCreate
	}
	if o.RecoveryRequired && o.Status != OutboxStatusProcessing {
		return ErrInvalidQuickCreate
	}
	if o.AvailableAt.IsZero() || o.CreatedAt.IsZero() || o.UpdatedAt.Before(o.CreatedAt) {
		return ErrInvalidQuickCreate
	}
	// Processing 必须拥有完整短租约，其他状态必须清除租约字段，避免过期 Owner 继续提交结果。
	if o.Status == OutboxStatusProcessing {
		if o.LeaseOwner == nil || *o.LeaseOwner == "" || o.LeaseExpiresAt == nil {
			return ErrInvalidQuickCreate
		}
	} else if o.LeaseOwner != nil || o.LeaseExpiresAt != nil {
		return ErrInvalidQuickCreate
	}
	if o.Status == OutboxStatusDelivered {
		if o.DeliveredAt == nil {
			return ErrInvalidQuickCreate
		}
	} else if o.DeliveredAt != nil {
		return ErrInvalidQuickCreate
	}

	if o.SchemaVersion == EnsureSessionSchemaVersionV2 {
		return o.validateV2Payload()
	}
	if !isZeroDigest(o.SkillSnapshotDigest) || o.SkillCount != 0 || o.BindingSetVersion != nil || o.ResolutionID != nil {
		return ErrInvalidQuickCreate
	}
	// V1 三种保留形态互斥：无 Prompt 全空；未清理 Prompt 具有完整密文；已清理 Prompt 仅在 delivered 后保留 Digest。
	if !o.HasInitialPrompt {
		if o.EncryptedPayload != nil || o.PayloadClearedAt != nil {
			return ErrInvalidQuickCreate
		}
		return nil
	}
	if o.PayloadClearedAt == nil {
		if !validEncryptedPayload(o.EncryptedPayload) {
			return ErrInvalidQuickCreate
		}
		return nil
	}
	if o.Status != OutboxStatusDelivered || o.DeliveredAt == nil || o.PayloadClearedAt.Before(*o.DeliveredAt) || !validClearedPayload(o.EncryptedPayload) {
		return ErrInvalidQuickCreate
	}
	return nil
}

// validateV2Payload 校验完整 Bootstrap v2 envelope、Snapshot metadata 与 delivered 清理三态。
func (o SessionOutbox) validateV2Payload() error {
	if isZeroDigest(o.SkillSnapshotDigest) || o.SkillCount < 0 || o.SkillCount > 32 ||
		o.BindingSetVersion == nil || *o.BindingSetVersion < 1 || o.ResolutionID == nil || !isUUIDv7(*o.ResolutionID) ||
		o.EncryptedPayload == nil {
		return ErrInvalidQuickCreate
	}
	if o.PayloadClearedAt == nil {
		if !validEncryptedPayload(o.EncryptedPayload) {
			return ErrInvalidQuickCreate
		}
		return nil
	}
	if o.Status != OutboxStatusDelivered || o.DeliveredAt == nil || o.PayloadClearedAt.Before(*o.DeliveredAt) || !validClearedPayload(o.EncryptedPayload) {
		return ErrInvalidQuickCreate
	}
	return nil
}

// QuickCreateResult 快速创建的安全领域结果，可直接支持首次提交和同键同语义重放。
type QuickCreateResult struct {
	// ProjectID 已创建或已重放的稳定项目标识。
	ProjectID string
	// LifecycleStatus 首次安全响应中的项目生命周期。
	LifecycleStatus LifecycleStatus
	// RecentRunStatus 首次安全响应中的最近运行摘要。
	RecentRunStatus RecentRunStatus
	// SessionProvisioningStatus 首次安全响应中的 Session 初始化状态。
	SessionProvisioningStatus ProvisioningStatus
	// InitialPromptStatus 首次安全响应中的首提示词状态。
	InitialPromptStatus InitialPromptStatus
	// CreatedAt 首次命令提交的 UTC 时间。
	CreatedAt time.Time
	// IdempotentReplay 表示本次调用是否命中已有同语义回执。
	IdempotentReplay bool
}

// BootstrapResult 项目工作台 Bootstrap 的 Business 权威读模型，合并 Project 与默认 Session 绑定。
// 该结果不包含 Outbox 密文、内部 RPC 错误或 Agent 业务数据，只暴露页面进入所需的安全引用。
type BootstrapResult struct {
	// ProjectID 当前用户拥有的 Project 标识。
	ProjectID string
	// Title Project 的安全展示标题。
	Title string
	// LifecycleStatus Project 生命周期状态。
	LifecycleStatus LifecycleStatus
	// RecentRunStatus Project 最近运行摘要。
	RecentRunStatus RecentRunStatus
	// InitialPromptStatus 首提示词初始化状态。
	InitialPromptStatus InitialPromptStatus
	// ProvisioningStatus 默认 Agent Session 的最终一致初始化状态。
	ProvisioningStatus ProvisioningStatus
	// AgentSessionID Agent 已确认的 Session 标识；未就绪时为空。
	AgentSessionID *string
	// AgentInputID Agent 已确认的首 Input 标识；空 Prompt 或未就绪时为空。
	AgentInputID *string
	// LastErrorCode 永久失败时的稳定代码；不包含内部 RPC 或数据库文本。
	LastErrorCode *string
	// UpdatedAt Project 或绑定最近变化的较晚 UTC 时间。
	UpdatedAt time.Time
}

// Validate 校验 Bootstrap 读模型的 UUIDv7、状态和 Session/Input 可选引用不变量。
func (r BootstrapResult) Validate() error {
	if !isUUIDv7(r.ProjectID) || r.Title == "" || r.UpdatedAt.IsZero() {
		return ErrInvalidQuickCreate
	}
	if r.LifecycleStatus != LifecycleStatusActive && r.LifecycleStatus != LifecycleStatusArchived &&
		r.LifecycleStatus != LifecycleStatusTrash && r.LifecycleStatus != LifecycleStatusDeleted {
		return ErrInvalidQuickCreate
	}
	if r.RecentRunStatus != RecentRunStatusIdle && r.RecentRunStatus != RecentRunStatusQueued &&
		r.RecentRunStatus != RecentRunStatusRunning && r.RecentRunStatus != RecentRunStatusWaitingUser &&
		r.RecentRunStatus != RecentRunStatusWaitingAsync && r.RecentRunStatus != RecentRunStatusSucceeded &&
		r.RecentRunStatus != RecentRunStatusPartialFailed && r.RecentRunStatus != RecentRunStatusFailed &&
		r.RecentRunStatus != RecentRunStatusCancelled {
		return ErrInvalidQuickCreate
	}
	if r.InitialPromptStatus != InitialPromptStatusAbsent && r.InitialPromptStatus != InitialPromptStatusPending &&
		r.InitialPromptStatus != InitialPromptStatusAccepted && r.InitialPromptStatus != InitialPromptStatusFailed {
		return ErrInvalidQuickCreate
	}
	if r.ProvisioningStatus != ProvisioningStatusPending && r.ProvisioningStatus != ProvisioningStatusReconciling &&
		r.ProvisioningStatus != ProvisioningStatusReady && r.ProvisioningStatus != ProvisioningStatusBlocked {
		return ErrInvalidQuickCreate
	}
	if r.AgentSessionID != nil && !isUUIDv7(*r.AgentSessionID) {
		return ErrInvalidQuickCreate
	}
	if r.AgentInputID != nil && (!isUUIDv7(*r.AgentInputID) || r.AgentSessionID == nil) {
		return ErrInvalidQuickCreate
	}
	// Ready 必须具有 Agent 权威 Session；其他状态不得伪装成可进入工作台的完成状态。
	if r.ProvisioningStatus == ProvisioningStatusReady && r.AgentSessionID == nil {
		return ErrInvalidQuickCreate
	}
	return nil
}

// CreationStatus 将内部 Provisioning 状态收敛为前端冻结的 provisioning、ready 或 failed。
func (r BootstrapResult) CreationStatus() string {
	switch r.ProvisioningStatus {
	case ProvisioningStatusReady:
		return "ready"
	case ProvisioningStatusBlocked:
		return "failed"
	default:
		return "provisioning"
	}
}

// Repository 定义 Project 快速创建与后续所有权读取所需的最小持久化能力。
type Repository interface {
	// CreateQuick 原子创建 Project、Receipt、Binding 和 Outbox；同键同摘要返回原结果，不同摘要返回 ErrIdempotencyConflict。
	CreateQuick(ctx context.Context, aggregate QuickCreateAggregate) (QuickCreateResult, error)
	// FindOwnedByID 按项目和可信所有者读取 Project；不存在或不属于该用户时统一返回 ErrProjectNotFound。
	FindOwnedByID(ctx context.Context, projectID string, ownerUserID string) (Project, error)
	// FindBootstrapOwnedByID 以一次集合查询读取 Project 与默认 Session 绑定；不存在或越权统一返回 ErrProjectNotFound。
	FindBootstrapOwnedByID(ctx context.Context, projectID string, ownerUserID string) (BootstrapResult, error)
}

// ResultFromReceipt 将持久化回执投影为安全结果；replay 只改变重放标记，不改变首次业务快照。
func ResultFromReceipt(receipt CreationReceipt, replay bool) QuickCreateResult {
	return QuickCreateResult{
		ProjectID:                 receipt.ProjectID,
		LifecycleStatus:           receipt.LifecycleStatus,
		RecentRunStatus:           receipt.RecentRunStatus,
		SessionProvisioningStatus: receipt.SessionProvisioningStatus,
		InitialPromptStatus:       receipt.InitialPromptStatus,
		CreatedAt:                 receipt.CreatedAt,
		IdempotentReplay:          replay,
	}
}

// validEncryptedPayload 校验 Outbox 仅携带完整 AES-256-GCM 密文元数据，避免持久化明文或半成品密文。
func validEncryptedPayload(payload *EncryptedPayload) bool {
	return payload != nil && payload.Algorithm == PromptEncryptionAlgorithm && payload.KeyVersion != "" && len(payload.KeyVersion) <= 64 && len(payload.Nonce) == 12 && len(payload.Ciphertext) > 16 && !isZeroDigest(payload.PayloadDigest)
}

// validClearedPayload 校验已交付 Prompt 仅保留不可逆摘要，不残留算法、密钥引用、Nonce 或密文。
func validClearedPayload(payload *EncryptedPayload) bool {
	return payload != nil && payload.Algorithm == "" && payload.KeyVersion == "" && len(payload.Nonce) == 0 && len(payload.Ciphertext) == 0 && !isZeroDigest(payload.PayloadDigest)
}

// cloneEncryptedPayload 深拷贝密文和随机数，避免调用方在事务提交前修改切片导致语义摘要与持久化内容不一致。
func cloneEncryptedPayload(payload *EncryptedPayload) *EncryptedPayload {
	if payload == nil {
		return nil
	}
	cloned := *payload
	cloned.Nonce = append([]byte(nil), payload.Nonce...)
	cloned.Ciphertext = append([]byte(nil), payload.Ciphertext...)
	return &cloned
}

// isZeroDigest 拒绝未计算的全零摘要，避免不同请求因默认值错误地共享幂等范围。
func isZeroDigest(digest Digest) bool {
	return digest == Digest{}
}

// isUUIDv7 校验标识是否为应用侧生成的 UUIDv7，确保主键和跨记录命令 ID 具有统一格式。
func isUUIDv7(value string) bool {
	_, err := normalizeUUIDv7(value)
	return err == nil
}

// normalizeUUIDv7 校验标识版本并返回 UUID 小写规范形式，确保 Business 与 Agent Canonical JSON 字节完全一致。
func normalizeUUIDv7(value string) (string, error) {
	id, err := uuid.Parse(value)
	if err != nil || id.Version() != 7 {
		return "", ErrInvalidQuickCreate
	}
	return id.String(), nil
}
