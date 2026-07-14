package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SkillRepository 使用 GORM 和 Business PostgreSQL 实现 W1 Skill Owner 与内部 Reviewer 持久化边界。
type SkillRepository struct {
	// db 只允许本 Repository 使用，领域和 HTTP 层不会接收 GORM 对象。
	db *gorm.DB
}

var _ skill.Repository = (*SkillRepository)(nil)

// NewSkillRepository 从 Business PostgreSQL Client 创建 Skill Repository，禁止 nil Client 隐式降级。
func NewSkillRepository(client *Client) (*SkillRepository, error) {
	if client == nil || client.db == nil {
		return nil, errors.New("create skill repository: postgres client is nil")
	}
	return &SkillRepository{db: client.db}, nil
}

// Create 先争夺 Owner 创建幂等回执，再原子写聚合和首个不可变内容修订。
func (r *SkillRepository) Create(ctx context.Context, aggregate skill.CreateAggregate) (skill.OwnerState, bool, error) {
	if err := validateCanonicalRevision(aggregate.Draft); err != nil {
		return skill.OwnerState{}, false, err
	}
	skillRecord := skillModelFromEntity(aggregate.Skill)
	draftRecord := skillContentRevisionModelFromEntity(aggregate.Draft)
	receiptRecord := skillCommandReceiptModelFromEntity(aggregate.Receipt)

	var state skill.OwnerState
	var replay bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claimed, existing, err := claimSkillReceipt(tx, receiptRecord)
		if err != nil {
			return err
		}
		if !claimed {
			if !bytes.Equal(existing.SemanticDigest, receiptRecord.SemanticDigest) {
				return skill.ErrIdempotencyConflict
			}
			state, err = findFrozenSkillReceiptState(tx, existing)
			replay = true
			return err
		}

		// 回执成功占有幂等作用域后，聚合与首修订必须同事务提交；任一失败都会释放回执和所有候选事实。
		transactionDB := tx.Session(&gorm.Session{SkipDefaultTransaction: true})
		if err := transactionDB.Create(&skillRecord).Error; err != nil {
			return fmt.Errorf("insert skill aggregate: %w", err)
		}
		if err := transactionDB.Create(&draftRecord).Error; err != nil {
			return fmt.Errorf("insert first skill content revision: %w", err)
		}
		state, err = findSkillOwnerState(tx, skillRecord.ID, skillRecord.OwnerUserID)
		return err
	})
	if err != nil {
		return skill.OwnerState{}, false, mapSkillRepositoryError(err)
	}
	return state, replay, nil
}

// FindOwnedByID 以 Skill ID 与可信 Owner 条件执行一次集合查询；越权与不存在统一收敛为 404 语义。
func (r *SkillRepository) FindOwnedByID(ctx context.Context, skillID string, ownerUserID string) (skill.OwnerState, error) {
	state, err := findSkillOwnerState(r.db.WithContext(ctx), skillID, ownerUserID)
	if err != nil {
		return skill.OwnerState{}, mapSkillRepositoryError(err)
	}
	return state, nil
}

