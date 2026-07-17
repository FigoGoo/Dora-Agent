package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/internal/textmaterial"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const textMaterialReplaySQL = `
SELECT
    asset.id AS asset_id,
    asset.owner_user_id,
    asset.project_id,
    asset.asset_version,
    asset.media_type,
    asset.status,
    evidence.id::text AS evidence_id,
    evidence.media_type AS evidence_media_type,
    evidence.evidence_kind,
    evidence.availability,
    evidence.content_digest,
    evidence.extractor_schema_version,
    evidence.extractor_version,
    evidence.locator_kind,
    evidence.text_start,
    evidence.text_end,
    evidence.text_source_length,
    evidence.content,
    asset.created_at,
    evidence.created_at AS evidence_created_at,
    count(evidence.id) OVER () AS evidence_count
FROM business.asset_analysis_preview_assets AS asset
LEFT JOIN business.asset_analysis_preview_evidence AS evidence
  ON evidence.asset_id = asset.id
 AND evidence.asset_version = asset.asset_version
WHERE asset.id = ?
ORDER BY evidence.id ASC
LIMIT 2`

const textMaterialListSQL = `
WITH selected_assets AS (
    SELECT
        asset.id,
        asset.owner_user_id,
        asset.project_id,
        asset.asset_version,
        asset.media_type,
        asset.status,
        asset.created_at
    FROM business.asset_analysis_preview_assets AS asset
    JOIN business.project AS project
      ON project.id = asset.project_id
    WHERE project.id = ?
      AND project.owner_user_id = ?
      AND project.lifecycle_status IN ('active', 'archived')
      AND asset.project_id = ?
      AND asset.owner_user_id = ?
      AND asset.media_type = 'text'
      AND asset.status = 'ready'
    ORDER BY asset.created_at DESC, asset.id DESC
    LIMIT ?
)
SELECT
    asset.id AS asset_id,
    asset.owner_user_id,
    asset.project_id,
    asset.asset_version,
    asset.media_type,
    asset.status,
    min(evidence.id::text) AS evidence_id,
    min(evidence.media_type) AS evidence_media_type,
    min(evidence.evidence_kind) AS evidence_kind,
    min(evidence.availability) AS availability,
    min(evidence.content_digest) AS content_digest,
    min(evidence.extractor_schema_version) AS extractor_schema_version,
    min(evidence.extractor_version) AS extractor_version,
    min(evidence.locator_kind) AS locator_kind,
    min(evidence.text_start) AS text_start,
    min(evidence.text_end) AS text_end,
    min(evidence.text_source_length) AS text_source_length,
    min(evidence.content) AS content,
    asset.created_at,
    min(evidence.created_at) AS evidence_created_at,
    count(evidence.id) AS evidence_count
FROM selected_assets AS asset
LEFT JOIN business.asset_analysis_preview_evidence AS evidence
  ON evidence.asset_id = asset.id
 AND evidence.asset_version = asset.asset_version
