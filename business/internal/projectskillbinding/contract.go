// Package projectskillbinding 定义 Business Project Skill Binding、Session Snapshot 解析和 QuickCreate v2 Producer 的有类型边界。
// 本包只描述 Business 内部契约；它不注册 HTTP、RPC、Graph、Runner 或可执行 Tool。
package projectskillbinding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/google/uuid"
)

const (
	// BindingSetSchemaVersionV1 是项目 Skill 绑定集合的冻结结构版本。
	BindingSetSchemaVersionV1 = "project_skill_binding_set.v1"
	// PermissionSnapshotSchemaVersionV1 是 owner-private 权限证明的冻结结构版本。
	PermissionSnapshotSchemaVersionV1 = "project_skill_permission_snapshot.v1"
	// PermissionSnapshotSchemaVersionV2 是 public-market 权限证明的冻结结构版本。
	PermissionSnapshotSchemaVersionV2 = "project_skill_permission_snapshot.v2"
	// RuntimeContentSchemaVersionV1 是 Published Definition 运行时子集的冻结结构版本。
	RuntimeContentSchemaVersionV1 = "skill_runtime_content.v1"
	// SessionSnapshotSchemaVersionV1 是 Agent Session Skill Snapshot 的冻结结构版本。
	SessionSnapshotSchemaVersionV1 = "session_skill_snapshot.v1"
	// EnsureProjectSessionSchemaVersionV2 是 Agent v2 Session 创建请求的冻结语义版本。
	EnsureProjectSessionSchemaVersionV2 = "ensure_project_session.v2"
	// QuickCreateSchemaVersionV2 是显式 QuickCreate v2 请求的冻结语义版本。
	QuickCreateSchemaVersionV2 = "project_quick_create.v2"
	// OutboxPayloadSchemaVersionV2 是完整加密 Session Bootstrap Outbox 明文版本。
	OutboxPayloadSchemaVersionV2 = "session_bootstrap_outbox_payload.v2"
	// RuntimePolicyRefV1 是 W1 唯一允许的 Runtime Skill 安全策略引用。
	RuntimePolicyRefV1 = "skill-runtime-policy:v1"
	// PermissionBasisOwnerPrivate 是 Project Owner 使用自有 Skill 的冻结权限依据。
	PermissionBasisOwnerPrivate = "owner_private"
	// PermissionBasisPublicMarket 是消费者使用其他 Publisher 公开 Skill 的冻结权限依据。
	PermissionBasisPublicMarket = "public_market"
	// PermissionPolicyRefOwnerPrivateV1 是 W1 owner-private 权限规则引用。
	PermissionPolicyRefOwnerPrivateV1 = "project-skill-permission:owner-private:v1"
	// PermissionPolicyRefPublicMarketV1 是 W1 public-market 权限规则引用。
	PermissionPolicyRefPublicMarketV1 = "project-skill-permission:public-market:v1"
	// SkillNamespaceUser 是 W1 唯一允许的用户 Skill namespace。
	SkillNamespaceUser = "user"
	// BindingPriorityW1 是 W1 固定 Skill 加载优先级，客户端不能覆盖。
	BindingPriorityW1 = 100
	// SnapshotKindEmpty 表示合法的显式空 Skill Snapshot。
	SnapshotKindEmpty = "empty"
	// SnapshotKindPublishedRefs 表示包含不可变 Published Snapshot 引用的 Snapshot。
	SnapshotKindPublishedRefs = "published_refs"
	// BindingStatusEnabled 是参与新 Session 解析的绑定状态。
	BindingStatusEnabled = "enabled"
	// BindingSourceQuickCreate 是显式 QuickCreate v2 初始绑定来源。
	BindingSourceQuickCreate = "quick_create"
	// OutboxEncryptionAlgorithm 是 v2 Bootstrap envelope 唯一允许的认证加密算法。
	OutboxEncryptionAlgorithm = "aes-256-gcm"
)

var (
	// ErrInvalidBinding 表示绑定集合、UUID、排序或版本不满足冻结契约。
	ErrInvalidBinding = errors.New("invalid project skill binding")
	// ErrSkillUnavailable 表示 Skill 不存在、未发布、不可公开或引用不一致；调用方不得据此泄露存在性。
	ErrSkillUnavailable = errors.New("project skill unavailable")
	// ErrGovernanceUnavailable 表示 Skill 当前治理状态不是 active。
	ErrGovernanceUnavailable = errors.New("project skill governance unavailable")
	// ErrPublicToolUnavailable 表示 W1 Published Definition 包含未批准的公共 Tool 引用。
	ErrPublicToolUnavailable = errors.New("project skill public tool unavailable")
	// ErrSnapshotInvalid 表示 Definition、Runtime、Permission 或 Snapshot 摘要无法按冻结 Canonical 重算。
	ErrSnapshotInvalid = errors.New("project skill snapshot invalid")
	// ErrSnapshotLimitExceeded 表示 Item 或 Canonical 字节数超过双方冻结 limits。
	ErrSnapshotLimitExceeded = errors.New("session skill snapshot limit exceeded")
	// ErrContentProtection 表示 v2 完整 Bootstrap plaintext 无法被用途隔离的本地保护器认证加密。
	ErrContentProtection = errors.New("session bootstrap content protection unavailable")
)

// Digest 是固定 32 字节 SHA-256，用于 Binding、Permission、Runtime、Snapshot、Request 与 Payload 完整性。
type Digest [sha256.Size]byte

// SHA256Digest 对已经完成契约规范化的字节计算 SHA-256；本方法不隐式 trim 或重排输入。
func SHA256Digest(value []byte) Digest { return sha256.Sum256(value) }

