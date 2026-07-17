// Package creationspec 定义 Business-owned CreationSpec Preview 草稿及保存命令的领域边界。
package creationspec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

const (
	// DraftSchemaVersion 是 Business 草稿内容当前唯一允许的稳定版本。
	DraftSchemaVersion = "creation_spec.draft.v1"
	// SaveDigestSchemaVersion 是 Agent 与 Business 计算保存请求摘要时使用的冻结版本。
	SaveDigestSchemaVersion = "creation_spec.preview.save-draft.digest.v1"
	// DraftStatus 是 Preview 阶段唯一允许持久化的状态。
	DraftStatus = "draft"
	// InitialDraftVersion 是首次创建草稿的冻结版本。
	InitialDraftVersion int64 = 1

	maxCanonicalContentBytes = 64 * 1024
)

var (
	// ErrInvalidInput 表示 RPC DTO 或领域聚合违反冻结的 ID、枚举、长度、数组或摘要约束。
	ErrInvalidInput = errors.New("invalid creation spec preview input")
	// ErrNotFound 表示 Project 不存在、不可见或当前状态不可用于 CreationSpec Preview。
	ErrNotFound = errors.New("creation spec preview project not found")
	// ErrVersionConflict 表示命令携带的 Project 版本已过期。
	ErrVersionConflict = errors.New("creation spec preview project version conflict")
	// ErrIdempotencyConflict 表示同一 command_id 已经绑定到不同请求摘要。
	ErrIdempotencyConflict = errors.New("creation spec preview command conflict")
	// ErrPersistence 表示持久化暂不可用且不得向边界外泄漏 SQL 或连接细节。
	ErrPersistence = errors.New("creation spec preview persistence unavailable")
)

// DeliverableType 是 CreationSpec 交付物类型的稳定小写代码。
type DeliverableType string

const (
	// DeliverableTypeVideo 表示视频交付物。
	DeliverableTypeVideo DeliverableType = "video"
	// DeliverableTypeImageSet 表示图片集合交付物。
	DeliverableTypeImageSet DeliverableType = "image_set"
	// DeliverableTypeAudio 表示音频交付物。
	DeliverableTypeAudio DeliverableType = "audio"
	// DeliverableTypeMixed 表示混合媒体交付物。
	DeliverableTypeMixed DeliverableType = "mixed"
)

// CommandDisposition 表示保存命令是首次创建还是同语义重放。
type CommandDisposition string

const (
	// CommandDispositionCreated 表示本次命令首次创建草稿。
	CommandDispositionCreated CommandDisposition = "created"
	// CommandDispositionReplayed 表示本次命令返回首次写入的冻结结果。
	CommandDispositionReplayed CommandDisposition = "replayed"
)

// QueryStatus 表示幂等命令查询的稳定业务状态。
type QueryStatus string

const (
	// QueryStatusNotFound 表示命令不存在或不属于给定用户与 Project。
	QueryStatusNotFound QueryStatus = "not_found"
	// QueryStatusCompleted 表示同 command_id、digest 的命令已经完成。
	QueryStatusCompleted QueryStatus = "completed"
	// QueryStatusConflict 表示 command_id 已绑定到不同 digest。
	QueryStatusConflict QueryStatus = "conflict"
)

// Digest 是长度固定的 SHA-256 摘要，避免在领域内传递可变长字符串。
type Digest [sha256.Size]byte

// Hex 返回小写、无前缀的 64 位十六进制编码。
func (digest Digest) Hex() string { return hex.EncodeToString(digest[:]) }

// Bytes 返回摘要副本，防止 Repository 修改领域值。
func (digest Digest) Bytes() []byte { return append([]byte(nil), digest[:]...) }

// ParseDigest 严格解析小写、无前缀的 64 位 SHA-256 十六进制编码。
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

// DigestFromBytes 严格复制数据库中的 32 字节摘要。
func DigestFromBytes(value []byte) (Digest, error) {
	var digest Digest
	if len(value) != sha256.Size {
		return digest, ErrPersistence
	}
	copy(digest[:], value)
	return digest, nil
}

// Phase 是 CreationSpec 内一个稳定顺序的执行阶段。
type Phase struct {
	// Key 是 phase_1 至 phase_6 范围内的稳定键。
	Key string `json:"key"`
	// Title 是阶段标题。
	Title string `json:"title"`
	// Objective 是阶段目标。
	Objective string `json:"objective"`
	// Output 是阶段预期产物。
	Output string `json:"output"`
}

