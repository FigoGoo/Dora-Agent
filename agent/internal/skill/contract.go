// Package skill 实现 Agent Session 创建阶段的 Skill Runtime Content、Snapshot Set 与摘要契约。
// 本包只处理强类型规范化、校验和 Canonical 编码，不读取 Business 当前状态，也不注册可执行 Tool。
package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	// SnapshotSchemaVersionV1 是 Session Skill Snapshot 的冻结结构版本。
	SnapshotSchemaVersionV1 = "session_skill_snapshot.v1"
	// RuntimeContentSchemaVersionV1 是可内联 Skill Runtime Content 的冻结结构版本。
	RuntimeContentSchemaVersionV1 = "skill_runtime_content.v1"
	// EnsureProjectSessionSchemaVersionV2 是携带 Skill Snapshot 的 Session 创建语义版本。
	EnsureProjectSessionSchemaVersionV2 = "ensure_project_session.v2"
	// DefinitionSchemaVersionV1 是上游完整 Published Skill Definition 的冻结结构版本。
	DefinitionSchemaVersionV1 = "skill_definition.v1"
	// CreationSourceQuickCreate 是 W1 唯一允许的 Session 创建来源。
	CreationSourceQuickCreate = "quick_create"
	// RuntimePolicyRefV1 冻结 inline-only、不得扩权和不得注册 Tool 的 Runtime Policy。
	RuntimePolicyRefV1 = "skill-runtime-policy:v1"
	// EmptySnapshotSetDigestHex 是空 metadata 数组 `[]` 的固定 SHA-256。
	EmptySnapshotSetDigestHex = "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945"
)

var (
	// ErrInvalidContract 表示输入违反版本、枚举、排序、字段或集合不变量。
	ErrInvalidContract = errors.New("invalid session skill snapshot contract")
	// ErrDigestMismatch 表示调用方摘要与 Agent 独立重算结果不一致。
	ErrDigestMismatch = errors.New("session skill snapshot digest mismatch")
	// ErrLimitExceeded 表示输入超过有效 limits profile，且不得通过截断继续处理。
	ErrLimitExceeded = errors.New("session skill snapshot limit exceeded")
)

// Digest 是固定 32 字节 SHA-256；数组值可直接比较，跨协议时使用 Hex。
type Digest [sha256.Size]byte

// Hex 返回 64 位小写十六进制摘要。
func (digest Digest) Hex() string { return hex.EncodeToString(digest[:]) }

// ParseDigest 严格解析 64 位小写 SHA-256；大写、前缀和非十六进制输入失败关闭。
func ParseDigest(value string) (Digest, error) {
	var result Digest
	if len(value) != sha256.Size*2 {
		return result, fmt.Errorf("%w: digest length", ErrInvalidContract)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return result, fmt.Errorf("%w: digest encoding", ErrInvalidContract)
	}
	copy(result[:], decoded)
	return result, nil
}

// SessionSkillSnapshotKindV1 表示 Session 冻结的是显式空集合或不可变 Published 引用集合。
type SessionSkillSnapshotKindV1 string

const (
	// SessionSkillSnapshotKindEmptyV1 表示显式空 Skill 集合。
	SessionSkillSnapshotKindEmptyV1 SessionSkillSnapshotKindV1 = "empty"
	// SessionSkillSnapshotKindPublishedRefsV1 表示非空 Published Skill 引用集合。
	SessionSkillSnapshotKindPublishedRefsV1 SessionSkillSnapshotKindV1 = "published_refs"
)

// SkillNamespaceV1 表示 Business 冻结的 Skill 命名空间。
type SkillNamespaceV1 string

const (
	// SkillNamespaceSystemV1 表示系统 Skill；W1 Business Producer 尚不得发送。
	SkillNamespaceSystemV1 SkillNamespaceV1 = "system"
	// SkillNamespaceUserV1 表示用户 Skill。
	SkillNamespaceUserV1 SkillNamespaceV1 = "user"
)

// SkillGuidanceApplicabilityV1 表示一个固定能力指导是否适用。
type SkillGuidanceApplicabilityV1 string