// Hex 将摘要编码为 64 位小写十六进制，供跨 Module DTO 和 golden vector 使用。
func (digest Digest) Hex() string { return hex.EncodeToString(digest[:]) }

// DigestFromHex 严格解析 64 位小写十六进制摘要；大写、空白或长度异常均失败关闭。
func DigestFromHex(value string) (Digest, error) {
	var result Digest
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) || strings.TrimSpace(value) != value {
		return Digest{}, ErrSnapshotInvalid
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return Digest{}, ErrSnapshotInvalid
	}
	copy(result[:], decoded)
	return result, nil
}

// LimitsV1 冻结 Business Producer 的有效数量和 Canonical 字节上限；不得大于 Agent 接收上限。
type LimitsV1 struct {
	// MaxItems 是单个 Session 允许冻结的最大 Skill 数量。
	MaxItems int
	// MaxRuntimeContentBytesPerItem 是单 Item Runtime Content Canonical 字节上限。
	MaxRuntimeContentBytesPerItem int
	// MaxTotalRuntimeContentBytes 是一个 Snapshot 全部 Runtime Content Canonical 字节总上限。
	MaxTotalRuntimeContentBytes int
	// MaxSnapshotMetadataBytes 是 Snapshot metadata Canonical 字节上限。
	MaxSnapshotMetadataBytes int
	// MaxExamplesPerItem 是单 Item examples 数量上限。
	MaxExamplesPerItem int
	// MaxStarterPromptsPerItem 是单 Item starter prompts 数量上限。
	MaxStarterPromptsPerItem int
	// MaxOutboxPlaintextBytes 是加密前完整 Bootstrap plaintext Canonical 字节上限。
	MaxOutboxPlaintextBytes int
}

// DefaultLimitsV1 返回契约建议默认值；部署仍必须校验所有 Agent 实例的接收上限不小于该值。
func DefaultLimitsV1() LimitsV1 {
	return LimitsV1{
		MaxItems: 16, MaxRuntimeContentBytesPerItem: 64 * 1024,
		MaxTotalRuntimeContentBytes: 256 * 1024, MaxSnapshotMetadataBytes: 128 * 1024,
		MaxExamplesPerItem: 16, MaxStarterPromptsPerItem: 16, MaxOutboxPlaintextBytes: 2 * 1024 * 1024,
	}
}

// Validate 校验有效值大于零且不突破 v1 协议 hard ceiling；失败时 Feature Flag 必须保持关闭。
func (limits LimitsV1) Validate() error {
	if limits.MaxItems <= 0 || limits.MaxItems > 32 ||
		limits.MaxRuntimeContentBytesPerItem <= 0 || limits.MaxRuntimeContentBytesPerItem > 128*1024 ||
		limits.MaxTotalRuntimeContentBytes <= 0 || limits.MaxTotalRuntimeContentBytes > 1024*1024 ||
		limits.MaxSnapshotMetadataBytes <= 0 || limits.MaxSnapshotMetadataBytes > 256*1024 ||
		limits.MaxExamplesPerItem <= 0 || limits.MaxExamplesPerItem > 32 ||
		limits.MaxStarterPromptsPerItem <= 0 || limits.MaxStarterPromptsPerItem > 32 ||
		limits.MaxOutboxPlaintextBytes <= 0 || limits.MaxOutboxPlaintextBytes > 4*1024*1024 {
		return ErrSnapshotLimitExceeded
	}
	return nil
}

// BindingSeed 绑定一个预生成 UUIDv7 与目标 Skill；调用方必须在事务前生成全部 ID。
type BindingSeed struct {
	// ID 是待创建 Binding 的 UUIDv7。
	ID string
	// SkillID 是客户端显式选择且已经规范化的 Skill UUIDv7。
	SkillID string
	// AuditID 是初始 enabled 审计行 UUIDv7。
	AuditID string
}

// BindingSelectionItemV1 是 Binding Set Canonical 的单项，字段顺序固定为 priority、namespace、skill_id。
type BindingSelectionItemV1 struct {
	// Priority 是 W1 固定加载优先级 100。
	Priority int `json:"priority"`
	// Namespace 是 W1 固定 user namespace。
	Namespace string `json:"namespace"`
	// SkillID 是规范小写 UUIDv7。
	SkillID string `json:"skill_id"`
}

// CapabilityGuidanceV1 是 Runtime Content 六能力字段之一，不携带 Tool 可执行证明。
type CapabilityGuidanceV1 struct {
	// Applicability 只允许 enabled 或 not_applicable。
	Applicability string `json:"applicability"`
	// Guidance 是 enabled 时的运行时指导正文。
	Guidance string `json:"guidance"`
	// NotApplicableReason 是 not_applicable 时的用户说明。
	NotApplicableReason string `json:"not_applicable_reason"`
}

// SkillExampleV1 是 Runtime Content 中稳定排序的输入输出示例。
type SkillExampleV1 struct {
	// Input 是示例输入正文。
	Input string `json:"input"`
	// Output 是示例输出正文。
	Output string `json:"output"`
}

