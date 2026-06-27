# 14-SQL迁移本地配置测试矩阵与验收设计

状态：archived
owner：业务服务责任域、测试与验收责任域 
更新时间：2026-06-28
适用范围：业务数据库 migration、本地配置、RPC contract test、HTTP API contract test、业务集成测试、Agent 依赖验收和交付 Done Gate  
相关代码路径：`db/migrations/iterations/**/business/**`、`.env.example`、`tests/business/**`、`tests/contract/**`

## 目标

汇总业务服务实现前后的 migration、配置、测试和验收要求。每个功能点进入开发时必须同步 SQL、RPC contract test、HTTP API contract test、业务集成测试和 Agent 依赖测试。

## SQL 目录

```text
db/migrations/iterations/2026-06-27-business-core/
  business/
    0001_common_idempotency_audit.up.sql
    0001_common_idempotency_audit.down.sql
    0002_account_auth_space_enterprise.up.sql
    0002_account_auth_space_enterprise.down.sql
    0003_platform_admin_user_audit.up.sql
    0003_platform_admin_user_audit.down.sql
    0004_project.up.sql
    0004_project.down.sql
    0005_model_config.up.sql
    0005_model_config.down.sql
    0006_tool_policy_pricing.up.sql
    0006_tool_policy_pricing.down.sql
    0007_skill_catalog_review.up.sql
    0007_skill_catalog_review.down.sql
    0008_credit_account_tool_charge_redeem.up.sql
    0008_credit_account_tool_charge_redeem.down.sql
    0009_asset_upload_element_access.up.sql
    0009_asset_upload_element_access.down.sql
    0010_asset_commit_credit_close_loop.up.sql
    0010_asset_commit_credit_close_loop.down.sql
    0011_work_public_snapshot_like.up.sql
    0011_work_public_snapshot_like.down.sql
    0012_notification.up.sql
    0012_notification.down.sql
```

## SQL 检查规则

- 不包含数据库级外键或引用约束关键字。
- 每张表有主键或唯一业务 ID。
- 列表查询路径有必要索引。
- 大表新增索引说明锁风险。
- down 脚本可回滚；不可回滚必须说明人工方式。
- 初始化数据标明来源，例如初始平台管理员、内置资产元素类型、作品分类、默认 Tool。
- `idempotency_records` 必须使用 `(tenant_id, scope, idempotency_key)` 唯一约束，避免个人空间和企业空间幂等键互相污染。
- 私有业务表按 02 公共字段基线补齐 `tenant_id/space_id/created_by/updated_by/deleted_at` 或在表设计中说明 append-only 例外。

## 本地配置

| 配置项 | 是否需要加入 `.env.example` | 说明 |
| --- | --- | --- |
| `BUSINESS_DATABASE_URL` | 是 | 业务库连接 |
| `ETCD_ENDPOINTS` | 是 | Kitex 注册发现 |
| `BUSINESS_SERVICE_NAME` | 是 | 服务名 |
| `BUSINESS_HTTP_ENABLED`、`BUSINESS_HTTP_ADDR` | 是 | 业务 HTTP API 适配层 |
| `ADMIN_BOOTSTRAP_ACCOUNT`、`ADMIN_BOOTSTRAP_PASSWORD_HASH`、`ADMIN_BOOTSTRAP_CREDENTIAL_SECRET_REF` | 是 | 空库初始化平台管理员；生产通过安全配置注入 hash 或凭证引用 |
| `PUBLIC_WEB_BASE_URL` | 是 | 生成公开作品可复制分享链接 |
| `TOS_ENDPOINT`、`TOS_BUCKET`、`TOS_BASE_URL`、`TOS_REGION` | 是 | 对象存储连接配置，本地允许使用 MinIO/TOS 兼容值 |
| `SECRET_ENCRYPTION_KEY_REF` | 是 | 密钥加密引用 |
| `VOLC_TLS_ENDPOINT`、`VOLC_TLS_REGION`、`VOLC_TLS_PROJECT_ID`、`VOLC_TLS_TOPIC_ID` | 是 | 生产日志检索配置；本地可为空并落本地结构化日志 |