// ListOwned 使用一个 JOIN/LATERAL 集合查询返回 Owner Skill 页，定义、发布和审核不会逐条查询。
func (r *SkillRepository) ListOwned(ctx context.Context, ownerUserID string, boundary *skill.PageBoundary, limit int) (skill.OwnerPage, error) {
	if limit <= 0 || limit > 100 {
		return skill.OwnerPage{}, skill.ErrPersistence
	}
	query := skillOwnerSelectSQL + `
WHERE skill_record.owner_user_id = ?`
	arguments := []any{ownerUserID}
	if boundary != nil {
		query += ` AND (skill_record.updated_at, skill_record.id) < (?, ?)`
		arguments = append(arguments, boundary.UpdatedAt, boundary.SkillID)
	}
	query += ` ORDER BY skill_record.updated_at DESC, skill_record.id DESC LIMIT ?`
	arguments = append(arguments, limit+1)

	var records []skillOwnerReadDTO
	if err := r.db.WithContext(ctx).Raw(query, arguments...).Scan(&records).Error; err != nil {
		return skill.OwnerPage{}, mapSkillRepositoryError(fmt.Errorf("list owner skills: %w", err))
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	page := skill.OwnerPage{Items: make([]skill.OwnerState, 0, len(records)), HasMore: hasMore}
	for _, record := range records {
		state, err := ownerStateFromReadDTO(record)
		if err != nil {
			return skill.OwnerPage{}, mapSkillRepositoryError(err)
		}
		page.Items = append(page.Items, state)
	}
	return page, nil
}

// AppendDraft 先以旧草稿逻辑指针 CAS 更新聚合，再在同事务追加新修订；插入失败会回滚指针。
func (r *SkillRepository) AppendDraft(ctx context.Context, command skill.AppendDraftCommand) (skill.OwnerState, error) {
	if err := validateCanonicalRevision(command.Draft); err != nil {
		return skill.OwnerState{}, err
	}
	draftRecord := skillContentRevisionModelFromEntity(command.Draft)
	var state skill.OwnerState
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		update := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&skillModel{}).
			Where("id = ? AND owner_user_id = ? AND current_draft_revision_id = ?", command.SkillID, command.OwnerUserID, command.ExpectedDraftRevisionID).
			Updates(map[string]any{
				"current_draft_revision_id": command.Draft.ID,
				"version":                   gorm.Expr("version + 1"),
				"updated_at":                command.UpdatedAt,
			})
		if update.Error != nil {
			return fmt.Errorf("CAS skill draft pointer: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			return skill.ErrDraftConflict
		}
		// 新指针和不可变内容修订在同一数据库事务中，不会向其他事务暴露悬空指针。
		if err := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Create(&draftRecord).Error; err != nil {
			return fmt.Errorf("append skill content revision: %w", err)
		}
		var err error
		state, err = findSkillOwnerState(tx, command.SkillID, command.OwnerUserID)
		return err
	})
	if err != nil {
		return skill.OwnerState{}, mapSkillRepositoryError(err)
	}
	return state, nil
}

// SubmitReview 原子争夺幂等回执、插入 reviewing 事实并按精确草稿指针 CAS 聚合。
func (r *SkillRepository) SubmitReview(ctx context.Context, aggregate skill.SubmitReviewAggregate) (skill.SubmitReviewResult, error) {
	reviewRecord := skillReviewSubmissionModelFromEntity(aggregate.Review)
	receiptRecord := skillCommandReceiptModelFromEntity(aggregate.Receipt)
	var result skill.SubmitReviewResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claimed, existing, err := claimSkillReceipt(tx, receiptRecord)
		if err != nil {
			return err
		}
		if !claimed {
			if !bytes.Equal(existing.SemanticDigest, receiptRecord.SemanticDigest) || existing.ResultReviewSubmissionID == nil {
				return skill.ErrIdempotencyConflict
			}
			state, readErr := findFrozenSkillReceiptState(tx, existing)
			if readErr != nil {
				return readErr
			}
			result = skill.SubmitReviewResult{State: state, ReviewID: *existing.ResultReviewSubmissionID, IdempotentReplay: true}
			return nil
		}
		current, readErr := findSkillOwnerState(tx, aggregate.Review.SkillID, aggregate.Review.SubmittedByUserID)
		if readErr != nil {
			return readErr
		}
		if aggregate.ExpectedDraftRevisionID == "" || current.Draft.ID != aggregate.ExpectedDraftRevisionID {
			return skill.ErrDraftConflict
		}
		if current.LatestReview != nil && current.LatestReview.Status == skill.ReviewStatusReviewing {
			return skill.ErrReviewConflict
		}
		if current.Published != nil && current.Published.ContentDigest == current.Draft.ContentDigest {
			return skill.ErrReviewConflict
		}

		// partial unique index 保证同一 Skill 只有一个 reviewing；冲突会回滚本事务回执。
		if err := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Create(&reviewRecord).Error; err != nil {
			return fmt.Errorf("insert skill review submission: %w", err)
		}
		update := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&skillModel{}).
			Where("id = ? AND owner_user_id = ? AND current_draft_revision_id = ?", aggregate.Review.SkillID, aggregate.Review.SubmittedByUserID, aggregate.ExpectedDraftRevisionID).
			Updates(map[string]any{"version": gorm.Expr("version + 1"), "updated_at": aggregate.Review.SubmittedAt})
		if update.Error != nil {
			return fmt.Errorf("CAS skill review draft: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			return skill.ErrDraftConflict
		}
		state, readErr := findSkillOwnerState(tx, aggregate.Review.SkillID, aggregate.Review.SubmittedByUserID)
		if readErr != nil {
			return readErr
		}
		// 提交事务结束前按数据库最终状态覆盖预构造回执，避免并发发布或治理变化使首次响应与重放分叉。
		if err := freezeSkillReceiptResponse(tx, receiptRecord.ID, state); err != nil {
			return err
		}
		result = skill.SubmitReviewResult{State: state, ReviewID: aggregate.Review.ID}
		return nil
	})
	if err != nil {
		return skill.SubmitReviewResult{}, mapSkillRepositoryError(err)
	}
	return result, nil
}