// SkillRuntimeContentV1 是 Published Definition 投影给 Agent inline Skill Loader 的最小运行时内容。
type SkillRuntimeContentV1 struct {
	// SchemaVersion 固定为 skill_runtime_content.v1。
	SchemaVersion string `json:"schema_version"`
	// Name 来自 Published Definition name。
	Name string `json:"name"`
	// InputDescription 描述 Skill 期望输入。
	InputDescription string `json:"input_description"`
	// OutputDescription 描述 Skill 预期输出。
	OutputDescription string `json:"output_description"`
	// InvocationRules 描述多 Skill 选择规则，不承载权限或预算。
	InvocationRules string `json:"invocation_rules"`
	// PlanCreationSpec 是规划创作规格能力指导。
	PlanCreationSpec CapabilityGuidanceV1 `json:"plan_creation_spec"`
	// AnalyzeMaterials 是分析素材能力指导。
	AnalyzeMaterials CapabilityGuidanceV1 `json:"analyze_materials"`
	// PlanStoryboard 是规划故事板能力指导。
	PlanStoryboard CapabilityGuidanceV1 `json:"plan_storyboard"`
	// GenerateMedia 是生成媒体能力指导。
	GenerateMedia CapabilityGuidanceV1 `json:"generate_media"`
	// WritePrompts 是编写提示词能力指导。
	WritePrompts CapabilityGuidanceV1 `json:"write_prompts"`
	// AssembleOutput 是组装输出能力指导。
	AssembleOutput CapabilityGuidanceV1 `json:"assemble_output"`
	// Examples 是按 input、output 稳定排序且非 nil 的示例数组。
	Examples []SkillExampleV1 `json:"examples"`
	// StarterPrompts 是按 UTF-8 字节序稳定排序且非 nil 的起始提示词数组。
	StarterPrompts []string `json:"starter_prompts"`
}

// PublicToolSnapshotRefV1 是未来公共 Tool 引用的版本化占位；W1 数组必须非 nil 且为空。
type PublicToolSnapshotRefV1 struct {
	// RefID 是未来稳定公共 Tool 引用标识。
	RefID string `json:"ref_id"`
	// RefDigest 是未来公共 Tool Definition 摘要。
	RefDigest string `json:"ref_digest"`
}

// PublishedSkillSnapshotRefV1 是发送给 Agent 的不可变 Published Skill 引用与 Runtime Content。
type PublishedSkillSnapshotRefV1 struct {
	// LoadOrder 是 Session 内从 1 开始的稠密加载顺序。
	LoadOrder int `json:"load_order"`
	// Priority 是 W1 固定优先级。
	Priority int `json:"priority"`
	// Namespace 是 W1 固定 user namespace。
	Namespace string `json:"namespace"`
	// SkillID 是 Business Skill UUIDv7。
	SkillID string `json:"skill_id"`
	// PublisherUserID 是 Skill 权威所有者；public-market 场景允许与项目所有者不同。
	PublisherUserID string `json:"publisher_user_id"`
	// PublishedSnapshotID 是不可变发布快照 UUIDv7。
	PublishedSnapshotID string `json:"published_snapshot_id"`
	// PublicationRevision 是 Skill 内部发布修订序号。
	PublicationRevision int64 `json:"publication_revision"`
	// DefinitionSchemaVersion 是完整发布定义版本。
	DefinitionSchemaVersion string `json:"definition_schema_version"`
	// ContentDigest 是完整发布定义 Canonical 摘要。
	ContentDigest string `json:"content_digest"`
	// RuntimeContentSchemaVersion 是运行时投影版本。
	RuntimeContentSchemaVersion string `json:"runtime_content_schema_version"`
	// RuntimeContentDigest 是运行时投影 Canonical 摘要。
	RuntimeContentDigest string `json:"runtime_content_digest"`
	// RuntimeContent 是只能进入加密 Outbox 的 inline 运行时正文。
	RuntimeContent SkillRuntimeContentV1 `json:"runtime_content"`
	// AllowedGraphToolKeys 是六能力 enabled 投影，只是声明而不是可执行证明。
	AllowedGraphToolKeys []string `json:"allowed_graph_tool_keys"`
	// PublicToolRefs 在 W1 必须是非 nil 空数组。
	PublicToolRefs []PublicToolSnapshotRefV1 `json:"public_tool_refs"`
	// PermissionSnapshotDigest 是 v1 owner-private 或 v2 public-market 权限 Canonical 摘要。
	PermissionSnapshotDigest string `json:"permission_snapshot_digest"`
	// RuntimePolicyRef 是固定运行时安全策略引用。
	RuntimePolicyRef string `json:"runtime_policy_ref"`
	// GovernanceEpoch 是解析时冻结的治理有效性纪元。
	GovernanceEpoch int64 `json:"governance_epoch"`
	// PublishedAtUnixMS 是发布时刻 Unix 毫秒整数。
	PublishedAtUnixMS int64 `json:"published_at_unix_ms"`
}

// SessionSkillSnapshotV1 是 Business Producer 冻结并发送给 Agent v2 的完整有类型 Snapshot。
type SessionSkillSnapshotV1 struct {
	// SchemaVersion 固定为 session_skill_snapshot.v1。
	SchemaVersion string `json:"schema_version"`
	// SnapshotKind 是 empty 或 published_refs。
	SnapshotKind string `json:"snapshot_kind"`
	// SkillCount 必须等于 Skills 长度。
	SkillCount int `json:"skill_count"`
	// SnapshotSetDigest 是不含 Runtime 明文的 metadata Canonical 摘要。
	SnapshotSetDigest string `json:"snapshot_set_digest"`
	// Skills 按 load_order 稳定排序；空集合仍必须是非 nil 数组。
	Skills []PublishedSkillSnapshotRefV1 `json:"skills"`
}

