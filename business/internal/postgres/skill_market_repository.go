package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
)

type skillMarketReadDTO struct {
	SkillID                      string     `gorm:"column:skill_id"`
	OwnerUserID                  string     `gorm:"column:owner_user_id"`
	CurrentPublishedSnapshotID   *string    `gorm:"column:current_published_snapshot_id"`
	SkillPublicationRevision     int64      `gorm:"column:skill_publication_revision"`
	GovernanceStatus             string     `gorm:"column:governance_status"`
	PublishedID                  *string    `gorm:"column:published_id"`
	PublishedSkillID             *string    `gorm:"column:published_skill_id"`
	PublishedSourceRevisionID    *string    `gorm:"column:published_source_revision_id"`
	PublishedReviewID            *string    `gorm:"column:published_review_id"`
	PublishedPublicationRevision *int64     `gorm:"column:published_publication_revision"`
	PublishedDefinitionSchema    *string    `gorm:"column:published_definition_schema"`
	PublishedDefinitionJSON      []byte     `gorm:"column:published_definition_json"`
	PublishedContentDigest       []byte     `gorm:"column:published_content_digest"`
	PublishedByUserID            *string    `gorm:"column:published_by_user_id"`
	PublishedAt                  *time.Time `gorm:"column:published_at"`
	SourceRevisionID             *string    `gorm:"column:source_revision_id"`
	SourceSkillID                *string    `gorm:"column:source_skill_id"`
	SourceContentDigest          []byte     `gorm:"column:source_content_digest"`
	PublisherID                  *string    `gorm:"column:publisher_id"`
	PublisherDisplayName         *string    `gorm:"column:publisher_display_name"`
}

