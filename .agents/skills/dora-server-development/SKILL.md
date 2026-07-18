---
name: dora-server-development
description: Apply Dora project backend development standards when creating, modifying, reviewing, or generating Go code, APIs, DTOs, GORM repositories, SQL migrations, service registration, tests, commits, runtime configuration, or cross-module contracts. Use for the independent business, worker, and agent Go Modules, PostgreSQL, Redis, etcd, Gin, Kitex/Thrift, Eino Agent, Graph Tool, Skill, Prompt, Runner, and middleware changes.
---

# Dora 服务端开发规范

## 1. 先确定改动范围

在制定计划或修改文件前，完整读取对应规范：

- `business/**`：读取 [业务服务端开发规范](reference/business-server-development-standards.md)。
- `worker/**`：读取 [Business Worker 开发规范](reference/business-worker-development-standards.md)。
- `agent/**`：读取 [Agent Service 开发规范](reference/agent-development-standards.md)，并按改动类型使用对应 Eino Skill：Agent/Runner/Middleware/HITL 使用 `$eino-agent`，Graph/Graph Tool/State/Checkpoint/Interrupt 使用 `$eino-compose`，ChatModel/Prompt/Tool Component 使用 `$eino-component`；不确定时先使用 `$eino-guide`。Eino Skill 或示例与 Dora Agent 规范冲突时，以 Dora Agent 规范为准。
- Agent-facing Graph Tool：实现前读取 `../../../docs/design/agent/graphtool/<tool_key>-design.md`；文档缺失，或缺少流程图、稳定 Node 清单/类型、Graph State、分离的业务状态机和审核结论时停止实现并先补齐设计评审。
- 现有 AIGC 行为：先从 `../../../docs/README.md` 选择对应功能设计，并核对 `../../../docs/requirements/delivery-status.md` 的当前实现边界；冻结审批证据只用于审计，Git 历史和已删除的里程碑文档不得作为当前能力真源。
- 跨任意 Module 的 DTO、RPC、Event、Job、数据库或持久化消费契约：完整读取所有受影响 Module 的规范。
- 根 `go.work`、Docker Compose、CI、构建脚本、共享 IDL 或 Event Schema：先识别受影响 Module，再读取所有对应规范。

只读取与当前改动有关的规范；规范中的 MUST 是合并门禁，SHOULD 偏离时必须说明理由。

固定 Module Root：

- Business Service：`Dora-Agent/business`，Runtime 为 `business/cmd/business-service`。
- Business Worker：`Dora-Agent/worker`，Runtime 为 `worker/cmd/business-worker`。
- Agent Service：`Dora-Agent/agent`，Runtime 为 `agent/cmd/agent-service`。
- 仓库根目录是多 Module 协作容器，不得当作任一生产 Runtime 的 Go Module；根 `go.work` 只用于本地联调。

## 2. 执行前检查

1. 确认目标 Go Module、Runtime 和 Migration Owner；不得默认使用仓库根 `go.mod`。
2. 检查最近的 `AGENTS.md`、目标 Module 的 `go.mod`、IDL/OpenAPI 和已有 Migration。
3. 保留用户已有改动，不修改无关文件。
4. 将跨 Module 数据设计为 DTO、Thrift、HTTP、Event 或明确版本化的持久化消费契约，禁止引用其他 Module 的 `internal` 包。
5. 本地默认使用 `/Users/figo/sdk/go1.26.3/bin/go`；Docker 中间件使用 PostgreSQL 16、Redis 和 etcd。

Migration Owner 是定义表生命周期、写入事务和数据不变量的 Module，不是碰巧读取、Claim、发布或消费该表的 Runtime。Migration 只能位于 Owner Module 的 `migrations/`；Outbox Owner 是将 Outbox 与领域 Aggregate 原子写入的生产者 Module。跨 Module Schema 变更由 Owner 提交 Migration，所有消费者执行契约测试。

### 2.1 保持最小充分设计

- 以用户当前目标、当前功能设计、已发布契约和现有代码边界为准，选择能完整通过门禁的最小改动；设计缺失或互相冲突时先补齐或澄清，不得从未来规划、冻结证据或 Git 历史推导当前需求。
- 优先复用现有包、契约和简单直接的实现；只有当前需求具有明确必要性时，才新增抽象层、共享 Module、通用框架、可插拔机制或配置开关。
- 不为“以后可能需要”预留未使用的表、字段、状态、接口、事件、配置、兼容分支、软删除或扩展点；只有现有数据、已发布契约或滚动发布确实需要时才引入兼容逻辑。
- 不顺带重构无关代码；必要的邻接调整只做到支撑当前变更，并在交付说明中解释原因。
- 最小实现不能省略安全、权限、幂等、事务、Migration、可观测性和测试门禁。