// ListReviewQueue 使用现有 status/time/id 索引 oldest-first 读取冻结修订摘要，禁止 N+1。
func (r *SkillRepository) ListReviewQueue(ctx context.Context, boundary *skill.ReviewQueueBoundary, limit int) (skill.ReviewQueuePage, error) {
	if limit <= 0 || limit > 100 {
		return skill.ReviewQueuePage{}, skill.ErrPersistence
	}
	query := `
SELECT
    review_record.id AS review_id,
    review_record.skill_id,
    content_record.definition_json ->> 'name' AS name,
    content_record.definition_json ->> 'summary' AS summary,
    content_record.definition_json ->> 'category' AS category,
    review_record.status,
    review_record.submitted_at,
    review_record.content_digest AS review_content_digest,
    content_record.content_digest AS revision_content_digest,
    content_record.definition_schema_version
FROM business.skill_review_submission AS review_record
JOIN business.skill_content_revision AS content_record
  ON content_record.id = review_record.content_revision_id
 AND content_record.skill_id = review_record.skill_id
WHERE review_record.status = ?`
	arguments := []any{skill.ReviewStatusReviewing}
	if boundary != nil {
		query += ` AND (review_record.submitted_at, review_record.id) > (?, ?)`
		arguments = append(arguments, boundary.SubmittedAt, boundary.ReviewID)
	}
	query += ` ORDER BY review_record.submitted_at ASC, review_record.id ASC LIMIT ?`
	arguments = append(arguments, limit+1)
	var records []skillReviewQueueReadDTO
	if err := r.db.WithContext(ctx).Raw(query, arguments...).Scan(&records).Error; err != nil {
		return skill.ReviewQueuePage{}, mapSkillRepositoryError(fmt.Errorf("list skill review queue: %w", err))
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	page := skill.ReviewQueuePage{Items: make([]skill.ReviewQueueItem, 0, len(records)), HasMore: hasMore}
	for _, record := range records {
		if record.Status != string(skill.ReviewStatusReviewing) || record.DefinitionSchemaVersion != skill.DefinitionSchemaVersionV1 ||
			len(record.ReviewContentDigest) != len(skill.Digest{}) || !bytes.Equal(record.ReviewContentDigest, record.RevisionContentDigest) {
			return skill.ReviewQueuePage{}, skill.ErrPersistence
		}
		page.Items = append(page.Items, skill.ReviewQueueItem{
			ReviewID: record.ReviewID, SkillID: record.SkillID, Name: record.Name, Summary: record.Summary,
			Category: record.Category, Status: skill.ReviewStatusReviewing, SubmittedAt: record.SubmittedAt,
		})
	}
	return page, nil
}

// FindReviewDetail 一次 JOIN 读取冻结审核 Definition 与 current published 对照。
func (r *SkillRepository) FindReviewDetail(ctx context.Context, reviewID string) (skill.ReviewDetail, error) {
	var record skillReviewDetailReadDTO
	result := r.db.WithContext(ctx).Raw(`
SELECT
    review_record.id AS review_id,
    review_record.skill_id,
	skill_record.id AS joined_skill_id,
    skill_record.owner_user_id,
	skill_record.current_published_snapshot_id,
    review_record.content_revision_id,
	content_record.id AS joined_content_revision_id,
    review_record.content_digest AS review_content_digest,
    review_record.status AS review_status,
    review_record.version AS review_version,
    review_record.safe_reason_code,
    review_record.submitted_by_user_id,
    review_record.decided_by_user_id,
    review_record.submitted_at,
    review_record.decided_at,
    review_record.updated_at,
    content_record.revision_no,
    content_record.definition_schema_version,
    content_record.definition_json,
    content_record.content_digest AS revision_content_digest,
    content_record.created_by_user_id AS revision_created_by_user_id,
    content_record.created_at AS revision_created_at,
    published_record.id AS published_id,
    published_record.source_content_revision_id AS published_source_revision_id,
    published_record.review_submission_id AS published_review_id,
    published_record.publication_revision AS published_revision,
    published_record.definition_schema_version AS published_definition_schema_version,
    published_record.definition_json AS published_definition_json,
    published_record.content_digest AS published_content_digest,
    published_record.published_by_user_id,
    published_record.published_at
FROM business.skill_review_submission AS review_record
LEFT JOIN business.skill AS skill_record ON skill_record.id = review_record.skill_id
LEFT JOIN business.skill_content_revision AS content_record
  ON content_record.id = review_record.content_revision_id
 AND content_record.skill_id = review_record.skill_id
LEFT JOIN business.skill_published_snapshot AS published_record
  ON published_record.id = skill_record.current_published_snapshot_id
 AND published_record.skill_id = skill_record.id
WHERE review_record.id = ?`, reviewID).Scan(&record)
	if result.Error != nil {
		return skill.ReviewDetail{}, mapSkillRepositoryError(fmt.Errorf("find skill review detail: %w", result.Error))
	}
	if result.RowsAffected != 1 {
		return skill.ReviewDetail{}, skill.ErrReviewNotFound
	}
	detail, err := reviewDetailFromReadDTO(record)
	if err != nil {
		return skill.ReviewDetail{}, mapSkillRepositoryError(err)
	}
	return detail, nil
}

// ApproveAndPublish 在一个事务内完成决定幂等、行锁、摘要重校验、快照、CAS、审核终态和追加审计。
func (r *SkillRepository) ApproveAndPublish(ctx context.Context, command skill.ApproveAndPublishCommand) (skill.ReviewDecisionResult, error) {
	var result skill.ReviewDecisionResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockActiveReviewerAccount(tx, command.ReviewerUserID); err != nil {
			return err
		}
		if err := lockActiveReviewerAssignment(tx, command.ReviewerUserID); err != nil {
			return err
		}

		var existing skillCommandReceiptModel
		receiptRead := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).
			Where("actor_user_id = ? AND command_type = ? AND scope_id = ? AND key_digest = ?", command.ReviewerUserID, skill.CommandTypeApproveAndPublish, command.ReviewID, digestBytes(command.KeyDigest)).
			Take(&existing)
		if receiptRead.Error == nil {
			if !bytes.Equal(existing.SemanticDigest, digestBytes(command.SemanticDigest)) {
				return skill.ErrIdempotencyConflict
			}
			var err error
			result, err = decisionResultFromReceipt(existing)
			if err == nil {
				result.IdempotentReplay = true
			}
			return err
		}
		if !errors.Is(receiptRead.Error, gorm.ErrRecordNotFound) {
			return fmt.Errorf("read skill review decision receipt: %w", receiptRead.Error)
		}

		decision, err := lockSkillReviewDecision(tx, command.ReviewID)
		if err != nil {
			return err
		}
		reviewDigest, err := digestFromBytes(decision.ReviewContentDigest)
		if err != nil {
			return skill.ErrPersistence
		}
		lockedReview := skill.ReviewSubmission{
			ID: decision.ReviewID, SkillID: decision.SkillID, ContentRevisionID: decision.ContentRevisionID,
			ContentDigest: reviewDigest, Status: skill.ReviewStatus(decision.ReviewStatus), Version: decision.ReviewVersion,
		}
		if decision.ReviewStatus != string(skill.ReviewStatusReviewing) || decision.GovernanceStatus != string(skill.GovernanceStatusActive) ||
			skill.ReviewETag(lockedReview) != command.IfMatch {
			return skill.ErrReviewConflict
		}
		definition, digest, err := skill.DefinitionFromCanonicalV1(decision.DefinitionJSON)
		if err != nil || decision.DefinitionSchemaVersion != skill.DefinitionSchemaVersionV1 ||
			!equalDigestBytes(decision.ReviewContentDigest, digest) || !equalDigestBytes(decision.RevisionContentDigest, digest) {
			return skill.ErrPersistence
		}
		canonical, recalculatedDigest, err := skill.CanonicalDefinitionV1(definition)
		if err != nil || recalculatedDigest != digest || len(canonical) > skill.MaxCanonicalDefinitionBytes {
			return skill.ErrPersistence
		}
		if len(definition.PublicToolRefs) != 0 {
			return skill.ErrReviewConflict
		}

		approvedStatus := string(skill.ReviewStatusApproved)
		reviewID := decision.ReviewID
		snapshotID := command.SnapshotID
		receiptRecord := skillCommandReceiptModel{
			ID: command.ReceiptID, ActorUserID: command.ReviewerUserID, CommandType: skill.CommandTypeApproveAndPublish,
			ScopeID: command.ReviewID, KeyDigest: digestBytes(command.KeyDigest), SemanticDigest: digestBytes(command.SemanticDigest),
			ResultSkillID: decision.SkillID, ResultContentRevisionID: stringValuePointer(decision.ContentRevisionID),
			ResultReviewSubmissionID: &reviewID, ResultPublishedSnapshotID: &snapshotID,
			ResponseDraftRevisionID: decision.ContentRevisionID, ResponsePublishedSnapshotID: &snapshotID,
			ResponseReviewSubmissionID: &reviewID, ResponseReviewStatus: &approvedStatus,
			ResponseReviewUpdatedAt: timeValuePointer(command.DecidedAt), ResponseGovernanceStatus: string(skill.GovernanceStatusActive),
			RequestID: stringValuePointer(command.RequestID), CreatedAt: command.DecidedAt,
		}
		if err := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Create(&receiptRecord).Error; err != nil {
			return fmt.Errorf("insert skill review decision receipt: %w", err)
		}

		publicationRevision := decision.PublicationRevision + 1
		snapshotRecord := skillPublishedSnapshotModel{
			ID: command.SnapshotID, SkillID: decision.SkillID, SourceContentRevisionID: decision.ContentRevisionID,
			ReviewSubmissionID: decision.ReviewID, PublicationRevision: publicationRevision,
			DefinitionSchemaVersion: skill.DefinitionSchemaVersionV1, DefinitionJSON: append(jsonbValue(nil), canonical...),
			ContentDigest: digestBytes(digest), PublishedByUserID: command.ReviewerUserID, PublishedAt: command.DecidedAt,
		}
		if err := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Create(&snapshotRecord).Error; err != nil {
			return fmt.Errorf("insert skill published snapshot: %w", err)
		}

		// 快照先写、指针后切且全部在同事务；后续审核或审计失败会回滚指针，旧快照继续生效。
		skillUpdate := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&skillModel{}).
			Where("id = ? AND version = ?", decision.SkillID, decision.SkillVersion).
			Updates(map[string]any{
				"current_published_snapshot_id": command.SnapshotID,
				"publication_revision":          publicationRevision,
				"version":                       gorm.Expr("version + 1"),
				"updated_at":                    command.DecidedAt,
			})
		if skillUpdate.Error != nil {
			return fmt.Errorf("CAS skill published pointer: %w", skillUpdate.Error)
		}
		if skillUpdate.RowsAffected != 1 {
			return skill.ErrReviewConflict
		}
		reviewUpdate := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&skillReviewSubmissionModel{}).
			Where("id = ? AND status = ? AND version = ?", decision.ReviewID, skill.ReviewStatusReviewing, decision.ReviewVersion).
			Updates(map[string]any{
				"status":             skill.ReviewStatusApproved,
				"decided_by_user_id": command.ReviewerUserID,
				"decided_at":         command.DecidedAt,
				"updated_at":         command.DecidedAt,
				"version":            gorm.Expr("version + 1"),
			})
		if reviewUpdate.Error != nil {
			return fmt.Errorf("CAS skill review approval: %w", reviewUpdate.Error)
		}
		if reviewUpdate.RowsAffected != 1 {
			return skill.ErrReviewConflict
		}
		auditRecord := skillGovernanceAuditModel{
			ID: command.AuditID, SkillID: decision.SkillID, ReviewSubmissionID: &reviewID,
			Action: "review_approved_and_published", FromStatus: string(skill.ReviewStatusReviewing),
			ToStatus: string(skill.ReviewStatusApproved), ActorUserID: command.ReviewerUserID,
			RequestID: stringValuePointer(command.RequestID), OccurredAt: command.DecidedAt,
		}
		if err := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Create(&auditRecord).Error; err != nil {
			return fmt.Errorf("append skill governance audit: %w", err)
		}
		result = skill.ReviewDecisionResult{
			ReviewID: decision.ReviewID, SkillID: decision.SkillID, Status: skill.ReviewStatusApproved,
			PublishedSnapshotID: command.SnapshotID, DecidedAt: command.DecidedAt,
		}
		return nil
	})
	if err != nil {
		return skill.ReviewDecisionResult{}, mapSkillRepositoryError(err)
	}
	return result, nil
}

