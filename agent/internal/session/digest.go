package session

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

// canonicalRequestV1 固定跨 Module 请求摘要字段及 JSON 顺序。
// RequestedAt、RequestID、TraceID 和 CommandID 都不属于命令业务语义，不进入摘要。
type canonicalRequestV1 struct {
	// SchemaVersion 是冻结 Session Bootstrap 契约版本。
	SchemaVersion string `json:"schema_version"`
	// ProjectID 是 Business Project UUIDv7。
	ProjectID string `json:"project_id"`
	// OwnerUserID 是 Business 可信 Project Owner UUIDv7。
	OwnerUserID string `json:"owner_user_id"`
	// CreationSource 是稳定业务创建来源。
	CreationSource string `json:"creation_source"`
	// PromptPresent 表示规范化后是否存在首 Prompt。
	PromptPresent bool `json:"prompt_present"`
	// PromptDigest 是可选规范化 Prompt 摘要。
	PromptDigest string `json:"prompt_digest"`
	// SkillSnapshotMode 是 Session 创建时冻结的 Skill 快照模式。
	SkillSnapshotMode SkillSnapshotKind `json:"skill_snapshot_mode"`
}

// canonicalCommand 是校验和规范化后的内部命令，不跨协议边界暴露。
type canonicalCommand struct {
	// requestID 是规范化的本次跨服务调用 UUIDv7，仅用于关联和预检，不进入语义摘要。
	requestID string
	// commandID 是规范化 Business Command UUIDv7。
	commandID string
	// projectID 是规范化 Business Project UUIDv7。
	projectID string
	// ownerUserID 是规范化 Business Project Owner UUIDv7。
	ownerUserID string
	// normalizedPrompt 是 NFC 规范化正文；空 Prompt 时为空。
	normalizedPrompt string
	// promptPresent 表示规范化后是否存在首 Prompt。
	promptPresent bool
	// promptDigest 是规范化 Prompt 摘要。
	promptDigest string
	// requestDigest 是冻结 Canonical Schema 摘要。
	requestDigest string
}

// CalculateRequestDigest 按 ensure_project_session.v1 Canonical Schema 计算跨 Module 语义摘要。
// Prompt 先执行 UTF-8 校验与 NFC 规范化；仅当全文均为 Unicode 空白时折叠为缺失，非空正文不裁剪首尾空白。
func CalculateRequestDigest(projectID, ownerUserID, initialPrompt string, mode SkillSnapshotKind) (requestDigest, promptDigest string, promptPresent bool, err error) {
	normalizedProjectID, err := normalizeUUIDv7(projectID)
	if err != nil {
		return "", "", false, fmt.Errorf("%w: project_id: %v", ErrInvalidCommand, err)
	}
	normalizedOwnerUserID, err := normalizeUUIDv7(ownerUserID)
	if err != nil {
		return "", "", false, fmt.Errorf("%w: owner_user_id: %v", ErrInvalidCommand, err)
	}
	if mode != SkillSnapshotKindEmpty {
		return "", "", false, fmt.Errorf("%w: skill_snapshot_mode must be empty", ErrInvalidCommand)
	}
	normalizedPrompt, promptPresent, err := normalizePrompt(initialPrompt)
	if err != nil {
		return "", "", false, err
	}
	if promptPresent {
		promptDigest = sha256Hex([]byte(normalizedPrompt))
	}
	canonical := canonicalRequestV1{
		SchemaVersion:     EnsureCommandSchemaVersionV1,
		ProjectID:         normalizedProjectID,
		OwnerUserID:       normalizedOwnerUserID,
		CreationSource:    CreationSourceQuickCreate,
		PromptPresent:     promptPresent,
		PromptDigest:      promptDigest,
		SkillSnapshotMode: mode,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", "", false, fmt.Errorf("%w: encode canonical request: %v", ErrInvalidCommand, err)
	}
	return sha256Hex(encoded), promptDigest, promptPresent, nil
}

