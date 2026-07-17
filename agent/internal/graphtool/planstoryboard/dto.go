// Package planstoryboard 实现获准但默认不注册的 plan_storyboard V2 Storyboard Draft 开发预览。
package planstoryboard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
)

const (
	// ToolKey 是主 Agent Catalog 中的稳定高层能力标识；本 Profile 不进入生产可执行 Registry。
	ToolKey = "plan_storyboard"
	// ToolDefinitionVersion 是本批唯一获准实现的 Tool Core 版本。
	ToolDefinitionVersion = "plan_storyboard.v2preview1"
	// GraphName 是启动阶段编译的开发预览 Graph 名称。
	GraphName = "plan_storyboard_graph_v2preview1"
	// StateSchemaName 是本地 Graph State 的稳定诊断名称；当前不启用 Eino Checkpoint。
	StateSchemaName = "dora.agent.graphtool.plan_storyboard.state.v2preview1"
	// IntentSchemaVersion 是模型可控 Tool Intent 的严格版本。
	IntentSchemaVersion = "plan_storyboard.preview.intent.v1"
	// CandidateSchemaVersion 是内部 ChatModel 候选的严格版本。
	CandidateSchemaVersion = "storyboard.preview.candidate.v1"
	// DraftSchemaVersion 是 Business Storyboard Preview Draft 的内容版本。
	DraftSchemaVersion = "storyboard.preview.draft.v1"
	// ResultSchemaVersion 是 Tool completed/failed 结果的严格版本。
	ResultSchemaVersion = "plan_storyboard.preview.result.v1"
	// CardSchemaVersion 是 Workspace 安全 Card 的冻结版本。
	CardSchemaVersion = "storyboard.preview.card.v1"
	// SaveDigestSchemaVersion 是 Agent 与 Business 共同冻结的保存请求摘要版本。
	SaveDigestSchemaVersion = "storyboard.preview.save-draft.digest.v1"
	// DurableDraftCommandSchemaVersion 是加密恢复工件的严格序列化版本。
	DurableDraftCommandSchemaVersion = "storyboard.preview.durable-draft-command.v1"
	// PromptKey 是 Storyboard 规划 Prompt 的稳定键。
	PromptKey = "graph_tool.plan_storyboard.preview.primary"
	// PromptVersion 是 Storyboard 规划 Prompt 的语义版本。
	PromptVersion = "graph_tool.plan_storyboard.preview.v1"
	// ValidatorVersion 是独立候选 Validator 的冻结版本。
	ValidatorVersion = "plan_storyboard.preview.validator.v1"
	// DAGValidatorVersion 是独立依赖图 Validator 的冻结版本。
	DAGValidatorVersion = "plan_storyboard.preview.dag-validator.v1"

	// ResultCodeCompleted 表示 Business Storyboard Draft 已创建或同义重放。
	ResultCodeCompleted = "STORYBOARD_PREVIEW_DRAFT_CREATED"
	// ResultCodeInvalidArgument 表示模型可控 Intent 违反严格协议。
	ResultCodeInvalidArgument = "STORYBOARD_PREVIEW_INVALID_ARGUMENT"
	// ResultCodeCreationSpecNotFound 对外折叠 CreationSpec 不存在与无权限。
	ResultCodeCreationSpecNotFound = "STORYBOARD_CREATION_SPEC_NOT_FOUND"
	// ResultCodeCreationSpecConflict 表示 CreationSpec 版本或摘要已变化。
	ResultCodeCreationSpecConflict = "STORYBOARD_CREATION_SPEC_CONFLICT"
	// ResultCodeCandidateInvalid 表示模型候选未通过字段、枚举、phase 或时长校验。
	ResultCodeCandidateInvalid = "STORYBOARD_PREVIEW_CANDIDATE_INVALID"
	// ResultCodeDependencyInvalid 表示局部引用、Slot 归属或依赖 DAG 非法。
	ResultCodeDependencyInvalid = "STORYBOARD_PREVIEW_DEPENDENCY_INVALID"
	// ResultCodeBusinessConflict 表示 Business 保存命令发生版本或幂等冲突。
	ResultCodeBusinessConflict = "STORYBOARD_PREVIEW_CONFLICT"
	// ResultCodeBusinessDisabled 表示 Business Storyboard Preview 明确关闭。
	ResultCodeBusinessDisabled = "STORYBOARD_PREVIEW_DISABLED"
	// ResultCodeInternal 表示未分类的安全内部失败。
	ResultCodeInternal = "STORYBOARD_PREVIEW_INTERNAL"
)