// Content 是通过严格 Validator 后允许持久化的 CreationSpec 内容。
// 字段顺序同时是冻结的 encoding/json Canonical 顺序，禁止无版本调整。
type Content struct {
	// Title 是 CreationSpec 标题。
	Title string `json:"title"`
	// Goal 是整体创作目标。
	Goal string `json:"goal"`
	// DeliverableType 是稳定交付物类型。
	DeliverableType DeliverableType `json:"deliverable_type"`
	// Audience 是可为空的目标受众说明。
	Audience string `json:"audience"`
	// Locale 是当前受支持的 BCP 47 子集。
	Locale string `json:"locale"`
	// Phases 是一至六个按原顺序保存的阶段。
	Phases []Phase `json:"phases"`
	// Constraints 是零至八条按原顺序保存的约束。
	Constraints []string `json:"constraints"`
	// AcceptanceCriteria 是一至八条按原顺序保存的验收标准。
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

// ProjectContext 是生成前允许 Agent 获取的最小 Business Project 上下文。
type ProjectContext struct {
	// ProjectID 是经过所有权校验的 Project UUIDv7。
	ProjectID string
	// Version 是 Agent 保存草稿时必须原样回传的 Project 乐观版本。
	Version int64
	// Title 是可用于生成建议的安全项目标题。
	Title string
}

// ValidateProjectContext 校验 Repository 返回的最小 Project 上下文仍满足 Preview 读取边界。
// Project 必须属于调用方已校验的 UUIDv7 空间、版本可用于乐观并发，且标题是可安全进入 Prompt 的 NFC 文本。
func ValidateProjectContext(project ProjectContext) error {
	if !CanonicalUUIDv7(project.ProjectID) || project.Version < 1 || !validText(project.Title, 1, 160, false) {
		return ErrPersistence
	}
	return nil
}

// Draft 是 Business 唯一真相源中的 CreationSpec Draft。
type Draft struct {
	// ID 是应用侧生成的 UUIDv7。
	ID string
	// ProjectID 是所属 Project 逻辑标识。
	ProjectID string
	// UserID 是创建时冻结的所有者逻辑标识。
	UserID string
	// Status 在 V1 Preview 中固定为 draft。
	Status string
	// Version 首次草稿固定为 1。
	Version int64
	// SchemaVersion 是草稿内容契约版本。
	SchemaVersion string
	// Content 是严格校验后的结构化内容。
	Content Content
	// ContentDigest 是 Content Canonical JSON 的 SHA-256。
	ContentDigest Digest
	// SourceToolCallID 是来源 Graph Tool 调用 UUIDv7。
	SourceToolCallID string
	// SourcePromptVersion 是来源 Prompt 版本。
	SourcePromptVersion string
	// SourceValidatorVersion 是来源 Validator 版本。
	SourceValidatorVersion string
	// CreatedAt 是事务冻结的创建时间。
	CreatedAt time.Time
	// UpdatedAt 是事务冻结的最近更新时间。
	UpdatedAt time.Time
}

// CommandReceipt 冻结首次保存命令的输入摘要与安全结果引用。
type CommandReceipt struct {
	// ID 是回执 UUIDv7。
	ID string
	// CommandID 是 Agent 提供的幂等命令 UUIDv7。
	CommandID string
	// RequestDigest 是完整保存请求 Canonical 的 SHA-256。
	RequestDigest Digest
	// UserID 是命令中的可信用户标识。
	UserID string
	// ProjectID 是命令中的 Project 标识。
	ProjectID string
	// ExpectedProjectVersion 是命令冻结的 Project 版本。
	ExpectedProjectVersion int64
	// SourceToolCallID 是命令来源 Tool Call。
	SourceToolCallID string
	// SourcePromptVersion 是命令来源 Prompt 版本。
	SourcePromptVersion string
	// SourceValidatorVersion 是命令来源 Validator 版本。
	SourceValidatorVersion string
	// CreationSpecID 是首次创建的草稿标识。
	CreationSpecID string
	// ResultVersion 是首次安全响应冻结的草稿版本。
	ResultVersion int64
	// ResultStatus 是首次安全响应冻结的草稿状态。
	ResultStatus string
	// ResultContentDigest 是首次安全响应冻结的内容摘要。
	ResultContentDigest Digest
	// CreatedAt 是首次命令提交时间。
	CreatedAt time.Time
}

// SaveAggregate 是 Repository 单事务保存的草稿与命令回执。
type SaveAggregate struct {
	// Draft 是待创建的 CreationSpec 草稿。
	Draft Draft
	// Receipt 是与草稿同事务创建的首次命令回执。
	Receipt CommandReceipt
}

// SaveResult 是保存或重放命令返回的安全结果。
type SaveResult struct {
	// Disposition 区分首次创建与同语义重放。
	Disposition CommandDisposition
	// Draft 是首次命令绑定的草稿。
	Draft Draft
}

// QueryResult 是幂等命令查询结果；非 completed 状态不携带 Draft。
type QueryResult struct {
	// Status 是 not_found、completed 或 conflict。
	Status QueryStatus
	// Draft 仅在 completed 时存在。
	Draft *Draft
}

// CanonicalJSON 校验 Content 并按冻结字段顺序生成紧凑 JSON。
func (content Content) CanonicalJSON() ([]byte, error) {
	if err := ValidateContent(content); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(content)
	if err != nil || len(encoded) > maxCanonicalContentBytes {
		return nil, ErrInvalidInput
	}
	return encoded, nil
}

// ContentDigest 校验并计算 Content Canonical JSON 的 SHA-256 摘要。
func ContentDigest(content Content) (Digest, error) {
	canonical, err := content.CanonicalJSON()
	if err != nil {
		return Digest{}, err
	}
	return sha256.Sum256(canonical), nil
}

// ParseContentJSON 从 PostgreSQL jsonb 文本严格恢复 Content，并重新执行全部领域校验。
func ParseContentJSON(encoded []byte) (Content, error) {
	if len(encoded) == 0 || len(encoded) > maxCanonicalContentBytes*2 || !utf8.Valid(encoded) {
		return Content{}, ErrPersistence
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var content Content
	if err := decoder.Decode(&content); err != nil {
		return Content{}, ErrPersistence
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Content{}, ErrPersistence
	}
	if err := ValidateContent(content); err != nil {
		return Content{}, ErrPersistence
	}
	return content, nil
}

// ValidateContent 执行冻结的枚举、Unicode、长度、数组数量和唯一性校验。
func ValidateContent(content Content) error {
	if !validText(content.Title, 1, 80, false) || !validText(content.Goal, 1, 2000, false) ||
		!validText(content.Audience, 0, 500, true) {
		return ErrInvalidInput
	}
	switch content.DeliverableType {
	case DeliverableTypeVideo, DeliverableTypeImageSet, DeliverableTypeAudio, DeliverableTypeMixed:
	default:
		return ErrInvalidInput
	}
	if content.Locale != "zh-CN" && content.Locale != "en-US" {
		return ErrInvalidInput
	}
	if content.Phases == nil || len(content.Phases) < 1 || len(content.Phases) > 6 ||
		content.Constraints == nil || len(content.Constraints) > 8 ||
		content.AcceptanceCriteria == nil || len(content.AcceptanceCriteria) < 1 || len(content.AcceptanceCriteria) > 8 {
		return ErrInvalidInput
	}
	phaseKeys := make(map[string]struct{}, len(content.Phases))
	for _, phase := range content.Phases {
		if !validPhaseKey(phase.Key) || !validText(phase.Title, 1, 80, false) ||
			!validText(phase.Objective, 1, 500, false) || !validText(phase.Output, 1, 500, false) {
			return ErrInvalidInput
		}
		if _, exists := phaseKeys[phase.Key]; exists {
			return ErrInvalidInput
		}
		phaseKeys[phase.Key] = struct{}{}
	}
	if !validUniqueTextList(content.Constraints, 1, 200) ||
		!validUniqueTextList(content.AcceptanceCriteria, 1, 240) {
		return ErrInvalidInput
	}
	return nil
}

// SaveRequestDigest 计算不含 command_id 的冻结八字段保存请求摘要。
func SaveRequestDigest(userID string, projectID string, expectedProjectVersion int64, toolCallID string, promptVersion string, validatorVersion string, content Content) (Digest, error) {
	if !CanonicalUUIDv7(userID) || !CanonicalUUIDv7(projectID) || expectedProjectVersion < 1 ||
		!CanonicalUUIDv7(toolCallID) || !validVersion(promptVersion) || !validVersion(validatorVersion) {
		return Digest{}, ErrInvalidInput
	}
	if err := ValidateContent(content); err != nil {
		return Digest{}, err
	}
	canonical := struct {
		SchemaVersion          string  `json:"schema_version"`
		UserID                 string  `json:"user_id"`
		ProjectID              string  `json:"project_id"`
		ExpectedProjectVersion int64   `json:"expected_project_version"`
		ToolCallID             string  `json:"tool_call_id"`
		PromptVersion          string  `json:"prompt_version"`
		ValidatorVersion       string  `json:"validator_version"`
		Content                Content `json:"content"`
	}{
		SchemaVersion: SaveDigestSchemaVersion, UserID: userID, ProjectID: projectID,
		ExpectedProjectVersion: expectedProjectVersion, ToolCallID: toolCallID,
		PromptVersion: promptVersion, ValidatorVersion: validatorVersion, Content: content,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return Digest{}, ErrInvalidInput
	}
	return sha256.Sum256(encoded), nil
}

// CanonicalUUIDv7 校验标识使用 UUIDv7 小写连字符唯一形式。
func CanonicalUUIDv7(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7 && id.String() == value
}

// ValidateDraft 校验 Repository 边界收到或读出的 Draft 全部不变量。
func ValidateDraft(draft Draft) error {
	if !CanonicalUUIDv7(draft.ID) || !CanonicalUUIDv7(draft.ProjectID) || !CanonicalUUIDv7(draft.UserID) ||
		draft.Status != DraftStatus || draft.Version != InitialDraftVersion || draft.SchemaVersion != DraftSchemaVersion ||
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

// ValidateAggregate 校验草稿与首次回执之间的跨记录一致性。
func ValidateAggregate(aggregate SaveAggregate) error {
	if err := ValidateDraft(aggregate.Draft); err != nil {
		return err
	}
	receipt := aggregate.Receipt
	if !CanonicalUUIDv7(receipt.ID) || !CanonicalUUIDv7(receipt.CommandID) || receipt.UserID != aggregate.Draft.UserID ||
		receipt.ProjectID != aggregate.Draft.ProjectID || receipt.ExpectedProjectVersion < 1 ||
		receipt.SourceToolCallID != aggregate.Draft.SourceToolCallID || receipt.SourcePromptVersion != aggregate.Draft.SourcePromptVersion ||
		receipt.SourceValidatorVersion != aggregate.Draft.SourceValidatorVersion || receipt.CreationSpecID != aggregate.Draft.ID ||
		receipt.ResultVersion != aggregate.Draft.Version || receipt.ResultStatus != aggregate.Draft.Status ||
		receipt.ResultContentDigest != aggregate.Draft.ContentDigest || !receipt.CreatedAt.Equal(aggregate.Draft.CreatedAt) {
		return ErrInvalidInput
	}
	requestDigest, err := SaveRequestDigest(
		receipt.UserID,
		receipt.ProjectID,
		receipt.ExpectedProjectVersion,
		receipt.SourceToolCallID,
		receipt.SourcePromptVersion,
		receipt.SourceValidatorVersion,
		aggregate.Draft.Content,
	)
	if err != nil || requestDigest != receipt.RequestDigest {
		return ErrInvalidInput
	}
	return nil
}

// ValidateSaveResult 校验 Repository 保存或重放结果没有越过原命令的可信身份与内容边界。
func ValidateSaveResult(result SaveResult, command SaveCommand) error {
	if result.Disposition != CommandDispositionCreated && result.Disposition != CommandDispositionReplayed {
		return ErrPersistence
	}
	if err := ValidateDraft(result.Draft); err != nil || result.Draft.UserID != command.UserID ||
		result.Draft.ProjectID != command.ProjectID || result.Draft.SourceToolCallID != command.ToolCallID ||
		result.Draft.SourcePromptVersion != command.PromptVersion || result.Draft.SourceValidatorVersion != command.ValidatorVersion {
		return ErrPersistence
	}
	providedDigest, err := ParseDigest(command.RequestDigestHex)
	if err != nil {
		return ErrPersistence
	}
	calculatedDigest, err := SaveRequestDigest(
		result.Draft.UserID,
		result.Draft.ProjectID,
		command.ExpectedProjectVersion,
		result.Draft.SourceToolCallID,
		result.Draft.SourcePromptVersion,
		result.Draft.SourceValidatorVersion,
		result.Draft.Content,
	)
	if err != nil || providedDigest != calculatedDigest {
		return ErrPersistence
	}
	return nil
}

func validPhaseKey(value string) bool {
	return len(value) == len("phase_1") && strings.HasPrefix(value, "phase_") && value[len(value)-1] >= '1' && value[len(value)-1] <= '6'
}

func validVersion(value string) bool { return validText(value, 1, 64, false) }

func validUniqueTextList(values []string, minimumRunes int, maximumRunes int) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validText(value, minimumRunes, maximumRunes, false) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validText(value string, minimumRunes int, maximumRunes int, allowEmpty bool) bool {
	if !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) || strings.TrimSpace(value) != value {
		return false
	}
	count := utf8.RuneCountInString(value)
	if count == 0 && allowEmpty {
		return true
	}
	if count < minimumRunes || count > maximumRunes {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || character == '\u2028' || character == '\u2029' {
			return false
		}
	}
	return true
}

// CorruptPersistenceError 将 Repository 读回的不变量错误收敛为稳定持久化错误。
func CorruptPersistenceError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w", ErrPersistence)
}
