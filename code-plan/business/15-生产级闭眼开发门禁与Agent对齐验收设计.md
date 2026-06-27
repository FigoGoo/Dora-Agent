# 15-生产级闭眼开发门禁与 Agent 对齐验收设计

状态：production-design-ready  
owner：业务微服务后端工程师、浏览器/RPC 与数据库测试工程师  
更新时间：2026-06-27  
适用范围：业务微服务从设计进入全量开发、联调、生产部署前检查、回滚和 Agent 对齐验收  
相关代码路径：`services/business/**`、`api/thrift/**`、`api/openapi/**`、`db/migrations/iterations/**/business/**`、`tests/business/**`、`tests/contract/**`、`.env.example`  
相关对齐文档：`code-plan/agent/07-RPC客户端业务能力调用与DTO映射设计.md`、`code-plan/agent/09-积分确认冻结生成资产保存扣费释放闭环设计.md`、`code-plan/agent/11-日志观测错误处理配置化与测试验收设计.md`、`code-plan/agent/14-生产级闭眼开发门禁与任务切片验收设计.md`

## 文档目标

- 定义业务微服务“全量闭眼开发到生产”的进入门禁。
- 将业务开发拆成可提交、可测试、可回滚、可与 Agent 联调的功能切片。
- 冻结业务侧需要交付给 Agent、前端、管理端和测试的契约、fixture、SQL 和配置。
- 明确生产单机 CentOS 8 部署前必须检查的配置、迁移、健康检查、日志和回滚证据。

## 闭眼开发定义

满足以下条件时，开发者可以按文档直接开发，不需要再猜字段、状态、错误码、事务、SQL、权限和验收标准。

| 维度 | 必须满足 |
| --- | --- |
| 契约 | Thrift IDL、HTTP OpenAPI、错误码、分页、幂等、preview/confirm 语义冻结。 |
| 数据库 | 每张业务表字段、类型、索引、唯一约束、状态枚举、up/down migration 和初始化数据明确。 |
| 代码落点 | 每个功能点有 package、Application、Domain、Repository、RPC handler、HTTP handler、DTO mapper 和测试路径。 |
| Agent 对齐 | Agent 07 中所有 RPC 方法、字段名、超时、重试、幂等、错误码和 fixture 在业务侧有对应实现说明。 |
| 前端对齐 | 用户端、公开端、管理端 HTTP API、DTO、错误状态和鉴权边界落回对应业务领域。 |
| 事务闭环 | 所有写操作的事务边界、幂等 request hash、审计、补偿和回滚路径明确。 |
| 安全边界 | 登录态、空间、企业角色、后台权限、TOS 签名、安全证据校验和日志脱敏规则明确。 |
| 配置 | `.env.example`、本地 `.env.local`、CentOS 环境变量和服务启动配置一致。 |
| 测试 | 单元、repository、RPC contract、HTTP contract、集成、迁移、Agent 联调和浏览器主链路都有入口。 |
| 生产 | 迁移执行、服务注册、健康检查、日志检索、数据备份、回滚和故障止血步骤可执行。 |

## Agent RPC 对齐冻结表

字段名以 Agent 07 的 Thrift baseline 为准；业务侧内部表字段可使用更清晰的来源字段，但 RPC DTO 不得改名。

