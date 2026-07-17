package mediapreview

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	// EnvelopeSchemaVersionV1 是 Worker 接受的媒体预览任务 Envelope 版本。
	EnvelopeSchemaVersionV1 = "agent.media_job.preview.v1"
	// ArtifactReceiptSchemaVersionV1 是 Worker 产物回执的版本。
	ArtifactReceiptSchemaVersionV1 = "media_artifact.preview.receipt.v1"
	// PNGWidth 是预览 PNG 和 MP4 的冻结宽度。
	PNGWidth = 640
	// PNGHeight 是预览 PNG 和 MP4 的冻结高度。
	PNGHeight = 360
	// MP4DurationMS 是预览 MP4 的冻结目标时长，单位为毫秒。
	MP4DurationMS int64 = 2000
	// DefaultStderrLimitBytes 是外部媒体命令允许保留的最大诊断字节数。
	DefaultStderrLimitBytes int64 = 16 * 1024
	// MaxStderrLimitBytes 是外部媒体命令诊断上限的配置硬边界。
	MaxStderrLimitBytes int64 = 64 * 1024
	maxEnvelopeBytes          = 64 * 1024
)

var lowercaseSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// RuntimeProfile 表示 Worker 显式启用的本地媒体预览能力版本。
type RuntimeProfile string

const (
	// RuntimeProfileMediaV3Preview1 是当前唯一允许的本地媒体预览 Profile。
	RuntimeProfileMediaV3Preview1 RuntimeProfile = "media.runtime.v3preview1"
)

// GeneratorVersion 表示确定性 PNG 算法的冻结实现版本。
type GeneratorVersion string

const (
	// GeneratorVersionPNG640x360V1 冻结像素派生和 PNG Encoder 选项；修改算法必须发布新版本。
	GeneratorVersionPNG640x360V1 GeneratorVersion = "png_640x360.deterministic.v1"
)

// JobType 表示 Worker 可执行的封闭媒体预览任务类型。
type JobType string

const (
	// JobTypeGeneratePNG 使用纯 Go 标准库生成确定性 PNG。
	JobTypeGeneratePNG JobType = "generate_png"
	// JobTypeAssembleMP4 使用固定 ffmpeg 参数把 ready PNG 装配为 MP4。
	JobTypeAssembleMP4 JobType = "assemble_mp4"
)

// OutputProfile 表示任务输出格式、尺寸和时长的冻结组合。
type OutputProfile string

const (
	// OutputProfilePNG640x360V1 是 640x360 PNG 预览输出。
	OutputProfilePNG640x360V1 OutputProfile = "png_640x360.v1"
	// OutputProfileMP4H264640x3602sV1 是 640x360、H.264、2 秒 MP4 预览输出。
	OutputProfileMP4H264640x3602sV1 OutputProfile = "mp4_h264_640x360_2s.v1"
)

// DefinitionVersion 表示产生 Job 的 Agent Tool Definition 版本。
type DefinitionVersion string

const (
	// DefinitionVersionGenerateMediaV3Preview1 是 generate_media 的预览定义版本。
	DefinitionVersionGenerateMediaV3Preview1 DefinitionVersion = "generate_media.v3preview1"
	// DefinitionVersionAssembleOutputV3Preview1 是 assemble_output 的预览定义版本。
	DefinitionVersionAssembleOutputV3Preview1 DefinitionVersion = "assemble_output.v3preview1"
)

// SourceRefV1 表示 Business 已核验并冻结的媒体任务输入引用，不包含正文、URL 或绝对路径。
type SourceRefV1 struct {
	// SourceType 是 Business 发布的封闭来源种类。
	SourceType string `json:"source_type"`
	// SourceID 是 Business 权威来源对象的 UUIDv7 标识。
	SourceID uuid.UUID `json:"source_id"`
	// SourceVersion 是来源对象的正整数版本。
	SourceVersion int64 `json:"source_version"`
	// SourceDigest 是来源对象冻结内容的 lowercase SHA-256。
	SourceDigest string `json:"source_digest"`
	// TargetLocalKey 是 generate_png 选中的 Prompt Entry 本地键；assemble_mp4 必须为空。
	TargetLocalKey string `json:"target_local_key,omitempty"`
	// TargetDigest 是选中 Prompt Entry 的 lowercase SHA-256；assemble_mp4 必须为空。
	TargetDigest string `json:"target_digest,omitempty"`
	// SourceObjectKey 是 assemble_mp4 的 Business 相对 PNG key；generate_png 必须为空。
	SourceObjectKey string `json:"source_object_key,omitempty"`
}

