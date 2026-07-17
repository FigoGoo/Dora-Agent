// Package mediapreview 定义 Business-owned 本地媒体预览 Asset、回执与文件边界。
package mediapreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// RuntimeProfile 是本地媒体预览唯一允许的 Profile。
	RuntimeProfile = "media.runtime.v3preview1"
	// PrepareRequestSchemaVersion 是内部 Prepare 请求版本。
	PrepareRequestSchemaVersion = "media_asset.prepare.preview.v1"
	// PrepareResultSchemaVersion 是内部 Prepare 响应版本。
	PrepareResultSchemaVersion = "media_asset.prepare.result.preview.v1"
	// QueryPreparationRequestSchemaVersion 是内部 Preparation 查询请求版本。
	QueryPreparationRequestSchemaVersion = "media_asset.query-preparation.preview.v1"
	// QueryPreparationResultSchemaVersion 是内部 Preparation 查询响应版本。
	QueryPreparationResultSchemaVersion = "media_asset.query-preparation.result.preview.v1"
	// FinalizeRequestSchemaVersion 是内部 Finalize 请求版本。
	FinalizeRequestSchemaVersion = "media_asset.finalize.preview.v1"
	// FinalizeResultSchemaVersion 是内部 Finalize 响应版本。
	FinalizeResultSchemaVersion = "media_asset.finalize.result.preview.v1"
	// QueryFinalizationRequestSchemaVersion 是内部 Finalization 查询请求版本。
	QueryFinalizationRequestSchemaVersion = "media_asset.query-finalization.preview.v1"
	// QueryFinalizationResultSchemaVersion 是内部 Finalization 查询响应版本。
	QueryFinalizationResultSchemaVersion = "media_asset.query-finalization.result.preview.v1"
	// ReadinessSchemaVersion 是内部 Readiness 响应版本。
	ReadinessSchemaVersion = "media_asset.readiness.preview.v1"
	// AssetVersion 是媒体预览 Asset 固定不可变版本。
	AssetVersion int64 = 1
	// PNGWidth 是冻结 PNG 与 MP4 宽度。
	PNGWidth = 640
	// PNGHeight 是冻结 PNG 与 MP4 高度。
	PNGHeight = 360
	// MP4DurationMS 是冻结 MP4 时长毫秒值。
	MP4DurationMS int64 = 2000
	// MP4DurationToleranceMS 是 Business 接受的 Worker 探针时长容差。
	MP4DurationToleranceMS int64 = 100
	// ToolGenerateMedia 是确定性 PNG 预览 Tool Key。
	ToolGenerateMedia = "generate_media"
	// ToolAssembleOutput 是固定 MP4 装配 Tool Key。
	ToolAssembleOutput = "assemble_output"
	// OutputProfilePNG 是冻结 PNG 输出 Profile。
	OutputProfilePNG = "png_640x360.v1"
	// OutputProfileMP4 是冻结 MP4 输出 Profile。
	OutputProfileMP4 = "mp4_h264_640x360_2s.v1"
	// SourceTypePromptPreview 是 generate_media 的 Business-owned Prompt Draft 来源。
	SourceTypePromptPreview = "prompt_preview"
	// SourceTypeImageAsset 是 assemble_output 的 ready PNG 来源。
	SourceTypeImageAsset = "image_asset"
	// StatusReserved 是 Prepare 后、Finalize 前的 Asset 状态。
	StatusReserved = "reserved"
	// StatusReady 是文件已验证并发布后的 Asset 状态。
	StatusReady = "ready"
	// StatusFailed 是 Worker 已提交稳定失败后的 Asset 状态。
	StatusFailed = "failed"
	// MediaKindImage 是 PNG Asset 媒体种类。
	MediaKindImage = "image"
	// MediaKindVideo 是 MP4 Asset 媒体种类。
	MediaKindVideo = "video"
	// MIMEPNG 是冻结 PNG MIME。
	MIMEPNG = "image/png"
	// MIMEMP4 是冻结 MP4 MIME。
	MIMEMP4 = "video/mp4"
)

