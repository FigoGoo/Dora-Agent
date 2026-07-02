# Dora-Agent Codex 入口

更新时间：2026-07-02

## 作用

`AGENTS.md` 只做 Codex 入口导航，不承载详细规范。

- 不使用预设智能体角色或多角色调度。
- Codex 直接按当前文档事实源、责任域边界和 Skill 执行任务。
- 项目文档、契约、规范、设计、测试口径和阶段事实源统一到 `docs/` 下查找。
- 代码实现以对应代码目录、接口定义和测试为准；文档侧入口不在 `AGENTS.md` 展开。

## 项目说明

Dora-Agent 是面向 AIGC 智能体创作工作台的全栈项目，覆盖用户端、管理端、Agent 工作台、账户与空间、项目与资产、作品中心、积分扣费、模型供应商、Tool、Skill、通知和内容安全等能力。

当前开发以本地环境为主，后续上线目标为 CentOS 8 单机。当前有效范围、阶段状态和文档入口统一以 `docs/` 为准。

## 技术架构

```text
前端应用：用户端 / 管理端 / Agent 工作台
  ↓ HTTP / SSE / WebSocket / AG-UI
智能体微服务：Go + Eino + Agent Runtime
  ↓ RPC
业务系统微服务：Go + CloudWeGo / Kitex + PostgreSQL + TOS
```

- 前端代码和设计入口以 `frontend/`、`admin_frontend/` 和 `docs/` 为准。
- 智能体微服务入口以 `services/agent/**` 和 `docs/` 为准。
- 业务系统微服务入口以 `services/business/**`、`api/thrift/**`、`api/openapi/**` 和 `docs/` 为准。
- 核心边界：智能体微服务只保存 Agent Runtime 数据；业务事实由业务系统微服务和业务数据库维护，智能体微服务通过 RPC 改变业务事实。
- 字段级契约事实源以 `docs/`、接口定义、migration 和 fixture 为准。

## Skill 使用规则

- 遇到对应任务时，先读对应 Skill，再执行。
- 一个任务可组合多个 Skill，但不要把 Skill 当成智能体角色。
- Skill 只提供方法和检查清单，事实源仍以 `docs/`、代码和测试为准。

## Skill 作用

| 类别 | Skill | 何时使用 |
| --- | --- | --- |
| 产品与推进 | `产品系统设计`、`项目管理推进`、`全栈智能体项目协调` | 需求、计划、功能拆分、跨责任域协调和验收。 |
| Agent 架构 | `Eino全能力掌握与选型`、`Eino智能体架构设计`、`EinoGraph工作流设计`、`TurnLoop多轮执行设计` | Eino 选型、Agent 架构、Graph/Workflow、多轮执行和中断恢复设计。 |
| Agent 实现 | `Eino开发实现`、`Agent领域数据建模` | Agent 服务代码、事件流、RPC client、Agent Runtime 数据模型和测试。 |
| 契约 | `RPC契约设计`、`AG-UI事件协议设计` | RPC 边界、AG-UI 事件协议、字段和消费规则。 |
| 业务与前端 | `业务微服务开发`、`前端开发实现`、`UI体验与设计系统`、`用户端UI-UX设计顾问`、`管理端UI-UX设计顾问` | 业务服务、前端实现、主题、用户端/管理端 UI/UX 和交互体验。 |
| 测试与质量 | `浏览器RPC数据库测试`、`编码规范执行`、`karpathy-guidelines` | 端到端验证、编码边界检查、复杂度控制和可验证成功标准。 |
| 文档 | `文档规范执行` | PRD、ADR、契约、数据模型、测试报告和缺陷报告写作。 |

## git 提交规范
- 项目提交需要使用中文描述
