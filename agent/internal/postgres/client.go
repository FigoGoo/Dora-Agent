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

func requiredCreationSpecPreviewTables() []string {
	return []string{
		"creation_spec_preview_model_receipt",
		"creation_spec_preview_projection",
		"creation_spec_preview_run",
		"creation_spec_preview_tool_receipt",
	}
}

func requiredUserMessageRuntimeTables() []string {
	return []string{
		"session_user_message_turn_context",
		"session_user_message_model_receipt",
		"session_user_message_output_projection",
		"session_user_message_output_receipt",
		"session_user_message_run",
		"session_user_message_turn",
		"session_user_message_upgrade_ledger",
	}
}

// requiredAnalyzeMaterialsRuntimeTables 返回素材分析开发预览启用时必须完整存在的隔离表。
func requiredAnalyzeMaterialsRuntimeTables() []string {
	return []string{
		"analyze_materials_preview_model_receipt",
		"analyze_materials_preview_projection",
		"analyze_materials_preview_run",
		"analyze_materials_preview_tool_receipt",
		"analyze_materials_preview_turn_context",
	}
}

// requiredPlanStoryboardRuntimeTables 返回故事板规划开发预览启用时必须完整存在的隔离表。
func requiredPlanStoryboardRuntimeTables() []string {
	return []string{
		"plan_storyboard_preview_model_receipt",
		"plan_storyboard_preview_run",
		"plan_storyboard_preview_tool_receipt",
		"plan_storyboard_preview_turn_context",
	}
}

// requiredWritePromptsRuntimeTables 返回 Prompt 写作开发预览启用时必须完整存在的隔离表。
func requiredWritePromptsRuntimeTables() []string {
	return []string{
		"write_prompts_preview_model_receipt",
		"write_prompts_preview_run",
		"write_prompts_preview_tool_receipt",
		"write_prompts_preview_turn_context",
	}
}