var (
	// ErrInvalidArgument 表示 DTO、ID、摘要、联合或对象键违反冻结契约。
	ErrInvalidArgument = errors.New("media preview invalid argument")
	// ErrNotFound 表示 Project、Prompt、Source Asset、Preparation 或 ready 内容不存在或不可访问。
	ErrNotFound = errors.New("media preview resource not found")
	// ErrVersionConflict 表示权威 Source 的版本或摘要与冻结引用不一致。
	ErrVersionConflict = errors.New("media preview version conflict")
	// ErrIdempotencyConflict 表示同一 command_id 或 Operation 已绑定到不同请求语义。
	ErrIdempotencyConflict = errors.New("media preview idempotency conflict")
	// ErrFenceStale 表示 Preparation 已由另一 Fence 终结，当前旧 Fence 不得覆盖。
	ErrFenceStale = errors.New("media preview fence stale")
	// ErrArtifactInvalid 表示 staging 或 ready 文件的 inode、摘要、magic 或元数据不可信。
	ErrArtifactInvalid = errors.New("media preview artifact invalid")
	// ErrDependencyNotReady 表示对象根或安全父目录尚未就绪。
	ErrDependencyNotReady = errors.New("media preview dependency not ready")
	// ErrUnknownOutcome 表示文件发布后无法确认目录同步或数据库提交结果。
	ErrUnknownOutcome = errors.New("media preview outcome unknown")
	// ErrPersistence 表示 PostgreSQL 或已存数据违反媒体预览不变量。
	ErrPersistence = errors.New("media preview persistence unavailable")

	lowercaseSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	targetLocalKeyPattern  = regexp.MustCompile(`^slot_([1-9]|[1-8][0-9]|9[0-6])$`)
)

// Digest 是冻结的 32 字节 SHA-256 值。
type Digest [sha256.Size]byte

// Hex 返回小写无前缀 SHA-256。
func (digest Digest) Hex() string { return hex.EncodeToString(digest[:]) }

// Bytes 返回摘要副本，避免 Repository 修改领域值。
func (digest Digest) Bytes() []byte { return append([]byte(nil), digest[:]...) }

// ParseDigest 严格解析小写 64 位 SHA-256。
func ParseDigest(value string) (Digest, error) {
	var digest Digest
	if !lowercaseSHA256Pattern.MatchString(value) {
		return digest, ErrInvalidArgument
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return digest, ErrInvalidArgument
	}
	copy(digest[:], decoded)
	return digest, nil
}

// DigestFromBytes 从数据库严格恢复 32 字节摘要。
func DigestFromBytes(value []byte) (Digest, error) {
	var digest Digest
	if len(value) != sha256.Size {
		return digest, ErrPersistence
	}
	copy(digest[:], value)
	return digest, nil
}

// CanonicalUUIDv7 只接受规范小写连字符 UUIDv7。
func CanonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.Variant() == uuid.RFC4122 && parsed.String() == value
}

// PromptSource 是 generate_media 必须精确复核的 Prompt Preview Draft 与图片目标。
type PromptSource struct {
	// ID 是 Business-owned Prompt Preview Draft UUIDv7。
	ID string
	// Version 固定为 1。
	Version int64
	// ContentDigest 是完整 Prompt Draft Canonical JSON 摘要。
	ContentDigest Digest
	// TargetLocalKey 是 Draft 中唯一选中的 image Prompt Entry 键。
	TargetLocalKey string
}

// Validate 校验 Prompt Source 精确引用。
func (source PromptSource) Validate() error {
	if !CanonicalUUIDv7(source.ID) || source.Version != AssetVersion || source.ContentDigest == (Digest{}) ||
		!targetLocalKeyPattern.MatchString(source.TargetLocalKey) {
		return ErrInvalidArgument
	}
	return nil
}

// ImageAssetSource 是 assemble_output 必须复核的 ready PNG Asset 引用。
type ImageAssetSource struct {
	// ID 是 Business 媒体预览 Asset UUIDv7。
	ID string
	// Version 固定为 1。
	Version int64
	// ContentDigest 是 ready PNG 文件字节摘要。
	ContentDigest Digest
}

// Validate 校验 ready PNG Source 精确引用。
func (source ImageAssetSource) Validate() error {
	if !CanonicalUUIDv7(source.ID) || source.Version != AssetVersion || source.ContentDigest == (Digest{}) {
		return ErrInvalidArgument
	}
	return nil
}

