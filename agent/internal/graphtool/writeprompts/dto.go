// Package writeprompts 实现获准但默认不注册的 write_prompts V2 Prompt Draft 开发预览。
package writeprompts

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
)

const (
	// ToolKey 是主 Agent Catalog 中的稳定高层能力标识；本 Profile 不进入生产可执行 Registry。
	ToolKey = "write_prompts"
	// ToolDefinitionVersion 是本批唯一获准实现的 Tool Core 版本。
	ToolDefinitionVersion = "write_prompts.v2preview1"
	// GraphName 是启动阶段编译的开发预览 Graph 名称。
	GraphName = "write_prompts_graph_v2preview1"
	// StateSchemaName 是本地 Graph State 的稳定诊断名称；当前不启用 Eino Checkpoint。
	StateSchemaName = "dora.agent.graphtool.write_prompts.state.v2preview1"
	// IntentSchemaVersion 是模型可控 Tool Intent 的严格版本。
	IntentSchemaVersion = "write_prompts.preview.intent.v1"
	// CandidateSchemaVersion 是内部 ChatModel 候选的严格版本。
	CandidateSchemaVersion = "prompt.preview.candidate.v1"
	// DraftSchemaVersion 是 Business Prompt Preview Draft 的内容版本。
	DraftSchemaVersion = "prompt.preview.draft.v1"
	// ResultSchemaVersion 是 Tool completed/failed 结果的严格版本。
	ResultSchemaVersion = "write_prompts.preview.result.v1"
	// CardSchemaVersion 是 Workspace 安全 Card 的冻结版本。
	CardSchemaVersion = "prompt.preview.card.v1"
	// SaveDigestSchemaVersion 是 Agent 与 Business 共同冻结的保存请求摘要版本。
	SaveDigestSchemaVersion = "prompt.preview.save-draft.digest.v1"
	// DurableDraftCommandSchemaVersion 是加密恢复工件的严格序列化版本。
	DurableDraftCommandSchemaVersion = "prompt.preview.durable-draft-command.v1"
	// PromptKey 是 Prompt 写作模板的稳定键。
	PromptKey = "graph_tool.write_prompts.preview.primary"
	// PromptVersion 是 Prompt 写作模板的语义版本。
	PromptVersion = "graph_tool.write_prompts.preview.v1"
	// ValidatorVersion 是候选协议 Validator 的冻结版本。
	ValidatorVersion = "write_prompts.preview.validator.v1"
	// ExactSetValidatorVersion 是目标全集 Validator 的冻结版本。
	ExactSetValidatorVersion = "write_prompts.preview.exact-set-validator.v1"
	// RuntimePolicyVersion 是目标预算与默认语言策略的冻结版本。
	RuntimePolicyVersion = "write_prompts.preview.runtime-policy.v1"

	// ResultCodeCompleted 表示 Business Prompt Preview Draft 已创建或同义重放。
	ResultCodeCompleted = "PROMPT_PREVIEW_DRAFT_CREATED"
	// ResultCodeInvalidArgument 表示模型可控 Intent 违反严格协议。
	ResultCodeInvalidArgument = "PROMPT_PREVIEW_INVALID_ARGUMENT"
	// ResultCodeStoryboardNotFound 对外折叠 Storyboard 不存在与无权限。
	ResultCodeStoryboardNotFound = "PROMPT_PREVIEW_STORYBOARD_NOT_FOUND"
	// ResultCodeStoryboardConflict 表示 Storyboard 版本或摘要已变化。
	ResultCodeStoryboardConflict = "PROMPT_PREVIEW_STORYBOARD_CONFLICT"
	// ResultCodeNoTargets 表示 Source Storyboard 没有任何 Slot。
	ResultCodeNoTargets = "PROMPT_PREVIEW_NO_TARGETS"
	// ResultCodeTargetBudgetExceeded 表示全部 Slot 超过冻结目标预算。
	ResultCodeTargetBudgetExceeded = "PROMPT_PREVIEW_TARGET_BUDGET_EXCEEDED"
	// ResultCodeCandidateInvalid 表示模型候选未通过严格协议校验。
	ResultCodeCandidateInvalid = "PROMPT_PREVIEW_CANDIDATE_INVALID"
	// ResultCodeExactSetInvalid 表示候选目标与冻结全集不一致。
	ResultCodeExactSetInvalid = "PROMPT_PREVIEW_EXACT_TARGET_SET_INVALID"
	// ResultCodeBusinessConflict 表示 Business 保存命令发生版本或幂等冲突。
	ResultCodeBusinessConflict = "PROMPT_PREVIEW_CONFLICT"
	// ResultCodeBusinessDisabled 表示 Business Prompt Preview 明确关闭。
	ResultCodeBusinessDisabled = "PROMPT_PREVIEW_DISABLED"
	// ResultCodeInternal 表示 Processor 内部恢复诊断码；它不得进入 completed/failed Tool Result。
	ResultCodeInternal = "PROMPT_PREVIEW_INTERNAL"
)

