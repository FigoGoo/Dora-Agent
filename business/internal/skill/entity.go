package skill

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrSkillNotFound 表示 Skill 不存在或不属于当前可信 Owner，避免泄露他人资源存在性。
	ErrSkillNotFound = errors.New("skill not found")
	// ErrDraftConflict 表示 opaque ETag 已过期或草稿 CAS 被并发更新击败。
	ErrDraftConflict = errors.New("skill draft conflict")
	// ErrReviewConflict 表示审核状态、提交内容或发布 CAS 不允许当前操作。
	ErrReviewConflict = errors.New("skill review conflict")
	// ErrReviewNotFound 表示合法审核标识不存在。
	ErrReviewNotFound = errors.New("skill review not found")
	// ErrIdempotencyConflict 表示同一幂等作用域和 Key 已绑定不同业务语义。
	ErrIdempotencyConflict = errors.New("skill idempotency conflict")
	// ErrInvalidIdempotencyKey 表示幂等键为空、过长或包含不安全字符。
	ErrInvalidIdempotencyKey = errors.New("invalid skill idempotency key")
	// ErrInvalidCursor 表示 Owner 列表游标格式或内容不合法。
	ErrInvalidCursor = errors.New("invalid skill cursor")
	// ErrInvalidReviewRequest 表示 Reviewer Query、路径、决定或 strong ETag 格式非法。
	ErrInvalidReviewRequest = errors.New("invalid skill review request")
	// ErrReviewCapabilityRequired 表示内部 Principal 未持有 skill.review，禁止执行审核决定。
	ErrReviewCapabilityRequired = errors.New("skill review capability required")
	// ErrPersistence 表示 Skill 持久化暂不可用，外层不得暴露数据库原错。
	ErrPersistence = errors.New("skill persistence unavailable")
)

const (
	// CommandTypeCreate 是创建 Skill 草稿的幂等命令类型。
	CommandTypeCreate = "create"
	// CommandTypeSubmitReview 是提交当前草稿审核的幂等命令类型。
	CommandTypeSubmitReview = "submit_review"
	// CommandTypeApproveAndPublish 是内部 Reviewer 审核通过并发布的幂等命令类型。
	CommandTypeApproveAndPublish = "approve_and_publish"
	// ReviewCapability 是受信 Reviewer 必须持有的正式能力键。
	ReviewCapability = "skill.review"
)

// GovernanceStatus 是独立于内容和审核状态的 Skill 可用性。
type GovernanceStatus string

const (
	// GovernanceStatusActive 表示 Skill 治理状态可用。
	GovernanceStatusActive GovernanceStatus = "active"
	// GovernanceStatusSuspended 表示 Skill 暂停对新使用入口可见。
	GovernanceStatusSuspended GovernanceStatus = "suspended"
	// GovernanceStatusOffline 表示 Skill 已治理下线。
	GovernanceStatusOffline GovernanceStatus = "offline"
)

// ReviewStatus 是与用户可见 draft/published 内容状态独立演进的审核状态。
type ReviewStatus string

const (
	// ReviewStatusReviewing 表示精确内容修订正在审核。
	ReviewStatusReviewing ReviewStatus = "reviewing"
	// ReviewStatusApproved 表示精确内容修订已批准并原子发布。
	ReviewStatusApproved ReviewStatus = "approved"
	// ReviewStatusRejected 表示审核被拒绝。
	ReviewStatusRejected ReviewStatus = "rejected"
	// ReviewStatusWithdrawn 表示 Owner 撤回审核。
	ReviewStatusWithdrawn ReviewStatus = "withdrawn"
)

// Skill 是 Skill 聚合根的内部领域实体，不直接返回 HTTP。
type Skill struct {
	// ID 是应用生成的 UUIDv7。
	ID string
	// OwnerUserID 是来自可信 Auth Principal 的所有者标识。
	OwnerUserID string
	// CurrentDraftRevisionID 指向当前不可变草稿修订。
	CurrentDraftRevisionID string
	// CurrentPublishedSnapshotID 指向当前发布快照，尚未发布时为空。
	CurrentPublishedSnapshotID *string
	// PublicationRevision 是内部发布递增序号，不向普通用户展示。
	PublicationRevision int64
	// GovernanceStatus 是治理可用性。
	GovernanceStatus GovernanceStatus
	// Version 是聚合条件更新版本。
	Version int64
	// CreatedAt 是 UTC 创建时间。
	CreatedAt time.Time
	// UpdatedAt 是 UTC 最近更新时间。
	UpdatedAt time.Time
}

