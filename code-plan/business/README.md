# 业务服务系统设计开发文档目录

状态：production-design-ready  
owner：业务微服务后端工程师  
更新时间：2026-06-27  
适用范围：`services/business/**`、`api/thrift/business/**`、业务数据库、Kitex RPC server、业务 API 适配层、业务服务测试  
相关代码路径：`services/business/**`、`api/thrift/**`、`db/migrations/iterations/**/business/**`、`tests/business/**`、`tests/contract/**`  
相关设计事实源：`code-plan/README.md`、`docs/product/**`、`docs/product/prd/**`、`docs/architecture/10-业务微服务领域与RPC能力设计.md`、`docs/architecture/11-业务数据模型与事务边界设计.md`、`code-plan/business/**`、`code-plan/agent/07-RPC客户端业务能力调用与DTO映射设计.md`、`code-plan/agent/09-积分确认冻结生成资产保存扣费释放闭环设计.md`、`code-plan/agent/14-生产级闭眼开发门禁与任务切片验收设计.md`、`code-plan/tests/**`、`docs/standards/**`
后续实现落点：`api/thrift/business_agent_service.thrift`、`api/openapi/business-api.yaml`、`db/migrations/iterations/2026-06-27-business-core/business/**`、`tests/contract/fixtures/business-rpc/**`、`tests/contract/fixtures/business-api/**`

## 目标

本目录用于把 Dora-Agent 已确认的产品设计转成业务服务可开发的正式生产级系统设计文档。文档按开发顺序和功能点拆分，覆盖字段、领域模型、RPC、DTO、函数出入参、事务、权限、幂等、审计、日志、SQL 和测试，目标是开发者照着文档即可进入 Kitex、GORM、migration 和测试实现。现有临时代码、临时 Thrift、OpenAPI、migration、seed 或 fixture 如与本目录不一致，以本目录设计为准，在对应功能切片内同步修正。

业务服务只负责业务事实、业务规则、事务一致性、权限、审计和业务数据库。业务服务不负责 Eino Agent 编排、AG-UI 事件生产、Agent 会话、run、消息、Tool 调用历史、黑板、记忆和 Agent Runtime 数据。

本次开发不涉及前端开发和部署上线文档；业务侧仍需要提供用户端、公开端和管理端 HTTP API 的服务端 DTO 契约，作为后续接入依据，但不实现前端页面、组件或浏览器 UI 自动化。

## 固定章节要求

每份功能点文档必须包含：

- 产品和契约事实源。
- 本功能点职责内做什么、不做什么。
- 需求映射矩阵。
- 业务能力接口清单，按用户端、管理端、公开端、Agent RPC 和内部 application 能力分组。
- 用户端 / 管理端 / 公开端 HTTP API 设计，包含 Method、Path、鉴权、请求 DTO、响应 DTO、幂等、错误码和页面状态映射。
- 领域模型和状态机。
- 数据库表字段、索引、唯一约束和无外键约束说明。
- RPC service、method、request DTO、response DTO、错误码、超时、重试和幂等。
- Application、Domain、Repository 函数出入参设计。
- 权限、事务、审计和日志输出。
- 业务流程图，必须用 Mermaid 表达用户端、管理端、公开端、Agent 或内部 application 的完整闭环。
- 代码逻辑图，必须用 Mermaid 表达 Handler、Application、Domain、Repository、RPC client/server、事务和审计的调用顺序。
- 【Agent开发】需要提供或消费的能力与参数。
- 测试设计和验收标准。

接口设计不得单独脱离业务领域另建“对接清单”。用户端 `frontend/**`、管理端 `admin_frontend/**`、公开页和 Agent 需要的所有能力，都必须落回对应业务领域文档；同一业务事实只允许有一个领域 owner 和一套状态机。

## 数据库表设计要求

每个业务领域必须提供可落 SQL 的详细表结构，不能只写字段摘要。最低要求：

- 每张表必须列出字段名、PostgreSQL 类型、是否必填、默认值、索引/唯一约束和字段说明。
- 每张表必须包含业务主键，例如 `*_id varchar(64)`，以及 `created_at timestamptz not null default now()`、`updated_at timestamptz not null default now()`。
- 状态字段必须写明枚举值、默认值和允许状态流转。
- 所有关联字段只作为普通字段和索引，不创建数据库级外键或引用约束。
- 高风险写操作相关表必须能追踪 `idempotency_key`、`trace_id`、操作者或通过 `business_audit_logs` 关联。
- 列表查询必须能从表结构看出分页排序索引，默认以 `created_at desc` 或领域定义的排序字段分页。
- 敏感字段必须说明保存 hash、加密密文或脱敏摘要，不保存明文。

## 开发顺序

