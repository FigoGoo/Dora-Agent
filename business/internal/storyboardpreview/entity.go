package storyboardpreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/creationspec"
	"github.com/google/uuid"
)

const (
	// RPCSchemaVersion 是 Storyboard Foundation Preview RPC 的唯一允许版本。
	RPCSchemaVersion = "storyboard.preview.rpc.v1"
	// DraftSchemaVersion 是 Storyboard Preview Draft JSON 的冻结版本。
	DraftSchemaVersion = "storyboard.preview.draft.v1"
	// SaveDigestSchemaVersion 是跨 Module 保存请求摘要的冻结版本。
	SaveDigestSchemaVersion = "storyboard.preview.save-draft.digest.v1"
	// DraftStatus 是 Development Preview 唯一允许持久化的状态。
	DraftStatus = "draft"
	// InitialDraftVersion 是不可变 Preview Draft 的固定资源版本。
	InitialDraftVersion int64 = 1
)

var (
	// ErrInvalidInput 表示 DTO、内容、ID、摘要或领域聚合违反冻结契约。
	ErrInvalidInput = errors.New("invalid storyboard preview input")
	// ErrNotFound 表示 Project 或 CreationSpec 不存在、跨 Owner 或状态不可用。
	ErrNotFound = errors.New("storyboard preview dependency not found")
	// ErrProjectVersionConflict 表示生成上下文冻结的 Project 版本已经变化。
	ErrProjectVersionConflict = errors.New("storyboard preview project version conflict")
	// ErrCreationSpecVersionConflict 表示 CreationSpec 版本或内容摘要已经变化。
	ErrCreationSpecVersionConflict = errors.New("storyboard preview creation spec version conflict")
	// ErrIdempotencyConflict 表示同一 command_id 已绑定到不同请求摘要。
	ErrIdempotencyConflict = errors.New("storyboard preview command conflict")
	// ErrPersistence 表示持久化不可用或数据库记录违反领域不变量。
	ErrPersistence = errors.New("storyboard preview persistence unavailable")
)

// Digest 是固定 32 字节的 SHA-256 摘要值。
type Digest [sha256.Size]byte

// Hex 返回小写、无前缀的 64 位十六进制编码。
func (digest Digest) Hex() string { return hex.EncodeToString(digest[:]) }

// Bytes 返回摘要副本，防止 Repository 修改领域值。
func (digest Digest) Bytes() []byte { return append([]byte(nil), digest[:]...) }

// ParseDigest 严格解析小写、无前缀的 SHA-256 十六进制编码。
func ParseDigest(value string) (Digest, error) {
	var digest Digest
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return digest, ErrInvalidInput
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return digest, ErrInvalidInput
	}
	copy(digest[:], decoded)
	return digest, nil
}

// DigestFromBytes 从数据库严格复制 32 字节摘要。
func DigestFromBytes(value []byte) (Digest, error) {
	var digest Digest
	if len(value) != sha256.Size {
		return digest, ErrPersistence
	}
	copy(digest[:], value)
	return digest, nil
}

// CreationSpecRef 冻结 Storyboard 规划依赖的 CreationSpec 精确版本。
type CreationSpecRef struct {
	// ID 是 Business CreationSpec Draft UUIDv7。
	ID string
	// Version 是 CreationSpec Draft 版本，当前固定为一。
	Version int64
	// ContentDigest 是 CreationSpec Canonical Content 的 SHA-256。
	ContentDigest Digest
}

// CreationSpecSnapshot 是 Owner 校验后可用于 Storyboard 规划的权威 Draft 快照。
type CreationSpecSnapshot struct {
	// ID 是 CreationSpec Draft UUIDv7。
	ID string
	// ProjectID 是所属 Project UUIDv7。
	ProjectID string
	// UserID 是创建时冻结的 Owner UUIDv7。
	UserID string
	// Status 在当前 Preview 固定为 draft。
	Status string
	// Version 是 CreationSpec Draft 版本。
	Version int64
	// SchemaVersion 是 CreationSpec 内容版本。
	SchemaVersion string
	// Content 是 Business 权威 CreationSpec 内容。
	Content creationspec.Content
	// ContentDigest 是 CreationSpec Canonical Content 摘要。
	ContentDigest Digest
}