const (
	maxJSONBytes      = 128 * 1024
	maxSourceSlots    = 96
	maxSourceElements = 24

	intentInstructionMin   = 1
	intentInstructionMax   = 1000
	projectTitleMin        = 1
	projectTitleMax        = 160
	sourceTitleMin         = 1
	sourceTitleMax         = 120
	sourceSummaryMin       = 1
	sourceSummaryMax       = 1000
	elementTitleMin        = 1
	elementTitleMax        = 120
	elementNarrativeMin    = 1
	elementNarrativeMax    = 1000
	slotPurposeMin         = 1
	slotPurposeMax         = 500
	positivePromptMin      = 1
	positivePromptMax      = 4000
	negativeConstraintMin  = 1
	negativeConstraintMax  = 500
	maxNegativeConstraints = 16
	trustedOwnerMin        = 1
	trustedOwnerMax        = 200
	resultSummaryMin       = 1
	resultSummaryMax       = 500
)

var (
	localeValues    = [...]string{"zh-CN", "en-US"}
	slotTypeValues  = [...]string{"image", "video", "audio", "voiceover", "caption"}
	mediaKindValues = [...]string{"image", "video", "audio", "text"}
)

// StoryboardPreviewRef 是 Runtime 冻结且模型无权填写的 Storyboard Preview Draft 引用。
type StoryboardPreviewRef struct {
	// ID 是 Business Storyboard Preview Draft UUIDv7。
	ID string `json:"id"`
	// Version 本批固定为 1。
	Version int64 `json:"version"`
	// ContentDigest 是 Source Draft 内容的小写 SHA-256。
	ContentDigest string `json:"content_digest"`
}

// Intent 是模型能够填写的最小 Prompt 写作意图，不包含资源身份、目标或版本。
type Intent struct {
	// SchemaVersion 固定为 write_prompts.preview.intent.v1。
	SchemaVersion string `json:"schema_version"`
	// WritingInstruction 是用户提供的 Prompt 写作要求。
	WritingInstruction string `json:"writing_instruction"`
	// OutputLanguage 是可选输出语言；空值由冻结 Runtime Policy 补齐。
	OutputLanguage string `json:"output_language,omitempty"`
}

// Policy 是启动时注入且用户与模型均无权修改的目标预算策略。
type Policy struct {
	// Version 固定为 write_prompts.preview.runtime-policy.v1。
	Version string `json:"version"`
	// MaxTargets 是单次必须完整处理的最大目标数。
	MaxTargets int `json:"max_targets"`
	// DefaultOutputLanguage 是 Intent 未指定时采用的语言。
	DefaultOutputLanguage string `json:"default_output_language"`
	// MaxCommandResends 是 Save Unknown Outcome 经权威 not_found 后允许的自动重发上限。
	MaxCommandResends int `json:"max_command_resends"`
}

// TrustedContext 是 Runtime 注入且绝不暴露到 Tool Schema 的可信命令上下文。
type TrustedContext struct {
	// Owner 是已认证的 Runtime Owner 类型。
	Owner string
	// RequestID 是当前 API 请求 UUIDv7。
	RequestID string
	// UserID 是当前 Owner 用户 UUIDv7。
	UserID string
	// ProjectID 是目标项目 UUIDv7。
	ProjectID string
	// SessionID 是当前会话 UUIDv7。
	SessionID string
	// InputID 是当前输入 UUIDv7。
	InputID string
	// TurnID 是当前 Turn UUIDv7。
	TurnID string
	// RunID 是当前 Run UUIDv7。
	RunID string
	// ToolCallID 是当前 ToolCall UUIDv7。
	ToolCallID string
	// BusinessCommandID 是必须跨 Unknown Outcome 复用的 Business 命令 UUIDv7。
	BusinessCommandID string
	// FenceToken 是当前 Claim 的正整数隔离令牌。
	FenceToken int64
	// StoryboardPreviewRef 是 Runtime 认证并冻结的上游 Draft 引用。
	StoryboardPreviewRef StoryboardPreviewRef
	// PromptVersion 是冻结的 Prompt 版本。
	PromptVersion string
	// ValidatorVersion 是冻结的候选校验器版本。
	ValidatorVersion string
	// ExactSetValidatorVersion 是冻结的目标全集校验器版本。
	ExactSetValidatorVersion string
	// Policy 是冻结的 Runtime 目标预算策略。
	Policy Policy
}

