// Package postgres 负责 Agent Service 的 GORM PostgreSQL 基础连接。
package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const schemaName = "agent"

// requiredAgentTables 返回 W1-B1 Agent Runtime Ready 前必须存在的权威基础表。
// 列表保持稳定顺序，使缺表错误和测试证据可重复；新增必需表时必须同步 Migration 与契约测试。
func requiredAgentTables() []string {
	return []string{
		"session",
		"session_command_receipt",
		"session_event_counter",
		"session_event_log",
		"session_input",
		"session_message",
		"session_runtime_lease",
		"session_sequence_counter",
		"session_skill_snapshot",
		"session_skill_snapshot_item",
	}
}

// tableNameRecord 承接一次固定 Schema 查询返回的表名，不向 Repository 或领域层暴露。
type tableNameRecord struct {
	// TableName 是 Agent Schema 中已经存在的普通表或分区表名称。
	TableName string `gorm:"column:table_name"`
}

// Client 封装 Agent PostgreSQL 连接和底层连接池生命周期。
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
		return nil, fmt.Errorf("open agent postgres: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get agent postgres pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping agent postgres: %w", err)
	}
	return &Client{db: db}, nil
}

// VerifySchema 确认 Agent 版本化 Migration 已创建权威 Schema。
func (c *Client) VerifySchema(ctx context.Context, timeout time.Duration) error {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var exists bool
	if err := c.db.WithContext(checkCtx).
		Raw("SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = ?)", schemaName).
		Scan(&exists).Error; err != nil {
		return fmt.Errorf("query agent schema: %w", err)
	}
	if !exists {
		return fmt.Errorf("agent schema is missing; run agent migrations")
	}
	if err := c.verifyRequiredTables(checkCtx); err != nil {
		return err
	}
	if err := c.verifySchemaContract(checkCtx); err != nil {
		return err
	}
	return nil
}

// verifyRequiredTables 使用一次固定查询核对 W0 所有权威基础表。
// 仅创建旧的 Agent Schema 不足以 Ready；缺表时返回稳定、有序的内部诊断并阻止接收流量。
func (c *Client) verifyRequiredTables(ctx context.Context) error {
	var records []tableNameRecord
	if err := c.db.WithContext(ctx).Raw(`
		SELECT relation.relname AS table_name
		FROM pg_class AS relation
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = ?
		  AND relation.relkind IN ('r', 'p')`, schemaName).
		Scan(&records).Error; err != nil {
		return fmt.Errorf("query agent required tables: %w", err)
	}
	existing := make(map[string]struct{}, len(records))
	for _, record := range records {
		existing[record.TableName] = struct{}{}
	}
	missing := make([]string, 0)
	for _, required := range requiredAgentTables() {
		if _, ok := existing[required]; !ok {
			missing = append(missing, required)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("agent schema missing required tables: %s; run agent migrations", strings.Join(missing, ","))
	}
	return nil
}

// verifySchemaContract 验证 Agent Schema 不含物理外键，且 Schema、表和字段均具有中文说明。
func (c *Client) verifySchemaContract(ctx context.Context) error {
	var physicalForeignKeyCount int64
	if err := c.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM pg_constraint AS constraint_record
		JOIN pg_class AS relation ON relation.oid = constraint_record.conrelid
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = ? AND constraint_record.contype = 'f'`, schemaName).
		Scan(&physicalForeignKeyCount).Error; err != nil {
		return fmt.Errorf("query agent physical foreign keys: %w", err)
	}
	// 物理外键会绕过 Agent 状态机和显式补偿，必须在 Runtime 就绪前失败关闭。
	if physicalForeignKeyCount != 0 {
		return fmt.Errorf("agent schema contains %d physical foreign key constraints", physicalForeignKeyCount)
	}

	var invalidSchemaCommentCount int64
	if err := c.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM pg_namespace AS namespace
		WHERE namespace.nspname = ?
		  AND COALESCE(obj_description(namespace.oid, 'pg_namespace'), '') !~ '[一-龥]'`, schemaName).
		Scan(&invalidSchemaCommentCount).Error; err != nil {
		return fmt.Errorf("query agent schema comment: %w", err)
	}
	if invalidSchemaCommentCount != 0 {
		return fmt.Errorf("agent schema is missing a Chinese database comment")
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
		return fmt.Errorf("query agent table comments: %w", err)
	}
	if invalidTableCommentCount != 0 {
		return fmt.Errorf("agent schema contains %d tables without Chinese database comments", invalidTableCommentCount)
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
		return fmt.Errorf("query agent column comments: %w", err)
	}
	if invalidColumnCommentCount != 0 {
		return fmt.Errorf("agent schema contains %d columns without Chinese database comments", invalidColumnCommentCount)
	}
	return nil
}

// Close 关闭 Agent PostgreSQL 底层连接池。
func (c *Client) Close() error {
	sqlDB, err := c.db.DB()
	if err != nil {
		return fmt.Errorf("get agent postgres pool for close: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("close agent postgres: %w", err)
	}
	return nil
}
