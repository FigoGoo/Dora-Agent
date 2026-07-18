# Dora 文档入口

> 状态：Current / 唯一文档入口

本页是仓库文档的唯一导航入口。文档按职责分层，避免在多份设计中重复维护功能清单、阶段判断或下一步计划。

## 先读这三份

| 要回答的问题 | 唯一入口 | 职责 |
| --- | --- | --- |
| 产品要做什么、哪些暂不做 | [产品范围](requirements/product-scope.md) | 维护当前 MVP、P1 生产化、P2 业务扩展的稳定边界，不维护完成进度 |
| 当前做到哪、下一步跑什么 | [交付阶段与当前状态](requirements/delivery-status.md) | **唯一阶段状态源**，维护当前结论、未关闭门禁和执行顺序 |
| 系统如何分层、模块如何协作 | [系统架构](design/system-architecture.md) | 维护三 Module、前端、基础设施和跨模块边界，不维护排期 |

D0～D3/P1/P2 阶段状态、门禁通过情况、执行顺序和下一步只在 `requirements/delivery-status.md` 维护。功能设计可以描述当前代码行为和稳定的未实现边界，但不得据此维护第二份阶段结论。

## 功能设计入口

每个功能域只保留一个聚合设计入口；六个 Graph Tool 使用固定独立设计，冻结审批材料仅供契约审计，不再并列维护功能总览。

| 功能域 | 唯一设计入口 | 覆盖范围 |
| --- | --- | --- |
| 身份与项目 | [Identity and Projects](design/functions/identity-and-projects.md) | 登录、Owner 边界、Project、QuickCreate |
| Skill 与治理 | [Skills and Governance](design/functions/skills-and-governance.md) | Skill 基础、发布/市场边界、治理与管理职责 |
| 素材与分析 | [Materials and Analysis](design/functions/materials-and-analysis.md) | 素材摄取、Evidence、MaterialAnalysis |
| 创作流程 | [Creation Workflow](design/functions/creation-workflow.md) | CreationSpec、Storyboard、Prompt、Approval 与创作状态 |
| 媒体与资产 | [Media and Assets](design/functions/media-and-assets.md) | Operation/Batch/Job、Worker、Asset、内容读取与媒体产物 |
| 工作台与事件 | [Workspace and Events](design/functions/workspace-and-events.md) | Workspace、Snapshot/SSE、A2UI、刷新恢复与动作入口 |
| 运行时与质量 | [Runtime and Quality](design/functions/runtime-and-quality.md) | Agent Runtime、Session Lane、恢复、观测、测试和发布门禁 |
| 六个 Agent-facing Graph Tool | [固定六 Tool 设计索引](design/agent/graphtool/README.md) | 六个稳定 Tool Key 及其独立中文设计 |

## 工程与开发约束

| 主题 | 入口 |
| --- | --- |
| 仓库布局、Skill 路由、协作要求 | [AGENTS.md](../AGENTS.md) |
| 服务端代码、API、数据、测试与提交规范 | [Dora 服务端开发规范](../.agents/skills/dora-server-development/SKILL.md) |

## 冻结审批材料

`design/agent/` 与 `design/cross-module/` 下保留的非入口文档和 JSON 是已批准 Preview、不可变上下文或跨对象 Receipt 的审计材料。它们由清单、哈希和契约测试约束，不作为当前阶段或功能总览；其中的历史链接允许保留，历史正文由 Git 追溯。

## 文档收口规则

1. 新功能先更新产品范围，再更新唯一功能域设计；禁止新建第二份总览。
2. 阶段变化只更新 `requirements/delivery-status.md`；设计文档不得维护阶段状态、门禁通过情况、执行顺序或下一步清单。
3. 跨 Module 契约必须明确 Owner、版本、幂等、错误语义和兼容策略，并由功能域入口索引。
4. 历史方案只用于追溯，不得作为当前实现或排期依据；未列入本页的文档不是项目入口。
5. 代码、Migration、IDL、配置和验收命令变化时，同一改动内同步更新对应入口及可执行校验。
