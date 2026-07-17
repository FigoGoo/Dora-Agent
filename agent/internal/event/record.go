// Package event 定义 Agent 会话事件日志的稳定领域契约与安全投影载荷。
package event

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/google/uuid"
)

// Type 表示会话 EventLog 中允许持久化的稳定事件类型。
type Type string

const (
	// TypeSessionCreated 表示默认会话和显式 Skill 快照已经原子建立。
	TypeSessionCreated Type = "session.created"
	// TypeSessionInputAccepted 表示非空用户输入已经可靠写入 PostgreSQL，尚未声明 Runner 已执行。
	TypeSessionInputAccepted Type = "session.input.accepted"
	// TypeCreationSpecPreviewCompleted 表示完整 Tool Result 已冻结且 Draft Card 投影已提交。
	TypeCreationSpecPreviewCompleted Type = "creation_spec.preview.completed"
	// TypeCreationSpecPreviewFailed 表示完整确定失败 Result 已冻结且安全失败投影已提交。
	TypeCreationSpecPreviewFailed Type = "creation_spec.preview.failed"
	// TypeSessionTurnCompleted 表示通用 User Message Turn 已冻结 Direct Response 并完成投影。
	TypeSessionTurnCompleted Type = "session.turn.completed"
	// TypeSessionTurnFailed 表示通用 User Message Turn 已冻结确定失败投影。
	TypeSessionTurnFailed Type = "session.turn.failed"
	// TypeSessionTurnRecoveryPending 表示通用 User Message Turn 因模型结果未知而保持 HOL 恢复阻塞。
	TypeSessionTurnRecoveryPending Type = "session.turn.recovery_pending"
	// TypeAnalyzeMaterialsPreviewAccepted 表示独立素材分析 Intent、Input 与冻结 Context 已原子入队。
	TypeAnalyzeMaterialsPreviewAccepted Type = "analyze_materials.preview.accepted"
	// TypeAnalyzeMaterialsPreviewCompleted 表示完整 Tool completed Result 已冻结并投影安全 Card。
	TypeAnalyzeMaterialsPreviewCompleted Type = "analyze_materials.preview.completed"
	// TypeAnalyzeMaterialsPreviewPartial 表示完整 Tool partial Result 已冻结并投影安全 Card。
	TypeAnalyzeMaterialsPreviewPartial Type = "analyze_materials.preview.partial"
	// TypeAnalyzeMaterialsPreviewFailed 表示合法 Tool failed Result 已冻结并投影安全 Card。
	TypeAnalyzeMaterialsPreviewFailed Type = "analyze_materials.preview.failed"
	// TypeAnalyzeMaterialsPreviewRuntimeFailed 表示 Runtime Failure 已独立投影，不伪造 Tool Result。
	TypeAnalyzeMaterialsPreviewRuntimeFailed Type = "analyze_materials.preview.runtime_failed"
	// TypePlanStoryboardPreviewAccepted 表示独立 Storyboard Intent、Input 与冻结 Context 已原子入队。
	TypePlanStoryboardPreviewAccepted Type = "plan_storyboard.preview.accepted"
	// TypePlanStoryboardPreviewCompleted 表示合法 Tool completed Result 已冻结并投影安全 Card。
	TypePlanStoryboardPreviewCompleted Type = "plan_storyboard.preview.completed"
	// TypePlanStoryboardPreviewFailed 表示合法 Tool failed Result 已冻结并投影安全 Card。
	TypePlanStoryboardPreviewFailed Type = "plan_storyboard.preview.failed"
	// TypePlanStoryboardPreviewRuntimeFailed 表示 Runtime Failure 已独立投影，不伪造 Tool Result。
	TypePlanStoryboardPreviewRuntimeFailed Type = "plan_storyboard.preview.runtime_failed"
	// TypeWritePromptsPreviewAccepted 表示独立 Prompt Intent、Input 与冻结 Context 已原子入队。
	TypeWritePromptsPreviewAccepted Type = "write_prompts.preview.accepted"
	// TypeWritePromptsPreviewCompleted 表示合法 Tool completed Result 已冻结并投影安全 Card。
	TypeWritePromptsPreviewCompleted Type = "write_prompts.preview.completed"
	// TypeWritePromptsPreviewFailed 表示合法 Tool failed Result 已冻结并投影安全 Card。
	TypeWritePromptsPreviewFailed Type = "write_prompts.preview.failed"
	// TypeWritePromptsPreviewRuntimeFailed 表示 Runtime Failure 已独立投影，不伪造 Tool Result。
	TypeWritePromptsPreviewRuntimeFailed Type = "write_prompts.preview.runtime_failed"
	// TypeMediaPreviewAccepted 表示媒体 Graph 已原子创建单 Operation/Batch/Job 与 Dispatch Outbox。
	TypeMediaPreviewAccepted Type = "media.preview.accepted"
	// TypeMediaPreviewCompleted 表示 Worker 终态已验证并投影 ready Asset Card。
	TypeMediaPreviewCompleted Type = "media.preview.completed"
	// TypeMediaPreviewFailed 表示 Graph 早期失败或 Worker 确定失败的安全 Card。
	TypeMediaPreviewFailed Type = "media.preview.failed"
	// TypeMediaPreviewRuntimeFailed 表示媒体运行时契约损坏的独立安全失败 Card。
	TypeMediaPreviewRuntimeFailed Type = "media.preview.runtime_failed"
)

var stableTurnErrorCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
var sha256DigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// SchemaVersionV1 是 W0 会话事件投影的首个稳定 Schema 版本。
const SchemaVersionV1 = "session.event.v1"

// SourceKindEnsureProjectSession 表示事件由 Business 的建会话命令产生。
const SourceKindEnsureProjectSession = "ensure_project_session"

// SourceKindCreationSpecPreview 表示事件由结构化 Preview Input 的 Processor 投影产生。
const SourceKindCreationSpecPreview = "creation_spec_preview"

// SourceKindUserMessageRuntime 表示事件由通用 User Message Session Lane Runtime 投影产生。
const SourceKindUserMessageRuntime = "user_message_runtime"

// SourceKindAnalyzeMaterialsPreview 表示事件由独立素材分析 Preview Runtime 产生。
const SourceKindAnalyzeMaterialsPreview = "analyze_materials_preview"

// SourceKindPlanStoryboardPreview 表示事件由独立 Storyboard Preview Runtime 产生。
const SourceKindPlanStoryboardPreview = "plan_storyboard_preview"

// SourceKindWritePromptsPreview 表示事件由独立 Prompt Preview Runtime 产生。
const SourceKindWritePromptsPreview = "write_prompts_preview"

// SourceKindMediaPreview 表示事件由 media.runtime.v3preview1 请求或终态 Processor 产生。
const SourceKindMediaPreview = "media_preview"