// SourceType 表示 Business 发布给 Agent/Worker 的封闭来源类型。
type SourceType string

const (
	// SourceTypePromptPreview 是 generate_png 唯一允许的 Prompt Preview 来源。
	SourceTypePromptPreview SourceType = "prompt_preview"
	// SourceTypeImageAsset 是 assemble_mp4 唯一允许的 ready PNG Asset 来源。
	SourceTypeImageAsset SourceType = "image_asset"
)

// TargetV1 表示 Business 为本次 Job 预留的输出 Asset 和 staging 相对 key。
type TargetV1 struct {
	// AssetID 是待终结 Asset 的 UUIDv7 标识。
	AssetID uuid.UUID `json:"asset_id"`
	// AssetVersion 是 Preview 契约冻结的 Asset 版本，当前只能为 1。
	AssetVersion int64 `json:"asset_version"`
	// PreparationID 是 Business Prepare 回执的 UUIDv7 标识。
	PreparationID uuid.UUID `json:"preparation_id"`
	// StagingObjectKey 是 Business 生成的受控相对输出 key，不是绝对路径或 URL。
	StagingObjectKey string `json:"staging_object_key"`
}

// MediaJobEnvelopeV1 表示 Worker 从 Agent 版本化消费契约取得的单产物任务快照。
//
// Envelope 只承载稳定 ID、摘要、相对 Object Key 和执行期限；不承载 Prompt、命令参数或 Secret。
type MediaJobEnvelopeV1 struct {
	// SchemaVersion 固定为 agent.media_job.preview.v1。
	SchemaVersion string `json:"schema_version"`
	// JobID 是 Agent-owned Job 的 UUIDv7 标识。
	JobID uuid.UUID `json:"job_id"`
	// BatchID 是 Agent-owned 单 Job Batch 的 UUIDv7 标识。
	BatchID uuid.UUID `json:"batch_id"`
	// OperationID 是 Agent-owned Operation 的 UUIDv7 标识。
	OperationID uuid.UUID `json:"operation_id"`
	// SessionID 是承接终态事件的 Agent Session UUIDv7 标识。
	SessionID uuid.UUID `json:"session_id"`
	// UserID 是 Business 已核验的用户 UUIDv7 标识。
	UserID uuid.UUID `json:"user_id"`
	// ProjectID 是 Business 已核验的项目 UUIDv7 标识。
	ProjectID uuid.UUID `json:"project_id"`
	// JobType 是 generate_png 或 assemble_mp4，未知值失败关闭。
	JobType JobType `json:"job_type"`
	// DefinitionVersion 是与 JobType 配套的冻结 Tool Definition 版本。
	DefinitionVersion DefinitionVersion `json:"definition_version"`
	// ScopeDigest 是本次 Tool Scope 的 lowercase SHA-256。
	ScopeDigest string `json:"scope_digest"`
	// OutputProfile 是与 JobType 配套的冻结输出 Profile。
	OutputProfile OutputProfile `json:"output_profile"`
	// SourceRef 是 Business 已冻结的最小来源引用。
	SourceRef SourceRefV1 `json:"source_ref"`
	// Target 是 Business Prepare 返回的预留输出引用。
	Target TargetV1 `json:"target"`
	// ArtifactRequestDigest 是产物请求的 lowercase SHA-256 幂等摘要。
	ArtifactRequestDigest string `json:"artifact_request_digest"`
	// AttemptID 是当前 Claim Attempt 的 UUIDv7 标识。
	AttemptID uuid.UUID `json:"attempt_id"`
	// Fence 是当前租约的正整数 Fencing Token。
	Fence int64 `json:"fence"`
	// LeaseExpiresAt 是 Agent PostgreSQL 返回的当前租约到期时间。
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
	// CreatedAt 是 Job 权威创建时间。
	CreatedAt time.Time `json:"created_at"`
	// DeadlineAt 是本次 Job 的绝对执行截止时间。
	DeadlineAt time.Time `json:"deadline_at"`
}

