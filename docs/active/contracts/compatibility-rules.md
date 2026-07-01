# 契约兼容规则

状态：active  
owner：文档与契约责任域  
更新时间：2026-07-01  
适用范围：JSON Schema、AG-UI、OpenAPI、Thrift、SQL migration、fixture 和生成类型

## JSON Schema

1. 每个 schema 必须有 `$schema`、`$id`、`title`、`type`、`schema_version` 或等价 const。
2. `required`、enum、nullable 必须明确。
3. 新增字段默认向后兼容；删除、重命名、改变语义必须新版本。
4. 每个 active schema 至少被一个 fixture 引用。

## AG-UI

1. Envelope 使用 `event_type`、`seq`、`created_at`、`dedupe_key`。
2. 同一 `run_id` 内 `seq` 单调递增。
3. `dedupe_key` 用于前端去重。
4. 未知 `event_type` 前端必须忽略并记录，不崩溃。
5. 事件 payload schema 独立放在 `api/agui/events/**`。

## Thrift

1. 新增字段只追加编号。
2. 写操作必须有 `RequestContext` 和 `idempotency_key`。
3. 返回对象必须包含业务状态，不只返回 `success=true`。
4. 破坏性变更必须新增 service 或 method 版本。

## OpenAPI

1. 每个 endpoint 必须有 `operationId`。
2. request / response DTO 不暴露 ORM 或内部 JSONB 原文。
3. 错误响应统一使用 `Error.v1`。
4. 用户端 API 不返回创作者可见数据；创作者 API 不返回用户私有创作数据。

## SQL

1. migration 必须有 up/down。
2. 禁止数据库级 `FOREIGN KEY` / `REFERENCES`。
3. 写路径必须有幂等唯一索引。
4. 常用查询路径必须有索引。
5. JSONB 字段必须有 `schema_version` 或 digest。

## Fixture

1. fixture 是契约可执行样例，不是随意 mock。
2. fixture 必须覆盖 happy path、失败路径、幂等重试、权限拒绝、replay / snapshot。
3. billing fixture 必须覆盖 freeze、commit、release、refund 的幂等。