// PlanningContext 是单次 Owner 查询返回的 Project 与 CreationSpec 联合快照。
type PlanningContext struct {
	// ProjectID 是经过 Owner 校验的 Project UUIDv7。
	ProjectID string
	// ProjectVersion 是保存 Storyboard Draft 时必须原样回传的乐观版本。
	ProjectVersion int64
	// ProjectTitle 是允许进入最小 Prompt 的安全项目标题。
	ProjectTitle string
	// CreationSpec 是指定 ID 的权威 CreationSpec Draft。
	CreationSpec CreationSpecSnapshot
}

// Draft 是 Business 唯一真相源中的不可变 Storyboard Preview Draft。
type Draft struct {
	// ID 是 Business 分配的 Storyboard Preview 根 UUIDv7。
	ID string
	// ProjectID 是所属 Project 逻辑标识。
	ProjectID string
	// UserID 是创建时冻结的 Project Owner 标识。
	UserID string
	// CreationSpecRef 是生成该 Draft 的精确 CreationSpec 引用。
	CreationSpecRef CreationSpecRef
	// Status 在本 Preview 固定为 draft。
	Status string
	// Version 在本 Preview 固定为一。
	Version int64
	// SchemaVersion 是严格内容 JSON 的版本。
	SchemaVersion string
	// Content 是仅含局部 key 的 Storyboard Preview 内容。
	Content Content
	// ContentDigest 是 Content Canonical JSON 的 SHA-256。
	ContentDigest Digest
	// SourceToolCallID 是来源 Agent Graph Tool Call UUIDv7。
	SourceToolCallID string
	// SourcePromptVersion 是来源 Prompt 冻结版本。
	SourcePromptVersion string
	// SourceValidatorVersion 是来源 Validator 冻结版本。
	SourceValidatorVersion string
	// CreatedAt 是事务冻结的创建时间。
	CreatedAt time.Time
	// UpdatedAt 是最近更新时间；不可变 Draft 中等于 CreatedAt。
	UpdatedAt time.Time
}

// CommandReceipt 冻结首次保存命令的完整语义与安全结果引用。
type CommandReceipt struct {
	// ID 是命令回执 UUIDv7。
	ID string
	// CommandID 是 Agent 提供的 first-write-wins UUIDv7。
	CommandID string
	// RequestDigest 是不含 CommandID 的保存请求 SHA-256。
	RequestDigest Digest
	// UserID 是可信调用用户 UUIDv7。
	UserID string
	// ProjectID 是目标 Project UUIDv7。
	ProjectID string
	// ExpectedProjectVersion 是生成时冻结的 Project 版本。
	ExpectedProjectVersion int64
	// CreationSpecRef 是生成时冻结的 CreationSpec 精确引用。
	CreationSpecRef CreationSpecRef
	// SourceToolCallID 是来源 Tool Call UUIDv7。
	SourceToolCallID string
	// SourcePromptVersion 是命令冻结的 Prompt 版本。
	SourcePromptVersion string
	// SourceValidatorVersion 是命令冻结的 Validator 版本。
	SourceValidatorVersion string
	// StoryboardPreviewID 是首次命令创建的 Draft 根 UUIDv7。
	StoryboardPreviewID string
	// ResultVersion 是首次响应冻结的 Draft 版本。
	ResultVersion int64
	// ResultStatus 是首次响应冻结的 Draft 状态。
	ResultStatus string
	// ResultContentDigest 是首次响应冻结的内容摘要。
	ResultContentDigest Digest
	// CreatedAt 是首次命令提交时间。
	CreatedAt time.Time
}

// SaveAggregate 是 Repository 单事务保存的 Storyboard Draft 与命令回执。
type SaveAggregate struct {
	// Draft 是待创建的不可变 Storyboard Preview Draft。
	Draft Draft
	// Receipt 是与 Draft 同事务提交的命令回执。
	Receipt CommandReceipt
}

// CommandDisposition 表示保存是首次创建还是同语义重放。
type CommandDisposition string

const (
	// CommandDispositionCreated 表示本次命令首次创建 Draft。
	CommandDispositionCreated CommandDisposition = "created"
	// CommandDispositionReplayed 表示本次命令返回首次冻结结果。
	CommandDispositionReplayed CommandDisposition = "replayed"
)

// SaveResult 是保存或重放命令返回的安全结果。
type SaveResult struct {
	// Disposition 区分首次创建与同语义重放。
	Disposition CommandDisposition
	// Draft 是首次命令绑定的权威 Storyboard Draft。
	Draft Draft
}

