// Package postgres 负责 Business Worker 的 GORM PostgreSQL 基础连接。
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/worker/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const schemaName = "worker"

// Client 封装 Worker PostgreSQL 连接和底层连接池生命周期。
type Client struct{ db *gorm.DB }

// Open 创建 GORM 连接、设置有界连接池并完成启动 Ping。
func Open(ctx context.Context, cfg config.PostgreSQLConfig) (*Client, error) {
	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open worker postgres: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get worker postgres pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping worker postgres: %w", err)
	}
	return &Client{db: db}, nil
}

// VerifySchema 确认 Worker 版本化 Migration 已创建权威 Schema。
func (c *Client) VerifySchema(ctx context.Context, timeout time.Duration) error {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var exists bool
	if err := c.db.WithContext(checkCtx).
		Raw("SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = ?)", schemaName).
		Scan(&exists).Error; err != nil {
		return fmt.Errorf("query worker schema: %w", err)
	}
	if !exists {
		return fmt.Errorf("worker schema is missing; run worker migrations")
	}
	if err := c.verifySchemaContract(checkCtx); err != nil {
		return err
	}
	return nil
}

// verifySchemaContract 验证 Worker Schema 不含物理外键，且 Schema、表和字段均具有中文说明。
func (c *Client) verifySchemaContract(ctx context.Context) error {
	var physicalForeignKeyCount int64
	if err := c.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM pg_constraint AS constraint_record
		JOIN pg_class AS relation ON relation.oid = constraint_record.conrelid
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = ? AND constraint_record.contype = 'f'`, schemaName).
		Scan(&physicalForeignKeyCount).Error; err != nil {
		return fmt.Errorf("query worker physical foreign keys: %w", err)
	}
	// 物理外键会绕过 Worker 显式状态迁移和补偿，必须在 Runtime 就绪前失败关闭。
	if physicalForeignKeyCount != 0 {
		return fmt.Errorf("worker schema contains %d physical foreign key constraints", physicalForeignKeyCount)
	}

	var invalidSchemaCommentCount int64
	if err := c.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM pg_namespace AS namespace
		WHERE namespace.nspname = ?
		  AND COALESCE(obj_description(namespace.oid, 'pg_namespace'), '') !~ '[一-龥]'`, schemaName).
		Scan(&invalidSchemaCommentCount).Error; err != nil {
		return fmt.Errorf("query worker schema comment: %w", err)
	}
	if invalidSchemaCommentCount != 0 {
		return fmt.Errorf("worker schema is missing a Chinese database comment")
	}

	var invalidTableCommentCount int64
	if err := c.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM pg_class AS relation
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = ?
		  AND relation.relkind IN ('r', 'p')
		  AND COALESCE(obj_description(relation.oid, 'pg_class'), '') !~ '[一-龥]'`, schemaName).
		Scan(&invalidTableCommentCount).Error; err != nil {
		return fmt.Errorf("query worker table comments: %w", err)
	}
	if invalidTableCommentCount != 0 {
		return fmt.Errorf("worker schema contains %d tables without Chinese database comments", invalidTableCommentCount)
	}

	var invalidColumnCommentCount int64
	if err := c.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM pg_attribute AS attribute
		JOIN pg_class AS relation ON relation.oid = attribute.attrelid
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = ?
		  AND relation.relkind IN ('r', 'p')
		  AND attribute.attnum > 0
		  AND NOT attribute.attisdropped
		  AND COALESCE(col_description(relation.oid, attribute.attnum), '') !~ '[一-龥]'`, schemaName).
		Scan(&invalidColumnCommentCount).Error; err != nil {
		return fmt.Errorf("query worker column comments: %w", err)
	}
	if invalidColumnCommentCount != 0 {
		return fmt.Errorf("worker schema contains %d columns without Chinese database comments", invalidColumnCommentCount)
	}
	return nil
}

// Close 关闭 Worker PostgreSQL 底层连接池。
func (c *Client) Close() error {
	sqlDB, err := c.db.DB()
	if err != nil {
		return fmt.Errorf("get worker postgres pool for close: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("close worker postgres: %w", err)
	}
	return nil
}
