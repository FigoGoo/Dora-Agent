# 三 Module 持久化基础 v1

> 文档状态：Implemented / v1
>
> 基线日期：2026-07-14
>
> 适用范围：Business、Agent、Worker 的时间、标识和 PostgreSQL Schema 门禁

## 1. 目的与边界

本基线建立后续领域 Repository、Outbox、Inbox、Job 和 Receipt 共同依赖的技术不变量，但不定义任何业务聚合、状态机或跨 Module 领域 Payload。

本轮明确不创建 User、Project、Session、Approval、Operation、Job、Outbox、Inbox、账本或 AIGC 领域表，也不解除 [AIGC 跨 Module 契约目录](aigc-contract-catalog.md) 的评审门禁。

## 2. Module 隔离

三个 Module 各自保存 `internal/clock` 和 `internal/idgen`，不创建第四个共享 Go Module，也不跨 Module import `internal` 包。

| 能力 | v1 约束 |
|---|---|
| System Clock | 只返回 UTC；具体业务在一次 Turn、Attempt 或事务入口冻结一次并复用 |
| UUID Generator | 使用 `google/uuid` 生成 RFC 9562 UUIDv7；随机源失败时返回错误，不降级为 UUIDv4、数据库默认值或时间字符串 |
| 测试替身 | 消费方继续定义最小 Clock/IDGenerator 接口，测试注入固定实现；生产包不提供可变全局时钟或全局 Seed |
| Worker Lease | Lease 到期和调度判断仍以 PostgreSQL 当前时间为准，不能改用进程 System Clock |

## 3. PostgreSQL Schema 契约

Business、Agent、Worker Runtime 在原有 Schema 存在性检查后继续验证：

1. Owner Schema 不存在 `pg_constraint.contype = 'f'` 的物理外键；
2. Schema 具有包含中文字符的数据库 COMMENT；
3. Owner Schema 中每张普通表或分区表具有包含中文字符的数据库 COMMENT；
4. 每个未删除的业务字段具有包含中文字符的数据库 COMMENT。

任何一项失败都会阻止 Runtime 进入 Readiness。检查只读取 PostgreSQL Catalog，不执行 DDL、AutoMigrate 或跨 Schema 查询。

静态 Migration 检查同时拒绝 `FOREIGN KEY`、`REFERENCES`、`ON DELETE CASCADE` 和 `ON UPDATE CASCADE`。静态检查不能替代真实 PostgreSQL Catalog 验证。

## 4. 测试和 CI

- `TestMigratedSchemaContract` 只在显式设置 `DORA_POSTGRES_CONTRACT_DSN` 时连接真实 PostgreSQL；普通单元测试不会隐式访问网络。
- `scripts/check-database-contracts.sh` 可验证单个 Module 或全部 Module，CI 的空库 Migration Matrix 在 Up → Down 1 → Up 后执行该检查。
- 本地 `make check-database-contracts` 使用 `.env.local` 或显式 `ENV_FILE` 中的三个独立数据库连接。
- 后续新增领域表时不需要复制新的门禁脚本；现有 Catalog 检查会自动覆盖新增表和字段。

## 5. 验收

- [x] 三 Module 均具有独立 UTC Clock 和 UUIDv7 Generator。
- [x] Foundation Handler/Client 已使用注入式生产实现，固定测试替身继续生效。
- [x] Runtime Schema 检查拒绝物理外键和缺失中文 COMMENT。
- [x] 真实 PostgreSQL 契约测试已进入 CI 空库 Migration Matrix。
- [x] 未创建未经评审的领域表、Event、Job Payload 或跨 Module Consumer。