// ListPublished 使用一条 SQL 先捕获悬空发布指针，再经键集索引读取公开候选，并在截断前校验 limit+1 全部行。
func (repository *SkillRepository) ListPublished(
	ctx context.Context,
	boundary *skill.MarketPageBoundary,
	limit int,
) (skill.MarketPublishedPage, error) {
	if limit <= 0 || limit > 100 || (boundary != nil &&
		(boundary.PublishedAt.IsZero() || boundary.PublishedAt.UTC().UnixNano() <= 0 || !validPostgresUUIDv7(boundary.SkillID))) {
		return skill.MarketPublishedPage{}, skill.ErrPersistence
	}
	query := skillMarketListQuery(boundary != nil)
	arguments := make([]any, 0, 4)
	if boundary != nil {
		arguments = append(arguments, boundary.PublishedAt.UTC(), boundary.SkillID)
	}
	arguments = append(arguments, limit+1, limit+1)

	var records []skillMarketReadDTO
	if err := repository.db.WithContext(ctx).Raw(query, arguments...).Scan(&records).Error; err != nil {
		return skill.MarketPublishedPage{}, mapSkillRepositoryError(fmt.Errorf("list skill market: %w", err))
	}
	items := make([]skill.MarketPublishedSkill, 0, len(records))
	for _, record := range records {
		item, err := marketPublishedSkillFromReadDTO(record)
		if err != nil {
			return skill.MarketPublishedPage{}, mapSkillRepositoryError(err)
		}
		items = append(items, item)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return skill.MarketPublishedPage{Items: items, HasMore: hasMore}, nil
}

func skillMarketListQuery(hasBoundary bool) string {
	query := `WITH market_dangling AS MATERIALIZED (` + skillMarketSelectSQL + `
WHERE skill_record.governance_status = 'active'
  AND skill_record.current_published_snapshot_id IS NOT NULL
  AND published_record.id IS NULL
ORDER BY skill_record.id DESC
LIMIT 1
),
market_valid AS MATERIALIZED (` + skillMarketValidSelectSQL
	if hasBoundary {
		query += `
WHERE (published_record.published_at, published_record.skill_id) < (?, ?)`
	}
	return query + `
ORDER BY published_record.published_at DESC, published_record.skill_id DESC
LIMIT ?
)
SELECT * FROM market_dangling
UNION ALL
SELECT * FROM market_valid
ORDER BY published_at DESC NULLS FIRST, skill_id DESC
LIMIT ?`
}

// FindPublishedByID 使用一次 LEFT JOIN 查询读取一个 active current published Skill。
func (repository *SkillRepository) FindPublishedByID(ctx context.Context, skillID string) (skill.MarketPublishedSkill, error) {
	if !validPostgresUUIDv7(skillID) {
		return skill.MarketPublishedSkill{}, skill.ErrMarketNotFound
	}
	var record skillMarketReadDTO
	result := repository.db.WithContext(ctx).Raw(skillMarketSelectSQL+`
WHERE skill_record.id = ?
  AND skill_record.governance_status = 'active'
  AND skill_record.current_published_snapshot_id IS NOT NULL`, skillID).Scan(&record)
	if result.Error != nil {
		return skill.MarketPublishedSkill{}, mapSkillRepositoryError(fmt.Errorf("find skill market detail: %w", result.Error))
	}
	if result.RowsAffected != 1 {
		return skill.MarketPublishedSkill{}, skill.ErrMarketNotFound
	}
	item, err := marketPublishedSkillFromReadDTO(record)
	if err != nil {
		return skill.MarketPublishedSkill{}, mapSkillRepositoryError(err)
	}
	return item, nil
}

func marketPublishedSkillFromReadDTO(record skillMarketReadDTO) (skill.MarketPublishedSkill, error) {
	if record.GovernanceStatus != string(skill.GovernanceStatusActive) ||
		record.CurrentPublishedSnapshotID == nil || record.PublishedID == nil || record.PublishedSkillID == nil ||
		record.PublishedSourceRevisionID == nil || record.PublishedReviewID == nil ||
		record.PublishedPublicationRevision == nil || record.PublishedDefinitionSchema == nil ||
		record.PublishedByUserID == nil || record.PublishedAt == nil || record.SourceRevisionID == nil ||
		record.SourceSkillID == nil || record.PublisherID == nil || record.PublisherDisplayName == nil {
		return skill.MarketPublishedSkill{}, skill.ErrPersistence
	}
	if !validPostgresUUIDv7(record.SkillID) || !validPostgresUUIDv7(record.OwnerUserID) ||
		!validPostgresUUIDv7(*record.PublishedID) || !validPostgresUUIDv7(*record.PublishedSourceRevisionID) ||
		!validPostgresUUIDv7(*record.PublishedReviewID) || !validPostgresUUIDv7(*record.PublishedByUserID) ||
		!validPostgresUUIDv7(*record.SourceRevisionID) || !validPostgresUUIDv7(*record.PublisherID) ||
		*record.CurrentPublishedSnapshotID != *record.PublishedID || *record.PublishedSkillID != record.SkillID ||
		record.SkillPublicationRevision < 1 || record.SkillPublicationRevision != *record.PublishedPublicationRevision ||
		*record.PublishedDefinitionSchema != skill.DefinitionSchemaVersionV1 ||
		*record.SourceRevisionID != *record.PublishedSourceRevisionID || *record.SourceSkillID != record.SkillID ||
		*record.PublisherID != record.OwnerUserID || record.PublishedAt.IsZero() || record.PublishedAt.UTC().UnixNano() <= 0 ||
		!validPublisherDisplayName(*record.PublisherDisplayName) {
		return skill.MarketPublishedSkill{}, skill.ErrPersistence
	}
	definition, digest, err := skill.DefinitionFromCanonicalV1(record.PublishedDefinitionJSON)
	if err != nil || !equalDigestBytes(record.PublishedContentDigest, digest) ||
		!equalDigestBytes(record.SourceContentDigest, digest) {
		return skill.MarketPublishedSkill{}, skill.ErrPersistence
	}
	return skill.MarketPublishedSkill{
		SkillID: record.SkillID, PublisherID: *record.PublisherID,
		PublisherDisplayName: *record.PublisherDisplayName, Definition: definition,
		PublishedAt: record.PublishedAt.UTC(),
	}, nil
}

func validPublisherDisplayName(value string) bool {
	if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > 160 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

const skillMarketSelectSQL = `
SELECT
    skill_record.id AS skill_id,
    skill_record.owner_user_id,
    skill_record.current_published_snapshot_id,
    skill_record.publication_revision AS skill_publication_revision,
    skill_record.governance_status,
    published_record.id AS published_id,
    published_record.skill_id AS published_skill_id,
    published_record.source_content_revision_id AS published_source_revision_id,
    published_record.review_submission_id AS published_review_id,
    published_record.publication_revision AS published_publication_revision,
    published_record.definition_schema_version AS published_definition_schema,
    published_record.definition_json AS published_definition_json,
    published_record.content_digest AS published_content_digest,
    published_record.published_by_user_id,
    published_record.published_at,
    source_record.id AS source_revision_id,
    source_record.skill_id AS source_skill_id,
    source_record.content_digest AS source_content_digest,
    publisher_record.id AS publisher_id,
    publisher_record.display_name AS publisher_display_name
FROM business.skill AS skill_record
LEFT JOIN business.skill_published_snapshot AS published_record
  ON published_record.id = skill_record.current_published_snapshot_id
 AND published_record.skill_id = skill_record.id
LEFT JOIN business.skill_content_revision AS source_record
  ON source_record.id = published_record.source_content_revision_id
 AND source_record.skill_id = skill_record.id
LEFT JOIN business.user_account AS publisher_record
  ON publisher_record.id = skill_record.owner_user_id`

const skillMarketValidSelectSQL = `
SELECT
    skill_record.id AS skill_id,
    skill_record.owner_user_id,
    skill_record.current_published_snapshot_id,
    skill_record.publication_revision AS skill_publication_revision,
    skill_record.governance_status,
    published_record.id AS published_id,
    published_record.skill_id AS published_skill_id,
    published_record.source_content_revision_id AS published_source_revision_id,
    published_record.review_submission_id AS published_review_id,
    published_record.publication_revision AS published_publication_revision,
    published_record.definition_schema_version AS published_definition_schema,
    published_record.definition_json AS published_definition_json,
    published_record.content_digest AS published_content_digest,
    published_record.published_by_user_id,
    published_record.published_at,
    source_record.id AS source_revision_id,
    source_record.skill_id AS source_skill_id,
    source_record.content_digest AS source_content_digest,
    publisher_record.id AS publisher_id,
    publisher_record.display_name AS publisher_display_name
FROM business.skill_published_snapshot AS published_record
JOIN LATERAL (
    SELECT skill_candidate.*
    FROM business.skill AS skill_candidate
    WHERE skill_candidate.id = published_record.skill_id
      AND skill_candidate.current_published_snapshot_id = published_record.id
      AND skill_candidate.governance_status = 'active'
    OFFSET 0
) AS skill_record ON TRUE
LEFT JOIN business.skill_content_revision AS source_record
  ON source_record.id = published_record.source_content_revision_id
 AND source_record.skill_id = skill_record.id
LEFT JOIN business.user_account AS publisher_record
  ON publisher_record.id = skill_record.owner_user_id`
