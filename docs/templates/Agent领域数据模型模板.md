# Agent 领域数据模型模板

状态：draft  
owner：Go Eino 智能体微服务架构工程师  
更新时间：YYYY-MM-DD  
适用范围：智能体微服务 Agent Runtime 数据库  

## 领域边界

说明该模型只保存 Agent Runtime 数据，不保存业务事实。业务事实必须通过 RPC 调用业务微服务产生。

## 表清单

| 表名 | 用途 | 主要关联 |
| --- | --- | --- |
| agent_sessions | 会话 | user_id、tenant_id |
| agent_runs | 运行 | session_id |
| agent_messages | 消息 | session_id、run_id |
| agent_events | 事件 | run_id |
| agent_tool_calls | 工具调用 | run_id |
| agent_tasks | 任务 | run_id |
| agent_interrupts | 中断 | run_id |
| agent_artifacts | 产物 | run_id |
| agent_memories | 记忆 | user_id、tenant_id |
| agent_runtime_configs | 配置 | tenant_id |

## 表字段

### 表名

| 字段 | 类型 | 必填 | 索引 | 说明 |
| --- | --- | --- | --- | --- |
| id | string | 是 | pk | 主键 |
|  |  |  |  |  |

## 状态机

| 对象 | 状态 | 可迁移到 | 触发条件 |
| --- | --- | --- | --- |
| run | pending | running | 开始执行 |
|  |  |  |  |

## 索引

| 表 | 索引字段 | 用途 |
| --- | --- | --- |
|  |  |  |

## CRUD

- Create：
- Read：
- Update：
- Delete / Archive：
- 幂等规则：

## 数据保留

- 事件：
- 消息：
- 产物：
- 记忆：
- 配置：

## 审计字段

- tenant_id：
- user_id：
- trace_id：
- created_at：
- updated_at：
- created_by：
- updated_by：
- deleted_at：

## 和业务数据库的边界

- 不保存业务事实：
- 不复制业务主数据：
- 业务写操作通过 RPC：
- 测试验证方式：

## 测试策略

- CRUD：
- 状态机：
- 幂等：
- 索引查询：
- 数据保留：
- 边界违规检查：