// ArtifactReceiptV1 表示 Worker 对已落盘且验证通过的单一产物所冻结的回执。
//
// 回执只暴露 Business 相对 key 和可由 Finalize 复核的元数据，不包含本机绝对路径或命令诊断。
type ArtifactReceiptV1 struct {
	// SchemaVersion 固定为 media_artifact.preview.receipt.v1。
	SchemaVersion string `json:"schema_version"`
	// JobID 是产生该产物的 Agent Job UUIDv7 标识。
	JobID uuid.UUID `json:"job_id"`
	// AttemptID 是写入该产物的当前 Attempt UUIDv7 标识。
	AttemptID uuid.UUID `json:"attempt_id"`
	// Fence 是写入该产物时使用的 Fencing Token。
	Fence int64 `json:"fence"`
	// JobType 是生成该产物的封闭任务类型。
	JobType JobType `json:"job_type"`
	// GeneratorVersion 是 PNG 算法版本；MP4 任务中为空。
	GeneratorVersion GeneratorVersion `json:"generator_version,omitempty"`
	// ArtifactRequestDigest 是输入 Envelope 中的稳定请求摘要。
	ArtifactRequestDigest string `json:"artifact_request_digest"`
	// ObjectKey 是 Business 生成的 staging 相对 key。
	ObjectKey string `json:"object_key"`
	// ContentDigest 是产物字节的 lowercase SHA-256。
	ContentDigest string `json:"content_digest"`
	// SizeBytes 是验证后产物的精确字节数。
	SizeBytes int64 `json:"size_bytes"`
	// MIMEType 是验证后的固定媒体类型。
	MIMEType string `json:"mime_type"`
	// Width 是验证后的像素宽度。
	Width int `json:"width"`
	// Height 是验证后的像素高度。
	Height int `json:"height"`
	// DurationMS 是 MP4 探针返回的时长毫秒数；PNG 中为 0。
	DurationMS int64 `json:"duration_ms,omitempty"`
	// Codec 是 MP4 探针返回的 codec；PNG 中为空。
	Codec string `json:"codec,omitempty"`
	// PixelFormat 是 MP4 探针返回的 pixel format；PNG 中为空。
	PixelFormat string `json:"pixel_format,omitempty"`
}