// GraphInput 是编译后 Graph 的有类型入口。
type GraphInput struct {
	// TrustedContext 是 Runtime 提供的不可变可信上下文。
	TrustedContext TrustedContext
	// IntentJSON 是 Tool 收到的 canonical 严格 JSON。
	IntentJSON []byte
}

// StoryboardElement 是 Source Draft 中解析出的最小 Element 上下文。
type StoryboardElement struct {
	// Key 是 element_1 至 element_24 的局部键。
	Key string `json:"key"`
	// Order 是从 1 开始的全局连续顺序。
	Order int `json:"order"`
	// Title 是进入模型数据区的元素标题。
	Title string `json:"title"`
	// NarrativePurpose 是进入模型数据区的叙事目的。
	NarrativePurpose string `json:"narrative_purpose"`
}

// StoryboardSlot 是 Source Draft 中解析出的最小 Slot 上下文。
type StoryboardSlot struct {
	// Key 是 slot_1 至 slot_96 的局部键。
	Key string `json:"key"`
	// ElementKey 指向同一 Source Draft 的 Element。
	ElementKey string `json:"element_key"`
	// SlotType 是冻结的 Source Slot 类型。
	SlotType string `json:"slot_type"`
	// Purpose 是冻结的 Source Slot 用途。
	Purpose string `json:"purpose"`
	// Required 表示后续生产是否必须满足该需求槽。
	Required bool `json:"required"`
}

// StoryboardContent 是 Business 返回并允许进入最小 Prompt 的 Source 摘要。
type StoryboardContent struct {
	// SchemaVersion 固定为 storyboard.preview.draft.v1。
	SchemaVersion string `json:"schema_version"`
	// Title 是 Source Draft 标题。
	Title string `json:"title"`
	// Summary 是 Source Draft 摘要。
	Summary string `json:"summary"`
	// Elements 是全局有序 Element 列表。
	Elements []StoryboardElement `json:"elements"`
	// Slots 是 Source Draft 的完整 Slot 列表。
	Slots []StoryboardSlot `json:"slots"`
}

// StoryboardResource 是 Business Owner 校验后的不可变 Source Draft 快照。
type StoryboardResource struct {
	// ID 是 Storyboard Preview Draft UUIDv7。
	ID string
	// ProjectID 是资源所属项目 UUIDv7。
	ProjectID string
	// Version 是冻结版本，本批必须与可信引用一致。
	Version int64
	// Status 是资源状态，本批只接受 draft。
	Status string
	// ContentDigest 是完整 Source Draft 内容的小写 SHA-256。
	ContentDigest string
	// Content 是进入 Prompt 的最小 Storyboard 上下文。
	Content StoryboardContent
}

// GenerationContext 是一次 RPC 返回的 Project 与 Storyboard 一致快照。
type GenerationContext struct {
	// ProjectID 是已通过 Owner 校验的项目 UUIDv7。
	ProjectID string
	// ProjectVersion 是保存时使用的乐观锁版本。
	ProjectVersion int64
	// ProjectTitle 是进入最小 Prompt 的项目标题。
	ProjectTitle string
	// Storyboard 是已通过 Owner 校验的不可变 Source Draft 快照。
	Storyboard StoryboardResource
}

