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
- 现有 AIGC 行为：按范围读取 `../../../docs/aigc-chatmodelagent-demo-design.md`、`../../../docs/aigc-tool-storyboard-design.md`、`../../../docs/aigc-worker-design.md`，核对当前实现、目标形态和旧目录路径，不得仅凭文档声明认定能力已实现。
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
- 应用生成 UUIDv7；当前没有 Tenant，不得预留无业务含义的 `tenant_id`。
- 积分使用 `bigint`；金额按字段明确使用分为单位的 `bigint` 或 `numeric(p,2)`，禁止浮点数。
- etcd 只用于服务注册发现。
- PostgreSQL 是 Job/Outbox 权威状态；Redis 只负责唤醒和缓存。
- 日志、DTO、Event 和 Job Payload 不得包含 Secret 或不必要的敏感数据。

## 4. Worker 额外门禁

- 明确 At-Least-Once 语义和稳定幂等键。
- Claim 使用短事务、Lease 和 Fencing Token。
- Heartbeat、终态和重试更新必须校验 Lease Owner、Lease Version 和旧状态。
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
- Runtime Skill 只从 PostgreSQL 动态加载系统/用户 Skill，并以内联模式注入唯一主 Agent；已发布版本不可变，同一 Turn 冻结版本，禁止 `fork`/`fork_with_context`，Skill 不得新增 Tool、扩权、提预算或绕过 HITL。
- Agent 使用独立数据库和 `agent` Schema，不直连 Business 数据库；普通 Business RPC 无必要不得包装为 Tool。
- Agent-owned Operation/Batch/Job/Outbox 由 `agent/migrations` 管理；Worker 只能按公开版本化消费契约和最小数据库权限访问，不得 import `agent/internal/**`。
- PostgreSQL 是 Session、Run、Checkpoint、Receipt、Approval、Operation、Batch、Job、Outbox 和 Event 权威来源；Redis 只缓存和唤醒。
- 生产调用统一经过 Runner 和持久化 Session Lane；模型/Tool 输出先冻结再投影，恢复不得重复外部副作用。
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