真实密钥不得提交。

## 测试矩阵

| 功能点 | RPC contract test | HTTP API contract test | 集成测试 | 数据库验证 |
| --- | --- | --- | --- | --- |
| 通用 DTO | AuthContext、RequestMeta、BusinessError | ApiResponse、ApiError、PageResult、错误状态映射 | 错误映射 | 幂等和审计表 |
| 账户空间 | ResolveSpace、企业成员错误码 | `/api/auth/**`、`/api/account/**`、`/api/enterprise/**` | 注册、企业创建、移除、转让 | user/space/member 状态 |
| 后台 | 管理员态、用户摘要脱敏 | `/api/admin/auth/**`、`/api/admin/users/**`、`/api/admin/audit-logs` | 初始管理员 seed、首次密码轮换、禁用启用、审计 | admin bootstrap + user status + audit |
| 项目 | CheckProjectAccess 全 purpose | `/api/projects/**` | 创建、归档、恢复、跨空间拒绝 | project/project_assets |
| 模型 | 可选模型、默认模型、价格快照、模型运行快照 | `/api/models/generation`、`/api/admin/models/**` | 停用默认模型保护、确认后模型停用阻断 | model/default/price |
| Tool | Tool policy DTO、charge mode DTO | `/api/tools/bindable`、`/api/admin/tools/**` | 停用、白名单、高风险确认、计价策略切换 | tool/policy/whitelist/pricing |
| Skill | runtime spec DTO、审核错误码、Memory 默认值、confirmation_policy、过程态/最终态输出元素 | `/api/skills/**`、`/api/admin/skills/**` | 发布、审核、回滚、通知、Memory 默认开启、企业空间个人 Skill 策略 | skill/version/review |
| 积分 | estimate/freeze/charge-tool/release DTO、兑换码绑定错误码 | `/api/credits/**`、`/api/enterprise/credits`、`/api/admin/credits/**` | 并发冻结、余额不足、Tool 独立扣费、重复明细防重、兑换码用户/企业/渠道绑定、释放 | batch/freeze/tool_charge/redeem/ledger |
| 资产 | upload intent、generated object slots、batch access、14 个元素类型同步审计 | `/api/assets/**`、`/api/asset-element-types` | TOS 确认、生成产物对象槽、权限、预览下载、内置元素类型发布/变更审计 | asset/object/slot/elements/change_records |
| 资产扣费 | commit DTO、`storage_object_ref` | 无直接 HTTP，验证禁止前端直连 | 保存成功扣费、object key 不匹配拒绝、失败释放 | asset + slot + freeze + ledger |
| 作品 | public snapshot DTO、可复制分享链接、企业成员状态校验 | `/api/public/**`、`/api/works/**`、`/api/admin/works/public/**` | 分享、取消、点赞、下架、企业成员移除后私有作品拒绝、公开列表详情返回 share_url | work/snapshot/like |
| 通知 | list/read DTO | `/api/notifications/**` | 审核通知、未读数 | notification read_at |

## 【Agent开发】联调验收矩阵

| Agent 主链路 | 业务 RPC | 验收点 |
| --- | --- | --- |
| 创建 run | `ResolveCurrentSpaceContext`、`CheckProjectAccess` | 权限上下文正确，项目归档阻断 |
| Skill 路由 | `ListRoutableSkills`、`GetPublishedSkillSpec` | 只返回 Published，个人/企业范围正确 |
| Tool 执行 | `CheckToolExecutionPolicy` | high risk 需要确认，disabled 阻断 |
| 模型选择 | `ListAvailableGenerationModels`、`ResolveDefaultModel`、`ResolveGenerationModelSnapshot` | 只返回展示名、价格快照和非敏感运行快照 |
| 安全前置 | 业务写 RPC 和积分预估 RPC 接收 `SafetyEvidenceDTO` | 缺失/过期/摘要不匹配拒绝，且不创建预估 |
| 积分闭环 | `EstimateGenerationCredits`、`EstimateToolCredits`、`FreezeCredits`、`ChargeToolUsageCredits`、`ReleaseFrozenCredits` | 安全通过后预估，确认后冻结，独立 Tool 成功扣费，失败释放，`credit_account_scope` 字段一致 |
| 资产保存扣费 | `PrepareGeneratedAssetObjects`、`CommitGeneratedAssetAndCharge` | 业务生成 object key，Agent 上传后 commit；保存成功才扣费，按 `estimate_item_id` 防重复扣费，部分完成部分扣费 |
| 资产引用 | `BatchCheckAssetAccess` | 批量校验，跨空间拒绝 |