// PrepareCommand 是内部 Prepare Handler 交给应用服务的可信冻结命令。
type PrepareCommand struct {
	// RequestID 只用于本次调用追踪，不参与幂等。
	RequestID string
	// CommandID 是 first-write-wins UUIDv7。
	CommandID string
	// OperationID 是 Agent 媒体 Operation UUIDv7。
	OperationID string
	// RequestDigest 是调用方冻结完整请求语义的 SHA-256。
	RequestDigest Digest
	// OwnerUserID 是 Agent 可信上下文绑定的用户 UUIDv7。
	OwnerUserID string
	// ProjectID 是 Agent 可信上下文绑定的 Project UUIDv7。
	ProjectID string
	// ToolKey 只允许 generate_media 或 assemble_output。
	ToolKey string
	// ScopeDigest 是 Agent 冻结 Tool Scope 的 SHA-256。
	ScopeDigest Digest
	// OutputProfile 必须与 ToolKey 精确配套。
	OutputProfile string
	// PromptSource 只在 generate_media 中存在。
	PromptSource *PromptSource
	// ImageAssetSource 只在 assemble_output 中存在。
	ImageAssetSource *ImageAssetSource
}

// Validate 校验 Prepare 的 exact-set 与 Tool/Profile/Source 联合。
func (command PrepareCommand) Validate() error {
	if !CanonicalUUIDv7(command.RequestID) || !CanonicalUUIDv7(command.CommandID) ||
		!CanonicalUUIDv7(command.OperationID) || !CanonicalUUIDv7(command.OwnerUserID) ||
		!CanonicalUUIDv7(command.ProjectID) || command.RequestDigest == (Digest{}) || command.ScopeDigest == (Digest{}) {
		return ErrInvalidArgument
	}
	switch command.ToolKey {
	case ToolGenerateMedia:
		if command.OutputProfile != OutputProfilePNG || command.PromptSource == nil || command.ImageAssetSource != nil ||
			command.PromptSource.Validate() != nil {
			return ErrInvalidArgument
		}
	case ToolAssembleOutput:
		if command.OutputProfile != OutputProfileMP4 || command.PromptSource != nil || command.ImageAssetSource == nil ||
			command.ImageAssetSource.Validate() != nil {
			return ErrInvalidArgument
		}
	default:
		return ErrInvalidArgument
	}
	return nil
}

// SourceRef 是 Business 复核后返回给 Agent/Worker 的最小权威 Source 引用。
type SourceRef struct {
	// SourceType 是 prompt_preview 或 image_asset。
	SourceType string
	// SourceID 是权威 Source UUIDv7。
	SourceID string
	// SourceVersion 固定为 1。
	SourceVersion int64
	// SourceDigest 是权威 Source 内容摘要。
	SourceDigest Digest
	// TargetLocalKey 只在 generate_media 中存在。
	TargetLocalKey string
	// TargetDigest 是 Business 从选中 Prompt Entry 规范 JSON 计算的摘要。
	TargetDigest Digest
	// SourceObjectKey 只在 assemble_output 中存在，是 ready PNG 相对对象键。
	SourceObjectKey string
}

// Validate 校验 SourceRef 严格联合。
func (source SourceRef) Validate() error {
	if !CanonicalUUIDv7(source.SourceID) || source.SourceVersion != AssetVersion || source.SourceDigest == (Digest{}) {
		return ErrInvalidArgument
	}
	switch source.SourceType {
	case SourceTypePromptPreview:
		if !targetLocalKeyPattern.MatchString(source.TargetLocalKey) || source.TargetDigest == (Digest{}) || source.SourceObjectKey != "" {
			return ErrInvalidArgument
		}
	case SourceTypeImageAsset:
		if source.TargetLocalKey != "" || source.TargetDigest != (Digest{}) || !ValidObjectKey(source.SourceObjectKey) ||
			!strings.HasPrefix(source.SourceObjectKey, "objects/") {
			return ErrInvalidArgument
		}
	default:
		return ErrInvalidArgument
	}
	return nil
}

// AssetRef 是内部端点和终态投影使用的安全媒体 Asset 引用。
type AssetRef struct {
	// AssetID 是 Business 生成的 UUIDv7。
	AssetID string
	// Version 固定为 1。
	Version int64
	// Status 是 reserved、ready 或 failed。
	Status string
	// MediaKind 是 image 或 video。
	MediaKind string
	// MIMEType 是 image/png 或 video/mp4。
	MIMEType string
	// ContentDigest 只在 ready 中存在。
	ContentDigest Digest
	// SizeBytes 只在 ready 中为正数。
	SizeBytes int64
}

// Validate 校验 AssetRef 的安全联合字段。
func (reference AssetRef) Validate() error {
	if !CanonicalUUIDv7(reference.AssetID) || reference.Version != AssetVersion ||
		!validMediaPair(reference.MediaKind, reference.MIMEType) {
		return ErrInvalidArgument
	}
	switch reference.Status {
	case StatusReserved, StatusFailed:
		if reference.ContentDigest != (Digest{}) || reference.SizeBytes != 0 {
			return ErrInvalidArgument
		}
	case StatusReady:
		if reference.ContentDigest == (Digest{}) || reference.SizeBytes <= 0 {
			return ErrInvalidArgument
		}
	default:
		return ErrInvalidArgument
	}
	return nil
}