// ContentRevision 是写入后不可更新的完整结构化内容修订。
type ContentRevision struct {
	// ID 是内容修订 UUIDv7。
	ID string
	// SkillID 是所属 Skill 逻辑关联标识。
	SkillID string
	// RevisionNo 是同 Skill 内部递增内容序号。
	RevisionNo int64
	// Definition 是完成规范化的强类型结构化内容。
	Definition SkillDefinitionV1
	// CanonicalJSON 是固定字段顺序的结构化内容字节。
	CanonicalJSON []byte
	// ContentDigest 是 CanonicalJSON 的 SHA-256 摘要。
	ContentDigest Digest
	// CreatedByUserID 是创建该修订的可信用户。
	CreatedByUserID string
	// CreatedAt 是 UTC 创建时间。
	CreatedAt time.Time
}

// ReviewSubmission 冻结提交审核时的精确内容修订和摘要。
type ReviewSubmission struct {
	// ID 是审核提交 UUIDv7。
	ID string
	// SkillID 是所属 Skill 标识。
	SkillID string
	// ContentRevisionID 是被审核的不可变内容修订。
	ContentRevisionID string
	// ContentDigest 是提交时冻结的内容摘要。
	ContentDigest Digest
	// Status 是审核状态。
	Status ReviewStatus
	// SafeReasonCode 是可返回 Owner 的稳定原因，未提供时为空。
	SafeReasonCode *string
	// Version 是审核状态 CAS 版本。
	Version int64
	// SubmittedByUserID 是可信 Owner 标识。
	SubmittedByUserID string
	// DecidedByUserID 是终态 Reviewer，审核中为空。
	DecidedByUserID *string
	// SubmittedAt 是 UTC 提交时间。
	SubmittedAt time.Time
	// DecidedAt 是 UTC 决定时间，审核中为空。
	DecidedAt *time.Time
	// UpdatedAt 是 UTC 最近状态时间。
	UpdatedAt time.Time
}

// PublishedSnapshot 是审核通过事务写入的不可变发布事实。
type PublishedSnapshot struct {
	// ID 是发布快照 UUIDv7。
	ID string
	// SkillID 是所属 Skill 标识。
	SkillID string
	// SourceContentRevisionID 是发布来源内容修订。
	SourceContentRevisionID string
	// ReviewSubmissionID 是批准本次发布的审核提交。
	ReviewSubmissionID string
	// PublicationRevision 是同 Skill 内部递增发布序号。
	PublicationRevision int64
	// Definition 是批准时重新校验的结构化内容。
	Definition SkillDefinitionV1
	// CanonicalJSON 是批准时重新核对的 Canonical JSON。
	CanonicalJSON []byte
	// ContentDigest 是发布内容摘要。
	ContentDigest Digest
	// PublishedByUserID 是受信 Reviewer 标识。
	PublishedByUserID string
	// PublishedAt 是 UTC 发布时间。
	PublishedAt time.Time
}

// CommandReceipt 是不保存原始幂等键的安全命令结果引用。
type CommandReceipt struct {
	// ID 是回执 UUIDv7。
	ID string
	// ActorUserID 是可信 Owner 或 Reviewer 标识。
	ActorUserID string
	// CommandType 是冻结的命令类型。
	CommandType string
	// ScopeID 是 Owner、Skill 或 Review 幂等作用域。
	ScopeID string
	// KeyDigest 是原始幂等键的 SHA-256 摘要。
	KeyDigest Digest
	// SemanticDigest 是稳定业务语义摘要。
	SemanticDigest Digest
	// ResultSkillID 是结果 Skill 安全引用。
	ResultSkillID string
	// ResultContentRevisionID 是可选结果内容修订。
	ResultContentRevisionID *string
	// ResultReviewSubmissionID 是可选结果审核提交。
	ResultReviewSubmissionID *string
	// ResultPublishedSnapshotID 是可选结果发布快照。
	ResultPublishedSnapshotID *string
	// ResponseDraftRevisionID 是首次安全响应的不可变草稿修订引用。
	ResponseDraftRevisionID string
	// ResponsePublishedSnapshotID 是首次安全响应的发布快照引用。
	ResponsePublishedSnapshotID *string
	// ResponseReviewSubmissionID 是首次安全响应的审核提交引用。
	ResponseReviewSubmissionID *string
	// ResponseReviewStatus 是首次安全响应冻结的审核状态。
	ResponseReviewStatus *ReviewStatus
	// ResponseReviewReasonCode 是首次安全响应冻结的审核原因。
	ResponseReviewReasonCode *string
	// ResponseReviewUpdatedAt 是首次安全响应冻结的审核状态时间。
	ResponseReviewUpdatedAt *time.Time
	// ResponseGovernanceStatus 是首次安全响应冻结的治理状态。
	ResponseGovernanceStatus GovernanceStatus
	// RequestID 是首次 HTTP 审核决定的服务端 UUIDv7；Owner 命令和历史回执为空。
	RequestID *string
	// CreatedAt 是首次可靠提交时间。
	CreatedAt time.Time
}