// QueryStatus 表示幂等命令查询的稳定业务状态。
type QueryStatus string

const (
	// QueryStatusNotFound 表示命令不存在或不属于给定用户与 Project。
	QueryStatusNotFound QueryStatus = "not_found"
	// QueryStatusCompleted 表示原命令已完成且摘要相同。
	QueryStatusCompleted QueryStatus = "completed"
	// QueryStatusConflict 表示 command_id 已绑定到不同摘要。
	QueryStatusConflict QueryStatus = "conflict"
)

// QueryCommand 是查询首次命令结果的完整安全键。
type QueryCommand struct {
	// CommandID 是原保存命令 UUIDv7。
	CommandID string
	// RequestDigest 是原保存命令摘要。
	RequestDigest Digest
	// UserID 是原命令可信用户 UUIDv7。
	UserID string
	// ProjectID 是原命令目标 Project UUIDv7。
	ProjectID string
}

// QueryResult 是幂等命令查询结果；非 completed 状态不得携带 Draft。
type QueryResult struct {
	// Status 是 not_found、completed 或 conflict。
	Status QueryStatus
	// Draft 仅在 completed 时存在。
	Draft *Draft
}

// ContentDigest 计算严格 Content Canonical JSON 的 SHA-256。
func ContentDigest(content Content) (Digest, error) {
	canonical, err := content.CanonicalJSON()
	if err != nil {
		return Digest{}, err
	}
	return sha256.Sum256(canonical), nil
}

// SaveRequestDigest 计算不含 command_id 的冻结保存请求摘要。
func SaveRequestDigest(userID string, projectID string, expectedProjectVersion int64, creationSpecRef CreationSpecRef, toolCallID string, promptVersion string, validatorVersion string, content Content) (Digest, error) {
	if !CanonicalUUIDv7(userID) || !CanonicalUUIDv7(projectID) || expectedProjectVersion < 1 ||
		!ValidateCreationSpecRef(creationSpecRef) || !CanonicalUUIDv7(toolCallID) ||
		!validVersion(promptVersion) || !validVersion(validatorVersion) {
		return Digest{}, ErrInvalidInput
	}
	if err := ValidateContent(content); err != nil {
		return Digest{}, err
	}
	canonical := struct {
		SchemaVersion          string `json:"schema_version"`
		UserID                 string `json:"user_id"`
		ProjectID              string `json:"project_id"`
		ExpectedProjectVersion int64  `json:"expected_project_version"`
		CreationSpecRef        struct {
			ID            string `json:"id"`
			Version       int64  `json:"version"`
			ContentDigest string `json:"content_digest"`
		} `json:"creation_spec_ref"`
		ToolCallID       string  `json:"tool_call_id"`
		PromptVersion    string  `json:"prompt_version"`
		ValidatorVersion string  `json:"validator_version"`
		Content          Content `json:"content"`
	}{
		SchemaVersion: SaveDigestSchemaVersion, UserID: userID, ProjectID: projectID,
		ExpectedProjectVersion: expectedProjectVersion,
		ToolCallID:             toolCallID, PromptVersion: promptVersion, ValidatorVersion: validatorVersion, Content: content,
	}
	canonical.CreationSpecRef.ID = creationSpecRef.ID
	canonical.CreationSpecRef.Version = creationSpecRef.Version
	canonical.CreationSpecRef.ContentDigest = creationSpecRef.ContentDigest.Hex()
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return Digest{}, ErrInvalidInput
	}
	return sha256.Sum256(encoded), nil
}

// ValidateCreationSpecRef 校验精确 CreationSpec 引用的 ID、版本与摘要。
func ValidateCreationSpecRef(reference CreationSpecRef) bool {
	return CanonicalUUIDv7(reference.ID) && reference.Version == creationspec.InitialDraftVersion && reference.ContentDigest != (Digest{})
}