const (
	// DirectResponseCardSchemaVersionV1 是无 Tool 首轮安全响应卡版本。
	DirectResponseCardSchemaVersionV1 = "session.turn.direct_response.card.v1"
	// FailureCardSchemaVersionV1 是确定失败或权威恢复阻塞卡版本。
	FailureCardSchemaVersionV1 = "session.turn.failure.card.v1"
	// DirectResponseCompletedStatus 是 Direct Response 唯一允许状态。
	DirectResponseCompletedStatus = "completed"
	// TurnFailedStatus 是确定失败卡状态。
	TurnFailedStatus = "failed"
	// TurnRecoveryPendingStatus 是模型结果未知、禁止自动重发时的恢复阻塞状态。
	TurnRecoveryPendingStatus = "recovery_pending"
	// DirectResponseMessageCode 是首轮需求受理的稳定展示代码。
	DirectResponseMessageCode = "creation_request_received"
	// DirectResponseSummary 是首轮需求受理的固定服务端安全文案。
	DirectResponseSummary = "已收到你的创作需求。你可以继续打开工具箱选择下一步流程。"
	// DirectResponseActionOpenToolbox 是 Direct Response 唯一允许的无副作用前端动作。
	DirectResponseActionOpenToolbox = "open_toolbox"
	// AnalyzeMaterialsPreviewCardSchemaVersionV1 是素材分析安全 Card 的冻结版本。
	AnalyzeMaterialsPreviewCardSchemaVersionV1 = "analyze_materials.preview.card.v1"
	// AnalyzeMaterialsPreviewFailureKindTool 表示 failed Card 来自合法 Tool Result。
	AnalyzeMaterialsPreviewFailureKindTool = "tool"
	// AnalyzeMaterialsPreviewFailureKindRuntime 表示 failed Card 来自独立 Runtime Failure。
	AnalyzeMaterialsPreviewFailureKindRuntime = "runtime"
	// PlanStoryboardPreviewAcceptedSchemaVersionV1 是 Storyboard accepted 安全载荷版本。
	PlanStoryboardPreviewAcceptedSchemaVersionV1 = "plan_storyboard.preview.accepted.v1"
	// PlanStoryboardPreviewCardSchemaVersionV1 是 Storyboard 安全 Card 的冻结版本。
	PlanStoryboardPreviewCardSchemaVersionV1 = "storyboard.preview.card.v1"
	// PlanStoryboardPreviewFailureKindTool 表示 failed Card 来自合法 Tool Result。
	PlanStoryboardPreviewFailureKindTool = "tool"
	// PlanStoryboardPreviewFailureKindRuntime 表示 failed Card 来自独立 Runtime Failure。
	PlanStoryboardPreviewFailureKindRuntime = "runtime"
	// WritePromptsPreviewAcceptedSchemaVersionV1 是 Prompt accepted 安全载荷版本。
	WritePromptsPreviewAcceptedSchemaVersionV1 = "write_prompts.preview.accepted.v1"
	// WritePromptsPreviewCardSchemaVersionV1 是 Prompt 安全 Card 的冻结版本。
	WritePromptsPreviewCardSchemaVersionV1 = writeprompts.CardSchemaVersion
	// WritePromptsPreviewFailureKindTool 表示 failed Card 来自合法 Tool Result。
	WritePromptsPreviewFailureKindTool = "tool"
	// WritePromptsPreviewFailureKindRuntime 表示 failed Card 来自独立 Runtime Failure。
	WritePromptsPreviewFailureKindRuntime = "runtime"
	// MediaPreviewCardSchemaVersionV1 是 accepted/completed/failed 共用的媒体安全 Card 版本。
	MediaPreviewCardSchemaVersionV1 = "media_preview.card.v1"
)

const maxAnalyzeMaterialsPreviewCardBytes = 64 * 1024
const maxPlanStoryboardPreviewCardBytes = 64 * 1024
const maxWritePromptsPreviewCardBytes = 128 * 1024

// AggregateType 表示事件关联的权威聚合类型。
type AggregateType string

const (
	// AggregateTypeSession 表示事件关联 Session 聚合。
	AggregateTypeSession AggregateType = "session"
	// AggregateTypeSessionInput 表示事件关联 Session Input 聚合。
	AggregateTypeSessionInput AggregateType = "session_input"
	// AggregateTypeCreationSpec 表示事件关联 Business CreationSpec 聚合。
	AggregateTypeCreationSpec AggregateType = "creation_spec"
	// AggregateTypeSessionTurn 表示事件关联 Agent-owned Session Turn 聚合。
	AggregateTypeSessionTurn AggregateType = "session_turn"
	// AggregateTypePlanStoryboardPreview 表示事件关联独立 Storyboard Preview Input 聚合。
	AggregateTypePlanStoryboardPreview AggregateType = "plan_storyboard_preview"
	// AggregateTypeWritePromptsPreview 表示事件关联独立 Prompt Preview Input 聚合。
	AggregateTypeWritePromptsPreview AggregateType = "write_prompts_preview"
)

// Record 表示待追加到会话 EventLog 的安全版本化事件。
// PayloadJSON 只能由本包的有类型构造函数生成，禁止调用方写入任意 JSON 或敏感正文。
type Record struct {
	// EventID 是应用生成的事件 UUIDv7。
	EventID string
	// SessionID 是事件所属 Session UUIDv7。
	SessionID string
	// Seq 是会话内单调序号；创建计划中由 Repository 事务统一分配。
	Seq int64
	// Type 是严格白名单事件类型。
	Type Type
	// SchemaVersion 是投影载荷版本。
	SchemaVersion string
	// SourceKind 是稳定事件来源类型。
	SourceKind string
	// SourceID 是来源命令 UUIDv7，用于 AppendOnce 去重。
	SourceID string
	// ProjectionIndex 是同一来源下投影事件的固定顺序索引。
	ProjectionIndex int
	// AggregateType 是事件关联聚合的稳定类型。
	AggregateType AggregateType
	// AggregateID 是事件关联聚合 UUIDv7。
	AggregateID string
	// AggregateVersion 是事件观察到的聚合版本。
	AggregateVersion int64
	// PayloadJSON 是经有类型 DTO 编码的安全 JSON，不包含 Prompt 或内部执行状态。
	PayloadJSON []byte
	// CreatedAt 是事件冻结时间，使用 UTC。
	CreatedAt time.Time
}

// SessionCreatedPayload 是 session.created 的前端安全投影载荷。
type SessionCreatedPayload struct {
	// SessionID 是已创建 Session UUIDv7。
	SessionID string `json:"session_id"`
	// ProjectID 是关联 Business Project UUIDv7。
	ProjectID string `json:"project_id"`
	// Status 是会话生命周期状态。
	Status string `json:"status"`
	// Version 是会话聚合版本。
	Version int64 `json:"version"`
}

// SessionInputAcceptedPayload 是 session.input.accepted 的前端安全投影载荷。
type SessionInputAcceptedPayload struct {
	// SessionID 是输入所属 Session UUIDv7。
	SessionID string `json:"session_id"`
	// InputID 是已持久化 Input UUIDv7。
	InputID string `json:"input_id"`
	// MessageID 是关联的用户 Message UUIDv7。
	MessageID string `json:"message_id"`
	// EnqueueSeq 是 Input 在 Session 内的 Head-of-Line 序号。
	EnqueueSeq int64 `json:"enqueue_seq"`
	// Status 是 Input 当前状态；W0 固定为 pending。
	Status string `json:"status"`
}

// CreationSpecPreviewFailedPayload 是 failed Event 唯一允许的安全字段集合。
type CreationSpecPreviewFailedPayload struct {
	// InputID 是失败 Preview Input UUIDv7。
	InputID string `json:"input_id"`
	// ResultCode 是稳定大写错误码。
	ResultCode string `json:"result_code"`
	// Summary 是不含内部错误或 Provider 原文的安全中文说明。
	Summary string `json:"summary"`
	// Retryable 表示用户以新幂等键重试同语义是否可能成功。
	Retryable bool `json:"retryable"`
}