// PromptTarget 是从 Source Draft 确定性映射出的不可变目标。
type PromptTarget struct {
	// TargetLocalKey 是 Source Slot 的局部键。
	TargetLocalKey string `json:"target_local_key"`
	// ElementLocalKey 是 Source Element 的局部键。
	ElementLocalKey string `json:"element_local_key"`
	// ElementTitle 是帮助模型写作的 Source Element 标题。
	ElementTitle string `json:"element_title"`
	// NarrativePurpose 是帮助模型写作的 Source Element 叙事目的。
	NarrativePurpose string `json:"narrative_purpose"`
	// SlotType 是冻结的 Source Slot 类型。
	SlotType string `json:"slot_type"`
	// MediaKind 是由 SlotType 确定性派生的媒体类型。
	MediaKind string `json:"media_kind"`
	// Purpose 是冻结的 Source Slot 用途。
	Purpose string `json:"purpose"`
	// Required 表示该 Slot 是否必须满足。
	Required bool `json:"required"`
}

// CandidatePrompt 是 ChatModel 唯一允许生成的单项目标 Prompt。
type CandidatePrompt struct {
	// TargetLocalKey 必须与冻结目标同序 exact-match。
	TargetLocalKey string `json:"target_local_key"`
	// PositivePrompt 是正向生成提示词。
	PositivePrompt string `json:"positive_prompt"`
	// NegativeConstraints 是零至十六项去重负面约束。
	NegativeConstraints []string `json:"negative_constraints"`
}

// Candidate 是 ChatModel 唯一允许输出的严格候选，不含可信身份或业务状态。
type Candidate struct {
	// SchemaVersion 固定为 prompt.preview.candidate.v1。
	SchemaVersion string `json:"schema_version"`
	// Prompts 必须覆盖冻结目标全集。
	Prompts []CandidatePrompt `json:"prompts"`
}

// PromptEntry 是双 Validator 通过后回填可信字段的 Draft 条目。
type PromptEntry struct {
	// TargetLocalKey 是 Source Slot 的局部键。
	TargetLocalKey string `json:"target_local_key"`
	// ElementLocalKey 是 Source Element 的局部键。
	ElementLocalKey string `json:"element_local_key"`
	// SlotType 是冻结的 Source Slot 类型。
	SlotType string `json:"slot_type"`
	// MediaKind 是由 SlotType 确定性派生的媒体类型。
	MediaKind string `json:"media_kind"`
	// Purpose 是冻结的 Source Slot 用途。
	Purpose string `json:"purpose"`
	// Required 表示该 Slot 是否必须满足。
	Required bool `json:"required"`
	// PositivePrompt 是模型生成且已校验的正向提示词。
	PositivePrompt string `json:"positive_prompt"`
	// NegativeConstraints 是模型生成且已校验的负面约束。
	NegativeConstraints []string `json:"negative_constraints"`
	// OutputLanguage 是 Intent 或 Runtime Policy 冻结的语言。
	OutputLanguage string `json:"output_language"`
}

// Content 是允许发送给 Business 的稳定 Prompt Preview Draft 内容。
type Content struct {
	// SchemaVersion 固定为 prompt.preview.draft.v1。
	SchemaVersion string `json:"schema_version"`
	// Mode 本批固定为 storyboard_preview。
	Mode string `json:"mode"`
	// SourceStoryboardPreviewRef 是冻结的上游 Draft 引用。
	SourceStoryboardPreviewRef StoryboardPreviewRef `json:"source_storyboard_preview_ref"`
	// Prompts 是按冻结目标顺序保存的完整 Prompt 列表。
	Prompts []PromptEntry `json:"prompts"`
}

// ValidationReport 是两个 Validator 共同写入的安全确定性报告。
type ValidationReport struct {
	// CandidateValid 表示候选协议、文本和集合边界校验通过。
	CandidateValid bool
	// ExactSetValid 表示目标全集和顺序校验通过。
	ExactSetValid bool
	// Code 是失败时可安全暴露的稳定结果码。
	Code string
}

// DraftCommand 是 Exact-set Validator 输出与 Business Command 输入之间的确定性边界。
type DraftCommand struct {
	// TrustedContext 是已冻结的身份、命令和版本 Pin。
	TrustedContext TrustedContext
	// DomainContext 是 Business 返回的一致读取快照。
	DomainContext GenerationContext
	// Targets 是完整冻结目标集合。
	Targets []PromptTarget
	// ExactTargetSetDigest 是 Scope Receipt 与保存 Guard 共用的摘要。
	ExactTargetSetDigest string
	// Content 是已通过两个 Validator 的正文。
	Content Content
	// ResendLimit 是 Save 前随完整命令冻结的自动重发上限。
	ResendLimit int
	// RequestDigest 是跨模块冻结的保存命令摘要。
	RequestDigest string
}