## 3. 不可省略的代码门禁

- Runtime 数据访问使用 GORM，Repository 之外不得调用 GORM。
- Schema 使用版本化 SQL Migration；所有生产 Runtime 启动时禁止 `AutoMigrate`。
- 严禁在数据库 Schema/Migration 中创建物理外键约束和数据库级联；表字段、Entity、GORM Model、DTO 中允许并应按业务关系保留 `user_id`、`asset_id`、`job_id` 等逻辑外键。
- GORM 可使用 `foreignKey`、`references` 描述查询映射，但不得使用 `constraint` 创建数据库约束；逻辑关联完整性和级联处理由业务代码显式保证。
- 每张表和每个字段必须通过 SQL Migration 添加中文数据库 COMMENT。
- 所有实体、持久化 Model、DTO、业务类/具名类型及其字段和方法必须有中文注释，说明职责、用途、前置条件、主要结果和错误语义。
- 类和方法的中文注释不能替代流程注释；业务判断、状态流转、幂等、事务、重试、补偿和兼容逻辑还必须在对应代码处用中文说明原因及一致性影响。
- 单次请求、UseCase、事务或 Attempt 内严禁在循环中执行同一或仅参数不同的同构 SQL。复杂读取优先使用一次 JOIN/CTE/子查询并映射为专用查询 DTO；JOIN 不适合时使用固定数量的批量查询或重新设计逻辑，禁止逐条查询和 N+1。
- 前端、RPC、Event、Job Payload 和跨领域参数必须使用 DTO；禁止暴露 Entity 或 GORM Model。
- `context.Context` 必须作为调用链首参并贯穿 HTTP、RPC、GORM、Redis 和对象存储，外部 I/O 显式设置超时；请求链禁止使用 `context.Background()` 脱离生命周期或 fire-and-forget，异步业务必须落持久化 Job/Outbox，goroutine 必须有 Owner、取消和回收路径。
- 事务只包含必要的数据库读写，禁止在事务内等待 HTTP、RPC、Redis、对象存储、模型或 Provider；强一致领域状态与本 Module Outbox 同事务提交，条件更新必须检查 `RowsAffected`。
- 同一外部副作用只能有一个重试 Owner；Unknown Outcome 先使用原幂等键查询权威状态，禁止换键或跨层叠加重试。
- 应用生成 UUIDv7；当前没有 Tenant，不得预留无业务含义的 `tenant_id`。
- 积分使用 `bigint`；金额按字段明确使用分为单位的 `bigint` 或 `numeric(p,2)`，禁止浮点数。
- etcd 只用于服务注册发现。
- PostgreSQL 是 Job/Outbox 权威状态；Redis 只负责唤醒和缓存。
- 日志、DTO、Event 和 Job Payload 不得包含 Secret 或不必要的敏感数据。

## 4. Worker 额外门禁

- 明确 At-Least-Once 语义和稳定幂等键。
- Claim 使用短事务、Lease 和 Fencing Token；在同一事务中设置 Lease Owner、递增 Lease Version 并创建 Attempt，提交后才执行外部调用。
- Heartbeat、终态和重试更新必须校验 Lease Owner、Lease Version 和旧状态；`RowsAffected == 0` 视为 Lease Lost，立即取消 Attempt Context 且禁止提交结果。
- 持久化 Job Payload 必须携带 `schema_version`；存量任务存在时不得原地改变字段语义，破坏性变化使用新版本，滚动发布期间新旧 Worker 必须兼容存量 Payload。
- Worker 跨 Module 访问只能通过公开 RPC/Event 或 Owner 发布的版本化 Job、Outbox、Inbox、Read Model 消费契约；禁止直连或 JOIN 普通业务操作表，批量 RPC/查询次数不得随 Job 数量增长。
- Retry 必须分类、有限、带退避和随机抖动；Unknown Outcome 先核对再决定。
- Worker Pool、Channel、Claim Batch 和下游并发必须有界。
- Redis 丢失唤醒时，PostgreSQL 轮询必须能够恢复。
- 优雅退出先停止 Claim，再 Drain、继续 Heartbeat，最后关闭资源。

## 5. Agent 额外门禁