const (
	maxJSONBytes = 64 * 1024

	intentInstructionMin = 1
	intentInstructionMax = 1000
	intentTargetMin      = 5
	intentTargetMax      = 600

	trustedOwnerMin           = 1
	trustedOwnerMax           = 200
	projectTitleMin           = 1
	projectTitleMax           = 160
	creationTitleMin          = 1
	creationTitleMax          = 80
	creationGoalMin           = 1
	creationGoalMax           = 2000
	creationAudienceMax       = 500
	creationPhasesMin         = 1
	creationPhasesMax         = 6
	creationListsMax          = 8
	creationAcceptanceMin     = 1
	creationPhaseTextMin      = 1
	creationPhaseTitleMax     = 80
	creationPhaseBodyMax      = 500
	creationConstraintMin     = 1
	creationConstraintMax     = 200
	creationAcceptanceTextMin = 1
	creationAcceptanceTextMax = 240

	candidateTitleMin   = 1
	candidateTitleMax   = 120
	candidateSummaryMin = 1
	candidateSummaryMax = 1000
	sectionTitleMin     = 1
	sectionTitleMax     = 100
	sectionObjectiveMin = 1
	sectionObjectiveMax = 500
	elementTitleMin     = 1
	elementTitleMax     = 120
	elementNarrativeMin = 1
	elementNarrativeMax = 1000
	elementDurationMin  = 1
	elementDurationMax  = 600
	totalDurationMin    = 5
	totalDurationMax    = 600
	targetToleranceRate = 0.05
	targetToleranceMin  = 2
	slotPurposeMin      = 1
	slotPurposeMax      = 500
	resultSummaryMin    = 1
	resultSummaryMax    = 500

	maxSections        = 8
	maxElements        = 24
	maxSlots           = 96
	maxElementDeps     = 8
	maxSlotsPerElement = 4
)

var (
	deliverableValues = [...]string{"video", "image_set", "audio", "mixed"}
	localeValues      = [...]string{"zh-CN", "en-US"}
	elementTypeValues = [...]string{"scene", "shot", "narration", "caption", "audio"}
	slotTypeValues    = [...]string{"image", "video", "audio", "voiceover", "caption"}
)

// CreationSpecRef 是 Runtime 冻结且模型无权填写的 CreationSpec 资源引用。
type CreationSpecRef struct {
	// ID 是 Business CreationSpec Draft UUIDv7。
	ID string `json:"id"`
	// Version 本批固定为 1。
	Version int64 `json:"version"`
	// ContentDigest 是 CreationSpec 内容的小写 SHA-256。
	ContentDigest string `json:"content_digest"`
}

// Intent 是模型能够填写的最小 Storyboard 规划意图，不包含资源身份或版本。
type Intent struct {
	// SchemaVersion 固定为 plan_storyboard.preview.intent.v1。
	SchemaVersion string `json:"schema_version"`
	// PlanningInstruction 是用户提供的 Storyboard 创作要求。
	PlanningInstruction string `json:"planning_instruction"`
	// TargetDurationSeconds 是可选的 5 至 600 秒目标时长。
	TargetDurationSeconds *int `json:"target_duration_seconds,omitempty"`
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
	// CreationSpecRef 是 Runtime 认证并冻结的上游 Draft 引用。
	CreationSpecRef CreationSpecRef
	// PromptVersion 是冻结的 Prompt 版本。
	PromptVersion string
	// ValidatorVersion 是冻结的候选校验器版本。
	ValidatorVersion string
	// DAGValidatorVersion 是冻结的依赖图校验器版本。
	DAGValidatorVersion string
}

// GraphInput 是编译后 Graph 的有类型入口。
type GraphInput struct {
	// TrustedContext 是 Runtime 提供的不可变可信上下文。
	TrustedContext TrustedContext
	// IntentJSON 是 Tool 收到的 canonical 严格 JSON。
	IntentJSON []byte
}

