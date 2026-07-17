package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/assetanalysis"
	"gorm.io/gorm"
)

const assetAnalysisAuthorizedCollectionSQL = `
SELECT
    asset.id,
    asset.asset_version,
    asset.media_type,
    asset.created_at,
    evidence.id,
    evidence.asset_id,
    evidence.asset_version,
    evidence.media_type,
    evidence.evidence_kind,
    evidence.availability,
    evidence.reason_code,
    evidence.content_digest,
    evidence.extractor_schema_version,
    evidence.extractor_version,
    evidence.locator_kind,
    evidence.text_start,
    evidence.text_end,
    evidence.text_source_length,
    evidence.image_x,
    evidence.image_y,
    evidence.image_width,
    evidence.image_height,
    evidence.content,
    evidence.created_at
FROM business.asset_analysis_preview_assets AS asset
JOIN business.project AS project
  ON project.id = asset.project_id
LEFT JOIN business.asset_analysis_preview_evidence AS evidence
  ON evidence.asset_id = asset.id
 AND evidence.asset_version = asset.asset_version
WHERE project.id = ?
  AND project.owner_user_id = ?
  AND asset.project_id = ?
  AND asset.owner_user_id = ?
  AND asset.status = 'ready'
  AND asset.id IN ?
ORDER BY asset.id ASC, evidence.evidence_kind ASC, evidence.id ASC`

// AssetAnalysisRepository 使用一次集合 SQL 同时完成 Project Owner、素材 exact-set 候选和 Evidence 读取。
type AssetAnalysisRepository struct {
	db *gorm.DB
}

var _ assetanalysis.Repository = (*AssetAnalysisRepository)(nil)

// NewAssetAnalysisRepository 从 Business PostgreSQL Client 创建只读 Repository。
func NewAssetAnalysisRepository(client *Client) (*AssetAnalysisRepository, error) {
	if client == nil || client.db == nil {
		return nil, errors.New("create asset analysis repository: postgres client is nil")
	}
	return &AssetAnalysisRepository{db: client.db}, nil
}

// BatchGetAuthorized 执行唯一一次集合查询；exact-set 与版本检查由领域 Service 按冻结顺序完成。
func (repository *AssetAnalysisRepository) BatchGetAuthorized(ctx context.Context, query assetanalysis.RepositoryQuery) ([]assetanalysis.Asset, error) {
	if ctx == nil || query.UserID == "" || query.ProjectID == "" || len(query.AssetIDs) == 0 {
		return nil, assetanalysis.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := repository.db.WithContext(ctx).Raw(
		assetAnalysisAuthorizedCollectionSQL,
		query.ProjectID, query.UserID, query.ProjectID, query.UserID, query.AssetIDs,
	).Rows()
	if err != nil {
		return nil, mapAssetAnalysisRepositoryError(err)
	}
	defer rows.Close()

	assets := make([]assetanalysis.Asset, 0, len(query.AssetIDs))
	assetIndexes := make(map[string]int, len(query.AssetIDs))
	for rows.Next() {
		var row assetAnalysisCollectionRow
		if err := rows.Scan(
			&row.AssetID, &row.AssetVersion, &row.AssetMediaType, &row.AssetCreatedAt,
			&row.EvidenceID, &row.EvidenceAssetID, &row.EvidenceAssetVersion, &row.EvidenceMediaType,
			&row.EvidenceKind, &row.Availability, &row.ReasonCode, &row.ContentDigest,
			&row.ExtractorSchemaVersion, &row.ExtractorVersion, &row.LocatorKind,
			&row.TextStart, &row.TextEnd, &row.TextSourceLength, &row.ImageX, &row.ImageY,
			&row.ImageWidth, &row.ImageHeight, &row.Content, &row.EvidenceCreatedAt,
		); err != nil {
			return nil, mapAssetAnalysisRepositoryError(err)
		}
		index, exists := assetIndexes[row.AssetID]
		if !exists {
			index = len(assets)
			assetIndexes[row.AssetID] = index
			assets = append(assets, assetanalysis.Asset{
				ID: row.AssetID, Version: row.AssetVersion, MediaType: assetanalysis.MediaType(row.AssetMediaType),
				Evidence: make([]assetanalysis.Evidence, 0), CreatedAt: row.AssetCreatedAt,
			})
		}
		if row.EvidenceID.Valid {
			assets[index].Evidence = append(assets[index].Evidence, row.evidence())
		}
	}
	if err := rows.Err(); err != nil {
		return nil, mapAssetAnalysisRepositoryError(err)
	}
	return assets, nil
}

type assetAnalysisCollectionRow struct {
	AssetID                string
	AssetVersion           int64
	AssetMediaType         string
	AssetCreatedAt         time.Time
	EvidenceID             sql.NullString
	EvidenceAssetID        sql.NullString
	EvidenceAssetVersion   sql.NullInt64
	EvidenceMediaType      sql.NullString
	EvidenceKind           sql.NullString
	Availability           sql.NullString
	ReasonCode             sql.NullString
	ContentDigest          sql.NullString
	ExtractorSchemaVersion sql.NullString
	ExtractorVersion       sql.NullString
	LocatorKind            sql.NullString
	TextStart              sql.NullInt64
	TextEnd                sql.NullInt64
	TextSourceLength       sql.NullInt64
	ImageX                 sql.NullInt32
	ImageY                 sql.NullInt32
	ImageWidth             sql.NullInt32
	ImageHeight            sql.NullInt32
	Content                sql.NullString
	EvidenceCreatedAt      sql.NullTime
}

func (row assetAnalysisCollectionRow) evidence() assetanalysis.Evidence {
	var locator *assetanalysis.Locator
	if row.LocatorKind.Valid || row.TextStart.Valid || row.TextEnd.Valid || row.TextSourceLength.Valid ||
		row.ImageX.Valid || row.ImageY.Valid || row.ImageWidth.Valid || row.ImageHeight.Valid {
		locator = &assetanalysis.Locator{Kind: assetanalysis.LocatorKind(row.LocatorKind.String)}
		locator.TextStart = optionalInt64(row.TextStart)
		locator.TextEnd = optionalInt64(row.TextEnd)
		locator.TextSourceLength = optionalInt64(row.TextSourceLength)
		locator.ImageX = optionalInt32(row.ImageX)
		locator.ImageY = optionalInt32(row.ImageY)
		locator.ImageWidth = optionalInt32(row.ImageWidth)
		locator.ImageHeight = optionalInt32(row.ImageHeight)
	}
	return assetanalysis.Evidence{
		ID: row.EvidenceID.String, AssetID: row.EvidenceAssetID.String, AssetVersion: row.EvidenceAssetVersion.Int64,
		MediaType: assetanalysis.MediaType(row.EvidenceMediaType.String), Kind: assetanalysis.EvidenceKind(row.EvidenceKind.String),
		Availability: assetanalysis.Availability(row.Availability.String), ReasonCode: row.ReasonCode.String,
		ContentDigest: row.ContentDigest.String, ExtractorSchemaVersion: row.ExtractorSchemaVersion.String,
		ExtractorVersion: row.ExtractorVersion.String, Locator: locator, Content: row.Content.String,
		CreatedAt: row.EvidenceCreatedAt.Time,
	}
}

func optionalInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func optionalInt32(value sql.NullInt32) *int32 {
	if !value.Valid {
		return nil
	}
	result := value.Int32
	return &result
}

func mapAssetAnalysisRepositoryError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, assetanalysis.ErrInvalidArgument) {
		return err
	}
	return fmt.Errorf("%w", assetanalysis.ErrPersistence)
}