- 项目只有一个主 ChatModelAgent；禁止子 Agent、DeepAgent、AgentAsTool 和多 Agent。
- Eino 锁定 v0.9.10，v1 使用经典 `*schema.Message` 和普通 DeepSeek Adapter。
- Agent 代码按 `middleware`、`tool`、`graphtool`、`chatmodelagent`、`prompt` 等功能包组织；一个 Graph Tool 一个功能包和一份独立中文设计文档。
- `graph.go` 只组装拓扑；ChatModel 使用 ChatModel Node，候选输出必须经独立确定性 Validator 后才能进入 Command。
- 无环 Graph 使用 AllPredecessor 并另定义 typed Fan-in；循环 Graph 使用 AnyPredecessor 和最大步骤。Tool 回执、Approval、Operation、Batch、Job 分别设计状态机。
- Runtime Skill 只从 Business 当前 published snapshot 加载，并以内联模式注入唯一主 Agent；产品仅有 draft/published，Agent 在 Session 创建时冻结快照，新发布只影响新会话。禁止 `fork`/`fork_with_context`，Skill 不得新增 Tool、扩权、提预算或绕过 HITL。
- Agent 使用独立数据库和 `agent` Schema，不直连 Business 数据库；普通 Business RPC 无必要不得包装为 Tool。
- Agent-owned Operation/Batch/Job/Outbox 由 `agent/migrations` 管理；Worker 只能按公开版本化消费契约和最小数据库权限访问，不得 import `agent/internal/**`。
- Skill 草稿/发布、Storyboard、Binding、Asset 和积分归 Business；Agent Graph 通过 RPC 访问，Worker 只负责生成/TOS 并调用 Business Finalize，terminal event 经 Agent Inbox 进入 Session Lane。
- A2UI 使用独立后端/前端包，所有组件组合具备安全 Markdown 的 Card 公共结构；Worker 不生成 A2UI。
- PostgreSQL 是 Session、Run、Checkpoint、Receipt、Approval、Operation、Batch、Job、Outbox 和 Event 权威来源；Redis 只缓存和唤醒。
- 所有 Session Input 先持久化 PostgreSQL 再唤醒 Processor；生产调用统一经过 Runner 和持久化 Session Lane，同一 Session 以 Lease/Fencing 串行且保持 Head-of-Line，技术重试复用原 InputID/TurnID。
- 模型可填写的 Tool Intent 与服务端可信 Turn/Command Context 必须分离；身份、权限、预算、资源版本和幂等基键不得进入模型可控 Schema，未知字段、版本或枚举失败关闭。
- 模型/Tool 输出先完整冻结再投影；Receipt 或冻结输出存在时只重放，恢复不得重新调用模型、Tool 或重复外部副作用。
- 高风险 Tool 在副作用前使用正式 Approval；通用 HTTP、宿主文件、Shell、任意 SQL 和动态 Tool Search 默认禁用。
- 每个 Agent Profile 必须配置迭代、模型、Tool、Token、时间、并发和费用硬预算，具体数值不得写死在代码、Prompt 或 Skill 中。
- 普通日志和 Trace 禁止保存完整 Prompt、会话、Tool Payload、Checkpoint 和 Reasoning。

## 6. 完成前验证

按对应规范的合并前清单逐项检查。对每个受影响 Module 分别设置 `MODULE=business`、`MODULE=worker` 或 `MODULE=agent`，从仓库根目录优先运行：

~~~bash
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C "$MODULE" mod verify
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C "$MODULE" vet ./...
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C "$MODULE" test -shuffle=on -count=1 ./...
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C "$MODULE" test -race ./...
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C "$MODULE" build ./cmd/...
~~~

跨 Module 改动不得只在 `go.work` 模式验证，也不得只验证仓库根 Module。

同时验证：

- gofmt、goimports、go mod tidy 和代码生成后无差异；
- Migration 可在 PostgreSQL 16 空库执行并从上一版本升级；
- Migration 和实际 Schema 不含物理外键约束，同时逻辑外键字段、索引和业务完整性校验符合规范；
- 所有业务表、字段具有中文数据库 COMMENT；
- DTO、类/方法中文注释、业务流程中文注释、幂等、日志脱敏和兼容性符合规范；
- 列表及批处理查询不存在循环 SQL 或 N+1，复杂查询结果已映射为专用 DTO。

无法运行的检查必须在交付说明中列出原因，不得表述为已通过。

Agent Graph Tool 还必须验证设计文档、实际 Node/Edge/Branch、Graph State 和状态机一致，Graph 在启动阶段 Compile，所有 Branch、Validator、HITL、幂等、预算和 Checkpoint/Receipt 恢复路径具有测试。

## 7. Commit 与 PR

- Commit 使用 `<type>(<scope>): <中文摘要>`。
- 一个 Commit 只处理一个明确目的。
- IDL/OpenAPI、DTO 和生成代码保持同一变更。
- Migration 与依赖代码在同一 PR 完整提供；已发布 Migration 禁止修改。
- 依赖升级使用独立 PR，并说明 `go.mod/go.sum` 变化。
- 禁止提交 Secret、`.env`、日志、数据库文件和构建产物。
- PR 说明业务影响、契约兼容性、数据库影响、回滚方式、幂等/重试策略及测试证据。
