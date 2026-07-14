package postgres

import (
	"bytes"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
)

// skillModel 映射 business.skill 聚合根表。
type skillModel struct {
	// ID 是 Skill UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// OwnerUserID 是可信所有者逻辑关联标识。
	OwnerUserID string `gorm:"column:owner_user_id;type:uuid"`
	// CurrentDraftRevisionID 是当前草稿逻辑指针。
	CurrentDraftRevisionID string `gorm:"column:current_draft_revision_id;type:uuid"`
	// CurrentPublishedSnapshotID 是当前发布逻辑指针。
	CurrentPublishedSnapshotID *string `gorm:"column:current_published_snapshot_id;type:uuid"`
	// PublicationRevision 是内部发布递增序号。
	PublicationRevision int64 `gorm:"column:publication_revision"`
	// GovernanceStatus 是治理可用性稳定代码。
	GovernanceStatus string `gorm:"column:governance_status"`
	// Version 是聚合并发版本。
	Version int64 `gorm:"column:version"`
	// CreatedAt 是 UTC 创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 是 UTC 最近更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Skill 聚合根权威表名，禁止 GORM 推导和 AutoMigrate。
func (skillModel) TableName() string { return "business.skill" }

// skillContentRevisionModel 映射不可变 Skill 内容修订表。
type skillContentRevisionModel struct {
	// ID 是内容修订 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// SkillID 是所属 Skill 逻辑关联标识。
	SkillID string `gorm:"column:skill_id;type:uuid"`
	// RevisionNo 是同 Skill 内部内容序号。
	RevisionNo int64 `gorm:"column:revision_no"`
	// DefinitionSchemaVersion 是冻结结构化定义版本。
	DefinitionSchemaVersion string `gorm:"column:definition_schema_version"`
	// DefinitionJSON 是完成规范化的结构化定义 JSONB。
	DefinitionJSON jsonbValue `gorm:"column:definition_json;type:jsonb"`
	// ContentDigest 是 Canonical JSON SHA-256 摘要。
	ContentDigest []byte `gorm:"column:content_digest"`
	// CreatedByUserID 是可信修订创建者。
	CreatedByUserID string `gorm:"column:created_by_user_id;type:uuid"`
	// CreatedAt 是 UTC 创建时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回 Skill 内容修订权威表名。
func (skillContentRevisionModel) TableName() string { return "business.skill_content_revision" }

// skillReviewSubmissionModel 映射冻结内容的审核提交表。
type skillReviewSubmissionModel struct {
	// ID 是审核提交 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// SkillID 是所属 Skill 逻辑关联标识。
	SkillID string `gorm:"column:skill_id;type:uuid"`
	// ContentRevisionID 是被冻结内容修订标识。
	ContentRevisionID string `gorm:"column:content_revision_id;type:uuid"`
	// ContentDigest 是提交时冻结的内容摘要。
	ContentDigest []byte `gorm:"column:content_digest"`
	// Status 是审核状态稳定代码。
	Status string `gorm:"column:status"`
	// SafeReasonCode 是可选安全原因代码。
	SafeReasonCode *string `gorm:"column:safe_reason_code"`
	// Version 是审核状态并发版本。
	Version int64 `gorm:"column:version"`
	// SubmittedByUserID 是可信提交 Owner。
	SubmittedByUserID string `gorm:"column:submitted_by_user_id;type:uuid"`
	// DecidedByUserID 是终态 Reviewer，审核中为空。
	DecidedByUserID *string `gorm:"column:decided_by_user_id;type:uuid"`
	// SubmittedAt 是 UTC 提交时间。
	SubmittedAt time.Time `gorm:"column:submitted_at"`
	// DecidedAt 是 UTC 决定时间。
	DecidedAt *time.Time `gorm:"column:decided_at"`
	// UpdatedAt 是 UTC 最近状态时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Skill 审核提交权威表名。
func (skillReviewSubmissionModel) TableName() string { return "business.skill_review_submission" }

// skillPublishedSnapshotModel 映射不可变发布快照表。
type skillPublishedSnapshotModel struct {
	// ID 是发布快照 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// SkillID 是所属 Skill 逻辑关联标识。
	SkillID string `gorm:"column:skill_id;type:uuid"`
	// SourceContentRevisionID 是发布来源修订。
	SourceContentRevisionID string `gorm:"column:source_content_revision_id;type:uuid"`
	// ReviewSubmissionID 是批准本次发布的审核提交。
	ReviewSubmissionID string `gorm:"column:review_submission_id;type:uuid"`
	// PublicationRevision 是内部发布序号。
	PublicationRevision int64 `gorm:"column:publication_revision"`
	// DefinitionSchemaVersion 是发布定义版本。
	DefinitionSchemaVersion string `gorm:"column:definition_schema_version"`
	// DefinitionJSON 是发布时冻结的结构化定义 JSONB。
	DefinitionJSON jsonbValue `gorm:"column:definition_json;type:jsonb"`
	// ContentDigest 是发布内容 SHA-256 摘要。
	ContentDigest []byte `gorm:"column:content_digest"`
	// PublishedByUserID 是受信 Reviewer。
	PublishedByUserID string `gorm:"column:published_by_user_id;type:uuid"`
	// PublishedAt 是 UTC 发布时间。
	PublishedAt time.Time `gorm:"column:published_at"`
}

// TableName 返回 Skill 发布快照权威表名。
func (skillPublishedSnapshotModel) TableName() string { return "business.skill_published_snapshot" }

// skillCommandReceiptModel 映射不保存原始幂等键的 Skill 命令回执表。
type skillCommandReceiptModel struct {
	// ID 是回执 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// ActorUserID 是可信 Owner 或 Reviewer。
	ActorUserID string `gorm:"column:actor_user_id;type:uuid"`
	// CommandType 是稳定命令类型。
	CommandType string `gorm:"column:command_type"`
	// ScopeID 是命令幂等作用域。
	ScopeID string `gorm:"column:scope_id;type:uuid"`
	// KeyDigest 是原始幂等键摘要。
	KeyDigest []byte `gorm:"column:key_digest"`
	// SemanticDigest 是稳定命令语义摘要。
	SemanticDigest []byte `gorm:"column:semantic_digest"`
	// ResultSkillID 是结果 Skill 引用。
	ResultSkillID string `gorm:"column:result_skill_id;type:uuid"`
	// ResultContentRevisionID 是可选内容修订引用。
	ResultContentRevisionID *string `gorm:"column:result_content_revision_id;type:uuid"`
	// ResultReviewSubmissionID 是可选审核提交引用。
	ResultReviewSubmissionID *string `gorm:"column:result_review_submission_id;type:uuid"`
	// ResultPublishedSnapshotID 是可选发布快照引用。
	ResultPublishedSnapshotID *string `gorm:"column:result_published_snapshot_id;type:uuid"`
	// ResponseDraftRevisionID 是首次安全响应的草稿修订引用。
	ResponseDraftRevisionID string `gorm:"column:response_draft_revision_id;type:uuid"`
	// ResponsePublishedSnapshotID 是首次安全响应的发布快照引用。
	ResponsePublishedSnapshotID *string `gorm:"column:response_published_snapshot_id;type:uuid"`
	// ResponseReviewSubmissionID 是首次安全响应的审核提交引用。
	ResponseReviewSubmissionID *string `gorm:"column:response_review_submission_id;type:uuid"`
	// ResponseReviewStatus 是首次安全响应冻结的审核状态。
	ResponseReviewStatus *string `gorm:"column:response_review_status"`
	// ResponseReviewReasonCode 是首次安全响应冻结的审核原因。
	ResponseReviewReasonCode *string `gorm:"column:response_review_reason_code"`
	// ResponseReviewUpdatedAt 是首次安全响应冻结的审核状态时间。
	ResponseReviewUpdatedAt *time.Time `gorm:"column:response_review_updated_at"`
	// ResponseGovernanceStatus 是首次安全响应冻结的治理状态。
	ResponseGovernanceStatus string `gorm:"column:response_governance_status"`
	// ResponseGovernanceEpoch 是治理命令首次安全响应冻结的治理纪元，其他命令为空。
	ResponseGovernanceEpoch *int64 `gorm:"column:response_governance_epoch"`
	// RequestID 是首次 HTTP 审核决定的服务端请求标识。
	RequestID *string `gorm:"column:request_id;type:uuid"`
	// CreatedAt 是首次命令提交时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回 Skill 命令回执权威表名。
func (skillCommandReceiptModel) TableName() string { return "business.skill_command_receipt" }

// skillGovernanceAuditModel 映射追加式审核治理审计表。
type skillGovernanceAuditModel struct {
	// ID 是审计 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// SkillID 是被审计 Skill。
	SkillID string `gorm:"column:skill_id;type:uuid"`
	// ReviewSubmissionID 是被决定审核提交。
	ReviewSubmissionID *string `gorm:"column:review_submission_id;type:uuid"`
	// Action 是稳定审计动作。
	Action string `gorm:"column:action"`
	// FromStatus 是动作前审核状态。
	FromStatus string `gorm:"column:from_status"`
	// ToStatus 是动作后审核状态。
	ToStatus string `gorm:"column:to_status"`
	// SafeReasonCode 是可选安全原因代码。
	SafeReasonCode *string `gorm:"column:safe_reason_code"`
	// ActorUserID 是受信 Reviewer 或 Governor。
	ActorUserID string `gorm:"column:actor_user_id;type:uuid"`
	// ActorRoleKey 是治理动作使用的权威角色，历史审核发布行为为空。
	ActorRoleKey *string `gorm:"column:actor_role_key"`
	// GovernanceEpoch 是治理动作提交后的纪元，历史审核发布行为为空。
	GovernanceEpoch *int64 `gorm:"column:governance_epoch"`
	// ApprovalReference 是治理动作关联的规范外部审批引用。
	ApprovalReference *string `gorm:"column:approval_reference"`
	// SourceAddress 是治理 HTTP 请求的规范直连 peer 地址。
	SourceAddress *string `gorm:"column:source_address;type:inet"`
	// CommandReceiptID 是治理审计唯一对应的命令回执，历史审核发布行为为空。
	CommandReceiptID *string `gorm:"column:command_receipt_id;type:uuid"`
	// RequestID 是首次 HTTP 审核决定的服务端请求标识。
	RequestID *string `gorm:"column:request_id;type:uuid"`
	// OccurredAt 是 UTC 动作时间。
	OccurredAt time.Time `gorm:"column:occurred_at"`
}

// TableName 返回 Skill 治理审计权威表名。
func (skillGovernanceAuditModel) TableName() string { return "business.skill_governance_audit" }

// jsonbValue 让 GORM 以 JSON 文本而非 bytea 参数写入 PostgreSQL jsonb，并在读取时深拷贝字节。
type jsonbValue []byte

// Value 返回合法 JSON 文本；内容已由 Skill Canonical 编码器生成，不在此层接受任意对象。
func (value jsonbValue) Value() (driver.Value, error) { return string(value), nil }

// Scan 从 PostgreSQL Driver 恢复 JSONB 文本，拒绝未知底层类型。
func (value *jsonbValue) Scan(source any) error {
	switch typed := source.(type) {
	case []byte:
		*value = append((*value)[:0], typed...)
		return nil
	case string:
		*value = append((*value)[:0], typed...)
		return nil
	default:
		return fmt.Errorf("scan skill jsonb: unsupported database value")
	}
}

// skillOwnerReadDTO 承载聚合、当前草稿、当前发布和最新审核的一次集合查询结果。
type skillOwnerReadDTO struct {
	// SkillID 是 Skill 标识。
	SkillID string `gorm:"column:skill_id"`
	// OwnerUserID 是所有者标识。
	OwnerUserID string `gorm:"column:owner_user_id"`
	// CurrentDraftRevisionID 是当前草稿指针。
	CurrentDraftRevisionID string `gorm:"column:current_draft_revision_id"`
	// CurrentPublishedSnapshotID 是当前发布指针。
	CurrentPublishedSnapshotID *string `gorm:"column:current_published_snapshot_id"`
	// PublicationRevision 是当前内部发布序号。
	PublicationRevision int64 `gorm:"column:publication_revision"`
	// GovernanceStatus 是治理状态。
	GovernanceStatus string `gorm:"column:governance_status"`
	// SkillVersion 是聚合版本。
	SkillVersion int64 `gorm:"column:skill_version"`
	// SkillCreatedAt 是聚合创建时间。
	SkillCreatedAt time.Time `gorm:"column:skill_created_at"`
	// SkillUpdatedAt 是聚合更新时间。
	SkillUpdatedAt time.Time `gorm:"column:skill_updated_at"`
	// DraftRevisionNo 是当前内容序号。
	DraftRevisionNo int64 `gorm:"column:draft_revision_no"`
	// DraftDefinitionJSON 是当前草稿结构化定义。
	DraftDefinitionJSON []byte `gorm:"column:draft_definition_json"`
	// DraftContentDigest 是当前草稿摘要。
	DraftContentDigest []byte `gorm:"column:draft_content_digest"`
	// DraftCreatedByUserID 是草稿修订创建者。
	DraftCreatedByUserID string `gorm:"column:draft_created_by_user_id"`
	// DraftCreatedAt 是草稿修订创建时间。
	DraftCreatedAt time.Time `gorm:"column:draft_created_at"`
	// PublishedID 是当前发布快照标识。
	PublishedID *string `gorm:"column:published_id"`
	// PublishedSourceRevisionID 是发布来源修订。
	PublishedSourceRevisionID *string `gorm:"column:published_source_revision_id"`
	// PublishedReviewID 是发布来源审核提交。
	PublishedReviewID *string `gorm:"column:published_review_id"`
	// PublishedDefinitionJSON 是当前发布结构化定义。
	PublishedDefinitionJSON []byte `gorm:"column:published_definition_json"`
	// PublishedContentDigest 是当前发布摘要。
	PublishedContentDigest []byte `gorm:"column:published_content_digest"`
	// PublishedByUserID 是发布 Reviewer。
	PublishedByUserID *string `gorm:"column:published_by_user_id"`
	// PublishedAt 是当前发布时间。
	PublishedAt *time.Time `gorm:"column:published_at"`
	// LatestReviewID 是最新审核提交标识。
	LatestReviewID *string `gorm:"column:latest_review_id"`
	// LatestReviewRevisionID 是最新审核内容修订。
	LatestReviewRevisionID *string `gorm:"column:latest_review_revision_id"`
	// LatestReviewContentDigest 是最新审核摘要。
	LatestReviewContentDigest []byte `gorm:"column:latest_review_content_digest"`
	// LatestReviewStatus 是最新审核状态。
	LatestReviewStatus *string `gorm:"column:latest_review_status"`
	// LatestReviewReasonCode 是最新安全原因。
	LatestReviewReasonCode *string `gorm:"column:latest_review_reason_code"`
	// LatestReviewVersion 是最新审核版本。
	LatestReviewVersion *int64 `gorm:"column:latest_review_version"`
	// LatestReviewSubmittedBy 是最新审核提交者。
	LatestReviewSubmittedBy *string `gorm:"column:latest_review_submitted_by"`
	// LatestReviewDecidedBy 是最新审核决定者。
	LatestReviewDecidedBy *string `gorm:"column:latest_review_decided_by"`
	// LatestReviewSubmittedAt 是最新审核提交时间。
	LatestReviewSubmittedAt *time.Time `gorm:"column:latest_review_submitted_at"`
	// LatestReviewDecidedAt 是最新审核决定时间。
	LatestReviewDecidedAt *time.Time `gorm:"column:latest_review_decided_at"`
	// LatestReviewUpdatedAt 是最新审核更新时间。
	LatestReviewUpdatedAt *time.Time `gorm:"column:latest_review_updated_at"`
}

// skillReviewDecisionReadDTO 在一个锁定查询中恢复审核、来源修订和 Skill CAS 所需事实。
type skillReviewDecisionReadDTO struct {
	// ReviewID 是待决定审核标识。
	ReviewID string `gorm:"column:review_id"`
	// SkillID 是所属 Skill。
	SkillID string `gorm:"column:skill_id"`
	// OwnerUserID 是 Skill Owner。
	OwnerUserID string `gorm:"column:owner_user_id"`
	// ContentRevisionID 是审核来源修订。
	ContentRevisionID string `gorm:"column:content_revision_id"`
	// ReviewContentDigest 是提交时摘要。
	ReviewContentDigest []byte `gorm:"column:review_content_digest"`
	// ReviewStatus 是当前审核状态。
	ReviewStatus string `gorm:"column:review_status"`
	// ReviewVersion 是审核 CAS 版本。
	ReviewVersion int64 `gorm:"column:review_version"`
	// SubmittedByUserID 是原提交 Owner。
	SubmittedByUserID string `gorm:"column:submitted_by_user_id"`
	// SubmittedAt 是原提交时间。
	SubmittedAt time.Time `gorm:"column:submitted_at"`
	// RevisionNo 是内容内部序号。
	RevisionNo int64 `gorm:"column:revision_no"`
	// DefinitionSchemaVersion 是内容定义版本。
	DefinitionSchemaVersion string `gorm:"column:definition_schema_version"`
	// DefinitionJSON 是审核来源结构化定义。
	DefinitionJSON []byte `gorm:"column:definition_json"`
	// RevisionContentDigest 是内容修订摘要。
	RevisionContentDigest []byte `gorm:"column:revision_content_digest"`
	// RevisionCreatedAt 是内容修订创建时间。
	RevisionCreatedAt time.Time `gorm:"column:revision_created_at"`
	// CurrentPublishedSnapshotID 是旧发布指针。
	CurrentPublishedSnapshotID *string `gorm:"column:current_published_snapshot_id"`
	// PublicationRevision 是旧内部发布序号。
	PublicationRevision int64 `gorm:"column:publication_revision"`
	// GovernanceStatus 是当前治理状态。
	GovernanceStatus string `gorm:"column:governance_status"`
	// SkillVersion 是聚合 CAS 版本。
	SkillVersion int64 `gorm:"column:skill_version"`
}

// skillReviewQueueReadDTO 是 frozen revision 派生的 Reviewer 队列行。
type skillReviewQueueReadDTO struct {
	ReviewID                string    `gorm:"column:review_id"`
	SkillID                 string    `gorm:"column:skill_id"`
	Name                    string    `gorm:"column:name"`
	Summary                 string    `gorm:"column:summary"`
	Category                string    `gorm:"column:category"`
	Status                  string    `gorm:"column:status"`
	SubmittedAt             time.Time `gorm:"column:submitted_at"`
	ReviewContentDigest     []byte    `gorm:"column:review_content_digest"`
	RevisionContentDigest   []byte    `gorm:"column:revision_content_digest"`
	DefinitionSchemaVersion string    `gorm:"column:definition_schema_version"`
}

// skillReviewDetailReadDTO 是 Review、冻结修订与当前发布的一次集合查询行。
type skillReviewDetailReadDTO struct {
	ReviewID                         string     `gorm:"column:review_id"`
	SkillID                          string     `gorm:"column:skill_id"`
	JoinedSkillID                    *string    `gorm:"column:joined_skill_id"`
	OwnerUserID                      *string    `gorm:"column:owner_user_id"`
	CurrentPublishedSnapshotID       *string    `gorm:"column:current_published_snapshot_id"`
	ContentRevisionID                string     `gorm:"column:content_revision_id"`
	JoinedContentRevisionID          *string    `gorm:"column:joined_content_revision_id"`
	ReviewContentDigest              []byte     `gorm:"column:review_content_digest"`
	ReviewStatus                     string     `gorm:"column:review_status"`
	ReviewVersion                    int64      `gorm:"column:review_version"`
	SafeReasonCode                   *string    `gorm:"column:safe_reason_code"`
	SubmittedByUserID                string     `gorm:"column:submitted_by_user_id"`
	DecidedByUserID                  *string    `gorm:"column:decided_by_user_id"`
	SubmittedAt                      time.Time  `gorm:"column:submitted_at"`
	DecidedAt                        *time.Time `gorm:"column:decided_at"`
	UpdatedAt                        time.Time  `gorm:"column:updated_at"`
	RevisionNo                       *int64     `gorm:"column:revision_no"`
	DefinitionSchemaVersion          *string    `gorm:"column:definition_schema_version"`
	DefinitionJSON                   []byte     `gorm:"column:definition_json"`
	RevisionContentDigest            []byte     `gorm:"column:revision_content_digest"`
	RevisionCreatedByUserID          *string    `gorm:"column:revision_created_by_user_id"`
	RevisionCreatedAt                *time.Time `gorm:"column:revision_created_at"`
	PublishedID                      *string    `gorm:"column:published_id"`
	PublishedSourceRevisionID        *string    `gorm:"column:published_source_revision_id"`
	PublishedReviewID                *string    `gorm:"column:published_review_id"`
	PublishedRevision                *int64     `gorm:"column:published_revision"`
	PublishedDefinitionSchemaVersion *string    `gorm:"column:published_definition_schema_version"`
	PublishedDefinitionJSON          []byte     `gorm:"column:published_definition_json"`
	PublishedContentDigest           []byte     `gorm:"column:published_content_digest"`
	PublishedByUserID                *string    `gorm:"column:published_by_user_id"`
	PublishedAt                      *time.Time `gorm:"column:published_at"`
}

// skillFrozenReceiptReadDTO 承载命令回执引用的不可变草稿、发布和冻结状态，不复制完整定义到回执。
type skillFrozenReceiptReadDTO struct {
	// SkillID 是结果 Skill。
	SkillID string `gorm:"column:skill_id"`
	// OwnerUserID 是结果 Skill Owner。
	OwnerUserID string `gorm:"column:owner_user_id"`
	// SkillCreatedAt 是聚合创建时间。
	SkillCreatedAt time.Time `gorm:"column:skill_created_at"`
	// DraftRevisionID 是首次响应草稿修订。
	DraftRevisionID string `gorm:"column:draft_revision_id"`
	// DraftRevisionNo 是首次响应草稿内部序号。
	DraftRevisionNo int64 `gorm:"column:draft_revision_no"`
	// DraftDefinitionJSON 是首次响应草稿结构化定义。
	DraftDefinitionJSON []byte `gorm:"column:draft_definition_json"`
	// DraftContentDigest 是首次响应草稿摘要。
	DraftContentDigest []byte `gorm:"column:draft_content_digest"`
	// DraftCreatedByUserID 是草稿修订创建者。
	DraftCreatedByUserID string `gorm:"column:draft_created_by_user_id"`
	// DraftCreatedAt 是草稿修订创建时间。
	DraftCreatedAt time.Time `gorm:"column:draft_created_at"`
	// PublishedID 是首次响应的发布快照。
	PublishedID *string `gorm:"column:published_id"`
	// PublishedSourceRevisionID 是发布来源修订。
	PublishedSourceRevisionID *string `gorm:"column:published_source_revision_id"`
	// PublishedReviewID 是发布来源审核。
	PublishedReviewID *string `gorm:"column:published_review_id"`
	// PublishedRevision 是发布内部序号。
	PublishedRevision *int64 `gorm:"column:published_revision"`
	// PublishedDefinitionJSON 是发布结构化定义。
	PublishedDefinitionJSON []byte `gorm:"column:published_definition_json"`
	// PublishedContentDigest 是发布内容摘要。
	PublishedContentDigest []byte `gorm:"column:published_content_digest"`
	// PublishedByUserID 是发布 Reviewer。
	PublishedByUserID *string `gorm:"column:published_by_user_id"`
	// PublishedAt 是发布时间。
	PublishedAt *time.Time `gorm:"column:published_at"`
}

// skillModelFromEntity 显式映射聚合根。
func skillModelFromEntity(entity skill.Skill) skillModel {
	return skillModel{
		ID: entity.ID, OwnerUserID: entity.OwnerUserID, CurrentDraftRevisionID: entity.CurrentDraftRevisionID,
		CurrentPublishedSnapshotID: cloneStringPointer(entity.CurrentPublishedSnapshotID), PublicationRevision: entity.PublicationRevision,
		GovernanceStatus: string(entity.GovernanceStatus), Version: entity.Version,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
}

// skillContentRevisionModelFromEntity 显式映射不可变内容修订。
func skillContentRevisionModelFromEntity(entity skill.ContentRevision) skillContentRevisionModel {
	return skillContentRevisionModel{
		ID: entity.ID, SkillID: entity.SkillID, RevisionNo: entity.RevisionNo,
		DefinitionSchemaVersion: skill.DefinitionSchemaVersionV1,
		DefinitionJSON:          append(jsonbValue(nil), entity.CanonicalJSON...), ContentDigest: digestBytes(entity.ContentDigest),
		CreatedByUserID: entity.CreatedByUserID, CreatedAt: entity.CreatedAt,
	}
}

// skillReviewSubmissionModelFromEntity 显式映射审核提交。
func skillReviewSubmissionModelFromEntity(entity skill.ReviewSubmission) skillReviewSubmissionModel {
	return skillReviewSubmissionModel{
		ID: entity.ID, SkillID: entity.SkillID, ContentRevisionID: entity.ContentRevisionID,
		ContentDigest: digestBytes(entity.ContentDigest), Status: string(entity.Status),
		SafeReasonCode: cloneStringPointer(entity.SafeReasonCode), Version: entity.Version,
		SubmittedByUserID: entity.SubmittedByUserID, DecidedByUserID: cloneStringPointer(entity.DecidedByUserID),
		SubmittedAt: entity.SubmittedAt, DecidedAt: cloneTimePointer(entity.DecidedAt), UpdatedAt: entity.UpdatedAt,
	}
}

// skillCommandReceiptModelFromEntity 显式映射安全回执引用和两个摘要。
func skillCommandReceiptModelFromEntity(entity skill.CommandReceipt) skillCommandReceiptModel {
	return skillCommandReceiptModel{
		ID: entity.ID, ActorUserID: entity.ActorUserID, CommandType: entity.CommandType, ScopeID: entity.ScopeID,
		KeyDigest: digestBytes(entity.KeyDigest), SemanticDigest: digestBytes(entity.SemanticDigest),
		ResultSkillID: entity.ResultSkillID, ResultContentRevisionID: cloneStringPointer(entity.ResultContentRevisionID),
		ResultReviewSubmissionID:    cloneStringPointer(entity.ResultReviewSubmissionID),
		ResultPublishedSnapshotID:   cloneStringPointer(entity.ResultPublishedSnapshotID),
		ResponseDraftRevisionID:     entity.ResponseDraftRevisionID,
		ResponsePublishedSnapshotID: cloneStringPointer(entity.ResponsePublishedSnapshotID),
		ResponseReviewSubmissionID:  cloneStringPointer(entity.ResponseReviewSubmissionID),
		ResponseReviewStatus:        reviewStatusStringPointer(entity.ResponseReviewStatus),
		ResponseReviewReasonCode:    cloneStringPointer(entity.ResponseReviewReasonCode),
		ResponseReviewUpdatedAt:     cloneTimePointer(entity.ResponseReviewUpdatedAt),
		ResponseGovernanceStatus:    string(entity.ResponseGovernanceStatus), RequestID: cloneStringPointer(entity.RequestID), CreatedAt: entity.CreatedAt,
	}
}

// reviewDetailFromReadDTO 重算 submitted/current published 的 Canonical 摘要并核对全部逻辑关联。
func reviewDetailFromReadDTO(record skillReviewDetailReadDTO) (skill.ReviewDetail, error) {
	// Review 是查询锚点；Skill、冻结修订或当前发布指针任一悬空都属于存储损坏，不能伪装成 404 或“尚未发布”。
	if record.JoinedSkillID == nil || *record.JoinedSkillID != record.SkillID || record.OwnerUserID == nil || *record.OwnerUserID == "" ||
		record.JoinedContentRevisionID == nil || *record.JoinedContentRevisionID != record.ContentRevisionID ||
		record.RevisionNo == nil || record.DefinitionSchemaVersion == nil || record.RevisionCreatedByUserID == nil ||
		record.RevisionCreatedAt == nil {
		return skill.ReviewDetail{}, skill.ErrPersistence
	}
	definition, digest, err := skill.DefinitionFromCanonicalV1(record.DefinitionJSON)
	if err != nil || *record.DefinitionSchemaVersion != skill.DefinitionSchemaVersionV1 ||
		!equalDigestBytes(record.ReviewContentDigest, digest) || !equalDigestBytes(record.RevisionContentDigest, digest) {
		return skill.ReviewDetail{}, skill.ErrPersistence
	}
	status := skill.ReviewStatus(record.ReviewStatus)
	if status != skill.ReviewStatusReviewing && status != skill.ReviewStatusApproved && status != skill.ReviewStatusRejected && status != skill.ReviewStatusWithdrawn {
		return skill.ReviewDetail{}, skill.ErrPersistence
	}
	detail := skill.ReviewDetail{
		Review: skill.ReviewSubmission{
			ID: record.ReviewID, SkillID: record.SkillID, ContentRevisionID: record.ContentRevisionID,
			ContentDigest: digest, Status: status, SafeReasonCode: cloneStringPointer(record.SafeReasonCode), Version: record.ReviewVersion,
			SubmittedByUserID: record.SubmittedByUserID, DecidedByUserID: cloneStringPointer(record.DecidedByUserID),
			SubmittedAt: record.SubmittedAt, DecidedAt: cloneTimePointer(record.DecidedAt), UpdatedAt: record.UpdatedAt,
		},
		OwnerUserID: *record.OwnerUserID, Definition: definition,
	}
	if record.PublishedID == nil {
		if record.CurrentPublishedSnapshotID != nil || record.PublishedSourceRevisionID != nil || record.PublishedReviewID != nil ||
			record.PublishedRevision != nil || record.PublishedDefinitionSchemaVersion != nil ||
			record.PublishedByUserID != nil || record.PublishedAt != nil || len(record.PublishedDefinitionJSON) != 0 || len(record.PublishedContentDigest) != 0 {
			return skill.ReviewDetail{}, skill.ErrPersistence
		}
		return detail, nil
	}
	if record.CurrentPublishedSnapshotID == nil || *record.CurrentPublishedSnapshotID != *record.PublishedID ||
		record.PublishedSourceRevisionID == nil || record.PublishedReviewID == nil || record.PublishedRevision == nil ||
		record.PublishedDefinitionSchemaVersion == nil || record.PublishedByUserID == nil || record.PublishedAt == nil ||
		*record.PublishedDefinitionSchemaVersion != skill.DefinitionSchemaVersionV1 {
		return skill.ReviewDetail{}, skill.ErrPersistence
	}
	publishedDefinition, publishedDigest, err := skill.DefinitionFromCanonicalV1(record.PublishedDefinitionJSON)
	if err != nil || !equalDigestBytes(record.PublishedContentDigest, publishedDigest) {
		return skill.ReviewDetail{}, skill.ErrPersistence
	}
	detail.CurrentPublished = &skill.PublishedSnapshot{
		ID: *record.PublishedID, SkillID: record.SkillID, SourceContentRevisionID: *record.PublishedSourceRevisionID,
		ReviewSubmissionID: *record.PublishedReviewID, PublicationRevision: *record.PublishedRevision,
		Definition: publishedDefinition, CanonicalJSON: append([]byte(nil), record.PublishedDefinitionJSON...),
		ContentDigest: publishedDigest, PublishedByUserID: *record.PublishedByUserID, PublishedAt: *record.PublishedAt,
	}
	return detail, nil
}

func decisionResultFromReceipt(record skillCommandReceiptModel) (skill.ReviewDecisionResult, error) {
	if record.ResultSkillID == "" || record.ResultReviewSubmissionID == nil || record.ResultPublishedSnapshotID == nil ||
		*record.ResultReviewSubmissionID != record.ScopeID ||
		record.ResponseReviewStatus == nil || *record.ResponseReviewStatus != string(skill.ReviewStatusApproved) ||
		record.ResponseReviewUpdatedAt == nil {
		return skill.ReviewDecisionResult{}, skill.ErrPersistence
	}
	return skill.ReviewDecisionResult{
		ReviewID: *record.ResultReviewSubmissionID, SkillID: record.ResultSkillID, Status: skill.ReviewStatusApproved,
		PublishedSnapshotID: *record.ResultPublishedSnapshotID, DecidedAt: record.ResponseReviewUpdatedAt.UTC(),
	}, nil
}

// ownerStateFromReadDTO 将一次 JOIN 结果恢复为领域投影并重算草稿和发布摘要。
func ownerStateFromReadDTO(record skillOwnerReadDTO) (skill.OwnerState, error) {
	draftDefinition, draftDigest, err := skill.DefinitionFromCanonicalV1(record.DraftDefinitionJSON)
	if err != nil {
		return skill.OwnerState{}, skill.ErrPersistence
	}
	storedDraftDigest, err := digestFromBytes(record.DraftContentDigest)
	if err != nil || storedDraftDigest != draftDigest {
		return skill.OwnerState{}, skill.ErrPersistence
	}
	state := skill.OwnerState{
		Skill: skill.Skill{
			ID: record.SkillID, OwnerUserID: record.OwnerUserID, CurrentDraftRevisionID: record.CurrentDraftRevisionID,
			CurrentPublishedSnapshotID: cloneStringPointer(record.CurrentPublishedSnapshotID),
			PublicationRevision:        record.PublicationRevision, GovernanceStatus: skill.GovernanceStatus(record.GovernanceStatus),
			Version: record.SkillVersion, CreatedAt: record.SkillCreatedAt, UpdatedAt: record.SkillUpdatedAt,
		},
		Draft: skill.ContentRevision{
			ID: record.CurrentDraftRevisionID, SkillID: record.SkillID, RevisionNo: record.DraftRevisionNo,
			Definition: draftDefinition, CanonicalJSON: append([]byte(nil), record.DraftDefinitionJSON...), ContentDigest: draftDigest,
			CreatedByUserID: record.DraftCreatedByUserID, CreatedAt: record.DraftCreatedAt,
		},
	}
	if record.PublishedID != nil {
		if record.PublishedSourceRevisionID == nil || record.PublishedReviewID == nil || record.PublishedByUserID == nil || record.PublishedAt == nil {
			return skill.OwnerState{}, skill.ErrPersistence
		}
		publishedDefinition, publishedDigest, parseErr := skill.DefinitionFromCanonicalV1(record.PublishedDefinitionJSON)
		storedPublishedDigest, digestErr := digestFromBytes(record.PublishedContentDigest)
		if parseErr != nil || digestErr != nil || storedPublishedDigest != publishedDigest {
			return skill.OwnerState{}, skill.ErrPersistence
		}
		state.Published = &skill.PublishedSnapshot{
			ID: *record.PublishedID, SkillID: record.SkillID, SourceContentRevisionID: *record.PublishedSourceRevisionID,
			ReviewSubmissionID: *record.PublishedReviewID, PublicationRevision: record.PublicationRevision,
			Definition: publishedDefinition, CanonicalJSON: append([]byte(nil), record.PublishedDefinitionJSON...),
			ContentDigest: publishedDigest, PublishedByUserID: *record.PublishedByUserID, PublishedAt: *record.PublishedAt,
		}
	}
	if record.LatestReviewID != nil {
		if record.LatestReviewRevisionID == nil || record.LatestReviewStatus == nil || record.LatestReviewVersion == nil ||
			record.LatestReviewSubmittedBy == nil || record.LatestReviewSubmittedAt == nil || record.LatestReviewUpdatedAt == nil {
			return skill.OwnerState{}, skill.ErrPersistence
		}
		reviewDigest, digestErr := digestFromBytes(record.LatestReviewContentDigest)
		if digestErr != nil {
			return skill.OwnerState{}, skill.ErrPersistence
		}
		state.LatestReview = &skill.ReviewSubmission{
			ID: *record.LatestReviewID, SkillID: record.SkillID, ContentRevisionID: *record.LatestReviewRevisionID,
			ContentDigest: reviewDigest, Status: skill.ReviewStatus(*record.LatestReviewStatus),
			SafeReasonCode: cloneStringPointer(record.LatestReviewReasonCode), Version: *record.LatestReviewVersion,
			SubmittedByUserID: *record.LatestReviewSubmittedBy, DecidedByUserID: cloneStringPointer(record.LatestReviewDecidedBy),
			SubmittedAt: *record.LatestReviewSubmittedAt, DecidedAt: cloneTimePointer(record.LatestReviewDecidedAt),
			UpdatedAt: *record.LatestReviewUpdatedAt,
		}
		if state.LatestReview.Status != skill.ReviewStatusReviewing && state.LatestReview.Status != skill.ReviewStatusApproved &&
			state.LatestReview.Status != skill.ReviewStatusRejected && state.LatestReview.Status != skill.ReviewStatusWithdrawn {
			return skill.OwnerState{}, skill.ErrPersistence
		}
	}
	if state.Skill.GovernanceStatus != skill.GovernanceStatusActive && state.Skill.GovernanceStatus != skill.GovernanceStatusSuspended && state.Skill.GovernanceStatus != skill.GovernanceStatusOffline {
		return skill.OwnerState{}, skill.ErrPersistence
	}
	if state.Skill.PublicationRevision == 0 && state.Published != nil || state.Skill.PublicationRevision > 0 && state.Published == nil {
		return skill.OwnerState{}, skill.ErrPersistence
	}
	return state, nil
}

// frozenOwnerStateFromReceipt 通过不可变内容引用和回执小型状态字段重建首次安全响应。
func frozenOwnerStateFromReceipt(record skillFrozenReceiptReadDTO, receipt skillCommandReceiptModel) (skill.OwnerState, error) {
	draftDefinition, draftDigest, err := skill.DefinitionFromCanonicalV1(record.DraftDefinitionJSON)
	if err != nil || !equalDigestBytes(record.DraftContentDigest, draftDigest) {
		return skill.OwnerState{}, skill.ErrPersistence
	}
	updatedAt := receipt.CreatedAt
	if receipt.ResponseReviewUpdatedAt != nil {
		updatedAt = *receipt.ResponseReviewUpdatedAt
	}
	state := skill.OwnerState{
		Skill: skill.Skill{
			ID: record.SkillID, OwnerUserID: record.OwnerUserID, CurrentDraftRevisionID: record.DraftRevisionID,
			CurrentPublishedSnapshotID: cloneStringPointer(receipt.ResponsePublishedSnapshotID),
			GovernanceStatus:           skill.GovernanceStatus(receipt.ResponseGovernanceStatus), Version: 1,
			CreatedAt: record.SkillCreatedAt, UpdatedAt: updatedAt,
		},
		Draft: skill.ContentRevision{
			ID: record.DraftRevisionID, SkillID: record.SkillID, RevisionNo: record.DraftRevisionNo,
			Definition: draftDefinition, CanonicalJSON: append([]byte(nil), record.DraftDefinitionJSON...),
			ContentDigest: draftDigest, CreatedByUserID: record.DraftCreatedByUserID, CreatedAt: record.DraftCreatedAt,
		},
	}
	if (receipt.ResponsePublishedSnapshotID == nil) != (record.PublishedID == nil) {
		return skill.OwnerState{}, skill.ErrPersistence
	}
	if record.PublishedID != nil {
		if record.PublishedSourceRevisionID == nil || record.PublishedReviewID == nil || record.PublishedRevision == nil ||
			record.PublishedByUserID == nil || record.PublishedAt == nil {
			return skill.OwnerState{}, skill.ErrPersistence
		}
		publishedDefinition, publishedDigest, parseErr := skill.DefinitionFromCanonicalV1(record.PublishedDefinitionJSON)
		if parseErr != nil || !equalDigestBytes(record.PublishedContentDigest, publishedDigest) {
			return skill.OwnerState{}, skill.ErrPersistence
		}
		state.Skill.PublicationRevision = *record.PublishedRevision
		state.Published = &skill.PublishedSnapshot{
			ID: *record.PublishedID, SkillID: record.SkillID, SourceContentRevisionID: *record.PublishedSourceRevisionID,
			ReviewSubmissionID: *record.PublishedReviewID, PublicationRevision: *record.PublishedRevision,
			Definition: publishedDefinition, CanonicalJSON: append([]byte(nil), record.PublishedDefinitionJSON...),
			ContentDigest: publishedDigest, PublishedByUserID: *record.PublishedByUserID, PublishedAt: *record.PublishedAt,
		}
	}
	if receipt.ResponseReviewSubmissionID != nil {
		if receipt.ResponseReviewStatus == nil || receipt.ResponseReviewUpdatedAt == nil {
			return skill.OwnerState{}, skill.ErrPersistence
		}
		status := skill.ReviewStatus(*receipt.ResponseReviewStatus)
		if status != skill.ReviewStatusReviewing && status != skill.ReviewStatusApproved && status != skill.ReviewStatusRejected && status != skill.ReviewStatusWithdrawn {
			return skill.OwnerState{}, skill.ErrPersistence
		}
		state.LatestReview = &skill.ReviewSubmission{
			ID: *receipt.ResponseReviewSubmissionID, SkillID: record.SkillID, Status: status,
			SafeReasonCode: cloneStringPointer(receipt.ResponseReviewReasonCode), Version: 1,
			SubmittedByUserID: record.OwnerUserID, SubmittedAt: *receipt.ResponseReviewUpdatedAt,
			UpdatedAt: *receipt.ResponseReviewUpdatedAt,
		}
	}
	if state.Skill.GovernanceStatus != skill.GovernanceStatusActive && state.Skill.GovernanceStatus != skill.GovernanceStatusSuspended && state.Skill.GovernanceStatus != skill.GovernanceStatusOffline {
		return skill.OwnerState{}, skill.ErrPersistence
	}
	return state, nil
}

// digestBytes 深拷贝固定摘要为 GORM bytea 值。
func digestBytes(digest skill.Digest) []byte { return append([]byte(nil), digest[:]...) }

// digestFromBytes 将数据库 bytea 恢复为固定摘要并拒绝长度异常。
func digestFromBytes(value []byte) (skill.Digest, error) {
	var digest skill.Digest
	if len(value) != len(digest) {
		return skill.Digest{}, skill.ErrPersistence
	}
	copy(digest[:], value)
	return digest, nil
}

// equalDigestBytes 以固定长度字节比较数据库摘要和领域摘要。
func equalDigestBytes(value []byte, digest skill.Digest) bool { return bytes.Equal(value, digest[:]) }

// cloneStringPointer 深拷贝可选字符串，避免 GORM Model 与领域实体共享可变指针。
func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// reviewStatusStringPointer 把可选领域审核状态映射为数据库稳定字符串副本。
func reviewStatusStringPointer(value *skill.ReviewStatus) *string {
	if value == nil {
		return nil
	}
	converted := string(*value)
	return &converted
}

// validateCanonicalRevision 在写事务前重算定义摘要，禁止 Repository 接受半成品修订。
func validateCanonicalRevision(revision skill.ContentRevision) error {
	definition, digest, err := skill.DefinitionFromCanonicalV1(revision.CanonicalJSON)
	if err != nil || digest != revision.ContentDigest || definition.SchemaVersion != revision.Definition.SchemaVersion {
		return fmt.Errorf("validate skill content revision: %w", skill.ErrInvalidDefinition)
	}
	return nil
}