// PermissionSnapshotV1 是 W1 owner-private 权限决策的不可变 Canonical 输入。
type PermissionSnapshotV1 struct {
	// SchemaVersion 固定为 project_skill_permission_snapshot.v1。
	SchemaVersion string `json:"schema_version"`
	// Decision 固定为 allow。
	Decision string `json:"decision"`
	// Basis 固定为 owner_private。
	Basis string `json:"basis"`
	// SubjectUserID 是可信当前用户。
	SubjectUserID string `json:"subject_user_id"`
	// ProjectID 是目标项目。
	ProjectID string `json:"project_id"`
	// ProjectOwnerUserID 是项目冻结所有者。
	ProjectOwnerUserID string `json:"project_owner_user_id"`
	// BindingID 是解析来源绑定。
	BindingID string `json:"binding_id"`
	// BindingVersion 是解析来源绑定版本。
	BindingVersion int64 `json:"binding_version"`
	// BindingSetVersion 是解析来源集合版本。
	BindingSetVersion int64 `json:"binding_set_version"`
	// Namespace 固定为 user。
	Namespace string `json:"namespace"`
	// SkillID 是被授权 Skill。
	SkillID string `json:"skill_id"`
	// SkillOwnerUserID 是 Skill 所有者，必须等于项目所有者。
	SkillOwnerUserID string `json:"skill_owner_user_id"`
	// PublishedSnapshotID 是精确发布快照。
	PublishedSnapshotID string `json:"published_snapshot_id"`
	// AllowedActions 固定为非 nil 单元素 session_snapshot。
	AllowedActions []string `json:"allowed_actions"`
	// PolicyRef 固定为 owner-private v1 权限策略引用。
	PolicyRef string `json:"policy_ref"`
}

// PermissionSnapshotV2 是 W1-F public-market 权限决策的不可变 Canonical 输入。
// 字段顺序刻意与 v1 一致，但 schema、basis、policy 和 Owner 关系严格隔离，禁止交叉组合。
type PermissionSnapshotV2 struct {
	// SchemaVersion 固定为 project_skill_permission_snapshot.v2。
	SchemaVersion string `json:"schema_version"`
	// Decision 固定为 allow。
	Decision string `json:"decision"`
	// Basis 固定为 public_market。
	Basis string `json:"basis"`
	// SubjectUserID 是可信当前消费者。
	SubjectUserID string `json:"subject_user_id"`
	// ProjectID 是目标项目。
	ProjectID string `json:"project_id"`
	// ProjectOwnerUserID 是项目冻结所有者。
	ProjectOwnerUserID string `json:"project_owner_user_id"`
	// BindingID 是解析来源绑定。
	BindingID string `json:"binding_id"`
	// BindingVersion 是解析来源绑定版本。
	BindingVersion int64 `json:"binding_version"`
	// BindingSetVersion 是解析来源集合版本。
	BindingSetVersion int64 `json:"binding_set_version"`
	// Namespace 固定为 user。
	Namespace string `json:"namespace"`
	// SkillID 是被授权的公开 Skill。
	SkillID string `json:"skill_id"`
	// SkillOwnerUserID 是冻结 Publisher，必须与项目所有者不同。
	SkillOwnerUserID string `json:"skill_owner_user_id"`
	// PublishedSnapshotID 是同一事务冻结的精确发布快照。
	PublishedSnapshotID string `json:"published_snapshot_id"`
	// AllowedActions 固定为非 nil 单元素 session_snapshot。
	AllowedActions []string `json:"allowed_actions"`
	// PolicyRef 固定为 public-market v1 权限策略引用。
	PolicyRef string `json:"policy_ref"`
}

// PublishedSkillReadDTO 是 Repository 一次集合查询映射的专用只读 DTO，不参与持久化。
type PublishedSkillReadDTO struct {
	// ProjectID 是当前事务中新建或锁定的项目。
	ProjectID string
	// ProjectOwnerUserID 是项目权威所有者。
	ProjectOwnerUserID string
	// ProjectLifecycleStatus 必须为 active。
	ProjectLifecycleStatus string
	// BindingID 是 enabled Binding UUIDv7。
	BindingID string
	// BindingVersion 是 Binding 行版本。
	BindingVersion int64
	// BindingStatus 必须为 enabled。
	BindingStatus string
	// Namespace 必须为 user。
	Namespace string
	// Priority 必须为 100。
	Priority int
	// SkillID 是绑定目标 Skill。
	SkillID string
	// SkillOwnerUserID 是 Skill 权威所有者，允许与项目所有者不同。
	SkillOwnerUserID string
	// PublisherUserID 是同一 SQL 关联到的 Publisher Account 标识，必须等于 SkillOwnerUserID。
	PublisherUserID string
	// CurrentPublishedSnapshotID 是 Skill 当前发布指针。
	CurrentPublishedSnapshotID string
	// SkillPublicationRevision 是 Skill 当前发布修订。
	SkillPublicationRevision int64
	// GovernanceStatus 必须为 active。
	GovernanceStatus string
	// GovernanceEpoch 是当前治理纪元。
	GovernanceEpoch int64
	// PublishedSnapshotID 是 JOIN 到的不可变发布快照。
	PublishedSnapshotID string
	// PublishedSkillID 是发布快照所属 Skill。
	PublishedSkillID string
	// SourceContentRevisionID 是发布快照精确来源修订。
	SourceContentRevisionID string
	// PublishedPublicationRevision 是发布快照修订序号。
	PublishedPublicationRevision int64
	// DefinitionSchemaVersion 是完整发布定义版本。
	DefinitionSchemaVersion string
	// DefinitionJSON 是数据库中完整发布定义 Canonical JSON。
	DefinitionJSON []byte
	// ContentDigest 是数据库中完整发布定义摘要。
	ContentDigest Digest
	// PublishedByReviewerUserID 是执行批准发布的 Reviewer，仅用于证明不会被误投影为 Publisher。
	PublishedByReviewerUserID string
	// PublishedAt 是发布时刻 UTC 时间。
	PublishedAt time.Time
	// RevisionID 是 JOIN 到的不可变来源修订标识。
	RevisionID string
	// RevisionSkillID 是来源修订所属 Skill。
	RevisionSkillID string
	// RevisionDefinitionSchemaVersion 是来源修订定义版本。
	RevisionDefinitionSchemaVersion string
	// RevisionDefinitionJSON 是来源修订 Canonical JSON。
	RevisionDefinitionJSON []byte
	// RevisionContentDigest 是来源修订摘要。
	RevisionContentDigest Digest
}