| 顺序 | 文档 | 目标 |
| --- | --- | --- |
| 00 | [事实源需求映射与开发顺序](./00-事实源需求映射与开发顺序.md) | 汇总产品事实源、功能点顺序、跨域依赖和 Agent 交付参数。 |
| 01 | [业务微服务总体架构与包结构设计](./01-业务微服务总体架构与包结构设计.md) | 定义 Kitex server、分层架构、Go package、依赖方向和本地启动。 |
| 02 | [通用上下文DTO错误码幂等审计与日志设计](./02-通用上下文DTO错误码幂等审计与日志设计.md) | 定义所有业务域共用 DTO、错误码、幂等、审计和结构化日志。 |
| 03 | [账户身份空间企业成员与权限设计](./03-账户身份空间企业成员与权限设计.md) | 实现用户、个人空间、企业空间、成员、邀请、拥有者转让和空间上下文。 |
| 04 | [平台后台管理员用户管理与审计设计](./04-平台后台管理员用户管理与审计设计.md) | 实现平台管理员、用户启禁用、后台审计和敏感信息脱敏。 |
| 05 | [项目归属项目资产项目作品与权限设计](./05-项目归属项目资产项目作品与权限设计.md) | 实现项目容器、归档、项目权限、项目资产和项目作品关系。 |
| 06 | [模型供应商模型目录价格与默认模型设计](./06-模型供应商模型目录价格与默认模型设计.md) | 实现模型供应商、加密凭证、模型、价格、默认模型、可选模型查询和非敏感模型运行快照。 |
| 07 | [Tool能力策略白名单风险与执行校验设计](./07-Tool能力策略白名单风险与执行校验设计.md) | 实现平台 Tool 定义、策略、白名单、风险等级、计价策略和 Agent 执行前校验。 |
| 08 | [Skill目录版本审核发布回滚与通知设计](./08-Skill目录版本审核发布回滚与通知设计.md) | 实现 Skill 编辑态版本、发布版本、输出元素结构、确认策略、测试样例、审核、发布、回滚和通知。 |
| 09 | [积分账户批次兑换码冻结扣减释放设计](./09-积分账户批次兑换码冻结扣减释放设计.md) | 实现积分账户、批次、兑换码、生成/Tool 预估、冻结、独立 Tool 扣费、释放和流水。 |
| 10 | [资产上传TOS对象资产元素预览下载设计](./10-资产上传TOS对象资产元素预览下载设计.md) | 实现上传意图、生成产物对象槽、TOS object key、资产登记、资产元素、预览和下载授权。 |
| 11 | [生成资产保存与积分扣费事务闭环设计](./11-生成资产保存与积分扣费事务闭环设计.md) | 实现 Agent 生成结果 TOS 对象校验、最终资产元素入库、项目绑定和积分扣费原子事务。 |
| 12 | [作品中心公开快照精选作品点赞与下架设计](./12-作品中心公开快照精选作品点赞与下架设计.md) | 实现作品、公开快照、精选、匿名访问、点赞、取消分享和下架。 |
| 13 | [站内信通知已读未读与跳转权限设计](./13-站内信通知已读未读与跳转权限设计.md) | 实现审核通知、系统通知、未读数、已读和跳转权限。 |
| 14 | [SQL迁移本地配置测试矩阵与验收设计](./14-SQL迁移本地配置测试矩阵与验收设计.md) | 汇总 migration、本地配置、测试矩阵、contract test 和验收闭环。 |
| 15 | [生产级闭眼开发门禁与Agent对齐验收设计](./15-生产级闭眼开发门禁与Agent对齐验收设计.md) | 对齐 Agent 生产门禁，冻结跨服务契约、功能切片、配置、发布、回滚和生产验收。 |

## Agent 依赖标注规则

所有跨 Agent 边界的内容必须用【Agent开发】标注：

```text
【Agent开发】能力：能力名称
【Agent开发】调用方：Agent Runtime / Tool Runtime / Safety Evaluator / TurnLoop
【Agent开发】需要提供参数：字段、类型、必填、说明
【Agent开发】业务服务返回参数：字段、类型、说明
【Agent开发】错误处理：错误码、是否重试、AG-UI 展示规则
```

## 边界总则

- 业务服务不保存 Agent session、run、message、event、tool_call、task、interrupt、blackboard、memory。
- 业务服务保存业务事实：用户、空间、企业、项目、模型、Tool 策略、Skill 生命周期、积分、资产、作品、通知、审计。
- Agent 需要读取或改变业务事实时，必须通过业务服务 RPC。
- 业务服务对权限、状态、事务和错误码拥有最终解释权。
- 所有写操作必须有 `idempotency_key`。
- 列表查询默认 `page_size=10`，最大值 `50`。
- 数据库 schema 不添加数据库级外键约束，不使用外键或引用约束关键字。
- DTO 按领域和场景拆分，不暴露 ORM 对象或数据库表结构。