// GovernanceAudit 是审核通过发布事务追加的安全审计记录。
type GovernanceAudit struct {
	// ID 是审计 UUIDv7。
	ID string
	// SkillID 是被审计 Skill。
	SkillID string
	// ReviewSubmissionID 是被决定的审核提交。
	ReviewSubmissionID string
	// Action 是稳定动作 review_approved_and_published。
	Action string
	// FromStatus 是动作前 reviewing。
	FromStatus ReviewStatus
	// ToStatus 是动作后 approved。
	ToStatus ReviewStatus
	// SafeReasonCode 是可选安全原因代码。
	SafeReasonCode *string
	// ActorUserID 是受信 Reviewer。
	ActorUserID string
	// RequestID 是首次 HTTP 审核决定的服务端 UUIDv7。
	RequestID *string
	// OccurredAt 是 UTC 动作时间。
	OccurredAt time.Time
}

// OwnerState 是 Repository 一次集合查询恢复的 Owner 权威内部投影。
type OwnerState struct {
	// Skill 是聚合根事实。
	Skill Skill
	// Draft 是当前不可变草稿内容。
	Draft ContentRevision
	// Published 是当前发布快照；尚未发布时为空。
	Published *PublishedSnapshot
	// LatestReview 是最新审核提交；从未提交时为空。
	LatestReview *ReviewSubmission
}

// CreateAggregate 是创建事务一次写入的 Skill、首修订和幂等回执。
type CreateAggregate struct {
	// Skill 是待创建聚合根。
	Skill Skill
	// Draft 是首个不可变内容修订。
	Draft ContentRevision
	// Receipt 是先争夺的创建幂等回执。
	Receipt CommandReceipt
}

// AppendDraftCommand 是按旧草稿指针 CAS 追加内容修订的持久化命令。
type AppendDraftCommand struct {
	// SkillID 是目标 Skill。
	SkillID string
	// OwnerUserID 是可信 Owner。
	OwnerUserID string
	// ExpectedDraftRevisionID 是 ETag 解析后当前观察到的草稿修订。
	ExpectedDraftRevisionID string
	// Draft 是待追加的新不可变内容修订。
	Draft ContentRevision
	// UpdatedAt 是聚合更新时间。
	UpdatedAt time.Time
}

// SubmitReviewAggregate 是冻结当前草稿审核的事务命令。
type SubmitReviewAggregate struct {
	// ExpectedDraftRevisionID 保证提交的是应用层刚读取的精确草稿。
	ExpectedDraftRevisionID string
	// Review 是待创建审核提交。
	Review ReviewSubmission
	// Receipt 是提交审核幂等回执。
	Receipt CommandReceipt
}

// SubmitReviewResult 是首次提交或同键重放的审核和 Owner 投影。
type SubmitReviewResult struct {
	// State 是提交事务后的 Owner 权威投影。
	State OwnerState
	// ReviewID 是首次幂等命令创建的审核提交标识。
	ReviewID string
	// IdempotentReplay 表示命中了同语义既有回执。
	IdempotentReplay bool
}