func lockActiveReviewerAccount(tx *gorm.DB, reviewerUserID string) error {
	var id string
	result := tx.Raw(`SELECT id FROM business.user_account WHERE id = ? AND status = 'active' FOR UPDATE`, reviewerUserID).Scan(&id)
	if result.Error != nil {
		return fmt.Errorf("lock active reviewer account: %w", result.Error)
	}
	if result.RowsAffected != 1 || id != reviewerUserID {
		return skill.ErrReviewCapabilityRequired
	}
	return nil
}

func lockActiveReviewerAssignment(tx *gorm.DB, reviewerUserID string) error {
	var id string
	result := tx.Raw(`SELECT id FROM business.user_role_assignment WHERE user_id = ? AND role_key = 'skill_reviewer' AND status = 'active' FOR UPDATE`, reviewerUserID).Scan(&id)
	if result.Error != nil {
		return fmt.Errorf("lock active reviewer assignment: %w", result.Error)
	}
	if result.RowsAffected != 1 || id == "" {
		return skill.ErrReviewCapabilityRequired
	}
	return nil
}

// claimSkillReceipt 使用唯一作用域争夺命令执行权；未获得时返回首次回执供语义核对和安全重放。
func claimSkillReceipt(tx *gorm.DB, candidate skillCommandReceiptModel) (bool, skillCommandReceiptModel, error) {
	insert := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(&candidate)
	if insert.Error != nil {
		return false, skillCommandReceiptModel{}, fmt.Errorf("claim skill command receipt: %w", insert.Error)
	}
	if insert.RowsAffected == 1 {
		return true, skillCommandReceiptModel{}, nil
	}
	var existing skillCommandReceiptModel
	if err := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).
		Where("actor_user_id = ? AND command_type = ? AND scope_id = ? AND key_digest = ?", candidate.ActorUserID, candidate.CommandType, candidate.ScopeID, candidate.KeyDigest).
		Take(&existing).Error; err != nil {
		return false, skillCommandReceiptModel{}, fmt.Errorf("read existing skill command receipt: %w", err)
	}
	return false, existing, nil
}

