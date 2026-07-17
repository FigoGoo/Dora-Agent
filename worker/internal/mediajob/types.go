// Package mediajob 实现 media.runtime.v3preview1 的单 Job Claim、Heartbeat、Finalize 和 Terminal 编排。
package mediajob

import (
	"encoding/json"
	"time"

	"github.com/FigoGoo/Dora-Agent/worker/internal/mediapreview"
	"github.com/google/uuid"
)

const (
	// FinalizeRequestSchemaV1 是 Worker 调用 Business Finalize 的请求版本。
	FinalizeRequestSchemaV1 = "media_asset.finalize.preview.v1"
	// FinalizeResultSchemaV1 是 Business Finalize 成功响应版本。
	FinalizeResultSchemaV1 = "media_asset.finalize.result.preview.v1"
	// QueryFinalizationRequestSchemaV1 是 Worker 核对原 Finalize 命令的请求版本。
	QueryFinalizationRequestSchemaV1 = "media_asset.query-finalization.preview.v1"
	// QueryFinalizationResultSchemaV1 是 Business Query-Finalization 响应版本。
	QueryFinalizationResultSchemaV1 = "media_asset.query-finalization.result.preview.v1"
	// ReadinessSchemaV1 是 Business 媒体内部端点 readiness 版本。
	ReadinessSchemaV1 = "media_asset.readiness.preview.v1"
	// TerminalResultSchemaV1 是提交 Agent Terminal Outbox 的冻结 Result 版本。
	TerminalResultSchemaV1 = "media_job.preview.result.v1"
)

// AttemptStatus 是 Worker 私有恢复表允许的封闭状态。
type AttemptStatus string

const (
	// AttemptStatusClaimPending 表示 Claim Intent 已持久化但尚未调用 Agent。
	AttemptStatusClaimPending AttemptStatus = "claim_pending"
	// AttemptStatusClaimUnknown 表示 Claim 响应未知，必须用原 claim_request_id 重放。
	AttemptStatusClaimUnknown AttemptStatus = "claim_unknown"
	// AttemptStatusRunning 表示 Agent 已返回当前 Attempt/Fence Envelope。
	AttemptStatusRunning AttemptStatus = "running"
	// AttemptStatusArtifactReady 表示本地产物已验证且摘要回执已持久化。
	AttemptStatusArtifactReady AttemptStatus = "artifact_ready"
	// AttemptStatusFinalizeUnknown 表示 Business Finalize 响应未知，必须按原键 Query。
	AttemptStatusFinalizeUnknown AttemptStatus = "finalize_unknown"
	// AttemptStatusReconciling 表示 Agent Job 和 Worker 尝试均等待权威核对。
	AttemptStatusReconciling AttemptStatus = "reconciling"
	// AttemptStatusTerminalUnknown 表示 Agent terminal commit 响应未知，必须按 Job Get 收敛。
	AttemptStatusTerminalUnknown AttemptStatus = "terminal_unknown"
	// AttemptStatusRetryScheduled 表示 Agent 已权威进入 retry_wait。
	AttemptStatusRetryScheduled AttemptStatus = "retry_scheduled"
	// AttemptStatusCompleted 表示成功终态已由 Agent 权威提交。
	AttemptStatusCompleted AttemptStatus = "completed"
	// AttemptStatusFailed 表示失败终态已由 Agent 权威提交。
	AttemptStatusFailed AttemptStatus = "failed"
	// AttemptStatusLeaseLost 表示当前 Worker 已失去 Attempt/Fence，禁止继续提交。
	AttemptStatusLeaseLost AttemptStatus = "lease_lost"
	// AttemptStatusClaimRejected 表示候选 Job 在调用时已经不可领取。
	AttemptStatusClaimRejected AttemptStatus = "claim_rejected"
)