| Agent RPC | 业务领域文档 | 业务实现服务 | 必须支持字段 | 超时/重试 | 关键错误 |
| --- | --- | --- | --- | --- | --- |
| `AccountSpaceService.ResolveCurrentSpaceContext` | 03 | `accountspace.App.ResolveCurrentSpaceContext` | `auth_context`、`request_meta`、`space_id` 可选 | 2s；读可重试 1 次 | `UNAUTHENTICATED`、`PERMISSION_DENIED` |
| `ProjectService.CheckProjectAccess` | 05 | `project.App.CheckProjectAccess` | `project_id`、`access_purpose`、`auth_context` | 2s；读可重试 1 次 | `PROJECT_ARCHIVED`、`PERMISSION_DENIED` |
| `SkillCatalogService.ListRoutableSkills` | 08 | `skillcatalog.App.ListRoutableSkills` | `skill_scope_filter`、`page_size=10`、`cursor` | 3s；读可重试 1 次 | `STATE_CONFLICT` |
| `SkillCatalogService.GetPublishedSkillSpec` | 08 | `skillcatalog.App.GetPublishedSkillSpec` | `skill_id`、`version` 可选 | 3s；读可重试 1 次 | `RESOURCE_NOT_FOUND` |
| `ToolCapabilityService.CheckToolExecutionPolicy` | 07 | `toolpolicy.App.CheckToolExecutionPolicy` | `tool_name`、`tool_type`、`project_id`、`risk_context` | 2s；读可重试 1 次 | `PERMISSION_DENIED`、`STATE_CONFLICT` |
| `ModelConfigService.ListAvailableGenerationModels` | 06 | `modelconfig.App.ListAvailableGenerationModels` | `resource_type`、`page_size=10`、`cursor` | 3s；读可重试 1 次 | `RESOURCE_UNAVAILABLE` |
| `ModelConfigService.ResolveDefaultModel` | 06 | `modelconfig.App.ResolveDefaultModel` | `resource_type` | 2s；读可重试 1 次 | `STATE_CONFLICT` |
| `CreditService.EstimateGenerationCredits` | 09 | `credit.App.EstimateGenerationCredits` | `project_id`、`resource_type`、`model_id`、`pricing_snapshot_id`、`quantity`、`duration_seconds`、`tool_usage_items[]` 非必填 | 3s；读可重试 1 次 | `CREDIT_INSUFFICIENT` |
| `CreditService.EstimateToolCredits` | 09 | `credit.App.EstimateToolCredits` | `project_id`、`tool_usage_items[]`、`auth_context`、`request_meta` | 3s；读可重试 1 次 | `CREDIT_INSUFFICIENT`、`TOOL_PRICING_POLICY_MISSING` |
| `CreditService.FreezeCredits` | 09 | `credit.App.FreezeCredits` | `estimate_id`、`points`、`run_id`、`request_meta.idempotency_key` | 5s；带幂等键可重试 | `IDEMPOTENCY_CONFLICT` |
| `CreditService.ChargeToolUsageCredits` | 09 | `credit.App.ChargeToolUsageCredits` | `project_id`、`estimate_id`、`freeze_id`、`session_id`、`run_id`、`charge_items[].estimate_item_id`、`tool_call_id`、`actual_quantity`、`idempotency_key` | 5s；带幂等键可重试 | `CREDIT_ESTIMATE_EXCEEDED`、`STATE_CONFLICT`、`IDEMPOTENCY_CONFLICT` |
| `CreditService.ReleaseFrozenCredits` | 09 | `credit.App.ReleaseFrozenCredits` | `freeze_id`、`release_points`、`reason`、`run_id`、`idempotency_key` | 5s；带幂等键可重试 | `STATE_CONFLICT` |
| `AssetService.BatchCheckAssetAccess` | 10 | `asset.App.BatchCheckAssetAccess` | `project_id`、`asset_ids[]`、`purpose` | 3s；批量读可重试 | `CROSS_SPACE_DENIED` |
| `AssetCreditCommitService.CommitGeneratedAssetAndCharge` | 11 | `asset.App.CommitGeneratedAssetAndCharge` | `project_id`、`session_id`、`run_id`、`freeze_id`、`artifacts[]`、`final_elements[]`、`safety_evidence`、`idempotency_key` | 10s；带幂等键可重试 | `SAFETY_EVIDENCE_INVALID`、`ASSET_SAVE_FAILED` |
| `PlatformDictionaryService.ListAssetElementTypes` | 10 | `asset.App.ListAssetElementTypes` | `page_size=50`、`schema_version` 可选 | 3s；可缓存 | `STATE_CONFLICT` |

不提供给 Agent 的能力：

