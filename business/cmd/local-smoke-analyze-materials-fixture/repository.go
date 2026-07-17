//go:build localsmoke

package main

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const fixtureAuthorityQuery = `
SELECT
    EXISTS (
        SELECT 1
        FROM business.project AS project
        WHERE project.id = @project_id
          AND project.owner_user_id = @owner_user_id
          AND project.lifecycle_status = 'active'
    ) AS project_valid,
    EXISTS (
        SELECT 1
        FROM business.asset_analysis_preview_assets AS asset
        WHERE asset.id = @asset_id
          AND asset.owner_user_id = @owner_user_id
          AND asset.project_id = @project_id
          AND asset.asset_version = @asset_version
          AND asset.media_type = 'image'
          AND asset.status = 'ready'
    ) AS asset_valid,
    EXISTS (
        SELECT 1
        FROM business.asset_analysis_preview_evidence AS evidence
        WHERE evidence.id = @evidence_id
          AND evidence.asset_id = @asset_id
          AND evidence.asset_version = @asset_version
          AND evidence.media_type = 'image'
          AND evidence.evidence_kind = 'visual_description'
          AND evidence.availability = 'ready'
          AND evidence.reason_code IS NULL
          AND evidence.content_digest = @content_digest
          AND evidence.extractor_schema_version = 'visual.evidence.v1'
          AND evidence.extractor_version = 'local-smoke.fixture.v1'
          AND evidence.locator_kind = 'image_whole'
          AND evidence.text_start IS NULL
          AND evidence.text_end IS NULL
          AND evidence.text_source_length IS NULL
          AND evidence.image_x IS NULL
          AND evidence.image_y IS NULL
          AND evidence.image_width IS NULL
          AND evidence.image_height IS NULL
          AND evidence.content = @content
    ) AS evidence_valid,
    CAST(@asset_id AS text) AS asset_id,
    CAST(@evidence_id AS text) AS evidence_id,
    CAST(@asset_version AS bigint) AS asset_version,
    CAST(@content_digest AS text) AS content_digest,
    (
        SELECT COUNT(*)
        FROM business.asset_analysis_preview_assets AS asset
        WHERE asset.owner_user_id = @owner_user_id
          AND asset.project_id = @project_id
          AND asset.status = 'ready'
    ) AS assets,
    (
        SELECT COUNT(*)
        FROM business.asset_analysis_preview_evidence AS evidence
        JOIN business.asset_analysis_preview_assets AS asset
          ON asset.id = evidence.asset_id
         AND asset.asset_version = evidence.asset_version
        WHERE asset.owner_user_id = @owner_user_id
          AND asset.project_id = @project_id
          AND asset.status = 'ready'
    ) AS evidence,
    (
        SELECT COUNT(*)
        FROM business.creation_spec AS creation_spec
        WHERE creation_spec.project_id = @project_id
          AND creation_spec.user_id = @owner_user_id
    ) AS creation_specs,
    (
        SELECT COUNT(*)
        FROM business.creation_spec_command_receipt AS receipt
        WHERE receipt.project_id = @project_id
          AND receipt.user_id = @owner_user_id
    ) AS creation_spec_command_receipts`

// postgresFixtureRepository 是 Analyze Materials 本地夹具专用 GORM Repository；仅在 localsmoke build tag 下存在。
type postgresFixtureRepository struct {
	// db 是禁用自动外键 Migration 且关闭 SQL 参数日志的有界 GORM 连接。
	db *gorm.DB
}

// fixtureAuthorityRow 是一次固定集合查询的持久化 ReadDTO，不包含 Evidence 正文。
type fixtureAuthorityRow struct {
	// ProjectValid 表示 Project 的 owner 与 active 状态满足夹具前置条件。
	ProjectValid bool `gorm:"column:project_valid"`
	// AssetValid 表示素材不可变字段满足夹具契约。
	AssetValid bool `gorm:"column:asset_valid"`
	// EvidenceValid 表示 Evidence 不可变字段和正文摘要满足夹具契约。
	EvidenceValid bool `gorm:"column:evidence_valid"`
	// AssetID 是固定素材 UUIDv7。
	AssetID string `gorm:"column:asset_id"`
	// EvidenceID 是固定 Evidence UUIDv7。
	EvidenceID string `gorm:"column:evidence_id"`
	// AssetVersion 是固定素材版本。
	AssetVersion int64 `gorm:"column:asset_version"`
	// ContentDigest 是 Evidence 正文的小写 SHA-256。
	ContentDigest string `gorm:"column:content_digest"`
	// Assets 是 Project 下就绪素材计数。
	Assets int64 `gorm:"column:assets"`
	// Evidence 是上述素材集合下 Evidence 计数。
	Evidence int64 `gorm:"column:evidence"`
	// CreationSpecs 是指定 Project/Owner 下 CreationSpec 计数。
	CreationSpecs int64 `gorm:"column:creation_specs"`
	// CreationSpecCommandReceipts 是指定 Project/Owner 下 CreationSpec 命令回执计数。
	CreationSpecCommandReceipts int64 `gorm:"column:creation_spec_command_receipts"`
}

