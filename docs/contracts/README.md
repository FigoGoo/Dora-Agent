# Dora-Agent 契约文档索引

状态：active
owner：文档与契约责任域
更新时间：2026-06-28
适用范围：Dora-Agent 第一版 RPC、API、AG-UI、Agent 数据模型和 SQL 契约入口

## 背景

PRD 套件已确认 `product_status：Done`，第一阶段服务端开发已经完成。本目录用于沉淀智能体微服务、业务微服务、前端和测试之间的契约边界、草案状态和冻结条件。

## 文档契约

- Codex 处理契约任务时先读 `docs/current/README.md`，再读本文档和对应契约正文。
- 契约变更必须先更新本文档索引和对应 RPC、API、AG-UI、数据模型或 SQL 文档，再修改实现。
- 第一阶段服务端历史设计只从 `docs/releases/phase-01-server/README.md` 追溯，不作为新增契约的默认入口。
- `draft` 契约可以作为设计方向和边界输入，但不能直接作为字段级冻结契约；进入实现前必须先关闭对应成熟度缺口。
- 契约状态、责任域、更新时间和适用范围必须与 `docs/standards/文档规范.md` 保持一致。

## 契约原则

- RPC 契约表达业务能力，不暴露数据库表 CRUD。
- API 契约表达前端页面和交互需要，不发明后端未定义字段。
- AG-UI 事件协议表达智能体微服务到前端的实时事件边界。
- Agent 领域数据模型只保存 Agent Runtime 数据，不保存业务事实。
- SQL 计划区分业务数据库和 Agent 领域数据库，不添加数据库级外键约束。
- 用户端前端工程目录固定为 `frontend/`。
- 管理端前端工程目录固定为 `admin_frontend/`。
- 项目状态为 `archived` 后不允许继续创作；只允许查看历史、资产和作品，需恢复项目后才能创建新 run。

## 契约清单

| 契约 | 路径 | 责任域 | 状态 | 说明 |
| --- | --- | --- | --- | --- |
| 契约成熟度复核 | `docs/contracts/契约成熟度复核.md` | 文档与契约责任域 | active | 契约冻结状态、缺口和升 active 条件 |
| 字段级契约索引 | `docs/contracts/字段级契约索引.md` | 文档与契约责任域 | active | Thrift、OpenAPI、AG-UI schema 和 fixture 字段级事实源索引 |
| 业务微服务 RPC 契约草案 | `docs/contracts/rpc/业务微服务RPC契约草案.md` | 文档与契约责任域汇总；业务服务确认；Agent 服务提出需求 | draft | 智能体微服务调用业务能力 |
| 统一 Agent 工作台 AG-UI 事件协议草案 | `docs/contracts/ag-ui/统一Agent工作台AGUI事件协议草案.md` | 文档与契约责任域汇总；Agent 服务生产；前端消费 | draft | SSE、事件类型、payload、断线重连 |
| Agent 工作台 API 契约草案 | `docs/contracts/api/Agent工作台API契约草案.md` | 文档与契约责任域汇总；Agent 服务与前端确认 | draft | session、run、stream、confirm、snapshot |
| C 端与后台业务 API 契约草案 | `docs/contracts/api/C端与后台业务API契约草案.md` | 文档与契约责任域汇总；业务服务与前端确认 | draft | Auth、Public、Project、Asset、Works、Enterprise、Admin |
| Agent 领域数据模型草案 | `docs/contracts/data/Agent领域数据模型草案.md` | Agent 服务责任域 | draft | Agent Runtime 表和状态机 |
| 第一版迭代 SQL 清单 | `docs/contracts/sql/第一版迭代SQL清单.md` | 文档与契约责任域汇总；业务服务与 Agent 服务按数据库边界维护 | draft | 业务库和 Agent 库 SQL 计划 |

## 契约产出顺序

1. RPC 能力边界：Account/Space、Project、Skill、Tool、Model、Credit、Asset、Work。
2. AG-UI 事件协议：统一 `type` / `event_type` 口径、事件命名、payload、sequence、补偿。
3. Agent API：工作台会话、运行、SSE、确认、取消、快照。
4. 业务 API：公开访问、项目、资产、作品、企业、后台。
5. Agent 数据模型：session、run、message、event、tool_call、task、interrupt、artifact。
6. SQL 计划：业务数据库和 Agent 领域数据库分开落脚。

## 契约裁决

- AG-UI 正式字段名使用 `type`；`event_type` 只允许第一版兼容读取，协议文档和测试以 `type` 为准。
- 事件命名统一为 `agent.run.*`、`agent.message.*`、`tool.call.*` 等分组命名；基础事件只保留映射兼容表。
- AG-UI 产品口径由产品与需求责任域确认：前端展示用户能理解的状态，不展示 Eino 内部节点、系统 Prompt、推理链路或供应商原始响应。
- 内容安全证据统一为 `safety_evidence` 摘要对象；业务写入只接受 `result=passed` 的证据摘要。
- TOS 使用公共桶；后端签发上传凭证，前端直接上传到 TOS，公开媒体展示使用公开快照中的 TOS 公共访问 URL。
- TOS object key、目录和公共 URL 规则以 [TOS 对象存储规范](../standards/TOS对象存储规范.md) 为准。
- TOS 已接入 CDN，公共访问域名固定为 `https://tos.doraigc.com`；第一版不支持 CDN 缓存失效。
- 项目 `archived` 后只读，不允许新建 run、继续生成、上传到该项目、保存新资产或确认扣费。

## 后续待细化

具体缺口以 [`契约成熟度复核.md`](./契约成熟度复核.md) 为准。字段级事实源以 [`字段级契约索引.md`](./字段级契约索引.md) 为准。当前 P0 设计缺口已关闭；后续待细化集中在 replay / contract fixture 覆盖、服务级执行报告、SQL up/down 证据和 Agent 数据模型运行证据。

## Done 条件

- [x] `契约成熟度复核.md` 中 P0 缺口关闭或明确延期且不影响本次实现。
- [x] RPC 契约草案完成字段级 request/response、错误码、幂等、timeout/retry 和 fixture 映射。
- [x] AG-UI 事件协议草案完成 canonical 事件、payload、补偿、snapshot fallback 和兼容映射。
- [x] API 契约草案完成 route 级 request/response、鉴权、幂等、分页和 OpenAPI 映射。
- [x] Agent 数据模型草案完成表字段、索引、状态机、保留周期和测试边界检查。
- [ ] SQL 清单完成业务库与 Agent 库落脚、up/down、无外键检查和执行证据。
