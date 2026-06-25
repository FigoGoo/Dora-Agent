# Dora-Agent 契约文档索引

状态：active
owner：主控 Codex 汇总维护
更新时间：2026-06-25
适用范围：Dora-Agent 第一版 RPC、API、AG-UI、Agent 数据模型和 SQL 契约入口

## 背景

PRD 套件已确认 `product_status：Done`，工程 subagent 已完成 M1 需求映射。正式开发进入 M2 契约先行阶段。本目录用于沉淀智能体微服务、业务微服务、前端和测试之间的稳定边界。

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

| 契约 | 路径 | owner | 状态 | 说明 |
| --- | --- | --- | --- | --- |
| 业务微服务 RPC 契约草案 | `docs/contracts/rpc/业务微服务RPC契约草案.md` | 主控汇总；业务后端确认；Go Eino 提需求 | draft | 智能体微服务调用业务能力 |
| 统一 Agent 工作台 AG-UI 事件协议草案 | `docs/contracts/ag-ui/统一Agent工作台AGUI事件协议草案.md` | 主控汇总；Go Eino 生产；前端消费 | draft | SSE、事件类型、payload、断线重连 |
| Agent 工作台 API 契约草案 | `docs/contracts/api/Agent工作台API契约草案.md` | 主控汇总；Go Eino 与前端确认 | draft | session、run、stream、confirm、snapshot |
| C 端与后台业务 API 契约草案 | `docs/contracts/api/C端与后台业务API契约草案.md` | 主控汇总；业务后端与前端确认 | draft | Auth、Public、Project、Asset、Works、Enterprise、Admin |
| Agent 领域数据模型草案 | `docs/contracts/data/Agent领域数据模型草案.md` | Go Eino 智能体微服务架构工程师 | draft | Agent Runtime 表和状态机 |
| 第一版迭代 SQL 清单 | `docs/contracts/sql/第一版迭代SQL清单.md` | 主控汇总；业务后端与 Go Eino 分库维护 | draft | 业务库和 Agent 库 SQL 计划 |

## 契约产出顺序

1. RPC 能力边界：Account/Space、Project、Skill、Tool、Model、Credit、Asset、Work。
2. AG-UI 事件协议：统一 `type` / `event_type` 口径、事件命名、payload、sequence、补偿。
3. Agent API：工作台会话、运行、SSE、确认、取消、快照。
4. 业务 API：公开访问、项目、资产、作品、企业、后台。
5. Agent 数据模型：session、run、message、event、tool_call、task、interrupt、artifact。
6. SQL 计划：业务数据库和 Agent 领域数据库分开落脚。

## M2 裁决

- AG-UI 正式字段名使用 `type`；`event_type` 只允许第一版兼容读取，协议文档和测试以 `type` 为准。
- 事件命名统一为 `agent.run.*`、`agent.message.*`、`tool.call.*` 等分组命名；基础事件只保留映射兼容表。
- AG-UI 产品口径由产品体验设计师确认：前端展示用户能理解的状态，不展示 Eino 内部节点、系统 Prompt、推理链路或供应商原始响应。
- 内容安全证据统一为 `safety_evidence` 摘要对象；业务写入只接受 `result=passed` 的证据摘要。
- TOS 使用公共桶；后端签发上传凭证，前端直接上传到 TOS，公开媒体展示使用公开快照中的 TOS 公共访问 URL。
- TOS object key、目录和公共 URL 规则以 [TOS 对象存储规范](../standards/TOS对象存储规范.md) 为准。
- TOS 已接入 CDN，公共访问域名固定为 `https://tos.doraigc.com`；第一版不支持 CDN 缓存失效。
- 项目 `archived` 后只读，不允许新建 run、继续生成、上传到该项目、保存新资产或确认扣费。

## 后续待细化

- `confirmation.required` 与标准 `interrupt.required` 的兼容映射。
- 补偿查询 API 的路径、分页上限和高频 progress 节流。
- TOS 公共桶下架后的已缓存 URL 不承诺即时失效；重新公开必须换 object key。
- 模型 API Key 加密、轮换和供应商配置运维细节。

## Done 条件

- [ ] RPC 契约草案完成并通过 Eino/业务后端双方确认。
- [ ] AG-UI 事件协议草案完成并通过 Eino/前端双方确认。
- [ ] API 契约草案完成并通过前端和服务 owner 确认。
- [ ] Agent 数据模型草案完成并通过测试边界检查。
- [ ] SQL 清单完成并区分业务库与 Agent 库。