// SessionTurnDirectResponsePayload 是 session.turn.completed 唯一允许的安全 Card 字段集合。
type SessionTurnDirectResponsePayload struct {
	SchemaVersion    string   `json:"schema_version"`
	TurnID           string   `json:"turn_id"`
	RunID            string   `json:"run_id"`
	InputID          string   `json:"input_id"`
	Status           string   `json:"status"`
	MessageCode      string   `json:"message_code"`
	Summary          string   `json:"summary"`
	AvailableActions []string `json:"available_actions"`
}

// SessionTurnFailurePayload 是 session.turn.failed/recovery_pending 唯一允许的安全 Card 字段集合。
type SessionTurnFailurePayload struct {
	SchemaVersion string `json:"schema_version"`
	TurnID        string `json:"turn_id"`
	RunID         string `json:"run_id"`
	InputID       string `json:"input_id"`
	Status        string `json:"status"`
	ErrorCode     string `json:"error_code"`
	Retryable     bool   `json:"retryable"`
	Summary       string `json:"summary"`
}

// AnalyzeMaterialsPreviewAcceptedPayload 是独立 accepted Event 唯一允许的安全字段集合。
// 该载荷不含 message_id、Intent 正文、密文或任何模型内容。
type AnalyzeMaterialsPreviewAcceptedPayload struct {
	InputID       string `json:"input_id"`
	SessionID     string `json:"session_id"`
	TurnID        string `json:"turn_id"`
	RunID         string `json:"run_id"`
	RequestID     string `json:"request_id"`
	SourceType    string `json:"source_type"`
	IntentDigest  string `json:"intent_digest"`
	ToolCallID    string `json:"tool_call_id"`
	ContextDigest string `json:"context_digest"`
}

// AnalyzeMaterialsPreviewCardPayload 是 completed、partial 与两类 failed Event 共用的严格安全 Card。
// Analysis、Coverage 与 EvidenceRefs 只使用 Tool Core 已公开的无正文 DTO。
type AnalyzeMaterialsPreviewCardPayload struct {
	SchemaVersion string                         `json:"schema_version"`
	InputID       string                         `json:"input_id"`
	TurnID        string                         `json:"turn_id"`
	RunID         string                         `json:"run_id"`
	ToolCallID    string                         `json:"tool_call_id"`
	Status        string                         `json:"status"`
	ResultCode    string                         `json:"result_code"`
	Analysis      *analyzematerials.Candidate    `json:"analysis,omitempty"`
	Coverage      *analyzematerials.Coverage     `json:"coverage,omitempty"`
	EvidenceRefs  []analyzematerials.EvidenceRef `json:"evidence_refs,omitempty"`
	FailureKind   string                         `json:"failure_kind,omitempty"`
	Summary       string                         `json:"summary,omitempty"`
	Retryable     *bool                          `json:"retryable,omitempty"`
}

// PlanStoryboardPreviewAcceptedPayload 是 Storyboard accepted Event 唯一允许的安全 exact-set。
// 该载荷保留 M2 已冻结的稳定身份与摘要，不包含 Intent、Prompt、访问范围或业务命令正文。
type PlanStoryboardPreviewAcceptedPayload struct {
	SchemaVersion             string `json:"schema_version"`
	InputID                   string `json:"input_id"`
	TurnID                    string `json:"turn_id"`
	RunID                     string `json:"run_id"`
	ToolCallID                string `json:"tool_call_id"`
	BusinessCommandID         string `json:"business_command_id"`
	IntentDigest              string `json:"intent_digest"`
	ContextDigest             string `json:"context_digest"`
	CreationSpecID            string `json:"creation_spec_id"`
	CreationSpecVersion       int64  `json:"creation_spec_version"`
	CreationSpecContentDigest string `json:"creation_spec_content_digest"`
}

// PlanStoryboardPreviewCardPayload 是 completed 与两类 failed Event/Snapshot 共用的安全联合。
// completed-only 字段使用 omitempty；构造器和 Workspace Decoder 会按 status 检查 exact-set。
type PlanStoryboardPreviewCardPayload struct {
	SchemaVersion       string                          `json:"schema_version"`
	InputID             string                          `json:"input_id"`
	TurnID              string                          `json:"turn_id"`
	RunID               string                          `json:"run_id"`
	ToolCallID          string                          `json:"tool_call_id"`
	Status              string                          `json:"status"`
	ResultCode          string                          `json:"result_code"`
	UpdatedAt           time.Time                       `json:"updated_at"`
	StoryboardPreviewID string                          `json:"storyboard_preview_id,omitempty"`
	ProjectID           string                          `json:"project_id,omitempty"`
	CreationSpecRef     *planstoryboard.CreationSpecRef `json:"creation_spec_ref,omitempty"`
	Version             int64                           `json:"version,omitempty"`
	ContentDigest       string                          `json:"content_digest,omitempty"`
	Title               string                          `json:"title,omitempty"`
	Summary             string                          `json:"summary"`
	Sections            *[]planstoryboard.Section       `json:"sections,omitempty"`
	Elements            *[]planstoryboard.Element       `json:"elements,omitempty"`
	Slots               *[]planstoryboard.Slot          `json:"slots,omitempty"`
	FailureKind         string                          `json:"failure_kind,omitempty"`
	Retryable           *bool                           `json:"retryable,omitempty"`
}

// WritePromptsPreviewAcceptedPayload 是 Prompt accepted Event 唯一允许的安全 exact-set。
type WritePromptsPreviewAcceptedPayload struct {
	SchemaVersion                  string `json:"schema_version"`
	InputID                        string `json:"input_id"`
	TurnID                         string `json:"turn_id"`
	RunID                          string `json:"run_id"`
	ToolCallID                     string `json:"tool_call_id"`
	BusinessCommandID              string `json:"business_command_id"`
	IntentDigest                   string `json:"intent_digest"`
	ContextDigest                  string `json:"context_digest"`
	StoryboardPreviewID            string `json:"storyboard_preview_id"`
	StoryboardPreviewVersion       int64  `json:"storyboard_preview_version"`
	StoryboardPreviewContentDigest string `json:"storyboard_preview_content_digest"`
}

// WritePromptsPreviewCardPayload 是 completed 与两类 failed Event/Snapshot 共用的安全联合。
type WritePromptsPreviewCardPayload = writeprompts.Card