GROUP BY asset.id, asset.owner_user_id, asset.project_id, asset.asset_version, asset.media_type, asset.status, asset.created_at
ORDER BY asset.created_at DESC, asset.id DESC`

// TextMaterialRepository 使用既有 Asset/Evidence 表实现文本素材事务创建和有界列表。
type TextMaterialRepository struct {
	db *gorm.DB
}

var _ textmaterial.Repository = (*TextMaterialRepository)(nil)

// NewTextMaterialRepository 从 Business PostgreSQL Client 创建文本素材 Repository。
func NewTextMaterialRepository(client *Client) (*TextMaterialRepository, error) {
	if client == nil || client.db == nil {
		return nil, errors.New("create text material repository: postgres client is nil")
	}
	return &TextMaterialRepository{db: client.db}, nil
}

// CreateOrReplay 在一个事务中先验证 Project Owner，再竞争 asset_id 并写入完整文本 Evidence。
// ON CONFLICT 会等待并发首写事务结束；未占有主键的调用随后读取首写事实，保证同义重放和异义冲突确定性收敛。
func (repository *TextMaterialRepository) CreateOrReplay(ctx context.Context, material textmaterial.TextMaterial) (textmaterial.CreateResult, error) {
	if ctx == nil || material.Validate() != nil {
		return textmaterial.CreateResult{}, textmaterial.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return textmaterial.CreateResult{}, err
	}
	result := textmaterial.CreateResult{}
	err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var owned projectModel
		ownerQuery := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).
			Where("id = ? AND owner_user_id = ? AND lifecycle_status IN ?", material.ProjectID, material.OwnerUserID,
				[]string{string(project.LifecycleStatusActive), string(project.LifecycleStatusArchived)}).
			Take(&owned)
		if errors.Is(ownerQuery.Error, gorm.ErrRecordNotFound) {
			return textmaterial.ErrProjectNotFound
		}
		if ownerQuery.Error != nil {
			return ownerQuery.Error
		}

		asset := textMaterialAssetModel{
			ID: material.AssetID, OwnerUserID: material.OwnerUserID, ProjectID: material.ProjectID,
			AssetVersion: material.AssetVersion, MediaType: "text", Status: "ready", CreatedAt: material.CreatedAt,
		}
		insertAsset := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).
			Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoNothing: true}).Create(&asset)
		if insertAsset.Error != nil {
			return insertAsset.Error
		}
		if insertAsset.RowsAffected == 0 {
			// 主键已被首写者占有时只读取首写事实；不得追加第二条 Evidence 或覆盖正文。
			existing, err := readTextMaterialReplay(tx, material.AssetID)
			if err != nil {
				return err
			}
			if !sameTextMaterialSemantic(existing, material) {
				return textmaterial.ErrIdempotencyConflict
			}
			result = textmaterial.CreateResult{Material: existing, Replayed: true}
			return nil
		}

		length := int64(utf8.RuneCountInString(material.Content))
		evidence := textMaterialEvidenceModel{
			ID: material.EvidenceID, AssetID: material.AssetID, AssetVersion: material.AssetVersion,
			MediaType: "text", EvidenceKind: "text_segment", Availability: "ready",
			ContentDigest: material.ContentDigest, ExtractorSchemaVersion: textmaterial.ExtractorSchemaVersion,
			ExtractorVersion: textmaterial.ExtractorVersion, LocatorKind: "text_range",
			TextStart: 0, TextEnd: length, TextSourceLength: length, Content: material.Content, CreatedAt: material.CreatedAt,
		}
		if err := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Create(&evidence).Error; err != nil {
			return err
		}
		result = textmaterial.CreateResult{Material: material, Replayed: false}
		return nil
	})
	if err != nil {
		return textmaterial.CreateResult{}, mapTextMaterialRepositoryError(err)
	}
	return result, nil
}

// ListOwned 使用一次 Project Owner 查询和一次固定集合 SQL，区分无权访问与合法空列表并读取完整正文。
func (repository *TextMaterialRepository) ListOwned(ctx context.Context, query textmaterial.ListQuery) ([]textmaterial.TextMaterial, error) {
	if ctx == nil || query.Validate() != nil {
		return nil, textmaterial.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var owned projectModel
	ownerQuery := repository.db.WithContext(ctx).
		Where("id = ? AND owner_user_id = ? AND lifecycle_status IN ?", query.ProjectID, query.OwnerUserID,
			[]string{string(project.LifecycleStatusActive), string(project.LifecycleStatusArchived)}).
		Take(&owned)
	if errors.Is(ownerQuery.Error, gorm.ErrRecordNotFound) {
		return nil, textmaterial.ErrProjectNotFound
	}
	if ownerQuery.Error != nil {
		return nil, mapTextMaterialRepositoryError(ownerQuery.Error)
	}
	var rows []textMaterialReadDTO
	err := repository.db.WithContext(ctx).Raw(
		textMaterialListSQL,
		query.ProjectID, query.OwnerUserID, query.ProjectID, query.OwnerUserID, query.Limit,
	).Scan(&rows).Error
	if err != nil {
		return nil, mapTextMaterialRepositoryError(err)
	}
	materials := make([]textmaterial.TextMaterial, 0, len(rows))
	for _, row := range rows {
		material, err := textMaterialFromReadDTO(row)
		if err != nil {
			return nil, mapTextMaterialRepositoryError(err)
		}
		materials = append(materials, material)
	}
	return materials, nil
}

// readTextMaterialReplay 最多读取两条 Evidence，以检测同一素材版本出现重复不可变正文的损坏状态。
func readTextMaterialReplay(tx *gorm.DB, assetID string) (textmaterial.TextMaterial, error) {
	var rows []textMaterialReadDTO
	if err := tx.Session(&gorm.Session{SkipDefaultTransaction: true}).Raw(textMaterialReplaySQL, assetID).Scan(&rows).Error; err != nil {
		return textmaterial.TextMaterial{}, err
	}
	if len(rows) != 1 || rows[0].EvidenceCount != 1 {
		return textmaterial.TextMaterial{}, textmaterial.ErrPersistence
	}
	return textMaterialFromReadDTO(rows[0])
}

// textMaterialFromReadDTO 显式恢复文本素材并拒绝缺失、重复或非完整 text_range Evidence。
func textMaterialFromReadDTO(row textMaterialReadDTO) (textmaterial.TextMaterial, error) {
	if row.EvidenceCount != 1 || row.EvidenceID == nil || row.EvidenceMediaType == nil || row.EvidenceKind == nil ||
		row.Availability == nil || row.ContentDigest == nil || row.ExtractorSchemaVersion == nil || row.ExtractorVersion == nil ||
		row.LocatorKind == nil || row.TextStart == nil || row.TextEnd == nil || row.TextSourceLength == nil ||
		row.Content == nil || row.EvidenceCreatedAt == nil || row.MediaType != "text" || row.Status != "ready" ||
		*row.EvidenceMediaType != "text" || *row.EvidenceKind != "text_segment" || *row.Availability != "ready" ||
		strings.TrimSpace(*row.ExtractorSchemaVersion) == "" || len(*row.ExtractorSchemaVersion) > 128 ||
		strings.TrimSpace(*row.ExtractorVersion) == "" || len(*row.ExtractorVersion) > 128 ||
		*row.LocatorKind != "text_range" || *row.TextStart != 0 || *row.TextEnd != *row.TextSourceLength ||
		!row.CreatedAt.Equal(*row.EvidenceCreatedAt) {
		return textmaterial.TextMaterial{}, textmaterial.ErrPersistence
	}
	material := textmaterial.TextMaterial{
		AssetID: row.AssetID, EvidenceID: *row.EvidenceID, OwnerUserID: row.OwnerUserID, ProjectID: row.ProjectID,
		AssetVersion: row.AssetVersion, ContentDigest: *row.ContentDigest, Content: *row.Content, CreatedAt: row.CreatedAt.UTC(),
	}
	if int64(utf8.RuneCountInString(material.Content)) != *row.TextSourceLength || material.Validate() != nil {
		return textmaterial.TextMaterial{}, textmaterial.ErrPersistence
	}
	return material, nil
}

// sameTextMaterialSemantic 只允许原 Owner、Project、asset_id、版本与完整正文完全一致的请求重放。
func sameTextMaterialSemantic(existing textmaterial.TextMaterial, requested textmaterial.TextMaterial) bool {
	return existing.AssetID == requested.AssetID && existing.OwnerUserID == requested.OwnerUserID &&
		existing.ProjectID == requested.ProjectID && existing.AssetVersion == requested.AssetVersion &&
		existing.ContentDigest == requested.ContentDigest && existing.Content == requested.Content
}

// mapTextMaterialRepositoryError 收敛数据库和损坏状态，不泄露 SQL、DSN 或正文。
func mapTextMaterialRepositoryError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, textmaterial.ErrInvalidArgument), errors.Is(err, textmaterial.ErrProjectNotFound),
		errors.Is(err, textmaterial.ErrIdempotencyConflict), errors.Is(err, textmaterial.ErrPersistence):
		return err
	default:
		return fmt.Errorf("%w", textmaterial.ErrPersistence)
	}
}
