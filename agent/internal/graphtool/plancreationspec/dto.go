// Package plancreationspec 实现获准的 plan_creation_spec V1 开发预览 Graph Tool。
package plancreationspec

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
)

const (
	// ToolKey 是唯一主 Agent 可见的稳定高层 Tool Key。
	ToolKey = "plan_creation_spec"
	// ToolDefinitionVersion 是 V1 开发预览 Tool Definition 版本。
	ToolDefinitionVersion = "plan_creation_spec.v1preview1"
	// GraphName 是启动阶段编译的稳定 Graph 名称。
	GraphName = "plan_creation_spec_graph_v1"
	// IntentSchemaVersion 是模型可控 Intent 的严格版本。
	IntentSchemaVersion = "plan_creation_spec.preview.intent.v1"
	// ProposalSchemaVersion 是 ChatModel 候选输出的严格版本。
	ProposalSchemaVersion = "creation_spec.preview.proposal.v1"
	// DraftSchemaVersion 是 Business 草稿内容版本。
	DraftSchemaVersion = "creation_spec.draft.v1"
	// CardSchemaVersion 是 Workspace 安全 Draft Card 版本。
	CardSchemaVersion = "creation_spec.preview.card.v1"
	// EnqueueSchemaVersion 是 HTTP 202 回执版本。
	EnqueueSchemaVersion = "plan_creation_spec.preview.enqueue.v1"
	// SaveDigestSchemaVersion 是 Agent 与 Business 共同冻结的保存请求摘要版本。
	SaveDigestSchemaVersion = "creation_spec.preview.save-draft.digest.v1"
	// PromptVersion 是预览 Graph ChatModel Node 的冻结 Prompt 版本。
	PromptVersion = "graph_tool.plan_creation_spec.preview.v1"
	// ValidatorVersion 是独立确定性 Validator 的冻结版本。
	ValidatorVersion = "plan_creation_spec.preview.validator.v1"
	// ResultCodeCreated 表示 Business Draft 已创建或同义重放。
	ResultCodeCreated = "CREATION_SPEC_DRAFT_CREATED"
	// ResultCodeProtocolInvalid 表示模型/Graph 输出违反冻结协议不变量。
	ResultCodeProtocolInvalid = "CREATION_SPEC_PREVIEW_INVALID"
	// ResultCodeRuntimeInputInvalid 表示已持久化 Intent 无法通过认证解密或摘要/协议复验。
	ResultCodeRuntimeInputInvalid = "CREATION_SPEC_PREVIEW_INPUT_INVALID"
	// ResultCodeRuntimeProcessingFailed 表示可重试执行技术失败已耗尽。
	ResultCodeRuntimeProcessingFailed = "CREATION_SPEC_PREVIEW_PROCESSING_FAILED"
	// ResultCodeProjectNotFound 是 Business 权威确认 Project 不存在或不可访问的确定失败。
	ResultCodeProjectNotFound = "CREATION_SPEC_PROJECT_NOT_FOUND"
	// ResultCodeBusinessConflict 是 Business 权威确认项目版本或幂等命令冲突的确定失败。
	ResultCodeBusinessConflict = "CREATION_SPEC_PREVIEW_CONFLICT"
	// ResultCodeBusinessDisabled 是 Business 权威确认 Preview 未启用的确定失败。
	ResultCodeBusinessDisabled = "CREATION_SPEC_PREVIEW_DISABLED"
	// RecoveryCodeBusinessResendExhausted 表示同键 Save 恢复重发预算已经耗尽，结果仍待人工核对。
	RecoveryCodeBusinessResendExhausted = "CREATION_SPEC_SAVE_RECOVERY_EXHAUSTED"
	// DurableDraftCommandSchemaVersion 是加密恢复工件的严格序列化版本。
	DurableDraftCommandSchemaVersion = "creation_spec.preview.durable-draft-command.v1"
)

// IsDeadLetterFailure 识别必须投影 failed Event 同时将 Input 终结为 dead 的安全结果码。
func IsDeadLetterFailure(result Result) bool {
	if result.Status != "failed" {
		return false
	}
	switch result.ResultCode {
	case ResultCodeProtocolInvalid, ResultCodeRuntimeInputInvalid, ResultCodeRuntimeProcessingFailed:
		return true
	default:
		return false
	}
}