// ResolveInputV1 是 QuickCreate v2 同一事务调用内部解析器的可信输入。
type ResolveInputV1 struct {
	// ResolutionID 是待写 Resolution Header UUIDv7。
	ResolutionID string
	// ProjectID 是当前 QuickCreate Project UUIDv7。
	ProjectID string
	// OwnerUserID 是 Auth Principal 与 Project Owner。
	OwnerUserID string
	// CommandID 是待写 Outbox 稳定 command_id。
	CommandID string
	// BindingSetVersion 是同事务 Binding Header 版本。
	BindingSetVersion int64
	// BindingSelectionDigest 是同事务 Binding Header 摘要。
	BindingSelectionDigest Digest
	// ResolvedAt 是同一事务冻结的 UTC 时间。
	ResolvedAt time.Time
}

// ResolutionHeader 是 Business 不可变 Session Skill 解析头。
type ResolutionHeader struct {
	// ID 是解析头 UUIDv7。
	ID string
	// CommandID 是 Session Bootstrap command_id。
	CommandID string
	// ProjectID 是所属项目。
	ProjectID string
	// OwnerUserID 是冻结的可信所有者。
	OwnerUserID string
	// BindingSetVersion 是绑定集合版本。
	BindingSetVersion int64
	// BindingSelectionDigest 是绑定集合摘要。
	BindingSelectionDigest Digest
	// SnapshotSchemaVersion 是 Session Snapshot 结构版本。
	SnapshotSchemaVersion string
	// SnapshotKind 是 empty 或 published_refs。
	SnapshotKind string
	// SkillCount 是冻结 Skill 数量。
	SkillCount int
	// SnapshotSetDigest 是 metadata 集合摘要。
	SnapshotSetDigest Digest
	// RuntimePolicyRef 是固定安全策略引用。
	RuntimePolicyRef string
	// ResolvedAt 是解析冻结时间。
	ResolvedAt time.Time
}

// ResolutionItem 是 Business 不可变解析元数据；它不复制 Runtime Content 明文。
type ResolutionItem struct {
	// ResolutionID 是所属解析头。
	ResolutionID string
	// ProjectID 是所属项目。
	ProjectID string
	// CommandID 是所属 Bootstrap command_id。
	CommandID string
	// LoadOrder 是稠密加载顺序。
	LoadOrder int
	// Priority 是冻结优先级。
	Priority int
	// Namespace 是冻结 namespace。
	Namespace string
	// BindingID 是来源绑定。
	BindingID string
	// BindingVersion 是来源绑定版本。
	BindingVersion int64
	// SkillID 是冻结 Skill。
	SkillID string
	// PublisherUserID 是冻结发布者。
	PublisherUserID string
	// PublishedSnapshotID 是冻结发布快照。
	PublishedSnapshotID string
	// PublicationRevision 是冻结发布修订。
	PublicationRevision int64
	// DefinitionSchemaVersion 是完整定义版本。
	DefinitionSchemaVersion string
	// ContentDigest 是完整定义摘要。
	ContentDigest Digest
	// RuntimeContentSchemaVersion 是运行时投影版本。
	RuntimeContentSchemaVersion string
	// RuntimeContentDigest 是运行时投影摘要。
	RuntimeContentDigest Digest
	// AllowedGraphToolKeys 是声明键快照。
	AllowedGraphToolKeys []string
	// PublicToolRefs 是 W1 非 nil 空数组。
	PublicToolRefs []PublicToolSnapshotRefV1
	// PermissionSnapshotDigest 是 v1 owner-private 或 v2 public-market 权限摘要。
	PermissionSnapshotDigest Digest
	// RuntimePolicyRef 是固定安全策略引用。
	RuntimePolicyRef string
	// GovernanceEpoch 是治理纪元。
	GovernanceEpoch int64
	// PublishedAtUnixMS 是发布 Unix 毫秒。
	PublishedAtUnixMS int64
	// CreatedAt 是解析项创建 UTC 时间。
	CreatedAt time.Time
}

// ReconstructedPermissionAudit 是由不可变 Resolution Header 与 Item 重建并核验的权限审计投影。
type ReconstructedPermissionAudit struct {
	// SchemaVersion 是重建出的 v1 owner-private 或 v2 public-market Schema。
	SchemaVersion string
	// Basis 是重建出的 owner_private 或 public_market 权限依据。
	Basis string
	// PolicyRef 是与 Schema/Basis 成对的冻结权限策略引用。
	PolicyRef string
	// CanonicalJSON 是按冻结字段顺序恢复的完整 Canonical JSON。
	CanonicalJSON []byte
	// Digest 是 CanonicalJSON 的 SHA-256，必须与 Resolution Item 持久化摘要一致。
	Digest Digest
}

// ResolutionV1 是内部解析契约输出，同时承载持久化 metadata 与待加密 Agent Snapshot。
type ResolutionV1 struct {
	// Header 是待写不可变解析头。
	Header ResolutionHeader
	// Items 是待 batch insert 的 metadata；空集合为非 nil 空数组。
	Items []ResolutionItem
	// Snapshot 是待编码进加密 Outbox 的完整 Agent v2 DTO。
	Snapshot SessionSkillSnapshotV1
}

