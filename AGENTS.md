# Dora-Agent Codex 入口

更新时间：2026-06-28

## 作用

`AGENTS.md` 只做 Codex 入口导航，不承载详细规范。

- 不使用预设智能体角色或多角色调度。
- Codex 直接按当前文档事实源、责任域边界和 Skill 执行任务。
- 详细文档规则以 `docs/current/README.md` 和 `docs/standards/文档规范.md` 为准。
- 详细编码、测试、契约、安全和发布规则以 `docs/standards/README.md` 为准。

## 项目说明

Dora-Agent 是面向 AIGC 智能体创作工作台的全栈项目，覆盖用户端、管理端、Agent 工作台、账户与空间、项目与资产、作品中心、积分扣费、模型供应商、Tool、Skill、通知和内容安全等能力。

当前开发以本地环境为主，后续上线目标为 CentOS 8 单机。当前有效范围、阶段状态和文档入口以 `docs/current/README.md` 为准。

## 技术架构

```text
前端应用：用户端 / 管理端 / Agent 工作台
  ↓ HTTP / SSE / WebSocket / AG-UI
智能体微服务：Go + Eino + Agent Runtime
  ↓ RPC
业务系统微服务：Go + CloudWeGo / Kitex + PostgreSQL + TOS
```

- 前端代码和设计入口以 `frontend/`、`admin_frontend/`、`docs/technical/frontend/**`、`docs/design/**` 为准。
- 智能体微服务入口以 `services/agent/**`、`docs/technical/backend/features/**`、`docs/contracts/api/**`、`docs/contracts/ag-ui/**`、`docs/contracts/data/**` 为准。
- 业务系统微服务入口以 `services/business/**`、`api/thrift/**`、`api/openapi/**`、`docs/contracts/rpc/**`、`docs/contracts/sql/**` 为准。
- 核心边界：智能体微服务只保存 Agent Runtime 数据；业务事实由业务系统微服务和业务数据库维护，智能体微服务通过 RPC 改变业务事实。
- 字段级契约事实源以 `docs/contracts/字段级契约索引.md` 指向的 Thrift、OpenAPI、AG-UI JSON Schema、migration 和 fixture 为准。

## 默认读取顺序

1. `AGENTS.md`
2. `docs/current/README.md`
3. 任务对应目录 README
4. 对应 `.agents/skills/**/SKILL.md`

常用目录入口：

- 技术：`docs/technical/README.md`
- 产品：`docs/product/README.md`
- 契约：`docs/contracts/README.md`
- 规范：`docs/standards/README.md`
- 测试：`docs/test/README.md`

历史内容：

- `code-plan/**`、`docs/releases/**`、`docs/archive/**` 默认不作为新迭代事实源。
- 如需复用历史结论，先迁移到当前 active 文档。

## Skill 使用规则

- 遇到对应任务时，先读对应 Skill，再执行。
- 一个任务可组合多个 Skill，但不要把 Skill 当成智能体角色。
- Skill 只提供方法和检查清单，事实源仍以当前文档、契约、代码和测试为准。

## Skill 作用

| 类别 | Skill | 何时使用 |
| --- | --- | --- |
| 产品与推进 | `产品系统设计`、`项目管理推进`、`全栈智能体项目协调` | 需求、计划、功能拆分、跨责任域协调和验收。 |
| Agent 架构 | `Eino全能力掌握与选型`、`Eino智能体架构设计`、`EinoGraph工作流设计`、`TurnLoop多轮执行设计` | Eino 选型、Agent 架构、Graph/Workflow、多轮执行和中断恢复设计。 |
| Agent 实现 | `Eino开发实现`、`Agent领域数据建模` | Agent 服务代码、事件流、RPC client、Agent Runtime 数据模型和测试。 |
| 契约 | `RPC契约设计`、`AG-UI事件协议设计` | RPC 边界、AG-UI 事件协议、字段和消费规则。 |
| 业务与前端 | `业务微服务开发`、`前端开发实现`、`UI体验与设计系统` | 业务服务、前端实现、主题和交互体验。 |
| 测试与质量 | `浏览器RPC数据库测试`、`编码规范执行`、`karpathy-guidelines` | 端到端验证、编码边界检查、复杂度控制和可验证成功标准。 |
| 文档 | `文档规范执行` | PRD、ADR、契约、数据模型、测试报告和缺陷报告写作。 |

## 最小协作契约

- 新功能开发前，必须先有产品、技术、契约或测试口径入口。
- 变更 RPC、API、AG-UI、数据模型、SQL、配置、权限、错误码或测试夹具时，同步对应文档和契约事实源。
- 开发完成后，在相关设计、契约、迭代文档或测试报告中记录验证命令、证据路径、遗留风险和后续动作。
- 服务边界、字段级事实源和测试要求不要写在 `AGENTS.md`，到 `docs/current/README.md`、`docs/contracts/README.md`、`docs/standards/README.md` 查询。

## git 提交规范
- 项目提交需要使用中文描述