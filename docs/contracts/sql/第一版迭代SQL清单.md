# 第一版迭代 SQL 清单

状态：draft
owner：主控 Codex 汇总维护；业务微服务后端工程师和 Go Eino 智能体微服务架构工程师按数据库边界维护
更新时间：2026-06-25
适用范围：Dora-Agent 第一版本地开发 SQL 迭代脚本计划

## SQL 规则

- 业务数据库和 Agent 领域数据库分目录维护。
- 不添加数据库级外键约束，不写 `FOREIGN KEY` / `REFERENCES`。
- 跨表一致性由事务、唯一约束、普通索引、幂等记录和测试保证。
- 列表查询必须有分页索引设计，默认 10 条每页。
- 每个 SQL 脚本必须包含 up/down 或说明不可回滚原因。

## 建议目录

```text
db/migrations/iterations/20260625_first_version/
  business/
    001_create_account_space_project.up.sql
    001_create_account_space_project.down.sql
  agent/
    001_create_agent_runtime.up.sql
    001_create_agent_runtime.down.sql
```

## 业务库首批表

| 表 | owner | 来源契约 | 状态 |
| --- | --- | --- | --- |
| business_users | 业务后端 | 账户/空间 | planned |
| business_spaces | 业务后端 | 账户/空间 | planned |
| enterprises | 业务后端 | 企业空间 | planned |
| enterprise_members | 业务后端 | 企业空间 | planned |
| projects | 业务后端 | 项目容器 | planned |
| credit_accounts | 业务后端 | 积分 | planned |
| credit_batches | 业务后端 | 积分 | planned |
| credit_freezes | 业务后端 | 积分 | planned |
| credit_ledger_entries | 业务后端 | 积分 | planned |
| assets | 业务后端 | 资产 | planned |
| asset_upload_intents | 业务后端 | TOS 直传上传意图 | planned |
| asset_elements | 业务后端 | 资产元素 | planned |
| works | 业务后端 | 作品 | planned |
| work_public_snapshots | 业务后端 | 公开快照 | planned |
| content_safety_evidences | 业务后端 | 内容安全证据摘要 | planned |
| notifications | 业务后端 | 站内信 | planned |
| business_audit_logs | 业务后端 | 审计 | planned |
| idempotency_records | 业务后端 | 幂等 | planned |

## Agent 库首批表

| 表 | owner | 来源契约 | 状态 |
| --- | --- | --- | --- |
| agent_sessions | Go Eino | Agent 数据模型 | planned |
| agent_runs | Go Eino | Agent 数据模型 | planned |
| agent_messages | Go Eino | Agent 数据模型 | planned |
| agent_events | Go Eino | Agent 数据模型 | planned |
| agent_tool_calls | Go Eino | Agent 数据模型 | planned |
| agent_tasks | Go Eino | Agent 数据模型 | planned |
| agent_interrupts | Go Eino | Agent 数据模型 | planned |
| agent_artifacts | Go Eino | Agent 数据模型 | planned |
| agent_safety_evaluations | Go Eino | Agent 数据模型 | planned |
| agent_memories | Go Eino | Agent 数据模型 | optional |
| agent_runtime_configs | Go Eino | Agent 数据模型 | planned |

## 验证要求

- SQL lint：禁止 `FOREIGN KEY` 和 `REFERENCES`。
- migration up/down 本地可执行。
- Agent DB 边界测试：不得出现积分、业务资产事实、企业成员、作品公开状态。
- 业务 DB 边界测试：不得出现 Agent run/message/event/tool_call 内部状态。

## 待确认

- PostgreSQL schema 是否分 `business` / `agent`。
- 本地 migration 目录命名是否按日期还是迭代号。
