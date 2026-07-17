// Package mediapreview 实现 media.runtime.v3preview1 的本地确定性媒体契约、Graph 与运行时边界。
// 本包不开放生产 Provider、计费、Approval、TOS 或静态 Catalog availability。
package mediapreview

import (
	"context"
	"errors"
	"time"
)

const (
	// Profile 是唯一批准的本地媒体纵切 Profile。
	Profile = "media.runtime.v3preview1"

	GenerateMediaToolKey            = "generate_media"
	AssembleOutputToolKey           = "assemble_output"
	GenerateMediaDefinitionVersion  = "generate_media.v3preview1"
	AssembleOutputDefinitionVersion = "assemble_output.v3preview1"
	GenerateMediaIntentVersion      = "generate_media.intent.v3preview1"
	AssembleOutputIntentVersion     = "assemble_output.intent.v3preview1"
	GenerateOutputProfile           = "png_640x360.v1"
	AssembleOutputProfile           = "mp4_h264_640x360_2s.v1"

	PrepareRequestSchemaVersion  = "media_asset.prepare.preview.v1"
	PrepareResultSchemaVersion   = "media_asset.prepare.result.preview.v1"
	PrepareQuerySchemaVersion    = "media_asset.query-preparation.preview.v1"
	PrepareQueryResultVersion    = "media_asset.query-preparation.result.preview.v1"
	JobEnvelopeSchemaVersion     = "agent.media_job.preview.v1"
	JobResultSchemaVersion       = "media_job.preview.result.v1"
	TerminalPayloadSchemaVersion = "media_job.preview.terminal.v1"
	ToolResultSchemaVersion      = "media_preview.tool_result.v1"
	CardSchemaVersion            = "media_preview.card.v1"
)

const (
	ResultCodeAccepted             = "MEDIA_PREVIEW_ACCEPTED"
	ResultCodeInvalidArgument      = "INVALID_ARGUMENT"
	ResultCodeNotFound             = "NOT_FOUND"
	ResultCodeVersionConflict      = "VERSION_CONFLICT"
	ResultCodeIdempotencyConflict  = "IDEMPOTENCY_CONFLICT"
	ResultCodeDependencyNotReady   = "DEPENDENCY_NOT_READY"
	ResultCodeUnsupportedProfile   = "UNSUPPORTED_PROFILE"
	ResultCodeUnknownOutcome       = "UNKNOWN_OUTCOME"
	ResultCodeInternal             = "INTERNAL"
	SourceTypePromptPreview        = "prompt_preview"
	SourceTypeImageAsset           = "image_asset"
	JobTypeGeneratePNG             = "generate_png"
	JobTypeAssembleMP4             = "assemble_mp4"
	OperationStatusPreparing       = "preparing"
	OperationStatusRecoveryPending = "recovery_pending"
	OperationStatusAccepted        = "accepted"
	DispatchStatusNotFound         = "not_found"
	DispatchStatusCommitted        = "committed"
	DispatchStatusConflict         = "conflict"
	PreparationStatusNotFound      = "not_found"
	PreparationStatusCompleted     = "completed"
	PreparationStatusConflict      = "conflict"
)

var (
	// ErrInvalidArgument 表示 exact DTO、身份或版本不满足冻结契约。
	ErrInvalidArgument = errors.New("media preview invalid argument")
	// ErrIdempotencyConflict 表示同一 first-write-wins 键携带了不同语义。
	ErrIdempotencyConflict = errors.New("media preview idempotency conflict")
	// ErrVersionConflict 表示聚合状态或乐观版本不允许当前迁移。
	ErrVersionConflict = errors.New("media preview version conflict")
	// ErrUnknownOutcome 表示副作用边界可能已提交，只能按原键查询。
	ErrUnknownOutcome = errors.New("media preview unknown outcome")
	// ErrDependencyNotReady 表示本地预览依赖没有通过 readiness。
	ErrDependencyNotReady = errors.New("media preview dependency not ready")
	// ErrBusinessConflict 表示 Business 权威确认同键异义或资源版本冲突。
	ErrBusinessConflict = errors.New("media preview Business conflict")
	// ErrBusinessPermanent 表示 Business 权威确认请求未提交且不可恢复。
	ErrBusinessPermanent = errors.New("media preview Business permanent failure")
)

// TrustedContext 是 Runtime 注入且永不暴露给调用方填写的媒体命令身份。
type TrustedContext struct {
	RequestID      string
	IdempotencyKey string
	UserID         string
	ProjectID      string
	SessionID      string
	InputID        string
	TurnID         string
	RunID          string
	ToolCallID     string
	FenceToken     int64
	DeadlineAt     time.Time
}

// PromptPreviewRef 是 generate_media 唯一允许引用的 Business Prompt Preview Draft。
type PromptPreviewRef struct {
	ID            string `json:"id"`
	Version       int64  `json:"version"`
	ContentDigest string `json:"content_digest"`
}