// ValidatePlanningContext 校验 Repository 返回的联合快照没有越过 Owner、Project 和 CreationSpec 不变量。
func ValidatePlanningContext(value PlanningContext) error {
	if !CanonicalUUIDv7(value.ProjectID) || value.ProjectVersion < 1 || !validText(value.ProjectTitle, 1, 160, false) {
		return ErrPersistence
	}
	snapshot := value.CreationSpec
	if !CanonicalUUIDv7(snapshot.ID) || snapshot.ProjectID != value.ProjectID || !CanonicalUUIDv7(snapshot.UserID) ||
		snapshot.Status != creationspec.DraftStatus || snapshot.Version != creationspec.InitialDraftVersion ||
		snapshot.SchemaVersion != creationspec.DraftSchemaVersion {
		return ErrPersistence
	}
	digest, err := creationspec.ContentDigest(snapshot.Content)
	if err != nil {
		return ErrPersistence
	}
	converted, err := DigestFromBytes(digest.Bytes())
	if err != nil || converted != snapshot.ContentDigest {
		return ErrPersistence
	}
	return nil
}

// ValidateDraft 校验 Storyboard Draft 全部固定状态、身份、时间和内容摘要不变量。
func ValidateDraft(draft Draft) error {
	if !CanonicalUUIDv7(draft.ID) || !CanonicalUUIDv7(draft.ProjectID) || !CanonicalUUIDv7(draft.UserID) ||
		!ValidateCreationSpecRef(draft.CreationSpecRef) || draft.Status != DraftStatus ||
		draft.Version != InitialDraftVersion || draft.SchemaVersion != DraftSchemaVersion ||
		!CanonicalUUIDv7(draft.SourceToolCallID) || !validVersion(draft.SourcePromptVersion) ||
		!validVersion(draft.SourceValidatorVersion) || draft.CreatedAt.IsZero() || !draft.CreatedAt.Equal(draft.UpdatedAt) {
		return ErrInvalidInput
	}
	digest, err := ContentDigest(draft.Content)
	if err != nil || digest != draft.ContentDigest {
		return ErrInvalidInput
	}
	return nil
}

// ValidateAggregate 校验 Draft 与首次命令回执之间的跨记录一致性。
func ValidateAggregate(aggregate SaveAggregate) error {
	if err := ValidateDraft(aggregate.Draft); err != nil {
		return err
	}
	receipt := aggregate.Receipt
	if !CanonicalUUIDv7(receipt.ID) || !CanonicalUUIDv7(receipt.CommandID) ||
		receipt.UserID != aggregate.Draft.UserID || receipt.ProjectID != aggregate.Draft.ProjectID ||
		receipt.ExpectedProjectVersion < 1 || receipt.CreationSpecRef != aggregate.Draft.CreationSpecRef ||
		receipt.SourceToolCallID != aggregate.Draft.SourceToolCallID ||
		receipt.SourcePromptVersion != aggregate.Draft.SourcePromptVersion ||
		receipt.SourceValidatorVersion != aggregate.Draft.SourceValidatorVersion ||
		receipt.StoryboardPreviewID != aggregate.Draft.ID || receipt.ResultVersion != aggregate.Draft.Version ||
		receipt.ResultStatus != aggregate.Draft.Status || receipt.ResultContentDigest != aggregate.Draft.ContentDigest ||
		!receipt.CreatedAt.Equal(aggregate.Draft.CreatedAt) {
		return ErrInvalidInput
	}
	requestDigest, err := SaveRequestDigest(
		receipt.UserID, receipt.ProjectID, receipt.ExpectedProjectVersion, receipt.CreationSpecRef,
		receipt.SourceToolCallID, receipt.SourcePromptVersion, receipt.SourceValidatorVersion, aggregate.Draft.Content,
	)
	if err != nil || requestDigest != receipt.RequestDigest {
		return ErrInvalidInput
	}
	return nil
}

// CanonicalUUIDv7 校验标识使用 UUIDv7 小写连字符唯一形式。
func CanonicalUUIDv7(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7 && id.String() == value
}

// validVersion 校验 Prompt 和 Validator 版本是有界 NFC 文本。
func validVersion(value string) bool { return validText(value, 1, 64, false) }

// IsStableError 判断错误是否属于允许映射到 RPC 的稳定领域分类。
func IsStableError(err error) bool {
	return errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrProjectVersionConflict) || errors.Is(err, ErrCreationSpecVersionConflict) ||
		errors.Is(err, ErrIdempotencyConflict) || errors.Is(err, ErrPersistence) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// CorruptPersistenceError 把 Repository 读回的不变量错误收敛为稳定持久化错误。
func CorruptPersistenceError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w", ErrPersistence)
}