// PreparationAllocation 是 Service 为首次 Prepare 预分配的 Business 身份与相对对象键。
type PreparationAllocation struct {
	// PreparationID 是回执 UUIDv7。
	PreparationID string
	// AssetID 是输出 Asset UUIDv7。
	AssetID string
	// StagingObjectKey 是 Worker 写入的 Business 相对键。
	StagingObjectKey string
	// FinalObjectKey 是 Finalize 原子发布的 Business 相对键。
	FinalObjectKey string
	// CreatedAt 是首次提交 UTC 时间。
	CreatedAt time.Time
}

// ValidateFor 校验预分配 ID、时间和 Business 唯一对象键格式。
func (allocation PreparationAllocation) ValidateFor(command PrepareCommand) error {
	stagingKey, finalKey, err := ObjectKeys(allocation.AssetID, allocation.PreparationID, command.ToolKey)
	if err != nil || !CanonicalUUIDv7(allocation.PreparationID) || !CanonicalUUIDv7(allocation.AssetID) ||
		allocation.StagingObjectKey != stagingKey || allocation.FinalObjectKey != finalKey ||
		allocation.CreatedAt.IsZero() || allocation.CreatedAt.Location() != time.UTC {
		return ErrInvalidArgument
	}
	return nil
}

// Preparation 是首次 Prepare 或同义重放得到的权威回执。
type Preparation struct {
	// PreparationID 是回执 UUIDv7。
	PreparationID string
	// CommandID 是首次命令 UUIDv7。
	CommandID string
	// RequestDigest 是首次命令摘要。
	RequestDigest Digest
	// OperationID 是绑定的 Agent Operation UUIDv7。
	OperationID string
	// OwnerUserID 是冻结 Owner UUIDv7。
	OwnerUserID string
	// ProjectID 是冻结 Project UUIDv7。
	ProjectID string
	// ToolKey 是 generate_media 或 assemble_output。
	ToolKey string
	// ScopeDigest 是冻结 Scope 摘要。
	ScopeDigest Digest
	// OutputProfile 是冻结输出 Profile。
	OutputProfile string
	// SourceRef 是 Business 复核后的 Source。
	SourceRef SourceRef
	// AssetRef 是 reserved 输出 Asset。
	AssetRef AssetRef
	// StagingObjectKey 是 Worker 允许消费的相对键。
	StagingObjectKey string
	// FinalObjectKey 只供 Business Repository 使用，不进入公开响应。
	FinalObjectKey string
	// CreatedAt 是首次 Prepare UTC 时间。
	CreatedAt time.Time
}

// Validate 校验 Preparation 全部冻结不变量。
func (preparation Preparation) Validate() error {
	if !CanonicalUUIDv7(preparation.PreparationID) || !CanonicalUUIDv7(preparation.CommandID) ||
		!CanonicalUUIDv7(preparation.OperationID) || !CanonicalUUIDv7(preparation.OwnerUserID) ||
		!CanonicalUUIDv7(preparation.ProjectID) || preparation.RequestDigest == (Digest{}) ||
		preparation.ScopeDigest == (Digest{}) || preparation.SourceRef.Validate() != nil ||
		preparation.AssetRef.Validate() != nil || preparation.AssetRef.Status != StatusReserved ||
		preparation.CreatedAt.IsZero() || preparation.CreatedAt.Location() != time.UTC {
		return ErrInvalidArgument
	}
	expectedStaging, expectedFinal, err := ObjectKeys(preparation.AssetRef.AssetID, preparation.PreparationID, preparation.ToolKey)
	if err != nil || preparation.StagingObjectKey != expectedStaging || preparation.FinalObjectKey != expectedFinal {
		return ErrInvalidArgument
	}
	if preparation.ToolKey == ToolGenerateMedia {
		if preparation.OutputProfile != OutputProfilePNG || preparation.SourceRef.SourceType != SourceTypePromptPreview ||
			preparation.AssetRef.MediaKind != MediaKindImage || preparation.AssetRef.MIMEType != MIMEPNG {
			return ErrInvalidArgument
		}
	} else if preparation.ToolKey == ToolAssembleOutput {
		if preparation.OutputProfile != OutputProfileMP4 || preparation.SourceRef.SourceType != SourceTypeImageAsset ||
			preparation.AssetRef.MediaKind != MediaKindVideo || preparation.AssetRef.MIMEType != MIMEMP4 {
			return ErrInvalidArgument
		}
	} else {
		return ErrInvalidArgument
	}
	return nil
}

