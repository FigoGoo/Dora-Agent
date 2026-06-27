# Dora-Agent 协作体系

## 角色关系

主控 Codex = 单一协作者。负责拆解任务、按领域边界执行或检查、维护跨服务契约并做最终验收。

`.codex/agents/*.toml` = 可选专项角色配置。保留产品、Eino、业务后端、前端和测试角色的职责边界，但本项目默认不采用 subagent 调度模式。除非用户显式要求切换到某个 agent，否则主控 Codex 在当前会话内直接完成任务，并把这些 agent 作为边界检查清单使用。

Skill = 可复用方法和规范。遇到产品、Eino、RPC、AG-UI、TurnLoop、数据建模、前端、测试、编码或文档任务时，先读取对应 Skill。

docs/standards = 规范事实源。所有实现和评审以这里的规范为准。

docs/templates = 文档模板。所有 PRD、ADR、契约、数据模型、测试和缺陷文档从这里派生。

RPC 契约 = 智能体微服务和业务系统微服务之间的边界。

AG-UI 事件协议 = 智能体微服务和前端之间的边界。

后端技术栈事实源 = `docs/standards/后端技术栈与操作规范.md`。

产品设计交接事实源 = `docs/standards/产品设计交接与开发约束规范.md`。

开发流程事实源 = `docs/standards/开发流程规范.md`。

GitHub 协作事实源 = `docs/standards/GitHub仓库协作规范.md`。

本地开发配置事实源 = `docs/standards/本地开发与配置规范.md` 和 `.env.example`。

安全规范事实源 = `docs/standards/安全规范.md`。

CloudWeGo 操作事实源 = `docs/standards/CloudWeGo开发操作规范.md`。

迭代 SQL 脚本事实源 = `docs/standards/迭代SQL脚本规范.md`。

## 系统架构

```text
前端应用
  ↓ HTTP / SSE / WebSocket / AG-UI
智能体微服务：Go + Eino
  ↓ RPC
业务系统微服务：Go + 数据库
```

系统由前端应用、智能体微服务、业务系统微服务组成。前端消费 HTTP、SSE、WebSocket 和 AG-UI 事件；智能体微服务负责 Agent Runtime 和 Eino 编排；业务系统微服务负责业务规则、事务、权限、领域模型和业务数据库。

## 智能体微服务边界

智能体微服务是独立的 Agent Runtime 微服务，负责 Go 服务端开发、Eino ADK、Agent、Graph、Workflow、Tool、Skill、Middleware、Retriever、Memory、Callback、Interrupt / Resume、TurnLoop、AG-UI 适配、Agent API、事件流、RPC client、Agent 会话状态、运行记录、任务状态、Agent 领域数据库、CRUD、测试和可观测性。

智能体微服务可以持久化 Agent Runtime 数据，例如 `agent_sessions`、`agent_runs`、`agent_messages`、`agent_events`、`agent_tool_calls`、`agent_tasks`、`agent_interrupts`、`agent_artifacts`、`agent_memories`、`agent_runtime_configs`。

智能体微服务不能直接访问或写入业务系统数据库，不能持久化订单、支付、审批结果、用户资产、业务交易、业务权限、业务主数据等业务事实。需要改变业务事实时，必须通过 RPC 调用业务系统微服务。

## 业务系统微服务边界

业务系统微服务负责业务规则、业务数据库、领域模型、事务一致性、权限校验、RPC server、业务数据读写、业务错误码、业务审计和服务端测试。

业务系统微服务不负责 Eino Agent 编排、AG-UI 事件协议、TurnLoop 执行逻辑、Agent 会话状态、Agent 运行记录、Agent 工具调用历史和 Agent 记忆系统。

业务服务拥有业务规则最终解释权，不允许为了方便 Agent 调用而破坏领域模型或暴露业务数据库内部结构。

## 前端边界

前端应用负责页面、组件、状态管理、HTTP API 集成、SSE / WebSocket 集成、AG-UI 事件消费、Agent 流式输出渲染、Tool 调用状态展示、人工确认交互、任务状态展示和前端测试。

前端不负责 Go 后端逻辑、RPC 契约设计、业务数据库、Eino Agent 编排和业务规则实现。前端字段、状态和事件必须以 API 契约与 AG-UI 事件协议为准，不允许自行发明后端字段。