// ApproveAndPublishCommand 是受信 Reviewer 原子批准并发布的事务命令。
type ApproveAndPublishCommand struct {
	// ReviewID 是待决定审核提交。
	ReviewID string
	// ReviewerUserID 是可信 Reviewer 标识。
	ReviewerUserID string
	// SnapshotID 是待创建不可变发布快照标识。
	SnapshotID string
	// ReceiptID 是验证完成后才插入的专用决定回执标识。
	ReceiptID string
	// RequestID 是本次首次决定写入回执与审计的服务端 UUIDv7。
	RequestID string
	// KeyDigest 是原始幂等键摘要。
	KeyDigest Digest
	// SemanticDigest 覆盖 review、approved 与原样 strong If-Match。
	SemanticDigest Digest
	// IfMatch 是客户端原样提交并已完成格式校验的 strong ETag。
	IfMatch string
	// AuditID 是待追加治理审计标识。
	AuditID string
	// DecidedAt 是审核决定和发布时间。
	DecidedAt time.Time
}

// ReviewQueueBoundary 是 Reviewer 队列 oldest-first keyset 游标边界。
type ReviewQueueBoundary struct {
	SubmittedAt time.Time
	ReviewID    string
}

// ReviewQueueItem 是冻结审核修订派生的最小队列项。
type ReviewQueueItem struct {
	ReviewID    string
	SkillID     string
	Name        string
	Summary     string
	Category    string
	Status      ReviewStatus
	SubmittedAt time.Time
}

// ReviewQueuePage 是固定查询数返回的 Reviewer 队列页。
type ReviewQueuePage struct {
	Items   []ReviewQueueItem
	HasMore bool
}

// ReviewDetail 是一次集合查询恢复的冻结审核详情和当前发布对照。
type ReviewDetail struct {
	Review           ReviewSubmission
	OwnerUserID      string
	Definition       SkillDefinitionV1
	CurrentPublished *PublishedSnapshot
}

// ReviewDecisionResult 是首次决定或回执重放专用的冻结结果。
type ReviewDecisionResult struct {
	ReviewID            string
	SkillID             string
	Status              ReviewStatus
	PublishedSnapshotID string
	DecidedAt           time.Time
	IdempotentReplay    bool
}

// PageBoundary 是 Owner 列表 keyset 游标解码后的内部边界。
type PageBoundary struct {
	// UpdatedAt 是上一页最后一项聚合更新时间。
	UpdatedAt time.Time
	// SkillID 是同一时间下的稳定 UUIDv7 次级排序键。
	SkillID string
}

// OwnerPage 是固定查询数返回的 Owner Skill 页。
type OwnerPage struct {
	// Items 是当前页 Owner 投影。
	Items []OwnerState
	// HasMore 表示存在下一页。
	HasMore bool
}

// Repository 定义 Skill Owner 纵切和内部批准发布所需的最小 GORM 持久化边界。
type Repository interface {
	// Create 原子创建聚合、首修订和回执，并收敛同键并发。
	Create(ctx context.Context, aggregate CreateAggregate) (OwnerState, bool, error)
	// FindOwnedByID 以 Owner 条件读取一个 Skill，不存在和越权统一返回 ErrSkillNotFound。
	FindOwnedByID(ctx context.Context, skillID string, ownerUserID string) (OwnerState, error)
	// ListOwned 以一次集合查询读取固定数量 Owner Skill，禁止逐条加载定义或审核。
	ListOwned(ctx context.Context, ownerUserID string, boundary *PageBoundary, limit int) (OwnerPage, error)
	// AppendDraft 使用当前草稿指针 CAS，先切换指针再在同事务追加不可变修订。
	AppendDraft(ctx context.Context, command AppendDraftCommand) (OwnerState, error)
	// SubmitReview 冻结精确草稿修订并创建 reviewing 记录和幂等回执。
	SubmitReview(ctx context.Context, aggregate SubmitReviewAggregate) (SubmitReviewResult, error)
	// ListReviewQueue 一次集合查询读取 reviewing 队列，字段只来自冻结修订。
	ListReviewQueue(ctx context.Context, boundary *ReviewQueueBoundary, limit int) (ReviewQueuePage, error)
	// FindReviewDetail 一次集合查询恢复冻结审核内容与当前发布对照。
	FindReviewDetail(ctx context.Context, reviewID string) (ReviewDetail, error)
	// ApproveAndPublish 锁定审核与聚合，重校验定义并原子写发布快照、决定、指针、回执和审计。
	ApproveAndPublish(ctx context.Context, command ApproveAndPublishCommand) (ReviewDecisionResult, error)
}