// ClaimIntent 是在 Agent Claim 副作用前持久化的稳定重放键。
type ClaimIntent struct {
	// AttemptID 是 Agent Claim 使用的 UUIDv7 Attempt 标识。
	AttemptID uuid.UUID
	// ClaimRequestID 是 Claim first-write-wins UUIDv7 请求标识。
	ClaimRequestID uuid.UUID
	// JobID 是候选 Agent Job UUIDv7 标识。
	JobID uuid.UUID
	// WorkerID 是当前 Worker 实例标识。
	WorkerID string
	// Status 是当前 Worker 私有恢复状态。
	Status AttemptStatus
	// StartedAt 是 Claim Intent 首次持久化时间。
	StartedAt time.Time
}

// ArtifactRecord 是 Worker PostgreSQL 保存的不可变产物摘要，不包含 Object Key 或路径。
type ArtifactRecord struct {
	// ReceiptID 是 Worker 应用生成的 UUIDv7 回执标识。
	ReceiptID uuid.UUID
	// AttemptID 是产生该产物的 Attempt UUIDv7。
	AttemptID uuid.UUID
	// Receipt 是内存产物回执；Repository 持久化时必须忽略 ObjectKey。
	Receipt mediapreview.ArtifactReceiptV1
	// CreatedAt 是产物验证完成时间。
	CreatedAt time.Time
}

// FinalizeIntent 是 Business Finalize 副作用前冻结的稳定命令键。
type FinalizeIntent struct {
	// AttemptID 是对应 Worker Attempt UUIDv7。
	AttemptID uuid.UUID
	// CommandID 是 Business first-write-wins UUIDv7 命令标识。
	CommandID uuid.UUID
	// RequestDigest 是 Finalize 语义请求的 lowercase SHA-256。
	RequestDigest string
	// ErrorCode 是 failed Finalize first-write-wins 的稳定错误码，ready 时为空。
	ErrorCode string
}

// TerminalIntent 是 Agent terminal commit 副作用前冻结的稳定事件键和结果摘要。
type TerminalIntent struct {
	// AttemptID 是对应 Worker Attempt UUIDv7。
	AttemptID uuid.UUID
	// EventID 是 Terminal Outbox AppendOnce UUIDv7 事件标识。
	EventID uuid.UUID
	// Status 是 succeeded 或 failed。
	Status string
	// ResultDigest 是 Result 规范 JSON 的 lowercase SHA-256。
	ResultDigest string
}

// FinalizeOutputV1 是 Business Finalize 校验的产物摘要和媒体探针结果。
type FinalizeOutputV1 struct {
	// ContentDigest 是产物字节 lowercase SHA-256。
	ContentDigest string `json:"content_digest"`
	// SizeBytes 是产物精确字节数。
	SizeBytes int64 `json:"size_bytes"`
	// MIMEType 是 image/png 或 video/mp4。
	MIMEType string `json:"mime_type"`
	// Width 是验证后的 640 像素宽度。
	Width int `json:"width"`
	// Height 是验证后的 360 像素高度。
	Height int `json:"height"`
	// DurationMS 是 MP4 时长毫秒数；PNG 省略。
	DurationMS int64 `json:"duration_ms,omitempty"`
	// Codec 是 MP4 h264 codec；PNG 省略。
	Codec string `json:"codec,omitempty"`
	// PixelFormat 是 MP4 yuv420p；PNG 省略。
	PixelFormat string `json:"pixel_format,omitempty"`
}