// openPostgresFixtureRepository 使用 BUSINESS_DATABASE_URL 打开有界 GORM 连接并完成 Ping；失败时调用方只输出固定错误。
func openPostgresFixtureRepository(ctx context.Context, dsn string) (*postgresFixtureRepository, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open analyze materials fixture repository: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get analyze materials fixture pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	sqlDB.SetConnMaxIdleTime(time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping analyze materials fixture repository: %w", err)
	}
	return &postgresFixtureRepository{db: db}, nil
}

// Close 关闭本地夹具的底层 PostgreSQL 连接池。
func (repository *postgresFixtureRepository) Close() error {
	if repository == nil || repository.db == nil {
		return nil
	}
	sqlDB, err := repository.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Ensure 在一个短事务中先校验 Project 所有权，再幂等写素材与 Evidence，最后读取固定权威计数。
func (repository *postgresFixtureRepository) Ensure(ctx context.Context, seed fixtureSeed) (fixtureAuthority, error) {
	if repository == nil || repository.db == nil {
		return fixtureAuthority{}, errInvalidFixtureInput
	}
	var authority fixtureAuthority
	err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var projectValid bool
		if err := tx.Raw(`
            SELECT EXISTS (
                SELECT 1 FROM business.project
                WHERE id = ? AND owner_user_id = ? AND lifecycle_status = 'active'
            )`, seed.ProjectID, seed.OwnerUserID).Scan(&projectValid).Error; err != nil {
			return fmt.Errorf("query analyze materials fixture project: %w", err)
		}
		// 先失败关闭 Project/Owner 不匹配，再进行任何写入，避免无物理外键设计产生孤儿素材。
		if !projectValid {
			return errFixtureConflict
		}
		if err := tx.Exec(`
            INSERT INTO business.asset_analysis_preview_assets
                (id, owner_user_id, project_id, asset_version, media_type, status, created_at)
            VALUES (?, ?, ?, ?, 'image', 'ready', ?)
            ON CONFLICT (id) DO NOTHING`,
			seed.AssetID, seed.OwnerUserID, seed.ProjectID, seed.AssetVersion, time.Now().UTC()).Error; err != nil {
			return fmt.Errorf("insert analyze materials fixture asset: %w", err)
		}
		if err := tx.Exec(`
            INSERT INTO business.asset_analysis_preview_evidence
                (id, asset_id, asset_version, media_type, evidence_kind, availability,
                 content_digest, extractor_schema_version, extractor_version, locator_kind, content, created_at)
            VALUES (?, ?, ?, 'image', 'visual_description', 'ready', ?,
                    'visual.evidence.v1', 'local-smoke.fixture.v1', 'image_whole', ?, ?)
            ON CONFLICT (id) DO NOTHING`,
			seed.EvidenceID, seed.AssetID, seed.AssetVersion, seed.ContentDigest, seed.Content, time.Now().UTC()).Error; err != nil {
			return fmt.Errorf("insert analyze materials fixture evidence: %w", err)
		}
		var err error
		authority, err = queryFixtureAuthority(ctx, tx, seed)
		if err != nil {
			return err
		}
		// 提交前核对幂等命中行的全部固定字段；同 ID 异义、额外素材或 CreationSpec 写入都回滚本次事务。
		return validateFixtureAuthority(seed, authority)
	})
	if err != nil {
		return fixtureAuthority{}, err
	}
	return authority, nil
}

// Authority 只执行一次集合查询，供 smoke 在 Agent 执行前后比较 Business 权威状态与负向副作用计数。
func (repository *postgresFixtureRepository) Authority(ctx context.Context, seed fixtureSeed) (fixtureAuthority, error) {
	if repository == nil || repository.db == nil {
		return fixtureAuthority{}, errInvalidFixtureInput
	}
	return queryFixtureAuthority(ctx, repository.db, seed)
}

// queryFixtureAuthority 使用固定数量的一次集合查询读取 Project、素材、Evidence 与两个禁止副作用表，避免 N+1。
func queryFixtureAuthority(ctx context.Context, db *gorm.DB, seed fixtureSeed) (fixtureAuthority, error) {
	arguments := map[string]any{
		"project_id": seed.ProjectID, "owner_user_id": seed.OwnerUserID,
		"asset_id": seed.AssetID, "evidence_id": seed.EvidenceID, "asset_version": seed.AssetVersion,
		"content_digest": seed.ContentDigest, "content": seed.Content,
	}
	var row fixtureAuthorityRow
	if err := db.WithContext(ctx).Raw(fixtureAuthorityQuery, arguments).Scan(&row).Error; err != nil {
		return fixtureAuthority{}, fmt.Errorf("query analyze materials fixture authority: %w", err)
	}
	return fixtureAuthority{
		AssetID: row.AssetID, EvidenceID: row.EvidenceID, AssetVersion: row.AssetVersion,
		ContentDigest: row.ContentDigest,
		Counts: fixtureCounts{
			Assets: row.Assets, Evidence: row.Evidence, CreationSpecs: row.CreationSpecs,
			CreationSpecCommandReceipts: row.CreationSpecCommandReceipts,
		},
		ProjectValid: row.ProjectValid, AssetValid: row.AssetValid, EvidenceValid: row.EvidenceValid,
	}, nil
}