// IsAuthoritativeBusinessFailure 只允许已冻结 Business 命令后的明确无提交/冲突结论覆盖开放 Receipt。
func IsAuthoritativeBusinessFailure(result Result) bool {
	if result.Status != "failed" {
		return false
	}
	switch result.ResultCode {
	case ResultCodeProjectNotFound, ResultCodeBusinessConflict, ResultCodeBusinessDisabled:
		return true
	default:
		return false
	}
}

// Intent 是模型能够填写的最小严格预览意图；Audience 使用指针保留“省略”和“存在”的区别。
type Intent struct {
	// SchemaVersion 固定为 plan_creation_spec.preview.intent.v1。
	SchemaVersion string `json:"schema_version"`
	// Goal 是用户期望达成的创作目标。
	Goal string `json:"goal"`
	// DeliverableType 是 video、image_set、audio 或 mixed。
	DeliverableType string `json:"deliverable_type"`
	// Audience 是可选目标受众；显式空字符串合法，并与省略语义保持区分。
	Audience *string `json:"audience,omitempty"`
	// Locale 是 zh-CN 或 en-US。
	Locale string `json:"locale"`
	// Constraints 是按用户顺序冻结且精确去重的硬约束。
	Constraints []string `json:"constraints"`
}

// Phase 是 CreationSpec 中一个有序、稳定键的执行阶段。
type Phase struct {
	// Key 是 phase_1 至 phase_6。
	Key string `json:"key"`
	// Title 是阶段标题。
	Title string `json:"title"`
	// Objective 是阶段目标。
	Objective string `json:"objective"`
	// Output 是阶段预期产物。
	Output string `json:"output"`
}