const (
	// SkillGuidanceEnabledV1 表示能力适用，必须提供 guidance。
	SkillGuidanceEnabledV1 SkillGuidanceApplicabilityV1 = "enabled"
	// SkillGuidanceNotApplicableV1 表示能力不适用，必须提供原因。
	SkillGuidanceNotApplicableV1 SkillGuidanceApplicabilityV1 = "not_applicable"
)

// CapabilityGuidanceV1 保存一个固定 Graph Tool 能力的互斥指导字段。
type CapabilityGuidanceV1 struct {
	// Applicability 只允许 enabled 或 not_applicable。
	Applicability SkillGuidanceApplicabilityV1 `json:"applicability"`
	// Guidance 是 enabled 时必填的内联指导正文。
	Guidance string `json:"guidance"`
	// NotApplicableReason 是 not_applicable 时必填的原因。
	NotApplicableReason string `json:"not_applicable_reason"`
}

// SkillExampleV1 保存一组已规范排序的示例输入与输出。
type SkillExampleV1 struct {
	// Input 是示例输入正文。
	Input string `json:"input"`
	// Output 是示例期望输出正文。
	Output string `json:"output"`
}

// SkillRuntimeContentV1 是允许 Agent 内联加载的最小发布内容。
type SkillRuntimeContentV1 struct {
	// SchemaVersion 固定为 skill_runtime_content.v1。
	SchemaVersion string `json:"schema_version"`
	// Name 来自 Published Definition name。
	Name string `json:"name"`
	// InputDescription 描述 Skill 接受的输入。
	InputDescription string `json:"input_description"`
	// OutputDescription 描述 Skill 产生的输出。
	OutputDescription string `json:"output_description"`
	// InvocationRules 描述多 Skill 场景下的选择规则，不承载权限或预算。
	InvocationRules string `json:"invocation_rules"`
	// PlanCreationSpec 是创作规格规划能力指导。
	PlanCreationSpec CapabilityGuidanceV1 `json:"plan_creation_spec"`
	// AnalyzeMaterials 是素材分析能力指导。
	AnalyzeMaterials CapabilityGuidanceV1 `json:"analyze_materials"`
	// PlanStoryboard 是故事板规划能力指导。
	PlanStoryboard CapabilityGuidanceV1 `json:"plan_storyboard"`
	// GenerateMedia 是媒体生成能力指导。
	GenerateMedia CapabilityGuidanceV1 `json:"generate_media"`
	// WritePrompts 是提示词编写能力指导。
	WritePrompts CapabilityGuidanceV1 `json:"write_prompts"`
	// AssembleOutput 是成片组装能力指导。
	AssembleOutput CapabilityGuidanceV1 `json:"assemble_output"`
	// Examples 必须为非 nil、无重复且按 input/output 升序的数组。
	Examples []SkillExampleV1 `json:"examples"`
	// StarterPrompts 必须为非 nil、无重复且按 UTF-8 字节升序的数组。
	StarterPrompts []string `json:"starter_prompts"`
}

// PublicToolSnapshotRefV1 是未来公共 Tool 契约的强类型占位；W1 列表必须为空。
type PublicToolSnapshotRefV1 struct {
	// RefID 是未来契约定义的稳定引用标识。
	RefID string `json:"ref_id"`
	// RefDigest 是未来契约定义的引用摘要。
	RefDigest string `json:"ref_digest"`
}