// findSkillOwnerState 使用一个集合查询恢复指定 Owner 的 Skill 当前草稿、发布和最新审核。
func findSkillOwnerState(db *gorm.DB, skillID string, ownerUserID string) (skill.OwnerState, error) {
	var record skillOwnerReadDTO
	query := skillOwnerSelectSQL + ` WHERE skill_record.id = ? AND skill_record.owner_user_id = ?`
	result := db.Raw(query, skillID, ownerUserID).Scan(&record)
	if result.Error != nil {
		return skill.OwnerState{}, fmt.Errorf("find owner skill: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return skill.OwnerState{}, skill.ErrSkillNotFound
	}
	return ownerStateFromReadDTO(record)
}

// findSkillOwnerStateBySkillID 仅供已通过内部 Reviewer capability 且事务已锁定审核后读取 Owner 投影。
func findSkillOwnerStateBySkillID(db *gorm.DB, skillID string) (skill.OwnerState, error) {
	var ownerID string
	if err := db.Model(&skillModel{}).Select("owner_user_id").Where("id = ?", skillID).Scan(&ownerID).Error; err != nil {
		return skill.OwnerState{}, fmt.Errorf("find published skill owner: %w", err)
	}
	if ownerID == "" {
		return skill.OwnerState{}, skill.ErrReviewConflict
	}
	return findSkillOwnerState(db, skillID, ownerID)
}

// findFrozenSkillReceiptState 通过不可变修订/快照引用重建首次安全响应，避免后续编辑或决定改变同键重放结果。
func findFrozenSkillReceiptState(db *gorm.DB, receipt skillCommandReceiptModel) (skill.OwnerState, error) {
	if receipt.ResponseDraftRevisionID == "" {
		return skill.OwnerState{}, skill.ErrPersistence
	}
	var record skillFrozenReceiptReadDTO
	result := db.Raw(`
SELECT
    skill_record.id AS skill_id,
    skill_record.owner_user_id,
    skill_record.created_at AS skill_created_at,
    draft_record.id AS draft_revision_id,
    draft_record.revision_no AS draft_revision_no,
    draft_record.definition_json AS draft_definition_json,
    draft_record.content_digest AS draft_content_digest,
    draft_record.created_by_user_id AS draft_created_by_user_id,
    draft_record.created_at AS draft_created_at,
    published_record.id AS published_id,
    published_record.source_content_revision_id AS published_source_revision_id,
    published_record.review_submission_id AS published_review_id,
    published_record.publication_revision AS published_revision,
    published_record.definition_json AS published_definition_json,
    published_record.content_digest AS published_content_digest,
    published_record.published_by_user_id AS published_by_user_id,
    published_record.published_at AS published_at
FROM business.skill AS skill_record
JOIN business.skill_content_revision AS draft_record
  ON draft_record.id = ? AND draft_record.skill_id = skill_record.id
LEFT JOIN business.skill_published_snapshot AS published_record
  ON published_record.id = ? AND published_record.skill_id = skill_record.id
WHERE skill_record.id = ?`, receipt.ResponseDraftRevisionID, receipt.ResponsePublishedSnapshotID, receipt.ResultSkillID).Scan(&record)
	if result.Error != nil {
		return skill.OwnerState{}, fmt.Errorf("find frozen skill receipt response: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return skill.OwnerState{}, skill.ErrPersistence
	}
	return frozenOwnerStateFromReceipt(record, receipt)
}

// freezeSkillReceiptResponse 只保存不可变内容引用与小型状态，不把完整 Skill 正文复制进回执。
func freezeSkillReceiptResponse(tx *gorm.DB, receiptID string, state skill.OwnerState) error {
	var publishedID *string
	if state.Published != nil {
		publishedID = stringValuePointer(state.Published.ID)
	}
	var reviewID *string
	var reviewStatus *string
	var reviewReason *string
	var reviewUpdatedAt *time.Time
	if state.LatestReview != nil {
		reviewID = stringValuePointer(state.LatestReview.ID)
		status := string(state.LatestReview.Status)
		reviewStatus = &status
		reviewReason = cloneStringPointer(state.LatestReview.SafeReasonCode)
		reviewUpdatedAt = timeValuePointer(state.LatestReview.UpdatedAt)
	}
	update := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Model(&skillCommandReceiptModel{}).
		Where("id = ?", receiptID).
		Updates(map[string]any{
			"response_draft_revision_id":     state.Draft.ID,
			"response_published_snapshot_id": publishedID,
			"response_review_submission_id":  reviewID,
			"response_review_status":         reviewStatus,
			"response_review_reason_code":    reviewReason,
			"response_review_updated_at":     reviewUpdatedAt,
			"response_governance_status":     state.Skill.GovernanceStatus,
		})
	if update.Error != nil {
		return fmt.Errorf("freeze skill command response: %w", update.Error)
	}
	if update.RowsAffected != 1 {
		return skill.ErrPersistence
	}
	return nil
}

// lockSkillReviewDecision 通过一个 JOIN 和行锁读取审批所需全部事实，避免修订、审核和聚合分散查询或竞态。
func lockSkillReviewDecision(tx *gorm.DB, reviewID string) (skillReviewDecisionReadDTO, error) {
	var record skillReviewDecisionReadDTO
	result := tx.Raw(`
SELECT
    review_record.id AS review_id,
    review_record.skill_id,
    skill_record.owner_user_id,
    review_record.content_revision_id,
    review_record.content_digest AS review_content_digest,
    review_record.status AS review_status,
    review_record.version AS review_version,
    review_record.submitted_by_user_id,
    review_record.submitted_at,
    content_record.revision_no,
    content_record.definition_schema_version,
    content_record.definition_json AS definition_json,
    content_record.content_digest AS revision_content_digest,
    content_record.created_at AS revision_created_at,
    skill_record.current_published_snapshot_id,
    skill_record.publication_revision,
    skill_record.governance_status,
    skill_record.version AS skill_version
FROM business.skill_review_submission AS review_record
JOIN business.skill AS skill_record ON skill_record.id = review_record.skill_id
JOIN business.skill_content_revision AS content_record ON content_record.id = review_record.content_revision_id
WHERE review_record.id = ?
FOR UPDATE OF review_record, skill_record`, reviewID).Scan(&record)
	if result.Error != nil {
		return skillReviewDecisionReadDTO{}, fmt.Errorf("lock skill review decision: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return skillReviewDecisionReadDTO{}, skill.ErrReviewNotFound
	}
	return record, nil
}

// skillOwnerSelectSQL 是单个和列表 Owner 查询共享的固定 JOIN 投影。
const skillOwnerSelectSQL = `
SELECT
    skill_record.id AS skill_id,
    skill_record.owner_user_id,
    skill_record.current_draft_revision_id,
    skill_record.current_published_snapshot_id,
    skill_record.publication_revision,
    skill_record.governance_status,
    skill_record.version AS skill_version,
    skill_record.created_at AS skill_created_at,
    skill_record.updated_at AS skill_updated_at,
    draft_record.revision_no AS draft_revision_no,
    draft_record.definition_json AS draft_definition_json,
    draft_record.content_digest AS draft_content_digest,
    draft_record.created_by_user_id AS draft_created_by_user_id,
    draft_record.created_at AS draft_created_at,
    published_record.id AS published_id,
    published_record.source_content_revision_id AS published_source_revision_id,
    published_record.review_submission_id AS published_review_id,
    published_record.definition_json AS published_definition_json,
    published_record.content_digest AS published_content_digest,
    published_record.published_by_user_id AS published_by_user_id,
    published_record.published_at AS published_at,
    latest_review.id AS latest_review_id,
    latest_review.content_revision_id AS latest_review_revision_id,
    latest_review.content_digest AS latest_review_content_digest,
    latest_review.status AS latest_review_status,
    latest_review.safe_reason_code AS latest_review_reason_code,
    latest_review.version AS latest_review_version,
    latest_review.submitted_by_user_id AS latest_review_submitted_by,
    latest_review.decided_by_user_id AS latest_review_decided_by,
    latest_review.submitted_at AS latest_review_submitted_at,
    latest_review.decided_at AS latest_review_decided_at,
    latest_review.updated_at AS latest_review_updated_at
FROM business.skill AS skill_record
JOIN business.skill_content_revision AS draft_record ON draft_record.id = skill_record.current_draft_revision_id
LEFT JOIN business.skill_published_snapshot AS published_record ON published_record.id = skill_record.current_published_snapshot_id
LEFT JOIN LATERAL (
    SELECT review_record.*
    FROM business.skill_review_submission AS review_record
    WHERE review_record.skill_id = skill_record.id
    ORDER BY review_record.submitted_at DESC, review_record.id DESC
    LIMIT 1
) AS latest_review ON true`

// mapSkillRepositoryError 收敛 PostgreSQL/GORM 原错，同时保留取消、超时和稳定领域错误。
func mapSkillRepositoryError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, skill.ErrInvalidDefinition):
		return skill.ErrInvalidDefinition
	case errors.Is(err, skill.ErrSkillNotFound):
		return skill.ErrSkillNotFound
	case errors.Is(err, skill.ErrReviewNotFound):
		return skill.ErrReviewNotFound
	case errors.Is(err, skill.ErrReviewCapabilityRequired):
		return skill.ErrReviewCapabilityRequired
	case errors.Is(err, skill.ErrDraftConflict):
		return skill.ErrDraftConflict
	case errors.Is(err, skill.ErrReviewConflict):
		return skill.ErrReviewConflict
	case errors.Is(err, skill.ErrIdempotencyConflict):
		return skill.ErrIdempotencyConflict
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.ConstraintName {
		case "uq_skill_review_submission__one_reviewing", "uq_skill_published_snapshot__review":
			return skill.ErrReviewConflict
		case "uq_skill_content_revision__skill_revision":
			return skill.ErrDraftConflict
		}
	}
	// 未分类数据库错误可能包含 SQL、DSN 或用户内容，只返回无底层 cause 的稳定边界错误。
	return skill.ErrPersistence
}

// valueOrEmpty 安全读取内部命令可选引用。
func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