// SaveDisposition 表示 Business Command 首次创建或同义重放。
type SaveDisposition string

const (
	// SaveDispositionCreated 表示首次创建 Prompt Preview Draft。
	SaveDispositionCreated SaveDisposition = "created"
	// SaveDispositionReplayed 表示 Business 返回同命令同摘要的原结果。
	SaveDispositionReplayed SaveDisposition = "replayed"
)

// Resource 是 Business Prompt Preview Draft 的安全冻结结果。
type Resource struct {
	// PromptPreviewID 是 Business Preview Draft UUIDv7。
	PromptPreviewID string
	// ProjectID 是资源所属项目 UUIDv7。
	ProjectID string
	// StoryboardPreviewRef 是资源保存时绑定的上游 Draft 引用。
	StoryboardPreviewRef StoryboardPreviewRef
	// Version 是 Preview Draft 版本。
	Version int64
	// Status 本批固定为 draft。
	Status string
	// ContentDigest 是 Draft 内容的小写 SHA-256。
	ContentDigest string
	// ExactTargetSetDigest 是保存时复核后的目标全集摘要。
	ExactTargetSetDigest string
	// Content 是 Business 权威回传的完整 Draft 内容。
	Content Content
}

// SaveOutcome 是 Save/Query 节点之间的有类型路由值。
type SaveOutcome struct {
	// Status 是 saved、unknown 或 recovery_pending。
	Status string
	// Disposition 是 saved 时的 created/replayed 判别。
	Disposition SaveDisposition
	// Resource 是 saved 时的 Business 权威资源。
	Resource *Resource
	// Command 是 Query 与恢复必须原样复用的命令。
	Command DraftCommand
	// Recovery 是无法在当前 Graph 消除 Unknown Outcome 时的恢复工件。
	Recovery *RecoveryDeferred
}

// RecoveryDeferred 表示 Save Unknown Outcome 尚未消除；它不是 Tool Result。
type RecoveryDeferred struct {
	// ToolCallID 是恢复绑定的 ToolCall UUIDv7。
	ToolCallID string
	// BusinessCommandID 是后续 Claim 必须复用的命令 UUIDv7。
	BusinessCommandID string
	// RequestDigest 是后续 Query/Resend 必须复用的请求摘要。
	RequestDigest string
	// ContentDigest 是完整 Draft 正文摘要。
	ContentDigest string
	// Command 是已冻结的完整保存命令。
	Command DraftCommand
	// ResendAttempts 是 Processor 已持久化的重发次数。
	ResendAttempts int
	// ResendLimit 是 Processor 已持久化的重发上限。
	ResendLimit int
	// ResendExhausted 表示后续 Claim 不得再次发送保存命令。
	ResendExhausted bool
}

// ResourceRef 是 Tool completed Result 中可安全重放的 Business 资源引用。
type ResourceRef struct {
	// ID 是 Business Prompt Preview Draft UUIDv7。
	ID string `json:"id"`
	// Version 是 Preview Draft 版本。
	Version int64 `json:"version"`
	// ContentDigest 是 Preview Draft 内容摘要。
	ContentDigest string `json:"content_digest"`
}

// InvocationRef 是 Tool Result 中的最小稳定调用关联。
type InvocationRef struct {
	// ToolCallID 是当前 ToolCall UUIDv7。
	ToolCallID string `json:"tool_call_id"`
	// BusinessCommandID 是本次保存命令 UUIDv7。
	BusinessCommandID string `json:"business_command_id"`
}

// Result 是 completed/failed 的严格 Tool 终态；Recovery 不编码为 Result。
type Result struct {
	// SchemaVersion 固定为 write_prompts.preview.result.v1。
	SchemaVersion string `json:"schema_version"`
	// Status 只能是 completed 或 failed。
	Status string `json:"status"`
	// ResultCode 是安全稳定结果码。
	ResultCode string `json:"result_code"`
	// PromptPreviewRef 仅 completed 必须存在。
	PromptPreviewRef *ResourceRef `json:"prompt_preview_ref,omitempty"`
	// StoryboardPreviewRef 仅 completed 必须存在。
	StoryboardPreviewRef *StoryboardPreviewRef `json:"storyboard_preview_ref,omitempty"`
	// TargetCount 仅 completed 必须大于零。
	TargetCount int `json:"target_count,omitempty"`
	// InvocationRef 是当前 Tool 与 Business 命令关联。
	InvocationRef InvocationRef `json:"invocation_ref"`
	// Summary 仅 failed 必须存在。
	Summary string `json:"summary,omitempty"`
	// Retryable 仅 failed 必须存在。
	Retryable *bool `json:"retryable,omitempty"`
	// Card 是内部投影 DTO，永不进入 Tool Result JSON。
	Card *Card `json:"-"`
}