| Agent 侧提及能力 | 业务侧结论 |
| --- | --- |
| `ContentSafetyConfigService` | 不由业务服务提供。Agent Safety Evaluator 拥有策略执行，业务只校验 `SafetyEvidenceDTO`。如需要业务下发安全配置，必须先更新 `docs/contracts/rpc/**` 和本目录。 |
| `Audit.AppendAuditLog` 供 Agent 直接写 | 不开放 Agent 直接写审计 RPC。业务写 RPC 在业务事务内写 `business_audit_logs`，通过 `trace_id` 与 Agent 日志关联。 |

## 阻断门禁

以下任一项未完成时，不允许进入对应功能点开发或联调。

| 阻断项 | 阻断范围 | 处理方式 |
| --- | --- | --- |
| Thrift IDL 字段编号未冻结 | Agent RPC client、业务 Kitex server、contract test | 先维护 `api/thrift/business_agent_service.thrift`，字段只增不改。 |
| HTTP OpenAPI 缺失 | 用户端、公开端、管理端联调 | 先维护 `api/openapi/business-api.yaml`，与 03～13 DTO 一致。 |
| migration 字段缺失 | GORM model、Repository、集成测试、生产迁移 | 先补 `db/migrations/iterations/<date>/business/*.sql` 和 down 脚本。 |
| `.env.example` 缺配置 | 本地启动、CentOS 部署、TOS、日志、HTTP API | 先补配置模板和 01/15 文档。 |
| request hash 规则缺失 | 所有写操作幂等 | 先在 02 和对应领域文档补规范化 hash 字段集合。 |
| RPC fixture 缺错误路径 | Agent A5/A8 联调 | 先补正常、权限、业务错误、幂等冲突、超时 fixture。 |
| Tool 积分 fixture 缺失 | Agent Tool 扣费联调 | 先补 `no_charge`、`model_generation`、`tool_usage`、`business_value`、重复 `estimate_item_id`、实际数量超预估 fixture。 |
| HTTP fixture 缺页面状态 | 前端联调 | 先补 401、403、409、422/400、500 的响应样例。 |
| SQL down 不可执行且无人工回滚 | 生产迁移 | 阻断发布，补回滚说明和备份恢复步骤。 |
| 真实密钥写入仓库 | 所有开发和发布 | 立即移除并轮换密钥。 |

## 功能切片

| 切片 | 代码范围 | 文档来源 | 必须产物 | 验收命令 |
| --- | --- | --- | --- | --- |
| B1 工程骨架 | `cmd/business`、`internal/bootstrap`、`internal/infra/config`、`internal/infra/logger` | 01、02、15 | go.mod、配置加载、Kitex server、Gin API、健康检查、结构化日志 | `gofmt ./services/business/...`、`go test ./services/business/internal/infra/config/...` |
| B2 通用基础 | `internal/pkg/errors`、`infra/idempotency`、`pkg/auditlog` | 02 | BusinessError、ApiResponse、IdempotencyGuard、Audit decorator | `go test ./services/business/internal/pkg/... ./services/business/internal/infra/idempotency/...` |
| B3 身份空间企业 | `application/accountspace`、`domain/accountspace`、migration 0002 | 03 | 用户、session、空间、企业、成员、邀请、转让 | `go test ./tests/business/accountspace/...` |
| B4 后台账号与审计 | `application/admin`、`application/audit`、migration 0003 | 04 | 初始平台管理员 seed、首次密码轮换、admin session、用户启禁用、审计查询、脱敏 DTO | `go test ./tests/business/admin/...` |
| B5 项目容器 | `application/project`、migration 0004 | 05 | 项目创建、归档、恢复、项目资产/作品关系、`CheckProjectAccess` | `go test ./tests/business/project/...` |
| B6 模型配置 | `application/modelconfig`、migration 0005 | 06 | 供应商、加密凭证、模型、价格快照、默认模型、连通性测试 | `go test ./tests/business/modelconfig/...` |
| B7 Tool 策略 | `application/toolpolicy`、migration 0006 | 07 | Tool 定义、策略、白名单、影响预览、`CheckToolExecutionPolicy` | `go test ./tests/business/toolpolicy/...` |
| B8 Skill 生命周期 | `application/skillcatalog`、migration 0007 | 08、13 | Skill 编辑态版本、发布版本、Memory 默认策略、测试样例、审核、发布、回滚、通知 | `go test ./tests/business/skillcatalog/...` |
| B9 积分账户 | `application/credit`、migration 0008 | 09 | 账户、批次、预估、冻结、Tool 扣费、释放、流水、兑换码绑定、后台发放 | `go test ./tests/business/credit/...` |
| B10 资产与字典 | `application/asset`、migration 0009 | 10 | 上传意图、TOS 签名、上传确认、资产元素、访问授权、元素字典 RPC、元素类型同步审计 | `go test ./tests/business/asset/...` |
| B11 保存扣费闭环 | `application/asset` + `application/credit`、migration 0010 | 11 | `CommitGeneratedAssetAndCharge` 原子事务、部分完成、失败释放 | `go test ./tests/business/assetcommit/... ./tests/contract/...` |
| B12 作品公开通知 | `application/work`、`application/notification`、migration 0011/0012 | 12、13 | 作品、公开快照、可复制分享链接、点赞、下架、通知已读未读 | `go test ./tests/business/work/... ./tests/business/notification/...` |
| B13 生产验收 | `tests/e2e`、`tests/contract`、部署脚本说明 | 14、15 | 主链路、跨服务、浏览器、RPC、DB、日志验证报告 | `go test ./tests/contract/... ./tests/business/...`，浏览器/E2E 按测试工程师脚本 |