// FinalizeRequestV1 是 Worker 调用 Business Finalize 的严格 DTO。
type FinalizeRequestV1 struct {
	// SchemaVersion 固定为 media_asset.finalize.preview.v1。
	SchemaVersion string `json:"schema_version"`
	// RequestID 是单次 HTTP 追踪 UUIDv7，可在重试时更新。
	RequestID uuid.UUID `json:"request_id"`
	// CommandID 是 first-write-wins UUIDv7 命令标识。
	CommandID uuid.UUID `json:"command_id"`
	// RequestDigest 是除 RequestID 外语义请求的 lowercase SHA-256。
	RequestDigest string `json:"request_digest"`
	// PreparationID 是 Business Prepare 回执 UUIDv7。
	PreparationID uuid.UUID `json:"preparation_id"`
	// OperationID 是 Agent Operation UUIDv7。
	OperationID uuid.UUID `json:"operation_id"`
	// BatchID 是 Agent Batch UUIDv7。
	BatchID uuid.UUID `json:"batch_id"`
	// JobID 是 Agent Job UUIDv7。
	JobID uuid.UUID `json:"job_id"`
	// AttemptID 是当前 Claim Attempt UUIDv7。
	AttemptID uuid.UUID `json:"attempt_id"`
	// Fence 是当前 Agent Fencing Token。
	Fence int64 `json:"fence"`
	// TerminalStatus 是 ready 或 failed。
	TerminalStatus string `json:"terminal_status"`
	// Output 在 ready 时必填，failed 时必须为空。
	Output *FinalizeOutputV1 `json:"output,omitempty"`
	// ErrorCode 在 failed 时必填且必须属于白名单，ready 时必须为空。
	ErrorCode string `json:"error_code,omitempty"`
}

// MediaAssetRefV1 是 Business 返回的权威 ready/failed Asset 引用。
type MediaAssetRefV1 struct {
	// AssetID 是 Business Media Asset UUIDv7。
	AssetID uuid.UUID `json:"asset_id"`
	// Version 是 Preview 契约冻结版本 1。
	Version int64 `json:"version"`
	// Status 是 ready 或 failed。
	Status string `json:"status"`
	// MediaKind 是 image 或 video。
	MediaKind string `json:"media_kind"`
	// MIMEType 是 image/png 或 video/mp4。
	MIMEType string `json:"mime_type"`
	// ContentDigest 是 ready 产物 lowercase SHA-256，failed 时为空。
	ContentDigest string `json:"content_digest,omitempty"`
	// SizeBytes 是 ready 产物字节数，failed 时为 0。
	SizeBytes int64 `json:"size_bytes,omitempty"`
}

// FinalizeResultV1 是 Business 对原 Finalize 命令的权威回执。
type FinalizeResultV1 struct {
	// SchemaVersion 固定为 media_asset.finalize.result.preview.v1。
	SchemaVersion string `json:"schema_version"`
	// RequestID 回显当前请求 UUIDv7。
	RequestID uuid.UUID `json:"request_id"`
	// CommandID 回显 first-write-wins 命令 UUIDv7。
	CommandID uuid.UUID `json:"command_id"`
	// Disposition 是 created 或 replayed。
	Disposition string `json:"disposition"`
	// AssetRef 是 Business 权威 Asset 引用。
	AssetRef MediaAssetRefV1 `json:"asset_ref"`
	// FinalizationReceiptID 是 Business 权威 Finalization Receipt UUIDv7。
	FinalizationReceiptID uuid.UUID `json:"finalization_receipt_id"`
	// CompletedAt 是 Business 提交终态的 UTC 时间。
	CompletedAt time.Time `json:"completed_at"`
}

// QueryFinalizationRequestV1 是按原 command/digest/preparation 核对 Finalize 的严格 DTO。
type QueryFinalizationRequestV1 struct {
	// SchemaVersion 固定为 media_asset.query-finalization.preview.v1。
	SchemaVersion string `json:"schema_version"`
	// RequestID 是本次查询追踪 UUIDv7。
	RequestID uuid.UUID `json:"request_id"`
	// CommandID 是原 Finalize UUIDv7 命令标识。
	CommandID uuid.UUID `json:"command_id"`
	// RequestDigest 是原 Finalize lowercase SHA-256。
	RequestDigest string `json:"request_digest"`
	// PreparationID 是原 Business Prepare UUIDv7 标识。
	PreparationID uuid.UUID `json:"preparation_id"`
}

// QueryFinalizationResultV1 是严格 not_found/completed/conflict 联合响应。
type QueryFinalizationResultV1 struct {
	// SchemaVersion 固定为 media_asset.query-finalization.result.preview.v1。
	SchemaVersion string `json:"schema_version"`
	// RequestID 回显查询 UUIDv7。
	RequestID uuid.UUID `json:"request_id"`
	// Status 是 not_found、completed 或 conflict。
	Status string `json:"status"`
	// Result 只在 completed 时存在。
	Result *FinalizeResultV1 `json:"result,omitempty"`
	// ErrorCode 只在 conflict 时存在且为稳定白名单错误码。
	ErrorCode string `json:"error_code,omitempty"`
}