// Proposal 是 ChatModel 只能生成的候选 DTO，不包含可信身份或业务状态。
type Proposal struct {
	// SchemaVersion 固定为 creation_spec.preview.proposal.v1。
	SchemaVersion string `json:"schema_version"`
	// Title 是候选 CreationSpec 标题。
	Title string `json:"title"`
	// Goal 是保留原用户目标语义的规范化目标。
	Goal string `json:"goal"`
	// DeliverableType 必须与 Intent 相同。
	DeliverableType string `json:"deliverable_type"`
	// Audience 是规范化目标受众；Intent 省略时允许空字符串。
	Audience string `json:"audience"`
	// Phases 是一至六个有序阶段。
	Phases []Phase `json:"phases"`
	// Constraints 必须完整包含 Intent 的全部硬约束。
	Constraints []string `json:"constraints"`
	// AcceptanceCriteria 是一至八条可验证验收标准。
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

// Content 是 Validator 通过后允许发送给 Business 的稳定 Draft 内容。
// 字段顺序属于保存请求摘要契约，禁止改为 map 或无版本调整。
type Content struct {
	// Title 是 CreationSpec 标题。
	Title string `json:"title"`
	// Goal 是整体创作目标。
	Goal string `json:"goal"`
	// DeliverableType 是稳定小写交付物类型。
	DeliverableType string `json:"deliverable_type"`
	// Audience 是可为空的目标受众。
	Audience string `json:"audience"`
	// Locale 是从可信 Intent 注入的 locale。
	Locale string `json:"locale"`
	// Phases 是一至六个有序阶段。
	Phases []Phase `json:"phases"`
	// Constraints 是零至八条硬约束。
	Constraints []string `json:"constraints"`
	// AcceptanceCriteria 是一至八条可验证验收标准。
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

// TrustedContext 是 Runtime 注入且绝不暴露到 Tool Schema 的命令上下文。
type TrustedContext struct {
	// Owner 是当前 Session Lane Lease owner，Receipt 写入必须与 PostgreSQL 当前值一致。
	Owner string
	// RequestID 是可信 Business 内部请求 UUIDv7。
	RequestID string
	// UserID 是可信 Business Principal UUIDv7。
	UserID string
	// ProjectID 是可信 Business Project UUIDv7。
	ProjectID string
	// SessionID 是 Agent Session UUIDv7。
	SessionID string
	// InputID 是稳定 Session Input UUIDv7。
	InputID string
	// TurnID 是稳定 Runner Turn UUIDv7。
	TurnID string
	// RunID 是稳定 Runner Run UUIDv7。
	RunID string
	// ToolCallID 是稳定 Graph Tool Call UUIDv7。
	ToolCallID string
	// BusinessCommandID 是 Save/Query 共同复用的稳定命令 UUIDv7。
	BusinessCommandID string
	// PromptVersion 是入队时冻结的 Prompt pin。
	PromptVersion string
	// ValidatorVersion 是入队时冻结的 Validator pin。
	ValidatorVersion string
	// FenceToken 是当前 Session Lane Claim Fence。
	FenceToken int64
}

// GraphInput 是编译后 Graph 的有类型入口。
type GraphInput struct {
	// TrustedContext 是 Runtime 提供的不可变可信上下文。
	TrustedContext TrustedContext
	// IntentJSON 是 Tool 收到的原始严格 JSON，用于在首 Node 再次失败关闭解码。
	IntentJSON []byte
}

// DomainContext 是 Business Owner 校验后返回的最小 Project 上下文。
type DomainContext struct {
	// ProjectID 是经过 Owner 校验的 Project UUIDv7。
	ProjectID string
	// ProjectVersion 是 Save Draft 必须原样携带的乐观版本。
	ProjectVersion int64
	// ProjectTitle 是允许进入 Prompt 的最小安全标题。
	ProjectTitle string
}

// DraftCommand 是 Validator 输出与 Business Command 输入之间的确定性边界。
type DraftCommand struct {
	// TrustedContext 是未经模型控制的命令上下文。
	TrustedContext TrustedContext
	// DomainContext 是 Owner 校验后的 Project 上下文。
	DomainContext DomainContext
	// Content 是通过独立 Validator 的 CreationSpec 内容。
	Content Content
	// RequestDigest 是与 Business 完全一致的保存请求摘要。
	RequestDigest string
}

// Resource 是 Business CreationSpec Draft 的安全冻结结果。
type Resource struct {
	// ID 是 Business CreationSpec UUIDv7。
	ID string
	// ProjectID 是所属 Business Project UUIDv7。
	ProjectID string
	// Version 是 Draft 聚合版本。
	Version int64
	// Status 在 V1 固定为 draft。
	Status string
	// ContentDigest 是 Business 内容摘要。
	ContentDigest string
	// Content 是 Business 回传并再次校验的完整安全内容。
	Content Content
}

// SaveDisposition 表示 Business Command 首次创建或同义重放。
type SaveDisposition string

const (
	// SaveDispositionCreated 表示首次创建 Draft。
	SaveDispositionCreated SaveDisposition = "created"
	// SaveDispositionReplayed 表示同键同义重放原 Draft。
	SaveDispositionReplayed SaveDisposition = "replayed"
)

// SaveOutcome 是 Save/Query 节点之间的有类型路由值。
type SaveOutcome struct {
	// Status 是 saved、unknown 或 unresolved。
	Status string
	// Disposition 在 saved 时是 created 或 replayed。
	Disposition SaveDisposition
	// Resource 在 saved 时存在。
	Resource *Resource
	// Command 是 Unknown Outcome 查询必须复用的原命令。
	Command DraftCommand
	// Recovery 保存持久化重发预算快照；unresolved 时必须存在。
	Recovery *RecoveryDeferred
}

// ResourceRef 是 Tool Result 中可安全重放的 Business 资源引用。
type ResourceRef struct {
	// ID 是 Business CreationSpec UUIDv7。
	ID string `json:"id"`
	// Version 是 Draft 版本。
	Version int64 `json:"version"`
	// Digest 是 Draft 内容摘要。
	Digest string `json:"digest"`
	// Status 在 V1 固定为 draft。
	Status string `json:"status"`
}

// ReceiptRef 是 Tool Result 中最小回执引用。
type ReceiptRef struct {
	// ToolCallID 是 Agent first-write-wins Tool Receipt 标识。
	ToolCallID string `json:"tool_call_id"`
	// BusinessCommandID 是 Business first-write-wins Command 标识。
	BusinessCommandID string `json:"business_command_id"`
}

// Result 是 Graph Tool 对 Runtime 的严格终态或恢复待定输出。
type Result struct {
	// Status 是 completed、failed 或 recovery_pending。
	Status string `json:"status"`
	// ResultCode 是稳定结果码。
	ResultCode string `json:"result_code"`
	// ResourceRef 仅在 completed 时存在。
	ResourceRef *ResourceRef `json:"resource_ref,omitempty"`
	// ReceiptRef 始终指向当前稳定 Tool/Business 命令。
	ReceiptRef ReceiptRef `json:"receipt_ref"`
	// Summary 仅在 failed/recovery_pending 时携带安全中文说明。
	Summary string `json:"summary,omitempty"`
	// Retryable 表示同语义技术恢复是否可能成功。
	Retryable bool `json:"retryable"`
	// Card 是 Processor 完成投影所需的安全 DTO，不作为模型自由文本暴露。
	Card *Card `json:"-"`
	// BusinessRequestDigest 是 Receipt 恢复所需的原保存摘要，不进入 Tool Result JSON。
	BusinessRequestDigest string `json:"-"`
}

// MarshalJSON 按 status 输出冻结 exact-set；completed 不得携带 retryable:false 或 summary。
func (result Result) MarshalJSON() ([]byte, error) {
	switch result.Status {
	case "completed":
		type completedWire struct {
			Status      string       `json:"status"`
			ResultCode  string       `json:"result_code"`
			ResourceRef *ResourceRef `json:"resource_ref"`
			ReceiptRef  ReceiptRef   `json:"receipt_ref"`
		}
		return json.Marshal(completedWire{
			Status: result.Status, ResultCode: result.ResultCode,
			ResourceRef: result.ResourceRef, ReceiptRef: result.ReceiptRef,
		})
	case "failed":
		type failedWire struct {
			Status     string     `json:"status"`
			ResultCode string     `json:"result_code"`
			Summary    string     `json:"summary"`
			Retryable  bool       `json:"retryable"`
			ReceiptRef ReceiptRef `json:"receipt_ref"`
		}
		return json.Marshal(failedWire{
			Status: result.Status, ResultCode: result.ResultCode, Summary: result.Summary,
			Retryable: result.Retryable, ReceiptRef: result.ReceiptRef,
		})
	default:
		return nil, fmt.Errorf("marshal creation spec result: invalid terminal status")
	}
}

// RecoveryDeferred 表示 Save Unknown Outcome 尚未消除；它不是 Tool Result，禁止写入 result_ciphertext/digest。
type RecoveryDeferred struct {
	// ToolCallID 是待恢复 Tool Receipt 标识。
	ToolCallID string
	// BusinessCommandID 是后续只能复用的原 Business Command 标识。
	BusinessCommandID string
	// RequestDigest 是后续 Query 必须复用的原保存摘要。
	RequestDigest string
	// ContentDigest 是首次保存命令内容摘要，用于无需重跑模型即可校验 Query completed 资源。
	ContentDigest string
	// Command 是从加密恢复工件中重建的完整 Business Save 命令；Owner/Fence 始终来自当前 Claim。
	Command DraftCommand
	// ResendAttempts 是 PostgreSQL 已原子预留的同键重发次数。
	ResendAttempts int
	// ResendLimit 是 Prepare 时从版本化运行配置冻结的重发上限。
	ResendLimit int
	// ResendExhausted 表示最终权威查询仍为 not_found 且已禁止继续自动重发。
	ResendExhausted bool
}

// Outcome 是 Graph 内部输出的显式联合；Terminal 与 Recovery 恰好一个存在。
type Outcome struct {
	// Terminal 是 completed 或确定 failed 的可冻结 Result。
	Terminal *Result
	// Recovery 是尚无终态时的恢复标记，绝不伪装或冻结为 Result。
	Recovery *RecoveryDeferred
}

// ResultStore 是 Graph Tool 消费的 first-write-wins Tool Receipt 端口。
type ResultStore interface {
	// ReplayTerminal 返回同 ToolCall 已冻结的 completed/failed Result；开放阶段返回 nil。
	ReplayTerminal(ctx context.Context, trusted TrustedContext) (*Result, error)
	// ReplayRecovery 返回 durable prepared/business_unknown 命令；pending 返回 nil。
	ReplayRecovery(ctx context.Context, trusted TrustedContext) (*RecoveryDeferred, error)
	// PrepareCommand 必须在 Save RPC 前以当前 owner/fence append-once 冻结原请求与内容摘要。
	PrepareCommand(ctx context.Context, command DraftCommand) error
	// ReserveCommandResend 在当前 fence 下原子消耗一次持久化重发预算；预算耗尽时返回 false 并冻结可观察阶段。
	ReserveCommandResend(ctx context.Context, trusted TrustedContext, recovery RecoveryDeferred) (RecoveryDeferred, bool, error)
	// FreezeTerminal 在投影前冻结 completed/failed 完整 Result；同键异义必须冲突。
	FreezeTerminal(ctx context.Context, trusted TrustedContext, result Result) error
	// MarkRecovery 只 CAS open stage 为 business_unknown，不写 result 密文或摘要。
	MarkRecovery(ctx context.Context, trusted TrustedContext, recovery RecoveryDeferred) error
}

// Card 是 Workspace Snapshot 与 completed Event 共用的完整替换 DTO。
type Card struct {
	// SchemaVersion 固定为 creation_spec.preview.card.v1。
	SchemaVersion string `json:"schema_version"`
	// CreationSpecID 是 Business CreationSpec UUIDv7。
	CreationSpecID string `json:"creation_spec_id"`
	// ProjectID 是 Business Project UUIDv7。
	ProjectID string `json:"project_id"`
	// Version 是 Draft 聚合版本。
	Version int64 `json:"version"`
	// Status 在 V1 固定为 draft。
	Status string `json:"status"`
	// ContentDigest 是小写 64 位 SHA-256。
	ContentDigest string `json:"content_digest"`
	// Title 是 CreationSpec 标题。
	Title string `json:"title"`
	// Goal 是 CreationSpec 目标。
	Goal string `json:"goal"`
	// DeliverableType 是稳定交付物类型。
	DeliverableType string `json:"deliverable_type"`
	// Audience 始终编码为字符串；Intent 省略时为空字符串而非 null。
	Audience string `json:"audience"`
	// Locale 是 zh-CN 或 en-US。
	Locale string `json:"locale"`
	// Phases 是完整阶段数组。
	Phases []Phase `json:"phases"`
	// Constraints 是完整约束数组。
	Constraints []string `json:"constraints"`
	// AcceptanceCriteria 是完整验收标准数组。
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	// UpdatedAt 是 Agent 冻结投影的 UTC 时间。
	UpdatedAt time.Time `json:"updated_at"`
}

// State 是单次 Graph 调用的 typed local state；它不是 PostgreSQL 权威状态。
type State struct {
	// TrustedContext 是 Runtime 注入的可信上下文。
	TrustedContext TrustedContext
	// Intent 是严格解码后的模型可控意图。
	Intent Intent
	// IntentDigest 是 Intent canonical 摘要。
	IntentDigest string
	// DomainContext 是 Business 返回的最小 Project 上下文。
	DomainContext DomainContext
	// PromptMessages 是版本化模板输出，仅存在于调用内存。
	PromptMessages []*schema.Message
	// ModelMessage 是候选模型完整消息，不写普通日志或 Event。
	ModelMessage *schema.Message
	// Proposal 是严格解析前的候选 DTO。
	Proposal Proposal
	// ValidationReport 是确定性 Validator 的安全结果码。
	ValidationReport string
	// Draft 是允许进入 Business Command 的内容。
	Draft Content
	// BusinessCommandReceipt 是 Business 返回的最小资源引用。
	BusinessCommandReceipt *Resource
	// Result 是最终 Graph Tool Result。
	Result Result
	// Error 是不包含 Provider 原文的稳定失败码。
	Error string
}

// BusinessClient 是 Graph 消费方定义的三方法最小 RPC 接口。
type BusinessClient interface {
	// GetCreationSpecContext 校验 Owner 并返回最小 Project 上下文。
	GetCreationSpecContext(ctx context.Context, requestID, userID, projectID string) (DomainContext, error)
	// SaveCreationSpecDraft 以稳定 command_id first-write-wins 保存 Draft。
	SaveCreationSpecDraft(ctx context.Context, command DraftCommand) (SaveDisposition, Resource, error)
	// QueryCreationSpecDraftCommand 只查询原 command_id 与原 request digest。
	QueryCreationSpecDraftCommand(ctx context.Context, command DraftCommand) (string, *Resource, error)
}