// PublishedSkillSnapshotRefV1 冻结一个 Published Skill 的 Runtime 内容和安全 metadata。
type PublishedSkillSnapshotRefV1 struct {
	// LoadOrder 是 Business 决定的稠密加载序号，从 1 开始。
	LoadOrder int32 `json:"load_order"`
	// Priority 是 Business 冻结的非负优先级。
	Priority int32 `json:"priority"`
	// Namespace 是冻结的 Skill 命名空间。
	Namespace SkillNamespaceV1 `json:"namespace"`
	// SkillID 是 Business Skill UUIDv7。
	SkillID string `json:"skill_id"`
	// PublisherUserID 是发布者 UUIDv7。
	PublisherUserID string `json:"publisher_user_id"`
	// PublishedSnapshotID 是不可变发布快照 UUIDv7。
	PublishedSnapshotID string `json:"published_snapshot_id"`
	// PublicationRevision 是大于零的发布修订号。
	PublicationRevision int64 `json:"publication_revision"`
	// DefinitionSchemaVersion 是完整 Published Definition 版本。
	DefinitionSchemaVersion string `json:"definition_schema_version"`
	// ContentDigest 是 Business-owned 完整 Definition 摘要。
	ContentDigest string `json:"content_digest"`
	// RuntimeContentSchemaVersion 是 Runtime 子集版本。
	RuntimeContentSchemaVersion string `json:"runtime_content_schema_version"`
	// RuntimeContentDigest 是 Agent 必须独立重算的 Runtime Content 摘要。
	RuntimeContentDigest string `json:"runtime_content_digest"`
	// RuntimeContent 是待加密持久化的 Runtime 明文 DTO，不得进入日志或 Event。
	RuntimeContent SkillRuntimeContentV1 `json:"-"`
	// AllowedGraphToolKeys 只是能力声明，不能作为 Tool 可执行证明。
	AllowedGraphToolKeys []string `json:"allowed_graph_tool_keys"`
	// PublicToolRefs 在 W1 必须为非 nil 空列表。
	PublicToolRefs []PublicToolSnapshotRefV1 `json:"public_tool_refs"`
	// PermissionSnapshotDigest 是 Business-owned 权限证明摘要，Agent 只校验格式。
	PermissionSnapshotDigest string `json:"permission_snapshot_digest"`
	// RuntimePolicyRef 必须为冻结的 inline-only policy 引用。
	RuntimePolicyRef string `json:"runtime_policy_ref"`
	// GovernanceEpoch 是非负治理纪元，只用于历史冻结。
	GovernanceEpoch int64 `json:"governance_epoch"`
	// PublishedAtUnixMS 是大于零的原始发布时间毫秒整数。
	PublishedAtUnixMS int64 `json:"published_at_unix_ms"`
}

// SessionSkillSnapshotV1 是 Session 创建时冻结且后续不可变的 Skill 集合。
type SessionSkillSnapshotV1 struct {
	// SchemaVersion 固定为 session_skill_snapshot.v1。
	SchemaVersion string
	// SnapshotKind 区分显式空集合和非空 Published 引用集合。
	SnapshotKind SessionSkillSnapshotKindV1
	// SkillCount 必须等于 Skills 长度。
	SkillCount int32
	// SnapshotSetDigest 是 metadata canonical 数组的调用方摘要。
	SnapshotSetDigest string
	// Skills 必须为非 nil，并按稠密 LoadOrder 和 SkillID 顺序到达。
	Skills []PublishedSkillSnapshotRefV1
}

// EnsureProjectSessionInputV2 是 Ensure v2 语义摘要的强类型输入，不包含传输追踪字段。
type EnsureProjectSessionInputV2 struct {
	// SchemaVersion 固定为 ensure_project_session.v2。
	SchemaVersion string
	// ProjectID 是规范小写 UUIDv7。
	ProjectID string
	// OwnerUserID 是可信 Project Owner 规范小写 UUIDv7。
	OwnerUserID string
	// CreationSource 在 W1 只允许 quick_create。
	CreationSource string
	// InitialPrompt 是可选敏感正文；非空正文保留边界空格并只执行 NFC。
	InitialPrompt string
	// SkillSnapshot 是 Business 同事务冻结的完整集合。
	SkillSnapshot SessionSkillSnapshotV1
}

// EnsureProjectSessionCanonicalV2 是 Agent 独立规范化和重算后的 Ensure v2 结果。
type EnsureProjectSessionCanonicalV2 struct {
	// NormalizedPrompt 是 NFC 后的 Prompt；纯 Unicode 空白折叠为空。
	NormalizedPrompt string
	// PromptPresent 表示是否存在非纯空白 Prompt。
	PromptPresent bool
	// PromptDigest 是存在 Prompt 时的 SHA-256 小写十六进制，否则为空。
	PromptDigest string
	// SkillSnapshot 是所有 Runtime/set digest 与 limits 校验成功后的规范化快照。
	SkillSnapshot SessionSkillSnapshotV1
	// CanonicalJSON 是 Ensure 语义固定字段顺序的 compact JSON。
	CanonicalJSON []byte
	// RequestDigest 是 CanonicalJSON 的 SHA-256。
	RequestDigest Digest
}