// Outcome 是 Graph 内部输出的显式联合；Terminal 与 Recovery 恰好一个存在。
type Outcome struct {
	// Terminal 是 completed/failed Tool 终态。
	Terminal *Result
	// Recovery 是不能伪装成 Tool Result 的内部非终态。
	Recovery *RecoveryDeferred
}

// Card 是 Workspace Snapshot 与 completed Event 共用的完整替换 DTO。
type Card struct {
	// SchemaVersion 固定为 prompt.preview.card.v1。
	SchemaVersion string `json:"schema_version"`
	// InputID 是 Card 绑定的 typed Input UUIDv7。
	InputID string `json:"input_id"`
	// TurnID 是 Card 绑定的 Turn UUIDv7。
	TurnID string `json:"turn_id"`
	// RunID 是 Card 绑定的 Run UUIDv7。
	RunID string `json:"run_id"`
	// ToolCallID 是 Card 绑定的 ToolCall UUIDv7。
	ToolCallID string `json:"tool_call_id"`
	// Status 是 completed 或 failed 的 Tool 投影状态。
	Status string `json:"status"`
	// ResultCode 是安全稳定的 Tool 结果码。
	ResultCode string `json:"result_code"`
	// UpdatedAt 是生成投影的 UTC 时间。
	UpdatedAt time.Time `json:"updated_at"`
	// PromptPreviewID 仅 completed 存在，是 Business Preview Draft UUIDv7。
	PromptPreviewID string `json:"prompt_preview_id,omitempty"`
	// ProjectID 仅 completed 存在，是资源所属项目 UUIDv7。
	ProjectID string `json:"project_id,omitempty"`
	// StoryboardPreviewRef 仅 completed 存在，是 Draft 绑定的上游 Storyboard 引用。
	StoryboardPreviewRef *StoryboardPreviewRef `json:"storyboard_preview_ref,omitempty"`
	// Version 仅 completed 存在，是 Preview Draft 版本。
	Version int64 `json:"version,omitempty"`
	// ContentDigest 仅 completed 存在，是完整 Draft 内容摘要。
	ContentDigest string `json:"content_digest,omitempty"`
	// TargetCount 仅 completed 存在，是完整 Prompt 数量。
	TargetCount int `json:"target_count,omitempty"`
	// Prompts 仅 completed 存在，是完整 Prompt 条目。
	Prompts []PromptEntry `json:"prompts,omitempty"`
	// FailureKind 仅 failed 存在，只允许 tool 或 runtime。
	FailureKind string `json:"failure_kind,omitempty"`
	// Summary 仅 failed 存在，是安全固定摘要。
	Summary string `json:"summary,omitempty"`
	// Retryable 仅 failed 存在，表示调用方是否可在新请求中重试。
	Retryable *bool `json:"retryable,omitempty"`
}