## RPC 契约优先原则

RPC 契约是智能体微服务和业务系统微服务之间的协作边界。RPC 方法表达业务能力，不表达数据库表操作。

所有跨服务业务读写必须先定义或更新 RPC 契约，再实现调用方和服务方。写操作必须定义幂等键；需要人工确认的操作必须支持 preview / confirm 模式；错误码、超时、重试、权限上下文、审计字段和 contract test 必须文档化。

RPC 契约冲突由主控 Codex 汇总，Go Eino 智能体微服务架构工程师提出调用需求，业务微服务后端工程师确认业务语义和权限边界。

## AG-UI 事件协议优先原则

AG-UI 事件协议是智能体微服务和前端之间的协作边界。智能体微服务负责把 Eino 内部事件、Agent 事件、Graph 节点事件、Tool 调用事件、Interrupt / Resume 事件映射为文档化的 AG-UI 事件。

前端只消费协议中定义的事件类型和 payload。新增或变更事件时，必须先更新 `docs/standards/AG-UI事件规范.md` 或 `docs/templates/AG-UI事件协议模板.md` 派生文档，再修改智能体服务和前端。

AG-UI 冲突由主控 Codex 汇总，Go Eino 智能体微服务架构工程师负责事件生产语义，前端开发工程师负责消费和展示确认。

## Agent 领域数据库边界

Agent 领域数据库只保存 Agent Runtime 数据，包括会话、运行、消息、事件、工具调用、中断、任务、产物、记忆和运行配置。

Agent 领域表不得复制业务主数据，不得作为业务事实来源。业务事实必须由业务系统微服务通过 RPC 产生并维护。测试时必须区分 Agent 领域数据库验证和业务数据库验证。

## 单一 Codex 协作规则

主控 Codex 默认不调度 subagent，也不并发拆给多个 agent。根据任务类型参考以下专项角色边界：

| 任务类型 | 参考角色边界 |
| --- | --- |
| 产品目标、用户流程、验收标准、UI/UE 规范 | 产品体验设计师 |
| Eino 架构、Agent Runtime、Graph、Workflow、Tool、Skill、AG-UI 生产、TurnLoop、Agent DB | Go Eino 智能体微服务架构工程师 |
| 业务规则、业务数据库、RPC server、权限、事务、业务错误码 | 业务微服务后端工程师 |
| 页面、组件、状态管理、API 集成、AG-UI 消费、流式渲染 | 前端开发工程师 |
| 浏览器测试、AG-UI 验证、RPC 链路、数据库验证、缺陷报告 | 浏览器、RPC 与数据库测试工程师 |

协作要求：

1. 主控 Codex 直接执行任务，不主动调用 `.codex/agents` 作为 subagent。
2. `.codex/agents/*.toml` 只作为可选专项角色提示和 ownership 边界来源。
3. 涉及 RPC 或 AG-UI 的任务必须先明确契约 owner。
4. 从产品设计进入开发前，主控 Codex 必须按 `docs/standards/产品设计交接与开发约束规范.md` 输出或维护需求映射矩阵，再进入契约或代码开发。
5. 产品文档未达到 `product_status: Done` 时，只能做澄清、评审、技术预研或文档修订，不能进入正式代码开发。
6. 开发按功能点拆分，并按功能点提交到 GitHub。
7. 跨边界冲突先停下并回报主控 Codex，不允许自行猜测。
8. 测试缺陷按 ownership 边界定位并修复。
9. 主控 Codex 最终汇总修改文件、测试结果、风险和后续建议。

## 文件 Ownership

```text
/docs/product/**                         产品体验设计师
/docs/design/**                          产品体验设计师
/docs/project/**                         产品体验设计师
/docs/architecture/**                    Go Eino 智能体微服务架构工程师
/docs/contracts/**                       主控 Codex 汇总维护
/docs/standards/**                       主控 Codex 汇总维护
/docs/templates/**                       主控 Codex 汇总维护
/.github/**                              主控 Codex 汇总维护
/.env.example                            主控 Codex 汇总维护

/services/agent/**                       Go Eino 智能体微服务架构工程师
/services/business/**                    业务微服务后端工程师
/db/migrations/iterations/**             Go Eino 智能体微服务架构工程师和业务微服务后端工程师按数据库边界维护

/api/proto/**                            Go Eino 智能体微服务架构工程师提出，业务微服务后端工程师确认
/api/thrift/**                           Go Eino 智能体微服务架构工程师提出，业务微服务后端工程师确认
/api/openapi/**                          Go Eino 智能体微服务架构工程师提出，前端开发工程师确认

/web/**                                  前端开发工程师
/frontend/**                             前端开发工程师

/tests/e2e/**                            浏览器、RPC 与数据库测试工程师
/tests/contract/**                       浏览器、RPC 与数据库测试工程师
/tests/agent/**                          浏览器、RPC 与数据库测试工程师
```