// CommandDisposition 表示命令首次提交或同语义重放。
type CommandDisposition string

const (
	// CommandDispositionCreated 表示首次提交成功。
	CommandDispositionCreated CommandDisposition = "created"
	// CommandDispositionReplayed 表示返回首次冻结事实。
	CommandDispositionReplayed CommandDisposition = "replayed"
)

// PrepareResult 是 Prepare 的安全结果。
type PrepareResult struct {
	// Disposition 是 created 或 replayed。
	Disposition CommandDisposition
	// Preparation 是权威首次回执。
	Preparation Preparation
}

// QueryStatus 是 Unknown Outcome 查询的封闭状态。
type QueryStatus string

const (
	// QueryStatusNotFound 表示当前没有匹配权威事实。
	QueryStatusNotFound QueryStatus = "not_found"
	// QueryStatusCompleted 表示原命令已经提交且摘要相同。
	QueryStatusCompleted QueryStatus = "completed"
	// QueryStatusConflict 表示 command_id 已绑定到不同摘要。
	QueryStatusConflict QueryStatus = "conflict"
)

// PreparationQuery 是查询原 Prepare 命令的完整安全键。
type PreparationQuery struct {
	// CommandID 是原 Prepare command_id。
	CommandID string
	// RequestDigest 是原请求摘要。
	RequestDigest Digest
	// OwnerUserID 是原可信用户。
	OwnerUserID string
	// ProjectID 是原目标 Project。
	ProjectID string
}

// Validate 校验 Preparation 查询键。
func (query PreparationQuery) Validate() error {
	if !CanonicalUUIDv7(query.CommandID) || query.RequestDigest == (Digest{}) ||
		!CanonicalUUIDv7(query.OwnerUserID) || !CanonicalUUIDv7(query.ProjectID) {
		return ErrInvalidArgument
	}
	return nil
}

// PreparationQueryResult 是 Prepare Unknown Outcome 查询结果。
type PreparationQueryResult struct {
	// Status 是 not_found、completed 或 conflict。
	Status QueryStatus
	// Preparation 只在 completed 中存在。
	Preparation *Preparation
}

// OutputMetadata 是 Worker 提供且 Business 必须按文件重新验证的冻结产物元数据。
type OutputMetadata struct {
	// ContentDigest 是文件字节 SHA-256。
	ContentDigest Digest
	// SizeBytes 是文件精确字节数。
	SizeBytes int64
	// MIMEType 是 image/png 或 video/mp4。
	MIMEType string
	// Width 固定 640。
	Width int
	// Height 固定 360。
	Height int
	// DurationMS 只在 MP4 中位于 1900..2100。
	DurationMS int64
	// Codec 只在 MP4 中固定 h264。
	Codec string
	// PixelFormat 只在 MP4 中固定 yuv420p。
	PixelFormat string
}

// Validate 校验冻结 PNG/MP4 元数据联合。
func (output OutputMetadata) Validate() error {
	if output.ContentDigest == (Digest{}) || output.SizeBytes <= 0 || output.Width != PNGWidth || output.Height != PNGHeight {
		return ErrInvalidArgument
	}
	switch output.MIMEType {
	case MIMEPNG:
		if output.DurationMS != 0 || output.Codec != "" || output.PixelFormat != "" {
			return ErrInvalidArgument
		}
	case MIMEMP4:
		if output.DurationMS < MP4DurationMS-MP4DurationToleranceMS ||
			output.DurationMS > MP4DurationMS+MP4DurationToleranceMS ||
			output.Codec != "h264" || output.PixelFormat != "yuv420p" {
			return ErrInvalidArgument
		}
	default:
		return ErrInvalidArgument
	}
	return nil
}