当工程骨架尚未创建对应目录时，验收报告必须写明“未执行：目录不存在”，不能把缺失测试报告为通过。

## 文件冻结清单

| 文件 | 维护方式 | Done 条件 |
| --- | --- | --- |
| `api/thrift/business_agent_service.thrift` | 先手写 IDL，再用 Kitex 生成 | 字段编号稳定，Agent 07 方法全覆盖，contract fixture 通过。 |
| `api/openapi/business-api.yaml` | 手写 OpenAPI 3.0 | `/api/**`、`/api/public/**`、`/api/admin/**` 与 03～13 DTO 一致。 |
| `db/migrations/iterations/2026-06-27-business-core/business/*.up.sql` | 按 14 切分 | 无外键，索引覆盖查询路径，初始化数据来源明确。 |
| `db/migrations/iterations/2026-06-27-business-core/business/*.down.sql` | 按 14 切分 | 可回滚；不可回滚处写人工恢复步骤。 |
| `.env.example` | 手写维护 | 包含 `BUSINESS_DATABASE_URL`、`BUSINESS_KITEX_PORT`、`BUSINESS_HTTP_ADDR`、TOS、TLS、etcd、Kitex 等配置项，不包含真实密钥。 |
| `tests/contract/fixtures/business-rpc/*.json` | 手写 fixture | 覆盖正常、权限、业务错误、幂等冲突、超时、Tool 四类 charge mode 和重复扣费冲突。 |
| `tests/contract/fixtures/business-api/*.json` | 手写 fixture | 覆盖用户端、公开端、后台端的错误响应和分页。 |
| `tests/business/seed/*.sql` | 手写测试数据 | 初始平台管理员、个人空间、企业空间、归档项目、余额不足、兑换码绑定、安全证据失效、跨空间资产等场景齐全。 |

## 配置和生产部署门禁