// BusinessReadinessV1 是媒体内部端点的严格 readiness 响应。
type BusinessReadinessV1 struct {
	// SchemaVersion 固定为 media_asset.readiness.preview.v1。
	SchemaVersion string `json:"schema_version"`
	// Profile 固定为 media.runtime.v3preview1。
	Profile string `json:"profile"`
	// ObjectRootReady 表示 Business 已校验共享对象根。
	ObjectRootReady bool `json:"object_root_ready"`
	// Prepare 表示 Business Prepare 能力已注册。
	Prepare bool `json:"prepare"`
	// Finalize 表示 Business Finalize/Query 能力已注册。
	Finalize bool `json:"finalize"`
}

// FinalizationObservation 是 Worker 持久化的 Business 权威查询摘要。
type FinalizationObservation struct {
	// ObservationID 是 Worker 应用生成的 UUIDv7 观察标识。
	ObservationID uuid.UUID
	// AttemptID 是对应 Worker Attempt UUIDv7。
	AttemptID uuid.UUID
	// JobID 是 Agent Job UUIDv7。
	JobID uuid.UUID
	// Fence 是该 Finalize 使用的 Fencing Token。
	Fence int64
	// CommandID 是原 Business Finalize 命令 UUIDv7。
	CommandID uuid.UUID
	// RequestDigest 是原 Finalize lowercase SHA-256。
	RequestDigest string
	// PreparationID 是原 Business Prepare UUIDv7。
	PreparationID uuid.UUID
	// QueryStatus 是 not_found、completed 或 conflict。
	QueryStatus string
	// Result 是 completed 时的权威回执，其他状态为空。
	Result *FinalizeResultV1
	// ErrorCode 是 conflict 时的白名单错误码。
	ErrorCode string
	// ObservedAt 是获得该权威事实的 UTC 时间。
	ObservedAt time.Time
}

// FinalizationRecovery 是不同 Worker/Fence 接管时查询旧 Finalize 命令所需的最小共享事实。
type FinalizationRecovery struct {
	// CommandID 是旧 Attempt 已冻结的 Business Finalize UUIDv7 命令标识。
	CommandID uuid.UUID
	// RequestDigest 是旧 Finalize 语义请求 lowercase SHA-256。
	RequestDigest string
	// QueryStatus 是最近观察到的 not_found、completed、conflict；尚未查询时为空。
	QueryStatus string
	// Result 是最近 completed 权威回执；其他状态为空。
	Result *FinalizeResultV1
	// ErrorCode 是最近 conflict 的稳定错误码。
	ErrorCode string
}

// TerminalAssetRefV1 是 Agent Terminal Result 成功分支的最小 ready Asset 引用。
type TerminalAssetRefV1 struct {
	// AssetID 是 Business Media Asset UUIDv7。
	AssetID uuid.UUID `json:"asset_id"`
	// Version 固定为 1。
	Version int64 `json:"version"`
	// Status 固定为 ready。
	Status string `json:"status"`
	// MediaKind 是 image 或 video。
	MediaKind string `json:"media_kind"`
	// MIMEType 是 image/png 或 video/mp4。
	MIMEType string `json:"mime_type"`
	// ContentDigest 是 Business 权威 lowercase SHA-256。
	ContentDigest string `json:"content_digest"`
	// SizeBytes 是 Business 权威产物字节数。
	SizeBytes int64 `json:"size_bytes"`
}

// TerminalResultV1 是提交 Agent 的严格成功/失败联合 Result。
type TerminalResultV1 struct {
	// SchemaVersion 固定为 media_job.preview.result.v1。
	SchemaVersion string `json:"schema_version"`
	// Status 是 succeeded 或 failed。
	Status string `json:"status"`
	// AssetRef 只在 succeeded 时存在。
	AssetRef *TerminalAssetRefV1 `json:"asset_ref,omitempty"`
	// FinalizationReceiptID 只在 succeeded 时存在。
	FinalizationReceiptID *uuid.UUID `json:"finalization_receipt_id,omitempty"`
	// ErrorCode 只在 failed 时存在。
	ErrorCode string `json:"error_code,omitempty"`
}

