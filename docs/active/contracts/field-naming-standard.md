# 字段命名规范

状态：active  
owner：文档与契约责任域  
更新时间：2026-07-01  
适用范围：Thrift、OpenAPI、AG-UI JSON Schema、SQL migration、fixture 和生成类型

## ID 字段

1. 所有 ID 使用 string。
2. 字段名必须表达业务语义：`run_id`、`session_id`、`board_id`、`listing_id`、`usage_id`。
3. 不混用 `id`、`uid`、`uuid` 作为领域字段名。
4. 外部 provider task id 必须使用 `provider_task_id`，不得覆盖平台 `task_id`。

## 时间字段

| 位置 | 类型 | 规则 |
| --- | --- | --- |
| API / AG-UI / fixture | RFC3339 string | 字段名使用 `created_at`、`updated_at`、`expires_at`、`deleted_at` |
| PostgreSQL | `timestamptz` | 默认 `now()` 只用于创建时间 |

## 状态字段

1. 状态必须来自 `api/schemas/common/state-enum-registry.schema.json`。
2. 不新增 `processing`、`in_progress`、`doing` 等临时状态。
3. 同一概念在 API、RPC、SQL、fixture 中必须使用同一个枚举值。

## 积分字段

1. 积分字段统一使用 `points` 后缀和 int64 / integer。
2. 不使用 float 表示积分。
3. 冻结、扣费、释放和退款必须记录 `source_type`、`source_id`、`idempotency_key` 和 `trace_id`。

## Digest 字段

1. 格式统一为 `sha256:<64 hex>`。
2. 用于 `tool_plan_digest`、`skill_spec_digest`、`graph_plan_digest`、`payload_digest`、`content_digest`。
3. digest schema 以 `api/schemas/common/digest.schema.json` 为准。

## JSONB 字段

JSONB 载荷必须包含：

```json
{
  "schema_version": "xxx.v1",
  "content_digest": "sha256:<hex>",
  "summary": {}
}
```

## 敏感字段

字段字典必须标注：

```text
sensitivity: public | user_private | enterprise_private | internal_secret | audit_only
```

创作者默认不能看到用户原始输入、上传资产、Creative Board 详情和生成资产。相关接口必须返回聚合指标或脱敏摘要。