// State 是单次 Graph 调用的 typed local state；它不是 PostgreSQL 权威状态。
type State struct {
	// TrustedContext 是 Graph 初始化后不可被模型覆盖的可信上下文。
	TrustedContext TrustedContext
	// Intent 是严格解码后的模型可控写作字段。
	Intent Intent
	// IntentDigest 是 canonical Intent 摘要。
	IntentDigest string
	// StoryboardPreviewRef 是 TrustedContext 的便捷冻结副本。
	StoryboardPreviewRef StoryboardPreviewRef
	// StoryboardContext 是 Business 返回的一致读取快照。
	StoryboardContext GenerationContext
	// ExactTargets 是从 Source Draft 冻结的完整目标。
	ExactTargets []PromptTarget
	// ExactTargetSetDigest 是 Scope、Model 与保存共同绑定的摘要。
	ExactTargetSetDigest string
	// OutputLanguage 是 Intent 或 Policy 冻结后的最终语言。
	OutputLanguage string
	// PromptMessages 是传给唯一 ChatModel 节点的经典消息。
	PromptMessages []*schema.Message
	// PromptDigest 是 Prompt 消息的稳定摘要。
	PromptDigest string
	// ModelMessage 是唯一 ChatModel 节点返回的原始消息，仅存在于本地 State。
	ModelMessage *schema.Message
	// Candidate 是严格 Validator 接受的候选。
	Candidate *Candidate
	// CandidateDigest 是候选 canonical JSON 摘要。
	CandidateDigest string
	// ValidationReport 是两个 Validator 的确定性报告。
	ValidationReport ValidationReport
	// Content 是 exact-set Validator 回填可信字段后的正文。
	Content *Content
	// SaveOutcome 是 Save/Query 节点的路由联合。
	SaveOutcome *SaveOutcome
	// Result 是 completed/failed Tool 终态。
	Result *Result
	// Error 是内部节点写入且仅由 emit_failed 归一化的安全码。
	Error string
}

// BusinessContextReader 是 Graph 消费方定义的只读 Prompt Generation Context 最小接口。
type BusinessContextReader interface {
	// GetPromptGenerationContext 一次读取 Owner 校验后的 Project 与 Storyboard 快照。
	GetPromptGenerationContext(context.Context, TrustedContext) (GenerationContext, error)
}

// BusinessDraftStore 是 Graph 消费方定义的 Prompt Draft 写入与查询最小接口。
type BusinessDraftStore interface {
	// SavePromptPreviewDraft 以冻结 command ID 与 request digest 创建或同义重放 Draft。
	SavePromptPreviewDraft(context.Context, DraftCommand) (SaveDisposition, Resource, error)
	// QueryPromptPreviewCommand 只查询原 Business command 的权威结果。
	QueryPromptPreviewCommand(context.Context, DraftCommand) (string, *Resource, error)
}

// CommandJournal 是 Save 前冻结完整命令与持久化重发预算的最小 Agent 端口。
type CommandJournal interface {
	// PrepareCommand 在 Save RPC 前持久化完整冻结命令。
	PrepareCommand(context.Context, DraftCommand) error
	// ReserveCommandResend 仅供后续 Processor Claim 预留有界重发预算；Graph 内不调用。
	ReserveCommandResend(context.Context, TrustedContext, RecoveryDeferred) (RecoveryDeferred, bool, error)
}

// Clock 为 completed Card 冻结一个可测试 UTC 时间。
type Clock interface {
	// Now 返回当前时间，调用方必须按 UTC 使用。
	Now() time.Time
}

// TrustedContextResolver 从 Runtime 私有 Context 读取 Prompt Preview 可信值。
// M1 Tool Core 通过消费方注入该函数，避免在本包创建字符串 Context Key。
type TrustedContextResolver func(context.Context) (TrustedContext, bool)

// MarshalJSON 按 status 输出冻结 exact-set；内部 Card 永不暴露给模型。
func (result Result) MarshalJSON() ([]byte, error) {
	switch result.Status {
	case "completed":
		type completedWire struct {
			SchemaVersion        string                `json:"schema_version"`
			Status               string                `json:"status"`
			ResultCode           string                `json:"result_code"`
			PromptPreviewRef     *ResourceRef          `json:"prompt_preview_ref"`
			StoryboardPreviewRef *StoryboardPreviewRef `json:"storyboard_preview_ref"`
			TargetCount          int                   `json:"target_count"`
			InvocationRef        InvocationRef         `json:"invocation_ref"`
		}
		return json.Marshal(completedWire{result.SchemaVersion, result.Status, result.ResultCode, result.PromptPreviewRef,
			result.StoryboardPreviewRef, result.TargetCount, result.InvocationRef})
	case "failed":
		type failedWire struct {
			SchemaVersion string        `json:"schema_version"`
			Status        string        `json:"status"`
			ResultCode    string        `json:"result_code"`
			InvocationRef InvocationRef `json:"invocation_ref"`
			Summary       string        `json:"summary"`
			Retryable     *bool         `json:"retryable"`
		}
		return json.Marshal(failedWire{result.SchemaVersion, result.Status, result.ResultCode, result.InvocationRef, result.Summary, result.Retryable})
	default:
		return nil, fmt.Errorf("marshal prompt preview result: invalid terminal status")
	}
}