// CreationSpecPhase 是 Storyboard 候选允许引用的 CreationSpec 阶段。
type CreationSpecPhase struct {
	// Key 是 phase_1 至 phase_6 的稳定局部键。
	Key string `json:"key"`
	// Title 是阶段标题。
	Title string `json:"title"`
	// Objective 是阶段目标。
	Objective string `json:"objective"`
	// Output 是阶段交付描述。
	Output string `json:"output"`
}

// CreationSpecContent 是 Business 返回并允许进入最小 Prompt 的 CreationSpec 内容。
type CreationSpecContent struct {
	// Title 是 CreationSpec 标题。
	Title string `json:"title"`
	// Goal 是 CreationSpec 创作目标。
	Goal string `json:"goal"`
	// DeliverableType 是冻结交付物枚举。
	DeliverableType string `json:"deliverable_type"`
	// Audience 是目标受众。
	Audience string `json:"audience"`
	// Locale 是输出语言区域。
	Locale string `json:"locale"`
	// Phases 是非空有序阶段。
	Phases []CreationSpecPhase `json:"phases"`
	// Constraints 是已冻结的硬约束。
	Constraints []string `json:"constraints"`
	// AcceptanceCriteria 是已冻结的验收标准。
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

// CreationSpecResource 是 Business Owner 校验后的不可变 Draft 快照。
type CreationSpecResource struct {
	// ID 是 CreationSpec Draft UUIDv7。
	ID string
	// ProjectID 是资源所属项目 UUIDv7。
	ProjectID string
	// Version 是冻结版本，本批必须与可信引用一致。
	Version int64
	// Status 是资源状态，本批只接受 draft。
	Status string
	// ContentDigest 是资源内容的小写 SHA-256。
	ContentDigest string
	// Content 是进入 Prompt 的最小 CreationSpec 内容。
	Content CreationSpecContent
}

// PlanningContext 是一次 RPC 返回的 Project 与 CreationSpec 一致快照。
type PlanningContext struct {
	// ProjectID 是已通过 Owner 校验的项目 UUIDv7。
	ProjectID string
	// ProjectVersion 是保存时使用的乐观锁版本。
	ProjectVersion int64
	// ProjectTitle 是进入最小 Prompt 的项目标题。
	ProjectTitle string
	// CreationSpec 是已通过 Owner 校验的不可变 Draft 快照。
	CreationSpec CreationSpecResource
}

// Section 是 Storyboard Preview 中一个有序章节。
type Section struct {
	// Key 是 section_1 至 section_8 的稳定局部键。
	Key string `json:"key"`
	// Title 是章节标题。
	Title string `json:"title"`
	// Objective 是章节叙事目标。
	Objective string `json:"objective"`
}

// Element 是 Storyboard Preview 中一个使用局部键引用的动态元素。
type Element struct {
	// Key 是 element_1 至 element_24 的稳定局部键。
	Key string `json:"key"`
	// SectionKey 指向同一候选中的 Section 局部键。
	SectionKey string `json:"section_key"`
	// Order 是从 1 开始的全局连续顺序。
	Order int `json:"order"`
	// ElementType 是 scene、shot、narration、caption 或 audio。
	ElementType string `json:"element_type"`
	// Title 是元素标题。
	Title string `json:"title"`
	// NarrativePurpose 是元素的叙事目的。
	NarrativePurpose string `json:"narrative_purpose"`
	// DurationSeconds 是一至六百秒的元素时长。
	DurationSeconds int `json:"duration_seconds"`
	// SourcePhaseKey 指向可信 CreationSpec 的 Phase 局部键。
	SourcePhaseKey string `json:"source_phase_key"`
	// DependencyKeys 是零至八个精确去重的 Element 局部键。
	DependencyKeys []string `json:"dependency_keys"`
}

// Slot 是 Storyboard Preview 中一个不含最终 Prompt 或 Asset 的需求槽。
type Slot struct {
	// Key 是 slot_1 至 slot_96 的稳定局部键。
	Key string `json:"key"`
	// ElementKey 指向同一候选中的 Element 局部键。
	ElementKey string `json:"element_key"`
	// SlotType 是 image、video、audio、voiceover 或 caption。
	SlotType string `json:"slot_type"`
	// Purpose 是该需求槽的用途描述。
	Purpose string `json:"purpose"`
	// Required 表示后续生产是否必须满足该需求槽。
	Required bool `json:"required"`
}

// Candidate 是 ChatModel 唯一允许输出的严格候选，不含可信身份或业务状态。
type Candidate struct {
	// SchemaVersion 固定为 storyboard.preview.candidate.v1。
	SchemaVersion string `json:"schema_version"`
	// Title 是候选标题。
	Title string `json:"title"`
	// Summary 是候选摘要。
	Summary string `json:"summary"`
	// Sections 是非空有序章节。
	Sections []Section `json:"sections"`
	// Elements 是非空全局有序元素。
	Elements []Element `json:"elements"`
	// Slots 是可为空但必须非 null 的需求槽列表。
	Slots []Slot `json:"slots"`
}

// Content 是独立 Validator 通过后允许发送给 Business 的稳定 Draft 内容。
type Content struct {
	// Title 是 Storyboard Preview Draft 标题。
	Title string `json:"title"`
	// Summary 是 Storyboard Preview Draft 摘要。
	Summary string `json:"summary"`
	// Sections 是校验后的章节副本。
	Sections []Section `json:"sections"`
	// Elements 是校验后的元素副本。
	Elements []Element `json:"elements"`
	// Slots 是校验后的需求槽副本。
	Slots []Slot `json:"slots"`
}

// ValidationReport 是 Candidate 与 DAG Validator 共同写入的安全确定性报告。
type ValidationReport struct {
	// CandidateValid 表示字段、枚举、数量、Phase 与时长校验通过。
	CandidateValid bool
	// DependencyValid 表示局部引用、Slot 归属和依赖 DAG 校验通过。
	DependencyValid bool
	// Code 是失败时可安全暴露的稳定结果码。
	Code string
}

// DraftCommand 是 DAG Validator 输出与 Business Command 输入之间的确定性边界。
type DraftCommand struct {
	// TrustedContext 是已冻结的身份、命令和版本 Pin。
	TrustedContext TrustedContext
	// DomainContext 是 Business 返回的一致读取快照。
	DomainContext PlanningContext
	// Content 是已通过 Candidate 与 DAG Validator 的正文。
	Content Content
	// RequestDigest 是跨模块冻结的保存命令摘要。
	RequestDigest string
}

// SaveDisposition 表示 Business Command 首次创建或同义重放。
type SaveDisposition string

const (
	// SaveDispositionCreated 表示首次创建 Storyboard Preview Draft。
	SaveDispositionCreated SaveDisposition = "created"
	// SaveDispositionReplayed 表示 Business 返回同命令同摘要的原结果。
	SaveDispositionReplayed SaveDisposition = "replayed"
)

// Resource 是 Business Storyboard Preview Draft 的安全冻结结果。
type Resource struct {
	// StoryboardPreviewID 是 Business Preview Draft UUIDv7。
	StoryboardPreviewID string
	// ProjectID 是资源所属项目 UUIDv7。
	ProjectID string
	// CreationSpecRef 是资源保存时绑定的上游 Draft 引用。
	CreationSpecRef CreationSpecRef
	// Version 是 Preview Draft 版本。
	Version int64
	// Status 本批固定为 draft。
	Status string
	// ContentDigest 是 Draft 内容的小写 SHA-256。
	ContentDigest string
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
	// StoryboardPreviewID 是 Business Preview Draft UUIDv7。
	StoryboardPreviewID string `json:"storyboard_preview_id"`
	// Version 是 Preview Draft 版本。
	Version int64 `json:"version"`
	// Digest 是 Preview Draft 内容摘要。
	Digest string `json:"digest"`
	// Status 本批固定为 draft。
	Status string `json:"status"`
	// CreationSpecRef 是 Draft 绑定的上游 CreationSpec 引用。
	CreationSpecRef CreationSpecRef `json:"creation_spec_ref"`
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
	// SchemaVersion 固定为 plan_storyboard.preview.result.v1。
	SchemaVersion string `json:"schema_version"`
	// Status 只能是 completed 或 failed。
	Status string `json:"status"`
	// ResultCode 是安全稳定结果码。
	ResultCode string `json:"result_code"`
	// ResourceRef 仅 completed 必须存在。
	ResourceRef *ResourceRef `json:"resource_ref,omitempty"`
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
	// SchemaVersion 固定为 storyboard.preview.card.v1。
	SchemaVersion string `json:"schema_version"`
	// StoryboardPreviewID 是 Business Preview Draft UUIDv7。
	StoryboardPreviewID string `json:"storyboard_preview_id"`
	// ProjectID 是资源所属项目 UUIDv7。
	ProjectID string `json:"project_id"`
	// CreationSpecRef 是 Draft 绑定的上游 CreationSpec 引用。
	CreationSpecRef CreationSpecRef `json:"creation_spec_ref"`
	// Version 是 Preview Draft 版本。
	Version int64 `json:"version"`
	// Status 本批固定为 draft。
	Status string `json:"status"`
	// ContentDigest 是完整 Draft 内容摘要。
	ContentDigest string `json:"content_digest"`
	// Title 是 Draft 标题。
	Title string `json:"title"`
	// Summary 是 Draft 摘要。
	Summary string `json:"summary"`
	// Sections 是完整章节列表。
	Sections []Section `json:"sections"`
	// Elements 是完整元素列表。
	Elements []Element `json:"elements"`
	// Slots 是完整需求槽列表。
	Slots []Slot `json:"slots"`
	// UpdatedAt 是生成投影的 UTC 时间。
	UpdatedAt time.Time `json:"updated_at"`
}

// State 是单次 Graph 调用的 typed local state；它不是 PostgreSQL 权威状态。
type State struct {
	// TrustedContext 是 Graph 初始化后不可被模型覆盖的可信上下文。
	TrustedContext TrustedContext
	// Intent 是严格解码后的模型可控规划字段。
	Intent Intent
	// IntentDigest 是 canonical Intent 摘要。
	IntentDigest string
	// CreationSpecRef 是 TrustedContext 的便捷冻结副本。
	CreationSpecRef CreationSpecRef
	// CreationSpecContext 是 Business 返回的一致读取快照。
	CreationSpecContext PlanningContext
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
	// SaveOutcome 是 Save/Query 节点的路由联合。
	SaveOutcome *SaveOutcome
	// Result 是 completed/failed Tool 终态。
	Result *Result
	// Error 是内部节点写入且仅由 emit_failed 归一化的安全码。
	Error string
}

// BusinessContextReader 是 Graph 消费方定义的只读 Planning Context 最小接口。
type BusinessContextReader interface {
	// GetStoryboardPlanningContext 一次读取 Owner 校验后的 Project 与 CreationSpec 快照。
	GetStoryboardPlanningContext(context.Context, TrustedContext) (PlanningContext, error)
}

// BusinessDraftStore 是 Graph 消费方定义的 Storyboard Draft 写入与查询最小接口。
type BusinessDraftStore interface {
	// SaveStoryboardDraft 以冻结 command ID 与 request digest 创建或同义重放 Draft。
	SaveStoryboardDraft(context.Context, DraftCommand) (SaveDisposition, Resource, error)
	// QueryStoryboardDraftCommand 只查询原 Business command 的权威结果。
	QueryStoryboardDraftCommand(context.Context, DraftCommand) (string, *Resource, error)
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

// TrustedContextResolver 从 Runtime 私有 Context 读取 Storyboard Preview 可信值。
// M1 Tool Core 通过消费方注入该函数，避免在本包创建字符串 Context Key。
type TrustedContextResolver func(context.Context) (TrustedContext, bool)

// MarshalJSON 按 status 输出冻结 exact-set；内部 Card 永不暴露给模型。
func (result Result) MarshalJSON() ([]byte, error) {
	switch result.Status {
	case "completed":
		type completedWire struct {
			SchemaVersion string        `json:"schema_version"`
			Status        string        `json:"status"`
			ResultCode    string        `json:"result_code"`
			ResourceRef   *ResourceRef  `json:"resource_ref"`
			InvocationRef InvocationRef `json:"invocation_ref"`
		}
		return json.Marshal(completedWire{result.SchemaVersion, result.Status, result.ResultCode, result.ResourceRef, result.InvocationRef})
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
		return nil, fmt.Errorf("marshal storyboard result: invalid terminal status")
	}
}