// QuickCreateV2Seed 是构造显式 v2 命令所需的事务外可信输入与预生成 UUIDv7。
type QuickCreateV2Seed struct {
	// SchemaVersion 必须显式等于 project_quick_create.v2。
	SchemaVersion string
	// ProjectID 是待创建 Project UUIDv7。
	ProjectID string
	// ReceiptID 是待 Claim 创建回执 UUIDv7。
	ReceiptID string
	// SessionBindingID 是待创建默认 Session Binding UUIDv7。
	SessionBindingID string
	// CommandID 是待创建 Outbox UUIDv7。
	CommandID string
	// ResolutionID 是待创建解析头 UUIDv7。
	ResolutionID string
	// OwnerUserID 是可信 Auth Principal。
	OwnerUserID string
	// InitialPrompt 是短暂存在的原始首提示词；只进入加密 plaintext，不得写日志或明文列。
	InitialPrompt string
	// KeyDigest 是原始 Idempotency-Key 摘要，数据库不保存原文。
	KeyDigest Digest
	// Bindings 必须是非 nil 数组，每项包含预生成 Binding/Audit ID 与客户端选择 Skill ID。
	Bindings []BindingSeed
	// MaxAttempts 是 Outbox 冻结的有限派发预算。
	MaxAttempts int32
	// OccurredAt 是 QuickCreate 与 Resolution 冻结 UTC 时间。
	OccurredAt time.Time
}

// QuickCreateV2Command 是完成 schema、Prompt、UUID、集合排序和语义摘要校验后的内部 Repository 命令。
type QuickCreateV2Command struct {
	// ProjectID 是待创建 Project。
	ProjectID string
	// ReceiptID 是创建回执。
	ReceiptID string
	// SessionBindingID 是默认 Session Binding。
	SessionBindingID string
	// CommandID 是 Outbox 与 Agent command_id。
	CommandID string
	// ResolutionID 是解析头。
	ResolutionID string
	// OwnerUserID 是可信所有者。
	OwnerUserID string
	// NormalizedPrompt 是 NFC 后首提示词，只在构造加密 payload 前存在。
	NormalizedPrompt string
	// PromptDigest 是非空 Prompt 摘要；absent 时为零值。
	PromptDigest Digest
	// PromptPresent 表示是否存在非空 Prompt。
	PromptPresent bool
	// KeyDigest 是幂等键摘要。
	KeyDigest Digest
	// SemanticDigest 是 Prompt 与 Binding Set 组成的 QuickCreate v2 语义摘要。
	SemanticDigest Digest
	// SelectionDigest 是排序后 Binding Set 摘要。
	SelectionDigest Digest
	// Bindings 按 SkillID 升序且非 nil。
	Bindings []BindingSeed
	// MaxAttempts 是 Outbox 派发预算。
	MaxAttempts int32
	// OccurredAt 是 UTC 冻结时间。
	OccurredAt time.Time
}

// QuickCreateV2Repository 定义显式 v2 创建所需的最小原子持久化能力。
// 当前只供 Business 内部应用层调用，不得据此注册 HTTP 或 Dispatcher。
type QuickCreateV2Repository interface {
	// CreateQuickV2 先 Claim 回执，再在一个事务内创建 Project、Binding、Resolution 与加密 Outbox；同键同义重放冻结结果。
	CreateQuickV2(ctx context.Context, command QuickCreateV2Command, limits LimitsV1, protector OutboxPayloadProtectorV2) (QuickCreateV2Result, error)
}

