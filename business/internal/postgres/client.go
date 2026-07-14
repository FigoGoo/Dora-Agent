// Package postgres 负责 Business Service 的 GORM PostgreSQL 基础连接。
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const schemaName = "business"

// requiredBusinessTables 是 Business Runtime 进入 Ready 前必须存在的 W0 权威表固定集合。
var requiredBusinessTables = [...]string{
	"user_account",
	"user_login_identity",
	"user_password_credential",
	"auth_web_session",
	"project",
	"project_creation_receipt",
	"project_session_binding",
	"project_session_outbox",
}

// Client 封装 Business PostgreSQL 连接和底层连接池生命周期。
type Client struct {
	db *gorm.DB
}

// Open 创建 GORM 连接、设置有界连接池并完成启动 Ping。
func Open(ctx context.Context, cfg config.PostgreSQLConfig) (*Client, error) {
	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open business postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get business postgres pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping business postgres: %w", err)
	}
	return &Client{db: db}, nil
}

// VerifySchema 确认 Business 版本化 Migration 已创建权威 Schema。
func (c *Client) VerifySchema(ctx context.Context, timeout time.Duration) error {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var exists bool
	if err := c.db.WithContext(checkCtx).
		Raw("SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = ?)", schemaName).
		Scan(&exists).Error; err != nil {
		return fmt.Errorf("query business schema: %w", err)
	}
	if !exists {
		return fmt.Errorf("business schema is missing; run business migrations")
	}
	if err := c.verifyRequiredTables(checkCtx); err != nil {
		return err
	}
	if err := c.verifyRequiredAuthColumns(checkCtx); err != nil {
		return err
	}
	if err := c.verifySchemaContract(checkCtx); err != nil {
		return err
	}
	return nil
}

// verifyRequiredAuthColumns 校验 W0 Transport Auth 需要的前向 Migration 字段，避免旧 Schema 通过 Ready 后才在登录 JOIN 失败。
func (c *Client) verifyRequiredAuthColumns(ctx context.Context) error {
	var displayNameColumnCount int64
	if err := c.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = ?
		  AND table_name = 'user_account'
		  AND column_name = 'display_name'`, schemaName).Scan(&displayNameColumnCount).Error; err != nil {
		return fmt.Errorf("query business auth required columns: %w", err)
	}
	if displayNameColumnCount != 1 {
		return fmt.Errorf("business schema is missing required W0 auth columns; run business migrations")
	}
	return nil
}

// verifyRequiredTables 使用一次固定集合查询校验 W0 全部权威表；任意 Migration 缺失都会阻止 Runtime Ready。
func (c *Client) verifyRequiredTables(ctx context.Context) error {
	var existingTableCount int64
	if err := c.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM pg_catalog.pg_tables
		WHERE schemaname = ? AND tablename IN ?`, schemaName, requiredBusinessTables[:]).
		Scan(&existingTableCount).Error; err != nil {
		return fmt.Errorf("query business required tables: %w", err)
	}
	if existingTableCount != int64(len(requiredBusinessTables)) {
		return fmt.Errorf("business schema is missing required W0 tables: found %d of %d; run business migrations", existingTableCount, len(requiredBusinessTables))
	}
	return nil
}

// verifySchemaContract 验证 Business Schema 不含物理外键，且 Schema、表和字段均具有中文说明。
func (c *Client) verifySchemaContract(ctx context.Context) error {
	var physicalForeignKeyCount int64
	if err := c.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM pg_constraint AS constraint_record
		JOIN pg_class AS relation ON relation.oid = constraint_record.conrelid
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = ? AND constraint_record.contype = 'f'`, schemaName).
		Scan(&physicalForeignKeyCount).Error; err != nil {
		return fmt.Errorf("query business physical foreign keys: %w", err)
	}
	// 物理外键会把跨聚合完整性隐藏在数据库级联中，必须在 Runtime 就绪前失败关闭。
	if physicalForeignKeyCount != 0 {
		return fmt.Errorf("business schema contains %d physical foreign key constraints", physicalForeignKeyCount)
	}

	var invalidSchemaCommentCount int64
	if err := c.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM pg_namespace AS namespace
		WHERE namespace.nspname = ?
		  AND COALESCE(obj_description(namespace.oid, 'pg_namespace'), '') !~ '[一-龥]'`, schemaName).
		Scan(&invalidSchemaCommentCount).Error; err != nil {
		return fmt.Errorf("query business schema comment: %w", err)
	}
	if invalidSchemaCommentCount != 0 {
		return fmt.Errorf("business schema is missing a Chinese database comment")
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
		return fmt.Errorf("query business table comments: %w", err)
	}
	if invalidTableCommentCount != 0 {
		return fmt.Errorf("business schema contains %d tables without Chinese database comments", invalidTableCommentCount)
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
		return fmt.Errorf("query business column comments: %w", err)
	}
	if invalidColumnCommentCount != 0 {
		return fmt.Errorf("business schema contains %d columns without Chinese database comments", invalidColumnCommentCount)
	}
	return nil
}

// Close 关闭 Business PostgreSQL 底层连接池。
func (c *Client) Close() error {
	sqlDB, err := c.db.DB()
	if err != nil {
		return fmt.Errorf("get business postgres pool for close: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("close business postgres: %w", err)
	}
	return nil
}