## 设计对齐 Done Gate

- [x] 所有文档中的业务表都有 migration 计划或明确落到对应业务领域文档。
- [x] 每张业务表都有字段名、类型、必填、默认值、索引/约束和说明。
- [x] 所有写 RPC 有 `idempotency_key` 设计。
- [x] 所有写 RPC 幂等记录以 `(tenant_id, scope, idempotency_key)` 唯一，跨空间同 key 不冲突。
- [x] 所有写 HTTP API 通过 `Idempotency-Key` 进入 `RequestMeta.idempotency_key`。
- [x] 所有列表 RPC 默认 `page_size=10`，最大 50；`ListAssetElementTypes` 明确使用 `page_size=50` 的字典类例外。
- [x] 所有列表 HTTP API 默认 `page_size=10`，最大 50。
- [x] 所有业务写入有权限校验和审计或明确不审计原因。
- [x] 用户端、管理端、公开端接口都落在对应业务领域文档，不存在脱离领域状态机的单独对接设计。
- [x] 所有 Agent 依赖都有【Agent开发】参数说明。
- [x] 空库初始化能创建且仅创建一个初始平台管理员，并强制首次密码轮换；轮换成功后 bootstrap 状态为 `rotated`。
- [x] Skill `memory_policy` 未传时默认 session summary 策略，Published runtime spec 原样返回 `enabled=true`、`allowed_scopes=["session_summary"]`、`retention_days=30`、`requires_user_authorization=true`、`write_mode=summary_only`、`redaction_level=strict`。
- [x] 兑换码绑定用户、企业、渠道的成功和失败路径已有 contract fixture 设计。
- [x] 公开作品列表、详情和分享结果都返回可复制 `share_url`，且不泄露私有 object key。
- [x] 内置资产元素类型发布或变更写 `asset_element_type_change_records` 和后台审计日志。
- [x] `generated_asset_object_slots` 有 migration、过期清理策略、object key 不匹配拒绝测试设计。
- [x] 企业空间个人 Skill 使用、企业 Tool allowlist、企业积分账户、企业作品权限失效均有集成测试设计。
- [x] Tool 积分表 `tool_pricing_policies`、`credit_estimate_items`、`credit_tool_charge_batches`、`credit_tool_charge_items` 均有 migration、repository test 和 contract fixture 设计。
- [x] contract test 覆盖正常、业务错误、权限错误、幂等冲突、超时的设计已列出。
- [x] 集成测试覆盖关键事务回滚的设计已列出。
- [x] migration 设计不包含数据库级外键。
- [x] 日志脱敏测试要求已列出。
- [x] Agent DB 与业务 DB 边界测试已列出：业务库不出现 Agent session/run/message/event/tool_call/blackboard/memory 表。
- [x] 进入生产前必须通过 [15-生产级闭眼开发门禁与Agent对齐验收设计](./15-生产级闭眼开发门禁与Agent对齐验收设计.md)。

## 后续实现验收项

以下事项在对应功能切片开发完成后验证，不作为当前文档设计对齐的未完成项：

- `api/thrift/business_agent_service.thrift`、`api/openapi/business-api.yaml`、migration、seed 和 fixture 必须按 `code-plan` 同步落地。
- contract test 必须实际覆盖正常、业务错误、权限错误、幂等冲突、超时和版本兼容。
- repository、集成测试、日志脱敏测试、事务回滚测试和数据库边界测试必须在实现完成后执行并记录结果。