// FinalizeCommand 是 Worker 提交 ready 或 failed 终态的可信冻结命令。
type FinalizeCommand struct {
	// RequestID 只用于本次调用追踪。
	RequestID string
	// CommandID 是 Finalize first-write-wins UUIDv7。
	CommandID string
	// RequestDigest 是调用方冻结请求语义摘要。
	RequestDigest Digest
	// PreparationID 是原 Prepare 回执 UUIDv7。
	PreparationID string
	// OperationID 是原 Agent Operation UUIDv7。
	OperationID string
	// BatchID 是 Agent 单 Job Batch UUIDv7。
	BatchID string
	// JobID 是 Agent 媒体 Job UUIDv7。
	JobID string
	// AttemptID 是当前 Worker Attempt UUIDv7。
	AttemptID string
	// Fence 是当前租约正整数 Fencing Token。
	Fence int64
	// TerminalStatus 只允许 ready 或 failed。
	TerminalStatus string
	// Output 只在 ready 中存在。
	Output *OutputMetadata
	// ErrorCode 只在 failed 中存在且必须来自白名单。
	ErrorCode string
}

// Validate 校验 Finalize 身份、Fence 与终态联合。
func (command FinalizeCommand) Validate() error {
	if !CanonicalUUIDv7(command.RequestID) || !CanonicalUUIDv7(command.CommandID) ||
		!CanonicalUUIDv7(command.PreparationID) || !CanonicalUUIDv7(command.OperationID) ||
		!CanonicalUUIDv7(command.BatchID) || !CanonicalUUIDv7(command.JobID) ||
		!CanonicalUUIDv7(command.AttemptID) || command.RequestDigest == (Digest{}) || command.Fence <= 0 {
		return ErrInvalidArgument
	}
	switch command.TerminalStatus {
	case StatusReady:
		if command.Output == nil || command.Output.Validate() != nil || command.ErrorCode != "" {
			return ErrInvalidArgument
		}
	case StatusFailed:
		if command.Output != nil || !ValidTerminalErrorCode(command.ErrorCode) {
			return ErrInvalidArgument
		}
	default:
		return ErrInvalidArgument
	}
	return nil
}

// FinalizationAllocation 是 Service 为首次 Finalize 预分配的回执身份与时间。
type FinalizationAllocation struct {
	// ReceiptID 是 Business 生成的 Finalization Receipt UUIDv7。
	ReceiptID string
	// CompletedAt 是首次完成 UTC 时间。
	CompletedAt time.Time
}

// Validate 校验 Finalization 预分配事实。
func (allocation FinalizationAllocation) Validate() error {
	if !CanonicalUUIDv7(allocation.ReceiptID) || allocation.CompletedAt.IsZero() ||
		allocation.CompletedAt.Location() != time.UTC {
		return ErrInvalidArgument
	}
	return nil
}

// Finalization 是首次 Finalize 或同义重放得到的权威回执。
type Finalization struct {
	// ReceiptID 是 Finalization Receipt UUIDv7。
	ReceiptID string
	// CommandID 是首次命令 UUIDv7。
	CommandID string
	// RequestDigest 是首次命令摘要。
	RequestDigest Digest
	// PreparationID 是被终结的 Preparation。
	PreparationID string
	// OperationID 是原 Operation。
	OperationID string
	// BatchID 是原 Batch。
	BatchID string
	// JobID 是原 Job。
	JobID string
	// AttemptID 是首次终结 Attempt。
	AttemptID string
	// Fence 是首次终结 Fence。
	Fence int64
	// TerminalStatus 是 ready 或 failed。
	TerminalStatus string
	// AssetRef 是权威终态 Asset 引用。
	AssetRef AssetRef
	// Output 只在 ready 中存在。
	Output *OutputMetadata
	// ErrorCode 只在 failed 中存在。
	ErrorCode string
	// CompletedAt 是首次终结 UTC 时间。
	CompletedAt time.Time
}

// Validate 校验 Finalization 回执联合不变量。
func (finalization Finalization) Validate() error {
	if !CanonicalUUIDv7(finalization.ReceiptID) || !CanonicalUUIDv7(finalization.CommandID) ||
		finalization.RequestDigest == (Digest{}) || !CanonicalUUIDv7(finalization.PreparationID) ||
		!CanonicalUUIDv7(finalization.OperationID) || !CanonicalUUIDv7(finalization.BatchID) ||
		!CanonicalUUIDv7(finalization.JobID) || !CanonicalUUIDv7(finalization.AttemptID) || finalization.Fence <= 0 ||
		finalization.AssetRef.Validate() != nil || finalization.CompletedAt.IsZero() ||
		finalization.CompletedAt.Location() != time.UTC {
		return ErrInvalidArgument
	}
	switch finalization.TerminalStatus {
	case StatusReady:
		if finalization.AssetRef.Status != StatusReady || finalization.Output == nil ||
			finalization.Output.Validate() != nil || finalization.ErrorCode != "" ||
			finalization.AssetRef.ContentDigest != finalization.Output.ContentDigest ||
			finalization.AssetRef.SizeBytes != finalization.Output.SizeBytes ||
			finalization.AssetRef.MIMEType != finalization.Output.MIMEType {
			return ErrInvalidArgument
		}
	case StatusFailed:
		if finalization.AssetRef.Status != StatusFailed || finalization.Output != nil ||
			!ValidTerminalErrorCode(finalization.ErrorCode) {
			return ErrInvalidArgument
		}
	default:
		return ErrInvalidArgument
	}
	return nil
}