// DecodeEnvelopeV1 对持久化 JSON 执行大小、未知字段、尾随值和业务联合约束校验。
//
// 成功时返回可直接交给 ArtifactEngine 的不可变值副本；失败时只返回稳定 INVALID_ARGUMENT。
func DecodeEnvelopeV1(payload []byte) (MediaJobEnvelopeV1, error) {
	if len(payload) == 0 || len(payload) > maxEnvelopeBytes {
		return MediaJobEnvelopeV1{}, newArtifactError(ErrorCodeInvalidArgument, "decode_envelope", nil)
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var envelope MediaJobEnvelopeV1
	if err := decoder.Decode(&envelope); err != nil {
		return MediaJobEnvelopeV1{}, newArtifactError(ErrorCodeInvalidArgument, "decode_envelope", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return MediaJobEnvelopeV1{}, newArtifactError(ErrorCodeInvalidArgument, "decode_envelope", err)
	}
	if err := envelope.Validate(); err != nil {
		return MediaJobEnvelopeV1{}, err
	}
	return envelope, nil
}

// Validate 校验 Envelope 的封闭版本、UUIDv7、摘要、时间以及 JobType/Profile 联合约束。
//
// 该方法不访问文件系统；相对 Object Key 的目录与 inode 安全性由 Engine 在实际打开前再次校验。
func (e MediaJobEnvelopeV1) Validate() error {
	if e.SchemaVersion != EnvelopeSchemaVersionV1 ||
		!isUUIDv7(e.JobID) || !isUUIDv7(e.BatchID) || !isUUIDv7(e.OperationID) ||
		!isUUIDv7(e.SessionID) || !isUUIDv7(e.UserID) || !isUUIDv7(e.ProjectID) ||
		!isUUIDv7(e.AttemptID) || !isUUIDv7(e.SourceRef.SourceID) ||
		!isUUIDv7(e.Target.AssetID) || !isUUIDv7(e.Target.PreparationID) ||
		e.SourceRef.SourceVersion <= 0 || e.Target.AssetVersion != 1 || e.Fence <= 0 ||
		!isLowercaseSHA256(e.ScopeDigest) || !isLowercaseSHA256(e.SourceRef.SourceDigest) ||
		!isLowercaseSHA256(e.ArtifactRequestDigest) ||
		e.SourceRef.SourceType == "" || len(e.SourceRef.SourceType) > 128 ||
		!utf8.ValidString(e.SourceRef.SourceType) || strings.TrimSpace(e.SourceRef.SourceType) != e.SourceRef.SourceType ||
		e.LeaseExpiresAt.IsZero() || e.CreatedAt.IsZero() || e.DeadlineAt.IsZero() ||
		!e.DeadlineAt.After(e.CreatedAt) {
		return newArtifactError(ErrorCodeInvalidArgument, "validate_envelope", nil)
	}

	if err := validateObjectKey(e.Target.StagingObjectKey); err != nil {
		return newArtifactError(ErrorCodeInvalidArgument, "validate_envelope", err)
	}

	switch e.JobType {
	case JobTypeGeneratePNG:
		if e.DefinitionVersion != DefinitionVersionGenerateMediaV3Preview1 ||
			e.OutputProfile != OutputProfilePNG640x360V1 ||
			e.SourceRef.SourceType != string(SourceTypePromptPreview) ||
			e.SourceRef.TargetLocalKey == "" || len(e.SourceRef.TargetLocalKey) > 128 ||
			!utf8.ValidString(e.SourceRef.TargetLocalKey) ||
			strings.TrimSpace(e.SourceRef.TargetLocalKey) != e.SourceRef.TargetLocalKey ||
			!isLowercaseSHA256(e.SourceRef.TargetDigest) ||
			e.SourceRef.SourceObjectKey != "" {
			return newArtifactError(ErrorCodeInvalidArgument, "validate_envelope", nil)
		}
	case JobTypeAssembleMP4:
		if e.DefinitionVersion != DefinitionVersionAssembleOutputV3Preview1 ||
			e.OutputProfile != OutputProfileMP4H264640x3602sV1 ||
			e.SourceRef.SourceType != string(SourceTypeImageAsset) ||
			e.SourceRef.TargetLocalKey != "" || e.SourceRef.TargetDigest != "" ||
			e.SourceRef.SourceObjectKey == e.Target.StagingObjectKey {
			return newArtifactError(ErrorCodeInvalidArgument, "validate_envelope", nil)
		}
		if err := validateObjectKey(e.SourceRef.SourceObjectKey); err != nil {
			return newArtifactError(ErrorCodeInvalidArgument, "validate_envelope", err)
		}
	default:
		return newArtifactError(ErrorCodeUnsupportedProfile, "validate_envelope", nil)
	}
	return nil
}

// isUUIDv7 仅接受 RFC 4122 variant 的 UUIDv7，避免 Worker 生成或接受另一套 ID 口径。
func isUUIDv7(value uuid.UUID) bool {
	return value != uuid.Nil && value.Version() == 7 && value.Variant() == uuid.RFC4122
}

// isLowercaseSHA256 校验摘要是无前缀的 64 位 lowercase SHA-256。
func isLowercaseSHA256(value string) bool {
	return lowercaseSHA256Pattern.MatchString(value)
}