func requiredMediaRuntimeTables() []string {
	return []string{"media_preview_request", "media_preview_operation", "media_preview_batch",
		"media_preview_job", "media_preview_dispatch_outbox", "media_preview_terminal_outbox"}
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

// VerifyCreationSpecPreviewSchema 在 local Preview flag 开启时额外要求全部版本化运行时表存在。
func (c *Client) VerifyCreationSpecPreviewSchema(ctx context.Context, timeout time.Duration) error {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var records []tableNameRecord
	if err := c.db.WithContext(checkCtx).Raw(`
		SELECT relation.relname AS table_name
		FROM pg_class AS relation
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = ? AND relation.relkind IN ('r', 'p')`, schemaName).Scan(&records).Error; err != nil {
		return fmt.Errorf("query creation spec preview tables: %w", err)
	}
	existing := make(map[string]struct{}, len(records))
	for _, record := range records {
		existing[record.TableName] = struct{}{}
	}
	var missing []string
	for _, table := range requiredCreationSpecPreviewTables() {
		if _, ok := existing[table]; !ok {
			missing = append(missing, table)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("agent schema missing Preview tables: %s; run agent migrations", strings.Join(missing, ","))
	}
	return nil
}

// VerifyUserMessageRuntimeSchema 在本地方案 A 开启时要求全部隔离表存在。
func (c *Client) VerifyUserMessageRuntimeSchema(ctx context.Context, timeout time.Duration) error {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var records []tableNameRecord
	if err := c.db.WithContext(checkCtx).Raw(`
		SELECT relation.relname AS table_name
		FROM pg_class AS relation
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = ? AND relation.relkind IN ('r', 'p')`, schemaName).Scan(&records).Error; err != nil {
		return fmt.Errorf("query user message runtime tables: %w", err)
	}
	existing := make(map[string]struct{}, len(records))
	for _, record := range records {
		existing[record.TableName] = struct{}{}
	}
	var missing []string
	for _, table := range requiredUserMessageRuntimeTables() {
		if _, ok := existing[table]; !ok {
			missing = append(missing, table)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("agent schema missing user message runtime tables: %s; run agent migrations", strings.Join(missing, ","))
	}
	var legacyUpgradeReady bool
	if err := c.db.WithContext(checkCtx).Raw(`
		SELECT
			EXISTS (
				SELECT 1 FROM schema_migrations
				WHERE version >= 20260717000900 AND dirty = false
			)
			AND EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'agent'
				  AND table_name = 'session_user_message_upgrade_ledger'
				  AND column_name = 'upgrade_generation'
			)
			AND EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'agent'
				  AND table_name = 'session_user_message_upgrade_ledger'
				  AND column_name = 'version'
			)
			AND EXISTS (
				SELECT 1
				FROM pg_trigger AS trigger_record
				JOIN pg_class AS relation ON relation.oid = trigger_record.tgrelid
				JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
				WHERE namespace.nspname = 'agent'
				  AND relation.relname = 'session_user_message_upgrade_ledger'
				  AND trigger_record.tgname = 'trg_session_user_message_upgrade_ledger__guard'
				  AND NOT trigger_record.tgisinternal
			)
			AND EXISTS (
				SELECT 1
				FROM pg_trigger AS trigger_record
				JOIN pg_class AS relation ON relation.oid = trigger_record.tgrelid
				JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
				WHERE namespace.nspname = 'agent'
				  AND relation.relname = 'session_command_receipt'
				  AND trigger_record.tgname = 'trg_session_command_receipt__immutable'
				  AND NOT trigger_record.tgisinternal
			)
			AND EXISTS (
				SELECT 1
				FROM pg_trigger AS trigger_record
				JOIN pg_class AS relation ON relation.oid = trigger_record.tgrelid
				JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
				WHERE namespace.nspname = 'agent'
				  AND relation.relname = 'session_message'
				  AND trigger_record.tgname = 'trg_session_message__immutable'
				  AND NOT trigger_record.tgisinternal
			)`).
		Scan(&legacyUpgradeReady).Error; err != nil {
		return fmt.Errorf("query user message legacy upgrade generation: %w", err)
	}
	if !legacyUpgradeReady {
		return fmt.Errorf("agent user message legacy upgrade generation is not ready; run agent migrations")
	}
	return nil
}

// VerifyAnalyzeMaterialsRuntimeSchema 在本地素材分析 Profile 开启时要求全部隔离表和数据库 Guard 存在。
func (c *Client) VerifyAnalyzeMaterialsRuntimeSchema(ctx context.Context, timeout time.Duration) error {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var records []tableNameRecord
	if err := c.db.WithContext(checkCtx).Raw(`
		SELECT relation.relname AS table_name
		FROM pg_class AS relation
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = ? AND relation.relkind IN ('r', 'p')`, schemaName).Scan(&records).Error; err != nil {
		return fmt.Errorf("query analyze materials runtime tables: %w", err)
	}
	existing := make(map[string]struct{}, len(records))
	for _, record := range records {
		existing[record.TableName] = struct{}{}
	}
	var missing []string
	for _, table := range requiredAnalyzeMaterialsRuntimeTables() {
		if _, ok := existing[table]; !ok {
			missing = append(missing, table)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("agent schema missing analyze materials runtime tables: %s; run agent migrations", strings.Join(missing, ","))
	}
	var guardCount int64
	if err := c.db.WithContext(checkCtx).Raw(`
		SELECT COUNT(*)
		FROM pg_trigger AS trigger_record
		JOIN pg_class AS relation ON relation.oid = trigger_record.tgrelid
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'agent'
		  AND NOT trigger_record.tgisinternal
		  AND trigger_record.tgname IN (
		      'trg_analyze_materials_preview_turn_context__immutable',
		      'trg_analyze_materials_preview_model_receipt__guard',
		      'trg_analyze_materials_preview_tool_receipt__guard',
		      'trg_analyze_materials_preview_projection__immutable'
		  )`).Scan(&guardCount).Error; err != nil {
		return fmt.Errorf("query analyze materials runtime guards: %w", err)
	}
	if guardCount != 4 {
		return fmt.Errorf("agent analyze materials runtime guards are incomplete; run agent migrations")
	}
	return nil
}

// VerifyPlanStoryboardRuntimeSchema 在本地故事板规划 Profile 开启时要求全部隔离表和数据库 Guard 存在。
func (c *Client) VerifyPlanStoryboardRuntimeSchema(ctx context.Context, timeout time.Duration) error {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var records []tableNameRecord
	if err := c.db.WithContext(checkCtx).Raw(`
		SELECT relation.relname AS table_name
		FROM pg_class AS relation
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = ? AND relation.relkind IN ('r', 'p')`, schemaName).Scan(&records).Error; err != nil {
		return fmt.Errorf("query plan storyboard runtime tables: %w", err)
	}
	existing := make(map[string]struct{}, len(records))
	for _, record := range records {
		existing[record.TableName] = struct{}{}
	}
	var missing []string
	for _, table := range requiredPlanStoryboardRuntimeTables() {
		if _, ok := existing[table]; !ok {
			missing = append(missing, table)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("agent schema missing plan storyboard runtime tables: %s; run agent migrations", strings.Join(missing, ","))
	}
	var guardCount int64
	if err := c.db.WithContext(checkCtx).Raw(`
		SELECT COUNT(*)
		FROM pg_trigger AS trigger_record
		JOIN pg_class AS relation ON relation.oid = trigger_record.tgrelid
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'agent'
		  AND NOT trigger_record.tgisinternal
		  AND trigger_record.tgname IN (
		      'trg_plan_storyboard_preview_turn_context__immutable',
		      'trg_plan_storyboard_preview_model_receipt__guard',
		      'trg_plan_storyboard_preview_tool_receipt__guard'
		  )`).Scan(&guardCount).Error; err != nil {
		return fmt.Errorf("query plan storyboard runtime guards: %w", err)
	}
	if guardCount != 3 {
		return fmt.Errorf("agent plan storyboard runtime guards are incomplete; run agent migrations")
	}
	return nil
}

// VerifyWritePromptsRuntimeSchema 在本地 Prompt 写作 Profile 开启时要求全部隔离表和数据库 Guard 存在。
func (c *Client) VerifyWritePromptsRuntimeSchema(ctx context.Context, timeout time.Duration) error {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var records []tableNameRecord
	if err := c.db.WithContext(checkCtx).Raw(`
		SELECT relation.relname AS table_name
		FROM pg_class AS relation
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = ? AND relation.relkind IN ('r', 'p')`, schemaName).Scan(&records).Error; err != nil {
		return fmt.Errorf("query write prompts runtime tables: %w", err)
	}
	existing := make(map[string]struct{}, len(records))
	for _, record := range records {
		existing[record.TableName] = struct{}{}
	}
	var missing []string
	for _, table := range requiredWritePromptsRuntimeTables() {
		if _, ok := existing[table]; !ok {
			missing = append(missing, table)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("agent schema missing write prompts runtime tables: %s; run agent migrations", strings.Join(missing, ","))
	}
	var guardCount int64
	if err := c.db.WithContext(checkCtx).Raw(`
		SELECT COUNT(*)
		FROM pg_trigger AS trigger_record
		JOIN pg_class AS relation ON relation.oid = trigger_record.tgrelid
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'agent'
		  AND NOT trigger_record.tgisinternal
		  AND trigger_record.tgname IN (
		      'trg_write_prompts_preview_turn_context__immutable',
		      'trg_write_prompts_preview_model_receipt__guard',
		      'trg_write_prompts_preview_tool_receipt__guard'
		  )`).Scan(&guardCount).Error; err != nil {
		return fmt.Errorf("query write prompts runtime guards: %w", err)
	}
	if guardCount != 3 {
		return fmt.Errorf("agent write prompts runtime guards are incomplete; run agent migrations")
	}
	return nil
}

// VerifyMediaRuntimeSchema 要求 media.runtime.v3preview1 的六张 Agent-owned 表全部存在。
func (c *Client) VerifyMediaRuntimeSchema(ctx context.Context, timeout time.Duration) error {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var records []tableNameRecord
	if err := c.db.WithContext(checkCtx).Raw(`SELECT relation.relname AS table_name
		FROM pg_class AS relation JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = ? AND relation.relkind IN ('r','p')`, schemaName).Scan(&records).Error; err != nil {
		return fmt.Errorf("query media runtime tables: %w", err)
	}
	existing := make(map[string]bool, len(records))
	for _, record := range records {
		existing[record.TableName] = true
	}
	var missing []string
	for _, name := range requiredMediaRuntimeTables() {
		if !existing[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("agent schema missing media runtime tables: %s; run agent migrations", strings.Join(missing, ","))
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