// ImageAssetRef 是 assemble_output 唯一允许引用的同项目 ready PNG。
type ImageAssetRef struct {
	ID            string `json:"id"`
	Version       int64  `json:"version"`
	ContentDigest string `json:"content_digest"`
}

// GenerateMediaIntent 是 generate_media.v3preview1 的 exact typed Intent。
type GenerateMediaIntent struct {
	SchemaVersion               string `json:"schema_version"`
	PromptPreviewID             string `json:"prompt_preview_id"`
	ExpectedPromptVersion       int64  `json:"expected_prompt_version"`
	ExpectedPromptContentDigest string `json:"expected_prompt_content_digest"`
	TargetLocalKey              string `json:"target_local_key"`
	OutputProfile               string `json:"output_profile"`
}

// AssembleOutputIntent 是 assemble_output.v3preview1 的 exact typed Intent。
type AssembleOutputIntent struct {
	SchemaVersion               string `json:"schema_version"`
	SourceAssetID               string `json:"source_asset_id"`
	ExpectedSourceVersion       int64  `json:"expected_source_version"`
	ExpectedSourceContentDigest string `json:"expected_source_content_digest"`
	OutputProfile               string `json:"output_profile"`
}

// SourceRef 是 Agent Job Envelope 中经过 Business 复核的封闭 source_ref。
type SourceRef struct {
	SourceType      string `json:"source_type"`
	SourceID        string `json:"source_id"`
	SourceVersion   int64  `json:"source_version"`
	SourceDigest    string `json:"source_digest"`
	TargetLocalKey  string `json:"target_local_key,omitempty"`
	TargetDigest    string `json:"target_digest,omitempty"`
	SourceObjectKey string `json:"source_object_key,omitempty"`
}

// Target 是 Business 生成且 Worker 只能相对对象根解析的封闭 target。
type Target struct {
	AssetID          string `json:"asset_id"`
	AssetVersion     int64  `json:"asset_version"`
	PreparationID    string `json:"preparation_id"`
	StagingObjectKey string `json:"staging_object_key"`
}

// AssetRef 是 Tool Result、终态与 Workspace Card 使用的最小资产引用。
type AssetRef struct {
	AssetID       string `json:"asset_id"`
	Version       int64  `json:"version"`
	Status        string `json:"status"`
	MediaKind     string `json:"media_kind"`
	MIMEType      string `json:"mime_type"`
	ContentDigest string `json:"content_digest,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
}

// PrepareRequest 是 Agent→Business loopback Prepare 的 exact V1 DTO。
type PrepareRequest struct {
	SchemaVersion    string            `json:"schema_version"`
	RequestID        string            `json:"request_id"`
	CommandID        string            `json:"command_id"`
	OperationID      string            `json:"operation_id"`
	RequestDigest    string            `json:"request_digest"`
	UserID           string            `json:"user_id"`
	ProjectID        string            `json:"project_id"`
	ToolKey          string            `json:"tool_key"`
	ScopeDigest      string            `json:"scope_digest"`
	OutputProfile    string            `json:"output_profile"`
	PromptSource     *PromptSource     `json:"prompt_source,omitempty"`
	ImageAssetSource *ImageAssetSource `json:"image_asset_source,omitempty"`
}

// PromptSource 是 Prepare generate 分支的 exact source。
type PromptSource struct {
	PromptPreviewID string `json:"prompt_preview_id"`
	Version         int64  `json:"version"`
	ContentDigest   string `json:"content_digest"`
	TargetLocalKey  string `json:"target_local_key"`
}

// ImageAssetSource 是 Prepare assemble 分支的 exact source。
type ImageAssetSource struct {
	AssetID       string `json:"asset_id"`
	Version       int64  `json:"version"`
	ContentDigest string `json:"content_digest"`
}

// PrepareResult 是 Business 权威预留结果；Object Key 永不进入 Tool Result 或 Event。
type PrepareResult struct {
	SchemaVersion    string    `json:"schema_version"`
	RequestID        string    `json:"request_id"`
	CommandID        string    `json:"command_id"`
	Disposition      string    `json:"disposition"`
	PreparationID    string    `json:"preparation_id"`
	AssetRef         AssetRef  `json:"asset_ref"`
	SourceRef        SourceRef `json:"source_ref"`
	OutputProfile    string    `json:"output_profile"`
	StagingObjectKey string    `json:"staging_object_key"`
	SourceObjectKey  string    `json:"source_object_key,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// PrepareQuery 按原 command/request digest 消除 Prepare Unknown Outcome。
type PrepareQuery struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id"`
	CommandID     string `json:"command_id"`
	RequestDigest string `json:"request_digest"`
	UserID        string `json:"user_id"`
	ProjectID     string `json:"project_id"`
}