// ClaimRequest 是调用 Agent claim 函数所需的稳定参数。
type ClaimRequest struct {
	// JobID 是待领取 Job UUIDv7。
	JobID uuid.UUID
	// WorkerID 是当前 Worker 实例标识。
	WorkerID string
	// AttemptID 是预先持久化的 UUIDv7 Attempt 标识。
	AttemptID uuid.UUID
	// ClaimRequestID 是预先持久化的 UUIDv7 幂等请求标识。
	ClaimRequestID uuid.UUID
	// LeaseTTL 是请求 Agent PostgreSQL 使用的租约时长。
	LeaseTTL time.Duration
}

// LeaseRequest 是 renew/retry/reconciling/terminal 共用的当前租约身份。
type LeaseRequest struct {
	// JobID 是当前 Agent Job UUIDv7。
	JobID uuid.UUID
	// WorkerID 是当前 Worker 实例标识。
	WorkerID string
	// AttemptID 是当前 Claim Attempt UUIDv7。
	AttemptID uuid.UUID
	// Fence 是当前正整数 Fencing Token。
	Fence int64
}

// ScheduleRetryRequest 是 Agent retry_wait 条件更新参数。
type ScheduleRetryRequest struct {
	// Lease 是当前 Attempt/Fence 身份。
	Lease LeaseRequest
	// Delay 是持久化到 available_at 的有限 Full Jitter 延迟。
	Delay time.Duration
	// ErrorCode 是允许重试的稳定错误码。
	ErrorCode string
}

// TerminalCommitRequest 是提交 Agent Job/Batch/Operation/Outbox 原子终态的参数。
type TerminalCommitRequest struct {
	// Lease 是当前 Attempt/Fence 身份。
	Lease LeaseRequest
	// EventID 是 Terminal Outbox AppendOnce UUIDv7 标识。
	EventID uuid.UUID
	// TerminalStatus 是 succeeded 或 failed。
	TerminalStatus string
	// ResultSchemaVersion 固定为 media_job.preview.result.v1。
	ResultSchemaVersion string
	// ResultDigest 是规范 Result JSON lowercase SHA-256。
	ResultDigest string
	// Result 是严格成功/失败联合 JSON，不含路径或内部诊断。
	Result json.RawMessage
}

// TerminalCommitResult 是 Agent commit_terminal 返回的权威状态投影。
type TerminalCommitResult struct {
	// JobStatus 是 succeeded 或 dead。
	JobStatus string
	// BatchStatus 是 completed 或 failed。
	BatchStatus string
	// OperationStatus 是 completed 或 failed。
	OperationStatus string
	// TerminalEventID 是权威 Terminal Outbox UUIDv7。
	TerminalEventID uuid.UUID
}

// JobSnapshot 是 Agent get 函数返回的最小终态核对投影。
type JobSnapshot struct {
	// JobStatus 是 pending、running、retry_wait、reconciling、succeeded 或 dead。
	JobStatus string
	// AttemptID 是当前或最后 Attempt UUIDv7。
	AttemptID uuid.UUID
	// Fence 是当前或最后 Fencing Token。
	Fence int64
	// LeaseOwner 是当前租约 Worker；非 running 时可为空。
	LeaseOwner string
	// LeaseExpiresAt 是当前租约到期时间；非 running 时可为空。
	LeaseExpiresAt time.Time
	// ResultSchemaVersion 是终态 Result 版本；非终态为空。
	ResultSchemaVersion string
	// ResultDigest 是终态 Result lowercase SHA-256；非终态为空。
	ResultDigest string
	// TerminalEventID 是终态 Outbox UUIDv7；非终态为空。
	TerminalEventID uuid.UUID
}