| 配置 | 本地默认 | 生产要求 |
| --- | --- | --- |
| `BUSINESS_DATABASE_URL` | `.env.example` 示例值 | CentOS 8 单机环境变量注入，禁止明文提交。 |
| `BUSINESS_SERVICE_NAME` | `dora.business` | 与 Agent `BUSINESS_SERVICE_NAME` 完全一致。 |
| `BUSINESS_KITEX_PORT` | `19001` | 防火墙仅开放内网或本机可访问。 |
| `BUSINESS_HTTP_ENABLED` | `true` | 如通过网关统一代理，可设为 true 但仅内网暴露。 |
| `BUSINESS_HTTP_ADDR` | `0.0.0.0:19080` | 生产绑定内网地址或反向代理后端地址。 |
| `ADMIN_BOOTSTRAP_ACCOUNT` | 空 | 空库初始化时必须由生产环境变量或安全配置注入。 |
| `ADMIN_BOOTSTRAP_PASSWORD_HASH` | 空 | 只允许注入 hash 或凭证引用，禁止提交明文密码。 |
| `ADMIN_BOOTSTRAP_CREDENTIAL_SECRET_REF` | 空 | 使用安全配置引用时填写，和 `ADMIN_BOOTSTRAP_PASSWORD_HASH` 二选一。 |
| `PUBLIC_WEB_BASE_URL` | `http://localhost:3000` | 生产必须配置为用户可访问的公开站点域名。 |
| `ETCD_ENDPOINTS` | `http://127.0.0.1:2379` | 生产 etcd 或本机 etcd 地址，启动前必须可连接。 |
| `TOS_ACCESS_KEY_ID` / `TOS_SECRET_ACCESS_KEY` | 空 | 只能由生产环境变量或安全配置注入。 |
| `SECRET_ENCRYPTION_KEY_REF` | 文档要求 | 生产必须配置，用于模型供应商密钥加密。 |
| `VOLC_TLS_*` | 空 | 生产接入日志时必须配置或明确本地文件日志替代。 |

## CentOS 8 单机发布流程

当前项目规范仍以本地开发为主；进入生产前，业务服务至少执行以下手动门禁。

1. 备份业务数据库：记录备份命令、备份文件路径、校验 hash。
2. 校验环境变量：确认数据库、etcd、TOS、日志、密钥加密配置齐全。
3. 执行 migration dry-run：在测试库执行 up/down，保存输出。
4. 执行生产 migration up：按 14 的顺序执行，失败立即停止。
5. 启动业务服务：确认 Kitex 注册到 etcd，Gin 健康检查通过。
6. 执行 smoke test：登录、当前空间、项目创建、模型列表、Tool 策略、积分摘要、上传意图、公开作品列表。
7. 执行 Agent 联调 smoke：Agent 调 `ResolveCurrentSpaceContext`、`CheckProjectAccess`、`ListRoutableSkills`、`EstimateGenerationCredits`、`EstimateToolCredits`、`ChargeToolUsageCredits`。
8. 检查日志：按 `trace_id` 检索业务日志、审计日志和 Agent 日志是否贯通。
9. 标记发布结果：记录版本、commit、migration 版本、配置摘要和测试结果。

## 回滚策略

| 失败点 | 处理方式 |
| --- | --- |
| 服务启动失败 | 回滚二进制或配置，数据库未迁移时不动数据库。 |
| migration up 失败 | 停止发布；若已部分执行，按 down 顺序回滚或使用备份恢复。 |
| migration 成功但服务失败 | 优先修配置；无法恢复则回滚二进制，必要时执行 down 或备份恢复。 |
| RPC contract 不兼容 | 阻断 Agent 发布，恢复旧业务服务或启用兼容字段。 |
| HTTP API 不兼容 | 阻断前端发布，恢复旧业务服务或保留旧响应字段。 |
| TOS 签名失败 | 禁用上传入口，保留已有资产读取，排查 TOS 配置。 |
| 积分扣费异常 | 立即关闭生成确认入口；冻结可释放，扣费异常通过 ledger 审计补偿。 |

## 生产验收矩阵