// PrepareQueryResult 是 not_found/completed/conflict 严格联合。
type PrepareQueryResult struct {
	SchemaVersion string         `json:"schema_version"`
	RequestID     string         `json:"request_id"`
	Status        string         `json:"status"`
	Result        *PrepareResult `json:"result,omitempty"`
}

// BusinessReadiness 是 Agent 启动时验证 Business 同 Profile 与 Prepare/Finalize 能力的严格响应。
type BusinessReadiness struct {
	// SchemaVersion 固定为 media_asset.readiness.preview.v1。
	SchemaVersion string `json:"schema_version"`
	// Profile 必须与 Agent 媒体 Profile 完全一致。
	Profile string `json:"profile"`
	// ObjectRootReady 表示 Business 已安全创建并验证本地对象根。
	ObjectRootReady bool `json:"object_root_ready"`
	// Prepare 表示 Prepare/Query Preparation 端点已注册。
	Prepare bool `json:"prepare"`
	// Finalize 表示 Worker Finalize/Query 端点已注册。
	Finalize bool `json:"finalize"`
}

// Operation 是 EnsureOperation first-write-wins 后冻结的稳定聚合身份。
type Operation struct {
	OperationID          string
	BatchID              string
	JobID                string
	DispatchEventID      string
	PreparationRequestID string
	PreparationCommandID string
	ToolKey              string
	ScopeDigest          string
	OutputProfile        string
	Status               string
	Replayed             bool
}

// EnsureOperationCommand 是 Repository 创建/恢复 preparing Operation 的命令。
type EnsureOperationCommand struct {
	TrustedContext TrustedContext
	ToolKey        string
	ScopeDigest    string
	OutputProfile  string
}

// JobSpec 是 Dispatch 前冻结的单 Job 规范。
type JobSpec struct {
	JobID                 string
	BatchID               string
	OperationID           string
	SessionID             string
	UserID                string
	ProjectID             string
	JobType               string
	DefinitionVersion     string
	ScopeDigest           string
	OutputProfile         string
	SourceRef             SourceRef
	Target                Target
	ArtifactRequestDigest string
	CreatedAt             time.Time
	DeadlineAt            time.Time
}

// DispatchCommand 原子写一 Operation=一 Batch=一 Job 与 Dispatch Outbox。
type DispatchCommand struct {
	Operation      Operation
	Preparation    PrepareResult
	Job            JobSpec
	DispatchDigest string
}

// DispatchReceipt 是派发已提交的权威回执。
type DispatchReceipt struct {
	Status          string
	OperationID     string
	BatchID         string
	JobID           string
	DispatchEventID string
	AssetRef        AssetRef
	Replayed        bool
}

// DispatchQueryResult 是 not_found/committed/conflict 严格联合。
type DispatchQueryResult struct {
	Status  string
	Receipt *DispatchReceipt
}

// GraphToolResult 是两个媒体 Graph 的 accepted/failed exact result。
type GraphToolResult struct {
	SchemaVersion string    `json:"schema_version"`
	ToolKey       string    `json:"tool_key"`
	Status        string    `json:"status"`
	ResultCode    string    `json:"result_code"`
	OperationID   string    `json:"operation_id,omitempty"`
	BatchID       string    `json:"batch_id,omitempty"`
	AssetID       string    `json:"asset_id,omitempty"`
	ReceiptID     string    `json:"receipt_id,omitempty"`
	ErrorCode     string    `json:"error_code,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// RecoveryDeferred 表示 Graph 必须按原 Prepare/Dispatch key 继续核对，不能冻结失败结果。
type RecoveryDeferred struct {
	OperationID string
	ReasonCode  string
}

// GraphOutcome 是 terminal result 与 recovery 的严格互斥联合。
type GraphOutcome struct {
	Terminal *GraphToolResult
	Recovery *RecoveryDeferred
}

// BusinessClient 是 Graph 唯一可见的 Business Prepare/Query 端口。
type BusinessClient interface {
	Prepare(context.Context, PrepareRequest) (PrepareResult, error)
	QueryPreparation(context.Context, PrepareQuery) (PrepareQueryResult, error)
}

// Repository 是两个媒体 Graph 共用且只由 Agent PostgreSQL 实现的单状态机端口。
type Repository interface {
	EnsureOperation(context.Context, EnsureOperationCommand) (Operation, error)
	FreezePreparationRequest(context.Context, string, PrepareRequest) error
	RecordPreparation(context.Context, string, PrepareResult) error
	Dispatch(context.Context, DispatchCommand) (DispatchReceipt, error)
	QueryDispatch(context.Context, string, string) (DispatchQueryResult, error)
	DeferRecovery(context.Context, string, string) error
}

// IDGenerator 只在首次持久化时产生 UUIDv7；重放路径不得调用。
type IDGenerator interface {
	New() (string, error)
}