// NewQuickCreateV2Command 严格构造显式 v2 命令；重复、非规范 UUIDv7、nil 数组或超限均在事务前失败。
func NewQuickCreateV2Command(seed QuickCreateV2Seed, limits LimitsV1) (QuickCreateV2Command, error) {
	if err := limits.Validate(); err != nil || seed.SchemaVersion != QuickCreateSchemaVersionV2 || seed.Bindings == nil ||
		seed.MaxAttempts <= 0 || seed.OccurredAt.IsZero() || isZeroDigest(seed.KeyDigest) {
		return QuickCreateV2Command{}, ErrInvalidBinding
	}
	ids := []string{seed.ProjectID, seed.ReceiptID, seed.SessionBindingID, seed.CommandID, seed.ResolutionID, seed.OwnerUserID}
	seenIDs := make(map[string]struct{}, len(ids)+len(seed.Bindings)*2)
	for _, id := range ids {
		if !isCanonicalUUIDv7(id) {
			return QuickCreateV2Command{}, ErrInvalidBinding
		}
		if _, exists := seenIDs[id]; exists {
			return QuickCreateV2Command{}, ErrInvalidBinding
		}
		seenIDs[id] = struct{}{}
	}
	if len(seed.Bindings) > limits.MaxItems {
		return QuickCreateV2Command{}, ErrSnapshotLimitExceeded
	}
	bindings := make([]BindingSeed, len(seed.Bindings))
	copy(bindings, seed.Bindings)
	seenSkills := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		if !isCanonicalUUIDv7(binding.ID) || !isCanonicalUUIDv7(binding.AuditID) || !isCanonicalUUIDv7(binding.SkillID) {
			return QuickCreateV2Command{}, ErrInvalidBinding
		}
		if _, exists := seenIDs[binding.ID]; exists {
			return QuickCreateV2Command{}, ErrInvalidBinding
		}
		seenIDs[binding.ID] = struct{}{}
		if _, exists := seenIDs[binding.AuditID]; exists {
			return QuickCreateV2Command{}, ErrInvalidBinding
		}
		seenIDs[binding.AuditID] = struct{}{}
		if _, duplicate := seenSkills[binding.SkillID]; duplicate {
			return QuickCreateV2Command{}, ErrInvalidBinding
		}
		seenSkills[binding.SkillID] = struct{}{}
	}
	// 客户端数组顺序不构成 priority；Repository 和 Agent 只接收这份 SkillID 升序冻结结果。
	sort.Slice(bindings, func(left, right int) bool { return bindings[left].SkillID < bindings[right].SkillID })
	selection := make([]BindingSelectionItemV1, len(bindings))
	for index, binding := range bindings {
		selection[index] = BindingSelectionItemV1{Priority: BindingPriorityW1, Namespace: SkillNamespaceUser, SkillID: binding.SkillID}
	}
	_, selectionDigest, err := CanonicalBindingSelectionV1(selection)
	if err != nil {
		return QuickCreateV2Command{}, err
	}
	normalizedPrompt, projectPromptDigest, promptPresent, err := project.NormalizeEnsureSessionPrompt(seed.InitialPrompt)
	if err != nil {
		return QuickCreateV2Command{}, ErrInvalidBinding
	}
	var promptDigest Digest
	copy(promptDigest[:], projectPromptDigest[:])
	_, semanticDigest, err := CanonicalQuickCreateSemanticV2(promptPresent, promptDigest, selectionDigest)
	if err != nil {
		return QuickCreateV2Command{}, err
	}
	return QuickCreateV2Command{
		ProjectID: seed.ProjectID, ReceiptID: seed.ReceiptID, SessionBindingID: seed.SessionBindingID,
		CommandID: seed.CommandID, ResolutionID: seed.ResolutionID, OwnerUserID: seed.OwnerUserID,
		NormalizedPrompt: normalizedPrompt, PromptDigest: promptDigest, PromptPresent: promptPresent,
		KeyDigest: seed.KeyDigest, SemanticDigest: semanticDigest, SelectionDigest: selectionDigest,
		Bindings: bindings, MaxAttempts: seed.MaxAttempts, OccurredAt: seed.OccurredAt.UTC(),
	}, nil
}

// Validate 重新构造并核对 QuickCreate v2 命令，防止调用方在构造后修改导出字段绕过 schema、排序或摘要不变量。
func (command QuickCreateV2Command) Validate(limits LimitsV1) error {
	rebuilt, err := NewQuickCreateV2Command(QuickCreateV2Seed{
		SchemaVersion: QuickCreateSchemaVersionV2, ProjectID: command.ProjectID, ReceiptID: command.ReceiptID,
		SessionBindingID: command.SessionBindingID, CommandID: command.CommandID, ResolutionID: command.ResolutionID,
		OwnerUserID: command.OwnerUserID, InitialPrompt: command.NormalizedPrompt, KeyDigest: command.KeyDigest,
		Bindings: command.Bindings, MaxAttempts: command.MaxAttempts, OccurredAt: command.OccurredAt,
	}, limits)
	if err != nil {
		return err
	}
	if rebuilt.PromptPresent != command.PromptPresent || rebuilt.PromptDigest != command.PromptDigest ||
		rebuilt.SemanticDigest != command.SemanticDigest || rebuilt.SelectionDigest != command.SelectionDigest ||
		!rebuilt.OccurredAt.Equal(command.OccurredAt) || len(rebuilt.Bindings) != len(command.Bindings) {
		return ErrInvalidBinding
	}
	for index := range rebuilt.Bindings {
		if rebuilt.Bindings[index] != command.Bindings[index] {
			return ErrInvalidBinding
		}
	}
	return nil
}

// EncryptedEnvelopeV2 是完整 Bootstrap plaintext 的认证加密结果；数据库只保存该 envelope 与非秘密摘要。
type EncryptedEnvelopeV2 struct {
	// Algorithm 必须为 aes-256-gcm。
	Algorithm string
	// KeyVersion 是用途隔离的密钥版本引用，不包含密钥材料。
	KeyVersion string
	// Nonce 是单次随机数，AES-GCM 固定 12 字节且不得复用。
	Nonce []byte
	// CiphertextAndTag 是完整 Bootstrap plaintext 密文与认证标签。
	CiphertextAndTag []byte
}

// OutboxPayloadProtectorV2 是事务前预加载的本地认证加密器。
// Protect 必须只执行有界本地 CPU/内存操作，禁止在 Repository 事务内访问 KMS、网络、文件或其他外部 I/O。
type OutboxPayloadProtectorV2 interface {
	// Protect 使用冻结 AAD 保护完整 Canonical plaintext；失败不得返回部分 envelope 或明文降级。
	Protect(ctx context.Context, canonicalPlaintext []byte, canonicalAAD []byte) (EncryptedEnvelopeV2, error)
}

// SessionBootstrapOutboxPayloadV2 是只允许进入认证加密 envelope 的完整版本化明文 DTO。
type SessionBootstrapOutboxPayloadV2 struct {
	// SchemaVersion 固定为 session_bootstrap_outbox_payload.v2。
	SchemaVersion string `json:"schema_version"`
	// CommandID 是稳定 Agent command_id。
	CommandID string `json:"command_id"`
	// ProjectID 是 Business Project。
	ProjectID string `json:"project_id"`
	// OwnerUserID 是冻结可信所有者。
	OwnerUserID string `json:"owner_user_id"`
	// CreationSource 固定为 quick_create。
	CreationSource string `json:"creation_source"`
	// PromptPresent 表示是否存在非空 Prompt。
	PromptPresent bool `json:"prompt_present"`
	// InitialPrompt 是 NFC 后正文；absent 时固定为空字符串且仍不落明文数据库列。
	InitialPrompt string `json:"initial_prompt"`
	// PromptDigest 是非空 Prompt 摘要；absent 时固定为空字符串。
	PromptDigest string `json:"prompt_digest"`
	// SkillSnapshot 是完整 Session Snapshot 与 Runtime Content。
	SkillSnapshot SessionSkillSnapshotV1 `json:"skill_snapshot"`
	// AcceptedAtUnixMS 是 Business 接受命令时间的 Unix 毫秒。
	AcceptedAtUnixMS int64 `json:"accepted_at_unix_ms"`
	// RequestDigest 是 ensure_project_session.v2 业务语义摘要。
	RequestDigest string `json:"request_digest"`
}