// MediaPreviewAssetRef 是 Workspace Card 使用的最小安全 Asset 引用；内部 wire 的 asset_id 不进入前端。
type MediaPreviewAssetRef struct {
	ID            string `json:"id"`
	Version       int64  `json:"version"`
	Status        string `json:"status"`
	MediaKind     string `json:"media_kind"`
	MIMEType      string `json:"mime_type"`
	ContentDigest string `json:"content_digest,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
}

// MediaPreviewCardPayload 是媒体 accepted/completed/failed 的严格安全联合。
type MediaPreviewCardPayload struct {
	SchemaVersion string                `json:"schema_version"`
	InputID       string                `json:"input_id"`
	TurnID        string                `json:"turn_id"`
	RunID         string                `json:"run_id"`
	ToolCallID    string                `json:"tool_call_id"`
	ToolKey       string                `json:"tool_key"`
	Status        string                `json:"status"`
	ResultCode    string                `json:"result_code"`
	UpdatedAt     time.Time             `json:"updated_at"`
	OperationID   string                `json:"operation_id,omitempty"`
	BatchID       string                `json:"batch_id,omitempty"`
	JobID         string                `json:"job_id,omitempty"`
	AssetRef      *MediaPreviewAssetRef `json:"asset_ref,omitempty"`
	ErrorCode     string                `json:"error_code,omitempty"`
	ContentURL    string                `json:"content_url,omitempty"`
}

// NewSessionCreated 创建不含敏感正文的 session.created 事件。
// 调用方必须提供已冻结的 Session 事实；编码失败时不返回部分事件。
func NewSessionCreated(eventID, sessionID, projectID, status, sourceID string, version int64, createdAt time.Time) (Record, error) {
	payload, err := json.Marshal(SessionCreatedPayload{
		SessionID: sessionID,
		ProjectID: projectID,
		Status:    status,
		Version:   version,
	})
	if err != nil {
		return Record{}, fmt.Errorf("encode session.created payload: %w", err)
	}
	return Record{
		EventID:          eventID,
		SessionID:        sessionID,
		Type:             TypeSessionCreated,
		SchemaVersion:    SchemaVersionV1,
		SourceKind:       SourceKindEnsureProjectSession,
		SourceID:         sourceID,
		ProjectionIndex:  0,
		AggregateType:    AggregateTypeSession,
		AggregateID:      sessionID,
		AggregateVersion: version,
		PayloadJSON:      payload,
		CreatedAt:        createdAt.UTC(),
	}, nil
}

// NewSessionInputAccepted 创建不含消息正文的 session.input.accepted 事件。
// 该事件仅表示 PostgreSQL 已接受输入，不得被解释为 Runner、Turn 或模型已经执行。
func NewSessionInputAccepted(eventID, sessionID, inputID, messageID, sourceID, status string, enqueueSeq int64, createdAt time.Time) (Record, error) {
	payload, err := json.Marshal(SessionInputAcceptedPayload{
		SessionID:  sessionID,
		InputID:    inputID,
		MessageID:  messageID,
		EnqueueSeq: enqueueSeq,
		Status:     status,
	})
	if err != nil {
		return Record{}, fmt.Errorf("encode session.input.accepted payload: %w", err)
	}
	return Record{
		EventID:          eventID,
		SessionID:        sessionID,
		Type:             TypeSessionInputAccepted,
		SchemaVersion:    SchemaVersionV1,
		SourceKind:       SourceKindEnsureProjectSession,
		SourceID:         sourceID,
		ProjectionIndex:  1,
		AggregateType:    AggregateTypeSessionInput,
		AggregateID:      inputID,
		AggregateVersion: 1,
		PayloadJSON:      payload,
		CreatedAt:        createdAt.UTC(),
	}, nil
}

// NewCreationSpecPreviewInputAccepted 复用既有 session.input.accepted 契约投影 Preview 入队事实，不新增 accepted 事件名。
func NewCreationSpecPreviewInputAccepted(
	eventID, sessionID, inputID, messageID, sourceID, status string,
	enqueueSeq int64,
	createdAt time.Time,
) (Record, error) {
	record, err := NewSessionInputAccepted(eventID, sessionID, inputID, messageID, sourceID, status, enqueueSeq, createdAt)
	if err != nil {
		return Record{}, err
	}
	record.SourceKind = SourceKindCreationSpecPreview
	record.ProjectionIndex = 0
	return record, nil
}

// NewCreationSpecPreviewCompleted 使用已经严格编码的完整 Card 作为 completed Payload。
// 调用方必须先冻结 Tool Result，再在同一 Processor 完成事务中写 Projection 与本事件。
func NewCreationSpecPreviewCompleted(
	eventID, sessionID, sourceID, creationSpecID string,
	version int64,
	cardJSON []byte,
	createdAt time.Time,
) Record {
	return Record{
		EventID: eventID, SessionID: sessionID, Type: TypeCreationSpecPreviewCompleted,
		SchemaVersion: SchemaVersionV1, SourceKind: SourceKindCreationSpecPreview, SourceID: sourceID,
		ProjectionIndex: 0, AggregateType: AggregateTypeCreationSpec, AggregateID: creationSpecID,
		AggregateVersion: version, PayloadJSON: append([]byte(nil), cardJSON...), CreatedAt: createdAt.UTC(),
	}
}

// NewCreationSpecPreviewFailed 构造 exact-set 的确定失败 Payload，聚合固定绑定 Input version 1。
func NewCreationSpecPreviewFailed(
	eventID, sessionID, inputID, sourceID, resultCode, summary string,
	retryable bool,
	createdAt time.Time,
) (Record, error) {
	payload, err := json.Marshal(CreationSpecPreviewFailedPayload{
		InputID: inputID, ResultCode: resultCode, Summary: summary, Retryable: retryable,
	})
	if err != nil {
		return Record{}, fmt.Errorf("encode creation_spec.preview.failed payload: %w", err)
	}
	return Record{
		EventID: eventID, SessionID: sessionID, Type: TypeCreationSpecPreviewFailed,
		SchemaVersion: SchemaVersionV1, SourceKind: SourceKindCreationSpecPreview, SourceID: sourceID,
		ProjectionIndex: 0, AggregateType: AggregateTypeSessionInput, AggregateID: inputID,
		AggregateVersion: 1, PayloadJSON: payload, CreatedAt: createdAt.UTC(),
	}, nil
}

// NewAnalyzeMaterialsPreviewAccepted 构造不含 Message 或 Intent 正文的独立入队事件。
func NewAnalyzeMaterialsPreviewAccepted(
	eventID string,
	payload AnalyzeMaterialsPreviewAcceptedPayload,
	createdAt time.Time,
) (Record, error) {
	if !validTurnCardIdentity(eventID, payload.SessionID, payload.InputID, payload.TurnID, payload.RunID, payload.RequestID, payload.ToolCallID) ||
		payload.SourceType != SourceKindAnalyzeMaterialsPreview ||
		!sha256DigestPattern.MatchString(payload.IntentDigest) || !sha256DigestPattern.MatchString(payload.ContextDigest) ||
		createdAt.IsZero() {
		return Record{}, fmt.Errorf("encode analyze_materials.preview.accepted payload: invalid safe payload")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Record{}, fmt.Errorf("encode analyze_materials.preview.accepted payload: %w", err)
	}
	return Record{
		EventID: eventID, SessionID: payload.SessionID, Type: TypeAnalyzeMaterialsPreviewAccepted,
		SchemaVersion: SchemaVersionV1, SourceKind: SourceKindAnalyzeMaterialsPreview, SourceID: payload.RequestID,
		ProjectionIndex: 0, AggregateType: AggregateTypeSessionInput, AggregateID: payload.InputID,
		AggregateVersion: 1, PayloadJSON: encoded, CreatedAt: createdAt.UTC(),
	}, nil
}

// NewAnalyzeMaterialsPreviewCompleted 构造已通过 Tool Result Validator 的 completed 安全 Card 事件。
func NewAnalyzeMaterialsPreviewCompleted(
	eventID, sessionID, sourceID string,
	card AnalyzeMaterialsPreviewCardPayload,
	aggregateVersion int64,
	createdAt time.Time,
) (Record, error) {
	return newAnalyzeMaterialsPreviewTerminal(
		eventID, sessionID, sourceID, card, aggregateVersion,
		TypeAnalyzeMaterialsPreviewCompleted, "completed", AnalyzeMaterialsPreviewFailureKindTool, createdAt,
	)
}

// NewAnalyzeMaterialsPreviewPartial 构造已通过 Tool Result Validator 的 partial 安全 Card 事件。
func NewAnalyzeMaterialsPreviewPartial(
	eventID, sessionID, sourceID string,
	card AnalyzeMaterialsPreviewCardPayload,
	aggregateVersion int64,
	createdAt time.Time,
) (Record, error) {
	return newAnalyzeMaterialsPreviewTerminal(
		eventID, sessionID, sourceID, card, aggregateVersion,
		TypeAnalyzeMaterialsPreviewPartial, "partial", AnalyzeMaterialsPreviewFailureKindTool, createdAt,
	)
}

// NewAnalyzeMaterialsPreviewFailed 构造合法 Tool failed Result 的安全 Card 事件。
func NewAnalyzeMaterialsPreviewFailed(
	eventID, sessionID, sourceID string,
	card AnalyzeMaterialsPreviewCardPayload,
	aggregateVersion int64,
	createdAt time.Time,
) (Record, error) {
	return newAnalyzeMaterialsPreviewTerminal(
		eventID, sessionID, sourceID, card, aggregateVersion,
		TypeAnalyzeMaterialsPreviewFailed, "failed", AnalyzeMaterialsPreviewFailureKindTool, createdAt,
	)
}

// NewAnalyzeMaterialsPreviewRuntimeFailed 构造独立 Runtime Failure 安全 Card，不伪造 Tool failed Result。
func NewAnalyzeMaterialsPreviewRuntimeFailed(
	eventID, sessionID, sourceID string,
	card AnalyzeMaterialsPreviewCardPayload,
	aggregateVersion int64,
	createdAt time.Time,
) (Record, error) {
	return newAnalyzeMaterialsPreviewTerminal(
		eventID, sessionID, sourceID, card, aggregateVersion,
		TypeAnalyzeMaterialsPreviewRuntimeFailed, "failed", AnalyzeMaterialsPreviewFailureKindRuntime, createdAt,
	)
}

func newAnalyzeMaterialsPreviewTerminal(
	eventID, sessionID, sourceID string,
	card AnalyzeMaterialsPreviewCardPayload,
	aggregateVersion int64,
	eventType Type,
	expectedStatus, expectedFailureKind string,
	createdAt time.Time,
) (Record, error) {
	if card.SchemaVersion != AnalyzeMaterialsPreviewCardSchemaVersionV1 ||
		!validTurnCardIdentity(eventID, sessionID, sourceID, card.InputID, card.TurnID, card.RunID, card.ToolCallID) ||
		aggregateVersion <= 0 || createdAt.IsZero() || card.Status != expectedStatus ||
		!validAnalyzeMaterialsPreviewCard(card, expectedFailureKind) {
		return Record{}, fmt.Errorf("encode %s payload: invalid safe card", eventType)
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return Record{}, fmt.Errorf("encode %s payload: %w", eventType, err)
	}
	if len(encoded) > maxAnalyzeMaterialsPreviewCardBytes {
		return Record{}, fmt.Errorf("encode %s payload: safe card exceeds limit", eventType)
	}
	return Record{
		EventID: eventID, SessionID: sessionID, Type: eventType,
		SchemaVersion: SchemaVersionV1, SourceKind: SourceKindAnalyzeMaterialsPreview, SourceID: sourceID,
		ProjectionIndex: 0, AggregateType: AggregateTypeSessionTurn, AggregateID: card.TurnID,
		AggregateVersion: aggregateVersion, PayloadJSON: encoded, CreatedAt: createdAt.UTC(),
	}, nil
}

func validAnalyzeMaterialsPreviewCard(card AnalyzeMaterialsPreviewCardPayload, expectedFailureKind string) bool {
	switch expectedFailureKind {
	case AnalyzeMaterialsPreviewFailureKindTool:
		if card.Status == "completed" || card.Status == "partial" {
			if card.FailureKind != "" {
				return false
			}
		} else if card.Status == "failed" {
			if card.FailureKind != AnalyzeMaterialsPreviewFailureKindTool {
				return false
			}
		} else {
			return false
		}
		result := analyzematerials.Result{
			SchemaVersion: analyzematerials.ResultSchemaVersion,
			Status:        card.Status,
			ResultCode:    card.ResultCode,
			Analysis:      card.Analysis,
			Coverage:      card.Coverage,
			EvidenceRefs:  card.EvidenceRefs,
			InvocationRef: analyzematerials.InvocationRef{ToolCallID: card.ToolCallID},
			Summary:       card.Summary,
			Retryable:     card.Retryable,
		}
		return analyzematerials.ValidateResult(result) == nil
	case AnalyzeMaterialsPreviewFailureKindRuntime:
		return card.Status == "failed" && card.FailureKind == AnalyzeMaterialsPreviewFailureKindRuntime &&
			stableTurnErrorCodePattern.MatchString(card.ResultCode) && card.Analysis == nil && card.Coverage == nil &&
			len(card.EvidenceRefs) == 0 && card.Retryable != nil && validSafeSummary(card.Summary)
	default:
		return false
	}
}

// NewPlanStoryboardPreviewAccepted 构造 M2 已冻结的 accepted exact-set，不引入 Card 或内部执行字段。
func NewPlanStoryboardPreviewAccepted(
	eventID, sessionID, sourceID string,
	payload PlanStoryboardPreviewAcceptedPayload,
	createdAt time.Time,
) (Record, error) {
	if payload.SchemaVersion != PlanStoryboardPreviewAcceptedSchemaVersionV1 ||
		!validTurnCardIdentity(eventID, sessionID, sourceID, payload.InputID, payload.TurnID, payload.RunID,
			payload.ToolCallID, payload.BusinessCommandID, payload.CreationSpecID) ||
		payload.CreationSpecVersion != 1 || !sha256DigestPattern.MatchString(payload.IntentDigest) ||
		!sha256DigestPattern.MatchString(payload.ContextDigest) ||
		!sha256DigestPattern.MatchString(payload.CreationSpecContentDigest) || createdAt.IsZero() {
		return Record{}, fmt.Errorf("encode plan_storyboard.preview.accepted payload: invalid safe payload")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Record{}, fmt.Errorf("encode plan_storyboard.preview.accepted payload: %w", err)
	}
	return Record{
		EventID: eventID, SessionID: sessionID, Type: TypePlanStoryboardPreviewAccepted,
		SchemaVersion: SchemaVersionV1, SourceKind: SourceKindPlanStoryboardPreview, SourceID: sourceID,
		ProjectionIndex: 0, AggregateType: AggregateTypePlanStoryboardPreview, AggregateID: payload.InputID,
		AggregateVersion: 1, PayloadJSON: encoded, CreatedAt: createdAt.UTC(),
	}, nil
}

// NewPlanStoryboardPreviewCompleted 构造合法 Tool completed Result 的完整安全 Card 事件。
func NewPlanStoryboardPreviewCompleted(
	eventID, sessionID, sourceID string,
	card PlanStoryboardPreviewCardPayload,
	aggregateVersion int64,
	createdAt time.Time,
) (Record, error) {
	return newPlanStoryboardPreviewTerminal(
		eventID, sessionID, sourceID, card, aggregateVersion,
		TypePlanStoryboardPreviewCompleted, "completed", "", createdAt,
	)
}

// NewPlanStoryboardPreviewFailed 构造合法 Tool failed Result 的安全 Card 事件。
func NewPlanStoryboardPreviewFailed(
	eventID, sessionID, sourceID string,
	card PlanStoryboardPreviewCardPayload,
	aggregateVersion int64,
	createdAt time.Time,
) (Record, error) {
	return newPlanStoryboardPreviewTerminal(
		eventID, sessionID, sourceID, card, aggregateVersion,
		TypePlanStoryboardPreviewFailed, "failed", PlanStoryboardPreviewFailureKindTool, createdAt,
	)
}

// NewPlanStoryboardPreviewRuntimeFailed 构造独立 Runtime Failure 安全 Card，不伪造 Tool failed Result。
func NewPlanStoryboardPreviewRuntimeFailed(
	eventID, sessionID, sourceID string,
	card PlanStoryboardPreviewCardPayload,
	aggregateVersion int64,
	createdAt time.Time,
) (Record, error) {
	return newPlanStoryboardPreviewTerminal(
		eventID, sessionID, sourceID, card, aggregateVersion,
		TypePlanStoryboardPreviewRuntimeFailed, "failed", PlanStoryboardPreviewFailureKindRuntime, createdAt,
	)
}

func newPlanStoryboardPreviewTerminal(
	eventID, sessionID, sourceID string,
	card PlanStoryboardPreviewCardPayload,
	aggregateVersion int64,
	eventType Type,
	expectedStatus, expectedFailureKind string,
	createdAt time.Time,
) (Record, error) {
	if card.SchemaVersion != PlanStoryboardPreviewCardSchemaVersionV1 || aggregateVersion != 1 ||
		createdAt.IsZero() || card.Status != expectedStatus {
		return Record{}, fmt.Errorf("encode %s payload: invalid card envelope", eventType)
	}
	if !validTurnCardIdentity(eventID, sessionID, sourceID, card.InputID, card.TurnID, card.RunID, card.ToolCallID) {
		return Record{}, fmt.Errorf("encode %s payload: invalid card identity", eventType)
	}
	if !validPlanStoryboardPreviewCard(card, expectedFailureKind) {
		return Record{}, fmt.Errorf("encode %s payload: invalid safe card", eventType)
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return Record{}, fmt.Errorf("encode %s payload: %w", eventType, err)
	}
	if len(encoded) > maxPlanStoryboardPreviewCardBytes {
		return Record{}, fmt.Errorf("encode %s payload: safe card exceeds limit", eventType)
	}
	return Record{
		EventID: eventID, SessionID: sessionID, Type: eventType,
		SchemaVersion: SchemaVersionV1, SourceKind: SourceKindPlanStoryboardPreview, SourceID: sourceID,
		ProjectionIndex: 1, AggregateType: AggregateTypePlanStoryboardPreview, AggregateID: card.InputID,
		AggregateVersion: aggregateVersion, PayloadJSON: encoded, CreatedAt: createdAt.UTC(),
	}, nil
}

func validPlanStoryboardPreviewCard(card PlanStoryboardPreviewCardPayload, expectedFailureKind string) bool {
	if card.UpdatedAt.IsZero() || card.UpdatedAt.Location() != time.UTC ||
		!stableTurnErrorCodePattern.MatchString(card.ResultCode) {
		return false
	}
	switch expectedFailureKind {
	case "":
		if card.Status != "completed" || card.ResultCode != planstoryboard.ResultCodeCompleted ||
			card.FailureKind != "" || card.Retryable != nil || card.CreationSpecRef == nil ||
			card.Sections == nil || card.Elements == nil || card.Slots == nil {
			return false
		}
		draft := planstoryboard.Card{
			SchemaVersion:       planstoryboard.CardSchemaVersion,
			StoryboardPreviewID: card.StoryboardPreviewID, ProjectID: card.ProjectID,
			CreationSpecRef: *card.CreationSpecRef, Version: card.Version, Status: "draft",
			ContentDigest: card.ContentDigest, Title: card.Title, Summary: card.Summary,
			Sections: *card.Sections, Elements: *card.Elements, Slots: *card.Slots, UpdatedAt: card.UpdatedAt,
		}
		return planstoryboard.ValidateCard(draft) == nil
	case PlanStoryboardPreviewFailureKindTool, PlanStoryboardPreviewFailureKindRuntime:
		return card.Status == "failed" && validPlanStoryboardResultCode(card.ResultCode, expectedFailureKind) &&
			card.FailureKind == expectedFailureKind && card.Retryable != nil && validSafeSummary(card.Summary) &&
			card.StoryboardPreviewID == "" && card.ProjectID == "" && card.CreationSpecRef == nil &&
			card.Version == 0 && card.ContentDigest == "" && card.Title == "" &&
			card.Sections == nil && card.Elements == nil && card.Slots == nil
	default:
		return false
	}
}

func validPlanStoryboardResultCode(value, failureKind string) bool {
	if failureKind == PlanStoryboardPreviewFailureKindRuntime {
		return value == "PLAN_STORYBOARD_RUNTIME_FAILED"
	}
	switch value {
	case planstoryboard.ResultCodeInvalidArgument,
		planstoryboard.ResultCodeCreationSpecNotFound,
		planstoryboard.ResultCodeCreationSpecConflict,
		planstoryboard.ResultCodeCandidateInvalid,
		planstoryboard.ResultCodeDependencyInvalid,
		planstoryboard.ResultCodeBusinessConflict,
		planstoryboard.ResultCodeBusinessDisabled,
		planstoryboard.ResultCodeInternal:
		return true
	default:
		return false
	}
}

// NewWritePromptsPreviewAccepted 构造 Prompt typed Input 的 accepted exact-set。
func NewWritePromptsPreviewAccepted(
	eventID, sessionID, sourceID string,
	payload WritePromptsPreviewAcceptedPayload,
	createdAt time.Time,
) (Record, error) {
	if payload.SchemaVersion != WritePromptsPreviewAcceptedSchemaVersionV1 ||
		!validTurnCardIdentity(eventID, sessionID, sourceID, payload.InputID, payload.TurnID, payload.RunID,
			payload.ToolCallID, payload.BusinessCommandID, payload.StoryboardPreviewID) ||
		payload.StoryboardPreviewVersion != 1 || !sha256DigestPattern.MatchString(payload.IntentDigest) ||
		!sha256DigestPattern.MatchString(payload.ContextDigest) ||
		!sha256DigestPattern.MatchString(payload.StoryboardPreviewContentDigest) || createdAt.IsZero() {
		return Record{}, fmt.Errorf("encode write_prompts.preview.accepted payload: invalid safe payload")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Record{}, fmt.Errorf("encode write_prompts.preview.accepted payload: %w", err)
	}
	return Record{
		EventID: eventID, SessionID: sessionID, Type: TypeWritePromptsPreviewAccepted,
		SchemaVersion: SchemaVersionV1, SourceKind: SourceKindWritePromptsPreview, SourceID: sourceID,
		ProjectionIndex: 0, AggregateType: AggregateTypeWritePromptsPreview, AggregateID: payload.InputID,
		AggregateVersion: 1, PayloadJSON: encoded, CreatedAt: createdAt.UTC(),
	}, nil
}

// NewWritePromptsPreviewCompleted 构造合法 Tool completed Result 的完整安全 Card 事件。
func NewWritePromptsPreviewCompleted(eventID, sessionID, sourceID string, card WritePromptsPreviewCardPayload, aggregateVersion int64, createdAt time.Time) (Record, error) {
	return newWritePromptsPreviewTerminal(eventID, sessionID, sourceID, card, aggregateVersion, TypeWritePromptsPreviewCompleted, "completed", "", createdAt)
}

// NewWritePromptsPreviewFailed 构造合法 Tool failed Result 的安全 Card 事件。
func NewWritePromptsPreviewFailed(eventID, sessionID, sourceID string, card WritePromptsPreviewCardPayload, aggregateVersion int64, createdAt time.Time) (Record, error) {
	return newWritePromptsPreviewTerminal(eventID, sessionID, sourceID, card, aggregateVersion, TypeWritePromptsPreviewFailed, "failed", WritePromptsPreviewFailureKindTool, createdAt)
}

// NewWritePromptsPreviewRuntimeFailed 构造独立 Runtime Failure 安全 Card，不伪造 Tool failed Result。
func NewWritePromptsPreviewRuntimeFailed(eventID, sessionID, sourceID string, card WritePromptsPreviewCardPayload, aggregateVersion int64, createdAt time.Time) (Record, error) {
	return newWritePromptsPreviewTerminal(eventID, sessionID, sourceID, card, aggregateVersion, TypeWritePromptsPreviewRuntimeFailed, "failed", WritePromptsPreviewFailureKindRuntime, createdAt)
}

func newWritePromptsPreviewTerminal(eventID, sessionID, sourceID string, card WritePromptsPreviewCardPayload, aggregateVersion int64, eventType Type, expectedStatus, expectedFailureKind string, createdAt time.Time) (Record, error) {
	if aggregateVersion != 1 || createdAt.IsZero() || card.Status != expectedStatus ||
		!validTurnCardIdentity(eventID, sessionID, sourceID, card.InputID, card.TurnID, card.RunID, card.ToolCallID) ||
		!validWritePromptsPreviewCard(card, expectedFailureKind) {
		return Record{}, fmt.Errorf("encode %s payload: invalid safe card", eventType)
	}
	encoded, err := json.Marshal(card)
	if err != nil || len(encoded) > maxWritePromptsPreviewCardBytes {
		return Record{}, fmt.Errorf("encode %s payload: invalid card encoding", eventType)
	}
	return Record{
		EventID: eventID, SessionID: sessionID, Type: eventType,
		SchemaVersion: SchemaVersionV1, SourceKind: SourceKindWritePromptsPreview, SourceID: sourceID,
		ProjectionIndex: 1, AggregateType: AggregateTypeWritePromptsPreview, AggregateID: card.InputID,
		AggregateVersion: aggregateVersion, PayloadJSON: encoded, CreatedAt: createdAt.UTC(),
	}, nil
}

func validWritePromptsPreviewCard(card WritePromptsPreviewCardPayload, failureKind string) bool {
	if card.SchemaVersion != WritePromptsPreviewCardSchemaVersionV1 || card.UpdatedAt.IsZero() || card.UpdatedAt.Location() != time.UTC {
		return false
	}
	if failureKind == "" {
		return card.Status == "completed" && card.FailureKind == "" && card.ResultCode == writeprompts.ResultCodeCompleted &&
			card.Retryable == nil && writeprompts.ValidateCard(card) == nil
	}
	if failureKind == WritePromptsPreviewFailureKindRuntime {
		return card.Status == "failed" && card.FailureKind == failureKind && card.ResultCode == "WRITE_PROMPTS_RUNTIME_FAILED" &&
			card.Retryable != nil && validSafeSummary(card.Summary) && card.PromptPreviewID == "" &&
			card.ProjectID == "" && card.StoryboardPreviewRef == nil && card.Version == 0 &&
			card.ContentDigest == "" && card.TargetCount == 0 && len(card.Prompts) == 0
	}
	if failureKind != WritePromptsPreviewFailureKindTool || card.Status != "failed" || card.FailureKind != failureKind ||
		card.Retryable == nil || !validSafeSummary(card.Summary) {
		return false
	}
	switch card.ResultCode {
	case writeprompts.ResultCodeInvalidArgument, writeprompts.ResultCodeStoryboardNotFound,
		writeprompts.ResultCodeStoryboardConflict, writeprompts.ResultCodeNoTargets,
		writeprompts.ResultCodeTargetBudgetExceeded, writeprompts.ResultCodeCandidateInvalid,
		writeprompts.ResultCodeExactSetInvalid, writeprompts.ResultCodeBusinessConflict,
		writeprompts.ResultCodeBusinessDisabled:
		return writeprompts.ValidateCard(card) == nil
	default:
		return false
	}
}

func validSafeSummary(summary string) bool {
	return utf8.ValidString(summary) && len([]byte(summary)) > 0 && len([]byte(summary)) <= 2_000
}

// NewMediaPreviewAccepted 构造 Graph 已派发的 accepted Card，聚合绑定原请求 Input。
func NewMediaPreviewAccepted(eventID, sessionID, sourceID, aggregateID string, card MediaPreviewCardPayload, createdAt time.Time) (Record, error) {
	return newMediaPreviewRecord(eventID, sessionID, sourceID, aggregateID, card, TypeMediaPreviewAccepted, createdAt)
}

// NewMediaPreviewCompleted 构造 Worker succeeded 终态 Card，聚合绑定 Terminal Bridge Input。
func NewMediaPreviewCompleted(eventID, sessionID, sourceID, aggregateID string, card MediaPreviewCardPayload, createdAt time.Time) (Record, error) {
	return newMediaPreviewRecord(eventID, sessionID, sourceID, aggregateID, card, TypeMediaPreviewCompleted, createdAt)
}

// NewMediaPreviewFailed 构造 Graph early-failed 或 Worker failed Card。
func NewMediaPreviewFailed(eventID, sessionID, sourceID, aggregateID string, card MediaPreviewCardPayload, createdAt time.Time) (Record, error) {
	return newMediaPreviewRecord(eventID, sessionID, sourceID, aggregateID, card, TypeMediaPreviewFailed, createdAt)
}

// NewMediaPreviewRuntimeFailed 构造独立 Runtime Failure Card，不伪造 Worker 业务失败。
func NewMediaPreviewRuntimeFailed(eventID, sessionID, sourceID, aggregateID string, card MediaPreviewCardPayload, createdAt time.Time) (Record, error) {
	return newMediaPreviewRecord(eventID, sessionID, sourceID, aggregateID, card, TypeMediaPreviewRuntimeFailed, createdAt)
}

func newMediaPreviewRecord(eventID, sessionID, sourceID, aggregateID string, card MediaPreviewCardPayload, eventType Type, createdAt time.Time) (Record, error) {
	if !validTurnCardIdentity(eventID, sessionID, sourceID, aggregateID, card.InputID, card.TurnID, card.RunID, card.ToolCallID) ||
		card.SchemaVersion != MediaPreviewCardSchemaVersionV1 || createdAt.IsZero() || card.UpdatedAt.IsZero() ||
		card.UpdatedAt.Location() != time.UTC || !validMediaPreviewCard(card, eventType) {
		return Record{}, fmt.Errorf("encode %s payload: invalid safe card", eventType)
	}
	payload, err := json.Marshal(card)
	if err != nil || len(payload) > 64*1024 {
		return Record{}, fmt.Errorf("encode %s payload: invalid card encoding", eventType)
	}
	projectionIndex := 0
	if eventType == TypeMediaPreviewCompleted || (eventType == TypeMediaPreviewFailed && card.JobID != "") ||
		eventType == TypeMediaPreviewRuntimeFailed {
		projectionIndex = 1
	}
	return Record{
		EventID: eventID, SessionID: sessionID, Type: eventType,
		SchemaVersion: SchemaVersionV1, SourceKind: SourceKindMediaPreview, SourceID: sourceID,
		ProjectionIndex: projectionIndex, AggregateType: AggregateTypeSessionInput, AggregateID: aggregateID,
		AggregateVersion: 1, PayloadJSON: payload, CreatedAt: createdAt.UTC(),
	}, nil
}

func validMediaPreviewCard(card MediaPreviewCardPayload, eventType Type) bool {
	if card.ToolKey != "generate_media" && card.ToolKey != "assemble_output" {
		return false
	}
	if !stableTurnErrorCodePattern.MatchString(card.ResultCode) {
		return false
	}
	switch eventType {
	case TypeMediaPreviewAccepted:
		return card.Status == "accepted" && card.ResultCode == "MEDIA_PREVIEW_ACCEPTED" &&
			validTurnCardIdentity(card.OperationID, card.BatchID) && card.JobID == "" && card.ErrorCode == "" &&
			card.ContentURL == "" && validMediaPreviewAsset(card.AssetRef, card.ToolKey, "reserved", false)
	case TypeMediaPreviewCompleted:
		return card.Status == "completed" && card.ResultCode == "MEDIA_PREVIEW_COMPLETED" &&
			validTurnCardIdentity(card.OperationID, card.BatchID, card.JobID) && card.ErrorCode == "" &&
			validMediaPreviewAsset(card.AssetRef, card.ToolKey, "ready", true) &&
			validMediaContentURL(card.ContentURL, card.AssetRef.ID)
	case TypeMediaPreviewFailed:
		if card.Status != "failed" || card.ErrorCode == "" || card.ResultCode != card.ErrorCode || card.ContentURL != "" ||
			!stableTurnErrorCodePattern.MatchString(card.ErrorCode) {
			return false
		}
		if card.JobID == "" {
			return card.OperationID == "" && card.BatchID == "" && card.AssetRef == nil
		}
		return validTurnCardIdentity(card.OperationID, card.BatchID, card.JobID) &&
			validMediaPreviewAsset(card.AssetRef, card.ToolKey, "failed", false)
	case TypeMediaPreviewRuntimeFailed:
		if card.Status != "failed" || card.ResultCode != "MEDIA_PREVIEW_RUNTIME_FAILED" ||
			card.ErrorCode != card.ResultCode || card.ContentURL != "" {
			return false
		}
		if card.JobID == "" {
			return card.OperationID == "" && card.BatchID == "" && card.AssetRef == nil
		}
		return validTurnCardIdentity(card.OperationID, card.BatchID, card.JobID) &&
			validMediaPreviewAsset(card.AssetRef, card.ToolKey, "failed", false)
	default:
		return false
	}
}

func validMediaPreviewAsset(asset *MediaPreviewAssetRef, toolKey, status string, terminalReady bool) bool {
	if asset == nil || !validTurnCardIdentity(asset.ID) || asset.Version != 1 || asset.Status != status {
		return false
	}
	if (toolKey == "generate_media" && (asset.MediaKind != "image" || asset.MIMEType != "image/png")) ||
		(toolKey == "assemble_output" && (asset.MediaKind != "video" || asset.MIMEType != "video/mp4")) {
		return false
	}
	if terminalReady {
		return sha256DigestPattern.MatchString(asset.ContentDigest) && asset.SizeBytes > 0
	}
	return asset.ContentDigest == "" && asset.SizeBytes == 0
}

func validMediaContentURL(value, assetID string) bool {
	const prefix = "/api/v1/projects/"
	const marker = "/media-preview-assets/"
	if len(value) <= len(prefix) || value[:len(prefix)] != prefix {
		return false
	}
	rest := value[len(prefix):]
	for index := 0; index+len(marker) <= len(rest); index++ {
		if rest[index:index+len(marker)] == marker {
			projectID := rest[:index]
			return validTurnCardIdentity(projectID) && rest[index:] == marker+assetID+"/content"
		}
	}
	return false
}

// NewSessionTurnCompleted 用强类型 Direct Response Card 构造通用 Turn 完成事件。
func NewSessionTurnCompleted(
	eventID, sessionID, sourceID string,
	card SessionTurnDirectResponsePayload,
	aggregateVersion int64,
	createdAt time.Time,
) (Record, error) {
	if card.SchemaVersion != DirectResponseCardSchemaVersionV1 ||
		!validTurnCardIdentity(card.TurnID, card.RunID, card.InputID) || aggregateVersion <= 0 || createdAt.IsZero() ||
		card.Status != DirectResponseCompletedStatus ||
		card.MessageCode != DirectResponseMessageCode ||
		card.Summary != DirectResponseSummary ||
		len(card.AvailableActions) != 1 || card.AvailableActions[0] != DirectResponseActionOpenToolbox {
		return Record{}, fmt.Errorf("encode session.turn.completed payload: invalid direct response card")
	}
	payload, err := json.Marshal(card)
	if err != nil {
		return Record{}, fmt.Errorf("encode session.turn.completed payload: %w", err)
	}
	return newSessionTurnRecord(
		eventID, sessionID, sourceID, card.TurnID, aggregateVersion,
		TypeSessionTurnCompleted, payload, createdAt,
	), nil
}

// NewSessionTurnFailed 用强类型 Failure Card 构造通用 Turn 确定失败事件。
func NewSessionTurnFailed(
	eventID, sessionID, sourceID string,
	card SessionTurnFailurePayload,
	aggregateVersion int64,
	createdAt time.Time,
) (Record, error) {
	return newSessionTurnFailureRecord(
		eventID, sessionID, sourceID, card, aggregateVersion,
		TypeSessionTurnFailed, TurnFailedStatus, createdAt,
	)
}

// NewSessionTurnRecoveryPending 用强类型 Failure Card 构造通用 Turn 权威恢复阻塞事件。
func NewSessionTurnRecoveryPending(
	eventID, sessionID, sourceID string,
	card SessionTurnFailurePayload,
	aggregateVersion int64,
	createdAt time.Time,
) (Record, error) {
	return newSessionTurnFailureRecord(
		eventID, sessionID, sourceID, card, aggregateVersion,
		TypeSessionTurnRecoveryPending, TurnRecoveryPendingStatus, createdAt,
	)
}

func newSessionTurnFailureRecord(
	eventID, sessionID, sourceID string,
	card SessionTurnFailurePayload,
	aggregateVersion int64,
	eventType Type,
	expectedStatus string,
	createdAt time.Time,
) (Record, error) {
	if card.SchemaVersion != FailureCardSchemaVersionV1 ||
		!validTurnCardIdentity(card.TurnID, card.RunID, card.InputID) || aggregateVersion <= 0 || createdAt.IsZero() ||
		card.Status != expectedStatus || !stableTurnErrorCodePattern.MatchString(card.ErrorCode) ||
		!utf8.ValidString(card.Summary) || len([]byte(card.Summary)) == 0 || len([]byte(card.Summary)) > 2000 {
		return Record{}, fmt.Errorf("encode %s payload: invalid failure card", eventType)
	}
	payload, err := json.Marshal(card)
	if err != nil {
		return Record{}, fmt.Errorf("encode %s payload: %w", eventType, err)
	}
	return newSessionTurnRecord(
		eventID, sessionID, sourceID, card.TurnID, aggregateVersion,
		eventType, payload, createdAt,
	), nil
}

func validTurnCardIdentity(values ...string) bool {
	for _, value := range values {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.Version() != 7 || parsed.String() != value {
			return false
		}
	}
	return true
}

func newSessionTurnRecord(
	eventID, sessionID, sourceID, turnID string,
	aggregateVersion int64,
	eventType Type,
	payload []byte,
	createdAt time.Time,
) Record {
	return Record{
		EventID: eventID, SessionID: sessionID, Type: eventType,
		SchemaVersion: SchemaVersionV1, SourceKind: SourceKindUserMessageRuntime, SourceID: sourceID,
		ProjectionIndex: 0, AggregateType: AggregateTypeSessionTurn, AggregateID: turnID,
		AggregateVersion: aggregateVersion, PayloadJSON: payload, CreatedAt: createdAt.UTC(),
	}
}