## 编码规范入口

编码任务先阅读：

1. `docs/standards/开发流程规范.md`
2. `docs/standards/编码规范.md`
3. `docs/standards/GitHub仓库协作规范.md`
4. `docs/standards/安全规范.md`
5. 对应技术域规范，例如 `Go服务端编码规范.md`、`Eino智能体微服务编码规范.md`、`前端编码规范.md`
6. 对应边界规范，例如 `RPC契约规范.md`、`AG-UI事件规范.md`、`Agent领域数据建模规范.md`、`TurnLoop执行规范.md`
7. 后端任务必须阅读 `docs/standards/后端技术栈与操作规范.md` 和 `docs/standards/CloudWeGo开发操作规范.md`
8. 本地开发任务必须阅读 `docs/standards/本地开发与配置规范.md` 和 `.env.example`
9. 涉及数据库的任务必须阅读 `docs/standards/迭代SQL脚本规范.md`
10. 涉及智能体配置的任务必须阅读 `docs/standards/智能体配置化规范.md`
11. 从产品设计进入开发的任务必须阅读 `docs/standards/产品设计交接与开发约束规范.md`
12. `.agents/skills/编码规范执行/SKILL.md`

`karpathy-guidelines` Skill 只在代码修改、评审或重构任务中使用；产品体验设计和测试报告任务不使用该 Skill。

## 后端开发补充规范

Go Eino 智能体微服务架构工程师和业务微服务后端工程师执行开发任务时必须额外遵守：

1. 数据库 schema 创建一律不添加数据库级外键约束，也不通过关联键表达表关联；跨表一致性通过业务流程、幂等键、唯一约束、必要索引、application/domain 校验和测试保证。
2. 尽量避免在 `for` 循环中逐条查询数据库或 RPC；优先使用批量查询、`IN` 查询、JOIN、预加载、批量 RPC 或当前批次缓存。
3. 列表查询必须使用分页查询，默认 10 条每页；分页参数、排序语义和上限需要写入对应 RPC、API 或数据模型文档。
4. 入参、出参和业务展示对象尽量使用 DTO 包装；DTO 按领域和业务场景划分，避免复用大对象、ORM 对象或跨场景通用对象。

## 文档规范入口

文档任务先阅读：

1. `docs/standards/文档规范.md`
2. `docs/templates/` 下对应模板
3. `.agents/skills/文档规范执行/SKILL.md`

## 测试要求

测试必须与修改范围匹配。涉及 Go 代码时运行 gofmt 和 go test；涉及前端时运行项目约定的 lint、typecheck、unit test 或 browser test；涉及 RPC 时补充 contract test；涉及 AG-UI 时验证事件类型、顺序、payload、断线重连和未知事件兼容；涉及数据库时分别验证 Agent 领域数据库和业务数据库。

无法运行测试时必须说明原因、影响和替代验证。

## 环境与上线阶段

当前阶段以本地开发为主：本地环境为 macOS + Docker，配置模板为 `.env.example`。线上目标环境为 CentOS 8 单机。发布流程、CI/CD、告警、SLO、可观测性运行手册、测试环境矩阵在上线阶段再补充；当前只保留日志、trace 字段和本地验证要求。

## 输出格式要求

专项角色输出可参考：

```text
任务结论
修改或产出文件
使用的 Skill
关键决策
边界检查
测试或验证结果
风险与待确认事项
需要交付给其他领域的内容
```

主控 Codex 最终输出必须包含：

```text
创建或修改文件清单
各专项角色或 agent 用途
各 Skill 用途
规范和模板位置
智能体微服务边界
业务系统微服务边界
RPC 契约边界
AG-UI 事件协议边界
Agent 领域数据库边界
后续使用方式
无法确认或需要补充的信息
```