// OutboxAADV2 是 v2 envelope 的固定 domain-separated AAD Canonical DTO。
type OutboxAADV2 struct {
	// SchemaVersion 是完整 Outbox plaintext 版本。
	SchemaVersion string `json:"schema_version"`
	// CommandID 是稳定 command_id。
	CommandID string `json:"command_id"`
	// ProjectID 是所属项目。
	ProjectID string `json:"project_id"`
	// OwnerUserID 是冻结所有者。
	OwnerUserID string `json:"owner_user_id"`
	// RequestDigest 是 Ensure v2 语义摘要。
	RequestDigest string `json:"request_digest"`
	// SkillSnapshotDigest 是不可变 Snapshot set digest。
	SkillSnapshotDigest string `json:"skill_snapshot_digest"`
}

// QuickCreateV2Result 是内部 Repository 原子入口的安全结果，不包含 Prompt、Runtime Content、密文或权限原文。
type QuickCreateV2Result struct {
	// ProjectID 是首次创建或重放的项目。
	ProjectID string
	// RequestSchemaVersion 固定为 project_quick_create.v2。
	RequestSchemaVersion string
	// SnapshotDigest 是首次冻结的 Snapshot set digest。
	SnapshotDigest Digest
	// SkillCount 是首次冻结的 Skill 数量。
	SkillCount int
	// BindingSetVersion 是首次冻结的集合版本。
	BindingSetVersion int64
	// ResolutionID 是首次冻结的 Resolution。
	ResolutionID string
	// IdempotentReplay 表示是否命中已有同义回执。
	IdempotentReplay bool
}

// PreparedOutboxV2 是同一数据库事务中从精确 Resolution 构造的完整加密 Outbox 结果。
type PreparedOutboxV2 struct {
	// RequestDigest 是 ensure_project_session.v2 业务语义摘要。
	RequestDigest Digest
	// PayloadDigest 是完整 Bootstrap plaintext Canonical 摘要。
	PayloadDigest Digest
	// SnapshotDigest 是 Resolution 冻结的 Snapshot set digest。
	SnapshotDigest Digest
	// SkillCount 是冻结 Skill 数量。
	SkillCount int
	// Envelope 是本地认证加密结果，不包含可记录明文。
	Envelope EncryptedEnvelopeV2
}

// OutboxExpectedV2 是 Dispatcher 从可信 Outbox 列读取的不可变 metadata，用于解密后逐层重算而非读取当前 Binding。
type OutboxExpectedV2 struct {
	// CommandID 是原 Outbox 与 Agent command_id。
	CommandID string
	// ProjectID 是原 Business Project。
	ProjectID string
	// OwnerUserID 是命令冻结 Owner。
	OwnerUserID string
	// RequestDigest 是持久化 EnsureProjectSessionV2 语义摘要。
	RequestDigest Digest
	// SnapshotDigest 是持久化 Snapshot set 摘要。
	SnapshotDigest Digest
	// PayloadDigest 是持久化完整 plaintext Canonical 摘要。
	PayloadDigest Digest
	// SkillCount 是持久化 Skill 数量。
	SkillCount int
}

// isCanonicalUUIDv7 校验规范小写 UUIDv7；解析后文本必须与输入完全一致。
func isCanonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

// isZeroDigest 判断摘要是否为未初始化零值；冻结语义摘要和密文摘要均不允许零值。
func isZeroDigest(value Digest) bool { return value == (Digest{}) }

// validateEnvelopeV2 校验本地保护器没有返回明文、弱算法或不完整认证元数据。
func validateEnvelopeV2(envelope EncryptedEnvelopeV2) error {
	if envelope.Algorithm != OutboxEncryptionAlgorithm || strings.TrimSpace(envelope.KeyVersion) != envelope.KeyVersion ||
		envelope.KeyVersion == "" || len(envelope.KeyVersion) > 128 || len(envelope.Nonce) != 12 || len(envelope.CiphertextAndTag) <= 16 {
		return ErrContentProtection
	}
	return nil
}

// validateResolutionInput 校验解析头的可信 ID、版本、摘要与时间，避免持久化部分或不可重放事实。
func validateResolutionInput(input ResolveInputV1) error {
	if !isCanonicalUUIDv7(input.ResolutionID) || !isCanonicalUUIDv7(input.ProjectID) ||
		!isCanonicalUUIDv7(input.OwnerUserID) || !isCanonicalUUIDv7(input.CommandID) ||
		input.BindingSetVersion < 1 || isZeroDigest(input.BindingSelectionDigest) || input.ResolvedAt.IsZero() {
		return ErrInvalidBinding
	}
	return nil
}

// wrapSnapshotError 只增加稳定阶段上下文，不拼接 Prompt、Definition 或数据库原始值。
func wrapSnapshotError(stage string, err error) error {
	return fmt.Errorf("%s: %w", stage, err)
}