| 主链路 | 必测点 |
| --- | --- |
| 注册/登录/身份切换 | 初始平台管理员 seed、首次密码轮换、session、空间、企业身份、前端 401/403 行为。 |
| 企业 owner 管理 | 邀请、移除、转让、成员权限、通知。 |
| 项目创建和归档 | `CheckProjectAccess` 各 purpose，归档只读。 |
| 模型选择 | 默认模型、停用模型、`pricing_snapshot_id`、价格缺失。 |
| Tool 策略 | disabled、高风险确认、白名单不匹配、超时策略、charge mode 返回。 |
| Skill 路由 | 只返回 Published，Draft/Pending/Deprecated 不返回，未传 Memory 策略时 runtime spec 返回 `enabled=true`。 |
| 积分闭环 | 预估、冻结、重复确认、余额不足、Tool 独立扣费、重复明细防重、兑换码用户/企业/渠道绑定、释放。 |
| 生成资产保存扣费 | `CommitGeneratedAssetAndCharge` 原子事务、`estimate_item_id` 防重复扣费、部分完成、保存失败释放。 |
| 上传素材 | TOS object key、上传签名、confirm 校验、过期/abort 清理、内置资产元素类型同步审计。 |
| 作品公开 | 分享、取消分享、匿名访问、复制分享链接、点赞、后台下架。 |
| 通知 | 审核通过/拒绝通知、未读数、已读、跳转权限。 |
| 审计和日志 | 后台写操作、Agent 写 RPC、trace_id 跨服务检索。 |

## 交付给 Agent 的 fixture

| 阶段 | 业务需要交付 |
| --- | --- |
| Agent A5 前 | Thrift IDL、Kitex mock server、RPC 正常响应、业务错误、权限错误、幂等冲突、超时 fixture。 |
| Agent A7 前 | Published Skill、Draft Skill、Disabled Tool、High Risk Tool、默认模型、停用模型、资产元素字典 fixture。 |
| Agent A8 前 | 余额足够、余额不足、冻结失败、Tool 独立扣费成功、Tool 实际数量超预估、提交成功、部分完成、保存失败、安全证据失效 fixture。 |
| E2E 前 | 个人空间、企业空间、归档项目、跨空间资产、公开作品、后台管理员测试账号、用户/企业/渠道绑定兑换码。 |

## Done Gate

- [ ] 00～14 中所有业务能力、DTO、数据库表、事务、权限、审计和测试入口均已补齐。
- [ ] 本文 Agent RPC 对齐冻结表中的方法在业务领域文档中都有实现说明。
- [ ] `pricing_snapshot_id`、`tool_name/tool_type`、`session_id/run_id` 等跨服务字段名与 Agent 07 一致。
- [ ] Agent 07 与 `api/thrift/business_agent_service.thrift` 已包含 `EstimateToolCredits`、`ChargeToolUsageCredits`、`GetReviewCandidateSkillSpec`、`SaveSkillTestResult`，业务 contract fixture 与 Agent RPC client fixture 字段完全一致。
- [ ] Tool 积分四类 charge mode、line items、`estimate_item_id` 防重复扣费和生成类 Tool 不重复扣费均有 contract 与集成测试。
- [ ] 初始平台管理员 seed、首次密码轮换、重复 seed 幂等和审计均有 repository 与集成测试。
- [ ] Skill Memory 默认开启、显式关闭、Published runtime spec 透传均有 contract 与集成测试。
- [ ] 兑换码绑定目标 `none/user/enterprise/channel` 的成功和失败路径均有 HTTP contract 与集成测试。
- [ ] 公开作品 `share_url` 在列表、详情、分享响应中稳定返回，并通过隐私字段泄露测试。
- [ ] 内置资产元素类型发布或变更有幂等同步、变更记录、后台审计和重复同步测试。
- [ ] 所有写 RPC 和写 HTTP API 都有幂等键、request hash 规则和冲突错误。
- [ ] 所有业务表都有 up/down migration 计划，且不包含数据库级外键。
- [ ] `.env.example` 覆盖业务 Kitex、业务 HTTP、PostgreSQL、etcd、TOS、日志和密钥引用配置，且不包含真实密钥。
- [ ] contract fixture 覆盖正常、业务错误、权限错误、幂等冲突、超时和版本兼容。
- [ ] 生产发布前有数据库备份、migration 输出、服务健康检查、smoke test 和回滚步骤记录。