// FinalizeResult 是 Finalize 安全结果。
type FinalizeResult struct {
	// Disposition 是 created 或 replayed。
	Disposition CommandDisposition
	// Finalization 是权威首次回执。
	Finalization Finalization
}

// FinalizationQuery 是查询原 Finalize 命令的安全键。
type FinalizationQuery struct {
	// CommandID 是原 Finalize command_id。
	CommandID string
	// RequestDigest 是原请求摘要。
	RequestDigest Digest
	// PreparationID 是原 Preparation UUIDv7。
	PreparationID string
}

// Validate 校验 Finalization 查询键。
func (query FinalizationQuery) Validate() error {
	if !CanonicalUUIDv7(query.CommandID) || query.RequestDigest == (Digest{}) ||
		!CanonicalUUIDv7(query.PreparationID) {
		return ErrInvalidArgument
	}
	return nil
}

// FinalizationQueryResult 是 Finalize Unknown Outcome 查询结果。
type FinalizationQueryResult struct {
	// Status 是 not_found、completed 或 conflict。
	Status QueryStatus
	// Finalization 只在 completed 中存在。
	Finalization *Finalization
}

// ContentQuery 是公开 Owner 鉴权内容端点的最小查询。
type ContentQuery struct {
	// OwnerUserID 只来自已认证 Principal。
	OwnerUserID string
	// ProjectID 来自受保护资源路径。
	ProjectID string
	// AssetID 来自受保护资源路径。
	AssetID string
}

// Validate 校验公开内容查询身份。
func (query ContentQuery) Validate() error {
	if !CanonicalUUIDv7(query.OwnerUserID) || !CanonicalUUIDv7(query.ProjectID) || !CanonicalUUIDv7(query.AssetID) {
		return ErrInvalidArgument
	}
	return nil
}

// ReadyContent 是 Owner 校验后的单一 ready 文件事实；ObjectKey 不进入 HTTP JSON 或日志。
type ReadyContent struct {
	// AssetRef 是 ready Asset 安全引用。
	AssetRef AssetRef
	// ObjectKey 是只传给受控 ArtifactStore 的相对键。
	ObjectKey string
	// Output 是 Business 已持久化并会再次按文件验证的元数据。
	Output OutputMetadata
}

// Validate 校验 ready 内容事实。
func (content ReadyContent) Validate() error {
	if content.AssetRef.Validate() != nil || content.AssetRef.Status != StatusReady ||
		!ValidObjectKey(content.ObjectKey) || !strings.HasPrefix(content.ObjectKey, "objects/") ||
		content.Output.Validate() != nil || content.AssetRef.ContentDigest != content.Output.ContentDigest ||
		content.AssetRef.SizeBytes != content.Output.SizeBytes || content.AssetRef.MIMEType != content.Output.MIMEType {
		return ErrInvalidArgument
	}
	return nil
}

// ArtifactStore 是 Business 管理对象根的最小安全文件边界。
type ArtifactStore interface {
	// EnsurePreparation 创建并复核固定 staging/objects Asset 父目录。
	EnsurePreparation(stagingObjectKey string, finalObjectKey string) error
	// Verify 复核 ready Source 或已发布目标的 inode、摘要、大小、magic 与元数据。
	Verify(objectKey string, expected OutputMetadata) error
	// Promote 验证 staging 后原子发布到 objects；同内容已发布时允许恢复收敛。
	Promote(stagingObjectKey string, finalObjectKey string, expected OutputMetadata) error
	// OpenVerified 打开并复核单个 ready 对象；调用方负责关闭。
	OpenVerified(objectKey string, expected OutputMetadata) (*os.File, error)
}