// canonicalizeCommand 验证调用方摘要并生成仅供 Service 使用的规范化命令。
// Agent 必须独立重算摘要，避免调用方通过伪造 Digest 让同一 CommandID 覆盖另一业务语义。
func canonicalizeCommand(command EnsureCommand) (canonicalCommand, error) {
	if command.SchemaVersion != EnsureCommandSchemaVersionV1 {
		return canonicalCommand{}, fmt.Errorf("%w: unsupported schema_version", ErrInvalidCommand)
	}
	requestID, err := normalizeUUIDv7(command.RequestID)
	if err != nil {
		return canonicalCommand{}, fmt.Errorf("%w: request_id: %v", ErrInvalidCommand, err)
	}
	commandID, err := normalizeUUIDv7(command.CommandID)
	if err != nil {
		return canonicalCommand{}, fmt.Errorf("%w: command_id: %v", ErrInvalidCommand, err)
	}
	projectID, err := normalizeUUIDv7(command.ProjectID)
	if err != nil {
		return canonicalCommand{}, fmt.Errorf("%w: project_id: %v", ErrInvalidCommand, err)
	}
	ownerUserID, err := normalizeUUIDv7(command.OwnerUserID)
	if err != nil {
		return canonicalCommand{}, fmt.Errorf("%w: owner_user_id: %v", ErrInvalidCommand, err)
	}
	if command.CreationSource != CreationSourceQuickCreate {
		return canonicalCommand{}, fmt.Errorf("%w: creation_source must be quick_create", ErrInvalidCommand)
	}
	if command.RequestedAt.IsZero() {
		return canonicalCommand{}, fmt.Errorf("%w: requested_at is required", ErrInvalidCommand)
	}
	requestDigest, promptDigest, promptPresent, err := CalculateRequestDigest(projectID, ownerUserID, command.InitialPrompt, command.SkillSnapshotMode)
	if err != nil {
		return canonicalCommand{}, err
	}
	if !validSHA256Hex(command.RequestDigest) || !sameDigest(command.RequestDigest, requestDigest) {
		return canonicalCommand{}, fmt.Errorf("%w: request_digest mismatch", ErrInvalidCommand)
	}
	if promptPresent {
		if !validSHA256Hex(command.PromptDigest) || !sameDigest(command.PromptDigest, promptDigest) {
			return canonicalCommand{}, fmt.Errorf("%w: prompt_digest mismatch", ErrInvalidCommand)
		}
	} else if command.PromptDigest != "" {
		return canonicalCommand{}, fmt.Errorf("%w: prompt_digest must be empty for blank prompt", ErrInvalidCommand)
	}
	normalizedPrompt, _, err := normalizePrompt(command.InitialPrompt)
	if err != nil {
		return canonicalCommand{}, err
	}
	return canonicalCommand{
		requestID:        requestID,
		commandID:        commandID,
		projectID:        projectID,
		ownerUserID:      ownerUserID,
		normalizedPrompt: normalizedPrompt,
		promptPresent:    promptPresent,
		promptDigest:     promptDigest,
		requestDigest:    requestDigest,
	}, nil
}

// normalizePrompt 校验 UTF-8 与大小边界，执行 NFC 规范化，并确定是否为纯 Unicode 空白。
// 非空正文不裁剪空白，避免用户有意输入的格式在 Business 与 Agent 间发生语义漂移。
func normalizePrompt(prompt string) (string, bool, error) {
	if !utf8.ValidString(prompt) {
		return "", false, fmt.Errorf("%w: initial_prompt must be valid UTF-8", ErrInvalidCommand)
	}
	normalized := norm.NFC.String(prompt)
	if len(normalized) > MaxInitialPromptBytes {
		return "", false, fmt.Errorf("%w: initial_prompt exceeds %d bytes", ErrInvalidCommand, MaxInitialPromptBytes)
	}
	if strings.TrimFunc(normalized, unicode.IsSpace) == "" {
		return "", false, nil
	}
	return normalized, true, nil
}

// normalizeUUIDv7 校验应用标识使用 RFC 9562 UUIDv7，并返回小写规范字符串。
func normalizeUUIDv7(value string) (string, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parse UUID: %w", err)
	}
	if id.Version() != 7 {
		return "", fmt.Errorf("UUID version is %d, want 7", id.Version())
	}
	return id.String(), nil
}

// sha256Hex 返回内容的 SHA-256 小写十六进制摘要。
func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

// validSHA256Hex 校验摘要必须是 64 位小写十六进制，避免大小写和编码差异形成第二语义。
func validSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// sameDigest 使用常量时间比较已经通过格式检查的摘要。
func sameDigest(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
