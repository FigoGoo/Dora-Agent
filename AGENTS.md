# Dora 项目协作指引

## 仓库 Module 布局

- `business/`：Business Service 独立 Go Module，生产 Runtime 为 `business/cmd/business-service`，Migration 位于 `business/migrations`。
- `worker/`：Business Worker 独立 Go Module，生产 Runtime 为 `worker/cmd/business-worker`，Worker 自有 Migration 位于 `worker/migrations`。
- `agent/`：Agent Service 独立 Go Module，生产 Runtime 为 `agent/cmd/agent-service`，Migration 位于 `agent/migrations`。
- 仓库根目录仅负责多 Module 协作、文档和共享工程配置，不作为任一生产 Runtime 的 Go Module；根 `go.work` 只能用于本地联调。

## 开发计划与状态口径

- [Dora 项目开发计划（Canonical）](docs/requirements/project-development-plan.md) 是当前状态、执行顺序、暂停项和上下文交接的唯一调度入口；收到“继续推进”时先读取该文件。
- [全功能冒烟开发推进计划](docs/requirements/full-function-smoke-development-plan.md) 只维护详细里程碑、SMK-P0 和长期 backlog；其中旧“下一批”或 Batch 编号不得覆盖 Canonical 计划。
- [全功能冒烟架构与推进审计](docs/design/cross-module/full-function-smoke-architecture-audit-2026-07-15.md) 只提供目标架构、阶段依赖和风险依据，不表示推荐项已 Approved 或已实现。
- 当前事实只由代码、Migration 和可重复 Evidence 证明；实现授权仍由对应 Design / ADR / Owner 审批决定。Canonical 计划只能安排顺序，不能解除门禁。
- 每次真实交付、Owner 审批、门禁解锁或主纵切变化后同步更新 Canonical 计划；不得预写完成状态。

## 服务端开发规范 Skill

下列任务必须使用项目级 `$dora-server-development` Skill：

- 新增、修改或评审 Business Service、Business Worker、Agent Service 的 Go 代码；
- 修改 HTTP API、Thrift/Kitex RPC、DTO、Event、Job Payload 或跨 Module 契约；
- 修改 GORM Repository、SQL Migration、PostgreSQL、Redis、etcd、服务注册发现或本地 Docker 配置；
- 修改服务端测试、CI、构建、发布、Commit 或 PR 规范。

执行规则：

1. 在制定实现计划或编辑代码前，先读取 [.agents/skills/dora-server-development/SKILL.md](.agents/skills/dora-server-development/SKILL.md)。
2. `business/**` 改动完整读取 [业务服务端开发规范](.agents/skills/dora-server-development/reference/business-server-development-standards.md)。
3. `worker/**` 改动完整读取 [Business Worker 开发规范](.agents/skills/dora-server-development/reference/business-worker-development-standards.md)。
4. `agent/**` 改动完整读取 [Agent Service 开发规范](.agents/skills/dora-server-development/reference/agent-development-standards.md)。
5. Agent、Runner、Middleware 或 HITL 改动同时使用项目级 `$eino-agent` Skill；Graph Tool、Graph、Branch、State、Checkpoint 或 Interrupt 改动同时使用 `$eino-compose` Skill；ChatModel、Prompt、Tool Component 改动同时使用 `$eino-component` Skill。不确定 Eino 能力归属时先使用 `$eino-guide`。Eino Skill 和示例与 Dora Agent 规范冲突时，以 Dora Agent 规范为准。
6. 新增或修改 Agent-facing Graph Tool 时，在实现前完整读取其 `docs/design/agent/graphtool/<tool_key>-design.md`；缺少独立中文设计文档、流程图、稳定 Node 清单/类型、Graph State、分离的业务状态机或审核结论时不得开始实现或合并。
7. 修改现有 AIGC 行为时按范围读取 `docs/aigc-chatmodelagent-demo-design.md`、`docs/aigc-tool-storyboard-design.md`、`docs/aigc-worker-design.md`，并核对“当前实现/目标形态”和旧目录路径，禁止把目标能力或历史路径直接当作当前事实。
8. 跨任意 Module 的 DTO、RPC、Event、Job、数据库或持久化消费契约改动，必须完整读取所有受影响 Module 的规范；不得引用其他 Module 的 `internal` 包。
9. 修改根 `go.work`、Docker Compose、CI、构建脚本、共享 IDL 或跨 Module Event Schema 时，先识别受影响的 Module，再读取所有对应规范。
10. 修改规范文档、Skill 路由、Module 布局或规范状态时，必须同步检查本文件与 Skill 中的链接和说明。