// Repository 定义 Business PostgreSQL 与本地对象根的一致性边界。
type Repository interface {
	// Prepare first-write-wins 校验 Owner/Source 并原子创建 reserved Asset 与 Preparation Receipt。
	Prepare(ctx context.Context, command PrepareCommand, allocation PreparationAllocation) (PrepareResult, error)
	// QueryPreparation 按原 command/digest/owner/project 查询 Prepare 权威事实。
	QueryPreparation(ctx context.Context, query PreparationQuery) (PreparationQueryResult, error)
	// Finalize first-write-wins 锁定 Preparation/Asset、验证文件与 Fence 并写终态回执。
	Finalize(ctx context.Context, command FinalizeCommand, allocation FinalizationAllocation) (FinalizeResult, error)
	// QueryFinalization 按原 command/digest/preparation 查询 Finalize 权威事实。
	QueryFinalization(ctx context.Context, query FinalizationQuery) (FinalizationQueryResult, error)
	// OpenReadyContent 按可信 Owner/Project/Asset 返回 ready 文件事实与同一安全复核后的文件句柄。
	OpenReadyContent(ctx context.Context, query ContentQuery) (ReadyContent, *os.File, error)
}

// ObjectKeys 生成唯一允许的 Business staging 与 objects 相对键。
func ObjectKeys(assetID string, preparationID string, toolKey string) (string, string, error) {
	if !CanonicalUUIDv7(assetID) || !CanonicalUUIDv7(preparationID) {
		return "", "", ErrInvalidArgument
	}
	extension := ""
	switch toolKey {
	case ToolGenerateMedia:
		extension = "png"
	case ToolAssembleOutput:
		extension = "mp4"
	default:
		return "", "", ErrInvalidArgument
	}
	staging := path.Join("staging", assetID, preparationID+"."+extension)
	final := path.Join("objects", assetID, "v1."+extension)
	if !ValidObjectKey(staging) || !ValidObjectKey(final) {
		return "", "", ErrInvalidArgument
	}
	return staging, final, nil
}

// ValidObjectKey 拒绝绝对路径、点段、空段、反斜杠、控制字符和超长组件。
func ValidObjectKey(key string) bool {
	if key == "" || len(key) > 1024 || strings.HasPrefix(key, "/") || strings.Contains(key, `\`) ||
		strings.IndexByte(key, 0) >= 0 || path.IsAbs(key) || path.Clean(key) != key {
		return false
	}
	components := strings.Split(key, "/")
	for _, component := range components {
		if component == "" || component == "." || component == ".." || len(component) > 200 {
			return false
		}
		for _, character := range component {
			if character < 0x20 || character == 0x7f {
				return false
			}
		}
	}
	return true
}

// ValidTerminalErrorCode 只允许设计冻结的稳定失败码进入持久化和终态响应。
func ValidTerminalErrorCode(value string) bool {
	switch value {
	case "FEATURE_DISABLED", "INVALID_ARGUMENT", "NOT_FOUND", "VERSION_CONFLICT", "IDEMPOTENCY_CONFLICT",
		"DEPENDENCY_NOT_READY", "UNSUPPORTED_PROFILE", "LEASE_LOST", "FENCE_STALE", "ARTIFACT_INVALID",
		"FFMPEG_UNAVAILABLE", "EXECUTION_TIMEOUT", "UNKNOWN_OUTCOME", "INTERNAL":
		return true
	default:
		return false
	}
}

// TargetDigest 按 Prompt Entry 的规范 JSON 字节计算 SHA-256。
func TargetDigest(canonicalPromptEntry []byte) (Digest, error) {
	if len(canonicalPromptEntry) == 0 || len(canonicalPromptEntry) > 32*1024 {
		return Digest{}, ErrInvalidArgument
	}
	return sha256.Sum256(canonicalPromptEntry), nil
}

func validMediaPair(mediaKind string, mimeType string) bool {
	return (mediaKind == MediaKindImage && mimeType == MIMEPNG) ||
		(mediaKind == MediaKindVideo && mimeType == MIMEMP4)
}

// MapInfrastructureError 收敛文件与数据库实现错误，不向上泄露路径、SQL 或 DSN。
func MapInfrastructureError(err error) error {
	if err == nil {
		return nil
	}
	for _, stable := range []error{
		ErrInvalidArgument, ErrNotFound, ErrVersionConflict, ErrIdempotencyConflict, ErrFenceStale,
		ErrArtifactInvalid, ErrDependencyNotReady, ErrUnknownOutcome, ErrPersistence,
		context.Canceled, context.DeadlineExceeded,
	} {
		if errors.Is(err, stable) {
			return err
		}
	}
	return fmt.Errorf("%w", ErrPersistence)
}
