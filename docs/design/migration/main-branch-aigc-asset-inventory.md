# `main` 分支 AIGC 迁移资产清单

> 状态：Draft / M0 迁移审计基线
>
> 审计快照：`main@6d0fc111fd49a874dad213a61389a6d83999ebc8`
>
> 快照时间：2026-07-12 16:52:22 +0800
>
> 审计日期：2026-07-14
>
> 约束：本清单不授权批量复制代码；每个迁移项仍须满足对应 Module 规范、详细设计和契约评审。

## 1. 审计结论

`main` 是一个根 Go Module 的单体 AIGC Demo：一个 `cmd/aigc-agent` 同时持有 Session、Skill、Creation Spec、Storyboard、Asset、Billing、Generation、Worker Loop、HTTP/SSE 和数据库 AutoMigrate。它包含 216 个 Go 文件、80 个测试文件，其中 `internal/aigc` 有 212 个文件、29 个一级包。

这些代码有较高的行为样例和测试价值，但目录边界、表 Owner、Runtime、Migration、计费顺序、Job 消费和五 Tool Registry 与当前目标不一致。结论如下：

- **不存在可整体恢复的目录。** `internal/aigc/**` 必须按 Business/Agent/Worker Owner 拆分；
- **优先复用算法、协议测试向量和失败用例，不复用旧聚合边界。**
- **所有 GORM AutoMigrate、Memory Store、Memory Broker 和单进程 Worker 编排不得进入生产实现。**
- **旧五 Tool Capability/Registry 只能作为反例和测试素材，六个 Graph Tool 按新独立设计重建。**
- **Provider 请求/响应映射和状态机测试可选择性移植到 Worker，但必须重做幂等、Fence、Receipt、Secret 和契约边界。**
- `frontend/` 在 `master..main` 无差异，当前前端资产已存在，不从 `main` 重复迁移。

共同目标设计见 [Graph Tool 设计索引](../agent/graphtool/README.md)、[AIGC 跨 Module 契约目录](../cross-module/aigc-contract-catalog.md)和[全功能冒烟工程设计](../testing/full-function-smoke-engineering-design.md)。

## 2. 分类定义

| 分类 | 含义 | 执行规则 |
|---|---|---|
| 选择性复用 | 纯算法、协议结构或测试向量与新边界基本一致 | 逐函数/逐测试移植，改包名和 DTO 后重新评审；禁止整包复制 |
| 重写 | 业务语义有参考价值，但 Owner、状态机、持久化或 Runtime 边界不符合目标 | 先写新契约/测试，再按目标设计实现；旧代码只作行为参考 |
| 废弃 | Demo、重复抽象、危险迁移方式或与冻结决策冲突 | 不进入目标分支；仅在需要解释历史差异时查看 |
| 历史保留 | 文档、样例或视觉资产 | 不作为当前事实或生产代码；必要时链接而非复制 |

“选择性复用”也不代表原文件可直接移动；实现者必须在迁移 PR 中列出原文件、目标文件、保留行为、改变行为和测试证据。

## 3. 顶层资产清单

| `main` 路径 | 分类 | 目标位置/处理 | 依据与风险 |
|---|---|---|---|
| `cmd/aigc-agent/main.go` | 废弃生产实现，保留启动检查清单 | 不复制；三个 Runtime 分别新建 `business/cmd/business-service`、`agent/cmd/agent-service`、`worker/cmd/business-worker` | 旧文件在一个进程 AutoMigrate 所有表、启动 Agent/Worker/HTTP，违反 Module 和 Migration Owner |
| `cmd/aigc-agent/main_test.go` | 重写 | 三 Runtime bootstrap/config/health/graceful shutdown 测试 | 旧断言可转成启动验收，不保留单体依赖图 |
| 根 `go.mod/go.sum` | 历史依赖清单 | 按实际依赖拆入三个 Module；根不再有生产 Module 语义 | 防止跨 Module 隐式依赖和 `go.work` 掩盖问题 |
| `docker-compose.local.yml` | 重写 | `deploy/local` 与 `deploy/local-smoke` | 旧 Compose 只有单 PostgreSQL/Redis，无三 DB 角色、etcd、Adapter、健康链路 |
| `docker/postgres/init/001-create-databases.sql` | 重写 | 本地基础设施初始化；生产 Migration 留在各 Module | 只能创建 DB/角色，不能替代版本化 Migration |
| `.env.example` | 选择性复用配置键清单 | 三 Runtime 各自 example + 根 local compose example | 必须拆 Secret、Owner 和 Runtime 前缀；禁止沿用 Demo 开关为生产行为 |
| `kitex-all.sh` | 重写 | 共享 IDL 的非交互生成/校验脚本 | 必须按版本化 IDL 和生成目录运行，不能依赖根 Module |
| `orchestration.go`、`tools_node.go` | 废弃 | 无 | 根级实验编排，不属于任一生产 Runtime；新 Graph 以独立设计为准 |
| `商品宣传短片_v2.Skill.md` | 历史保留/测试 Fixture 候选 | 脱敏后放 `business/testdata` 或 `smoke/fixtures` | 不能作为生产 Skill Snapshot 或默认权限配置 |
| `docs/superpowers/**` | 历史保留 | 不迁移为当前规范；必要结论已经过新需求/设计复核 | 多份历史方案存在旧五 Tool、旧路径和单体假设 |
| `docs/aigc-*-design.md` | 历史保留 | 当前 `master` 已保留并标明历史/目标差异 | 开发时仍需核对当前分支，不能把目标形态当当前实现 |
| `.agents/skills/eino-*` | 不从 `main` 迁移 | 使用当前项目已安装 Skill | 当前规范和 Skill 路由优先，避免旧副本漂移 |
| `frontend/**` | 无迁移动作 | 使用当前 `master/frontend` | `master..main` 当前无文件差异；后续只按真实 API 契约改造 |

## 4. `internal/aigc` 包级清单

### 4.1 A2UI、Agent 与会话

| `main` 路径 | 分类 | 目标 Module | 迁移决定 |
|---|---|---|---|
| `internal/aigc/a2ui/types.go`、`events.go`、`content.go` | 选择性复用协议结构/测试向量 | Agent | 对照 `docs/aigc-a2ui-design.md` 重建版本化 DTO；保留未知组件、Markdown 清洗和稳定 Event ID 用例 |
| `internal/aigc/a2ui/*_test.go` | 选择性复用 | Agent tests + frontend protocol tests | 转成跨端协议 golden vectors；补 Cursor、Card Revision、未知 Action、脱敏 |
| `internal/aigc/a2ui/broker.go` | 废弃 | 无 | Memory Broker 不是权威；目标使用 PostgreSQL EventLog + SSE Wake |
| `internal/aigc/a2ui/prompt.go`、`model_policy.go` | 重写/部分废弃 | Agent | A2UI 不由模型自由决定危险 Action；只保留安全策略测试思想 |
| `internal/aigc/agent/deepseek.go` | 重写 | Agent | Eino DeepSeek 组件装配可参考；改为 Runtime 配置、单 ChatModelAgent、Receipt/预算和 Secret 规范 |
| `internal/aigc/agent/model_receipt.go` | 重写 | Agent | 统一到 Agent ModelReceipt 聚合，不沿用单体 Store 类型 |
| `internal/aigc/agent/message_rebuilder.go`、`store_message_rebuilder.go` | 选择性复用算法/测试 | Agent | 保留 classic `*schema.Message` 重建用例；数据来源改为 Agent Repository/Resource Ref |
| `internal/aigc/agent/dynamic_skill.go` | 重写 | Agent | 只能使用 Session 冻结的 Business Published Skill Snapshot，禁止动态扩大 Tool 权限 |
| `internal/aigc/agent/next_capability.go`、`agentcontrol/next_capability.go` | 废弃旧决策器 | Agent | 两套相近抽象合并为六 Tool Registry/显式 requested_tool_key 规则；旧行为只作回归反例 |
| `internal/aigc/session/**` | 重写，复用 transcript 测试思想 | Agent | 新 Session/Input/Turn/Run 表与版本化 Migration；不共享 Business 用户表 |
| `internal/aigc/sessionruntime/**` | 重写，复用 Session Lane/Terminal Event 用例 | Agent | 用 PostgreSQL Lease、Input/Turn/Continuation 权威状态；Memory Store 和 AutoMigrate 废弃 |
| `internal/aigc/turncontext/**` | 选择性复用上下文裁剪测试 | Agent | Builder 改读可信 Resource Ref/Published Snapshot；不把权限、价格、完整对象塞给模型 |
| `internal/aigc/modelreceipt/**` | 重写 | Agent | 保留 unknown outcome/重放测试；使用 Agent Migration、UUIDv7、中文 COMMENT 和新模型契约 |
| `internal/aigc/middleware/tool_exception.go` | 选择性复用错误归一化思想 | Agent | 错误码对齐跨 Module 目录；不泄漏 Node/Provider/栈 |

### 4.2 Approval、领域对象与计费

| `main` 路径 | 分类 | 目标 Module | 迁移决定 |
|---|---|---|---|
| `internal/aigc/approval/models.go`、`store.go` | 重写，复用状态/绑定用例 | Agent | 新 Approval 绑定用户、动作、resource version/digest、Quote、金额、有效期和消费 CAS |
| `internal/aigc/approval/postgres_store.go`、`migrations.go` | 废弃实现 | Agent | 禁止 AutoMigrate/运行时建表；按 Agent Migration 与 Repository 规范重写 |
| `internal/aigc/approval/memory_store.go` | 废弃生产路径 | Agent tests only | 测试用 Fake 必须实现与 Repository 相同 CAS 语义，不作为 Runtime fallback |
| `internal/aigc/approvalruntime/**` | 重写 | Agent | 保留“决策→Continuation”用例；按新 ApprovalConsumptionReceipt 和新 Turn 实现，不恢复旧 Graph |
| `internal/aigc/artifact/**` | 重写 | Business | 拆为 CreationSpec/MaterialAnalysis/PromptArtifact/AssemblyPlan 等明确聚合，禁止 Agent 拥有通用 Artifact 真源 |
| `internal/aigc/spec/**` | 重写，复用候选版本测试 | Business | CreationSpec 候选/激活状态机归 Business；Agent 仅 RPC/Approval |
| `internal/aigc/storyboard/**` | 重写，选择性复用 aggregate/asset-binding 测试 | Business | 新 Revision/Element/Slot/Prompt/Binding 模型和事务；无物理 FK，稳定 ID/版本/digest |
| `internal/aigc/asset/**` | 拆分重写 | Business + Worker | Asset/Binding/授权归 Business；上传 Provider/Receipt 归 Worker；`local_uploader` 只可转 local Adapter |
| `internal/aigc/billing/**` | 重写，复用幂等账本测试 | Business | 追加式 Ledger、Charge/Preparation/Finalize/收益/冲正；废弃 Agent 内嵌计费 Store 和零价 Demo |
| `internal/aigc/skill/**` | 拆分重写 | Business + Agent | Draft/Published Snapshot 归 Business；Agent 只消费冻结 Snapshot；Parser 测试可选择性复用 |

### 4.3 Graph Tool、Capability 与 Prompt

| `main` 路径 | 分类 | 目标 Module | 迁移决定 |
|---|---|---|---|
| `internal/aigc/capability/context.go`、`intents.go`、`result.go` | 重写 | Agent | 用六份独立 Tool Schema、`TrustedCommandContextV1` 和 `GraphToolResultV1` |
| `internal/aigc/capability/registry.go`、`tools.go` | 废弃五 Tool Registry，保留 Registry 测试思想 | Agent | 重建六 Tool Definition/Version/发布状态/展示顺序/显式调用治理 |
| `internal/aigc/capability/executor.go` | 重写 | Agent | 每 Tool 启动时编译独立 `compose.Graph`，不共享隐藏流程或绕过预算/Approval |
| `internal/aigc/capabilityruntime/model_graph.go` | 废弃旧拓扑，保留 Eino 装配参考 | Agent | 新 Graph 的稳定 Node/State/Branch 以独立设计为准 |
| `internal/aigc/capabilityruntime/runtime.go` | 重写 | Agent | 移除对 Business 领域 Store/Generation Store 的直接依赖，改 RPC/Repository Owner |
| `internal/aigc/tools/pipeline_tools.go`、`storyboard_designer.go`、`write_prompt.go` | 废弃为用户 Graph Tool 实现 | Agent | 旧隐藏 Prompt/故事板流程与六 Tool 边界冲突；测试输入可转新 Graph 验收向量 |
| `internal/aigc/tools/registry.go`、`envelope.go`、`generative_result.go` | 拆分重写 | Agent/Worker | 高层 Graph Tool 与原子 Provider Tool 分离；Envelope 对齐版本化契约 |
| `internal/aigc/tools/image2.go`、`seedance.go`、`provider_http.go` | 选择性复用 Provider 映射/测试 | Worker | 迁移请求/响应字段和 golden tests；重做 Secret、Idempotency、Query Unknown、Fence、Receipt |
| `internal/aigc/tools/media_generator.go` | 重写 | Worker | 不能由 Agent Tool 直接调用；接入 Worker Job Handler/Provider Adapter |
| `internal/aigc/tools/text_editor.go`、`echo.go` | 历史测试工具/默认废弃 | Agent testkit | 若作为公开原子 Tool，需单独产品定义、权限、版本和计费评审 |

### 4.4 Generation、Worker 与持久化

| `main` 路径 | 分类 | 目标 Module | 迁移决定 |
|---|---|---|---|
| `internal/aigc/generation/models.go`、`state_machine.go` | 重写，选择性复用状态测试 | Agent | Operation/Batch/Job 归 Agent；状态按 `AGT-JOB-V1`、Fence、Terminal Outbox 重建 |
| `internal/aigc/generation/request_fingerprint.go` | 选择性复用算法/测试 | Agent/Worker 各自实现 | 跨 Module 不共享 Go 包；算法规则写入契约，各 Module 用 golden vectors 验证一致 |
| `internal/aigc/generation/barrier.go` | 选择性复用算法/测试 | Agent | Barrier 只基于 Agent DB Job 终态；补重复/乱序/取消/partial_failed |
| `internal/aigc/generation/command.go`、`dispatcher.go`、`outbox_dispatcher.go` | 重写 | Agent | Operation/Batch/Job/Dispatch Outbox 同事务；Preparation Receipt 前置 |
| `internal/aigc/generation/postgres_store.go`、`postgres_workflow_store.go` | 废弃实现、复用查询测试思想 | Agent | 禁止 AutoMigrate；发布受控 `AGT-JOB-V1` 视图/函数，不给 Worker 普通表写权限 |
| `internal/aigc/generation/redis_queue.go` | 重写 | Agent + Worker | Redis 只携带最小唤醒 Envelope，PostgreSQL 扫描必须可恢复 |
| `internal/aigc/generation/worker.go`、`lifecycle_worker.go` | 重写 | Worker | 从 Agent Runtime 移出，使用 Worker 私有 Attempt/Receipt、Claim/Lease/Fence、优雅退出 |
| `internal/aigc/generation/recovery_scheduler.go` | 拆分重写 | Agent + Worker | Agent 处理 charged-undispatched/Terminal；Worker 处理 Provider/Upload/Finalize unknown outcome |
| `internal/aigc/generation/finalization.go` | 重写 | Worker + Business RPC | Worker 调 `FinalizeGeneration`，Business 权威绑定；旧单库事务不能复用 |
| `internal/aigc/generation/provider_policy.go` | 选择性复用策略测试 | Worker | 配置化 Provider Capability/限流/重试；价格与授权不由 Worker 决定 |
| `internal/aigc/generation/handlers/image2.go`、`seedance.go` | 选择性复用 Provider Adapter/测试 | Worker | 同 Provider 工具迁移规则；补状态查询、unknown outcome、Receipt |
| `internal/aigc/generation/handlers/demo_visual.go`、`demo_audio.go`、`demo_assembly.go` | 废弃生产实现，转测试 Adapter 需求 | `test-adapters` | 不注册为生产 Handler；固定输出可成为 Local Deterministic Fixture |
| `internal/aigc/generation/legacy_provider_adapter.go` | 废弃 | 无 | 禁止为旧接口保留双写/隐式兼容层 |
| `internal/aigc/generationruntime/**` | 重写 | Agent + Worker | Outbox/Adapter 装配按 Runtime 拆分，禁止跨 Module 内存调用 |
| `internal/aigc/mediagraph/**` | 废弃旧实现/重写真实渲染 | Worker | `assemble_output` 必须用版本化 Render Manifest 和真实媒体输出，不接受 Demo generator |

### 4.5 HTTP、Event、Storage 与集成

| `main` 路径 | 分类 | 目标 Module | 迁移决定 |
|---|---|---|---|
| `internal/aigc/server/router.go`、`invoker.go` | 重写 | Agent | 按新 Session/Tool/Approval/Operation HTTP 契约；鉴权来自 Business/网关契约 |
| `internal/aigc/server/a2ui_stream.go`、`durable_events.go` | 选择性复用 SSE/Cursor 测试 | Agent | PostgreSQL EventLog 权威，补断线重放、Card Revision、未知 Action |
| `internal/aigc/server/event_bridge.go`、`runtime_processor.go`、`runtime_bridge.go` | 重写 | Agent | Outbox/Inbox/Continuation 与 Session Lane 持久化；不依赖内存 Bridge 真源 |
| `internal/aigc/server/wakeup.go` | 重写 | Agent | Redis 只唤醒；丢失可由 PostgreSQL 扫描恢复 |
| `internal/aigc/server/approval_projection_test.go`、`durable_router_test.go`、`event_bridge_test.go` | 选择性复用场景 | Agent contract/integration tests | 转成 Approval/A2UI/重放的黑盒测试，不复制旧 Router API |
| `internal/aigc/events/**` | 重写，复用 Outbox/Relay 测试思想 | 各 Module 自有 | 每个 Producer 自有 Outbox、Consumer 自有 Inbox；禁止共享单体 event store |
| `internal/aigc/storage/postgres.go`、`redis.go` | 重写 | 三 Module 各自 | 各 Runtime 独立配置/连接池/Readiness；Worker 最小跨库角色单独配置 |
| `internal/aigc/storage/redis_checkpoint.go` | 重写/用途收窄 | Agent | Checkpoint 只用于短调用恢复，不能承载 Approval/Job/业务状态 |
| `internal/aigc/config/**` | 重写 | 三 Module | 配置分域、严格校验、Secret 不落日志；去掉单体 Demo fallback |
| `internal/aigc/integration/local_demo_flow_test.go` | 重写为 Smoke | `smoke/` + Module integration | 场景可映射 `SMK-009`～`SMK-016`，但必须使用三真实 Runtime 和 Evidence Bundle |
| `internal/aigc/patch/json_patch.go` | 选择性复用 | 明确使用方所在 Module | 先补 RFC/路径/资源上限/安全测试；不得作为跨 Module 共享包 |

## 5. 高价值测试资产

以下测试优先转成“新测试先行”，不直接搬旧实现：

| 旧测试组 | 新测试目标 | 优先级 |
|---|---|---|
| `a2ui/*_test.go`、`server/*event*test.go` | A2UI Schema、危险 Markdown、未知 Action、Cursor 重放、Event 去重 | P0 |
| `approval/**/*_test.go`、`approvalruntime/*_test.go` | Approval CAS、过期/重复/版本冲突、Continuation 新 Turn | P0 |
| `generation/state_machine*`、`barrier*`、`request_fingerprint*`、`worker*` | Operation/Batch/Job、Fence、Barrier、unknown outcome、取消 | P0 |
| `generation/handlers/*_test.go`、`tools/image2*`、`tools/seedance*` | Provider 请求映射、状态查询、错误归一化、幂等 | P0 |
| `sessionruntime/*_test.go`、`agent/model_receipt*` | Session Lane、Receipt 重放、进程恢复 | P0 |
| `storyboard/*_test.go`、`spec/*_test.go` | Revision、稳定 ID、锁/绑定保护、版本冲突 | P0 |
| `billing/*_test.go` | 并发扣费、追加式账本、失败不退款、冲正 | P0，但需按 Business 账务模型重写 |
| `skill/parser_test.go`、`turncontext/*_test.go` | Published Snapshot、权限收窄、上下文最小化 | P1 |
| `integration/local_demo_flow_test.go` | SMK-P0 场景种子 | P1；不能作为当前端到端证明 |

每个移植测试必须加入新 Requirement/Smoke ID、目标 Module Owner 和权威状态断言。只把旧测试改到编译通过，不算迁移完成。

## 6. 明确禁止迁移的模式

- 根 Module import path `github.com/FigoGoo/Dora-Agent/internal/aigc/...`；
- 生产启动时 `AutoMigrate/Migrate` 自动建业务表；
- Memory Store/Broker/Notification Hub 作为生产 fallback；
- 一个 Runtime 同时打开三类领域 Store 并直接调用；
- Agent/Graph 直接调用媒体 Provider、TOS 或 Worker Handler；
- Worker 读取/更新 Agent 或 Business 普通表；
- Redis Checkpoint/List 作为 Approval、Job 或账本权威；
- `stableID` 哈希替代 UUIDv7 主键；
- 零价 Demo Cost Map 或代码硬编码价格；
- 五 Tool Registry、隐藏 `write_prompts`、Storyboard 内部生成最终 Prompt；
- manifest/占位文件冒充真实视频导出；
- 旧表双写、长期兼容开关或 `legacy_provider_adapter`；
- 从 `main` 整目录 checkout/copy 后再“慢慢拆”。

## 7. 推荐迁移批次

### Batch A：只移植测试向量和契约样例

先在目标 Module 写 A2UI、Approval、状态机、Provider、Receipt 的新测试/Golden Vector。此批不引入旧生产 Store/Runtime。

### Batch B：M1 基础设施

新建三 Module 的配置、PostgreSQL、Redis、etcd、Migration、UUIDv7、Clock、Outbox/Inbox 和健康检查。旧 `storage/config/events` 只作为遗漏项清单。

### Batch C：Business 领域真源

按新模型实现 User/Project/Skill、CreationSpec、MaterialAnalysis、Storyboard、Prompt、AssemblyPlan、Asset/Binding、Billing/Payment。旧 `spec/storyboard/asset/billing/artifact/skill` 的测试场景逐项迁移。

### Batch D：Agent 执行真源

实现 Session/Input/Turn/Run、Tool Registry、Receipt、Approval、Operation/Batch/Job、A2UI EventLog 和六个 Graph。旧 `agent/session/sessionruntime/server/capability` 只选择性参考。

### Batch E：Worker 与 Provider

实现 `AGT-JOB-V1` Client、Attempt/Receipt、Provider/TOS、Finalize、Terminal Commit。选择性移植 Image2/Seedance 等映射与测试，Demo Handler 进入 test-adapters。

### Batch F：Smoke 与前端接入

把 `local_demo_flow_test` 的业务意图拆入 SMK-P0，接真实三 Runtime；当前前端逐页替换 Mock，不从 `main` 复制。

## 8. 迁移 PR 证据模板

每个迁移 PR 必须填写：

```text
来源快照：main@<sha>
来源文件：<exact paths>
目标 Module/文件：<exact paths>
分类：选择性复用 / 重写 / 废弃
保留行为：<list>
主动改变行为：<list>
Requirement/Smoke IDs：<ids>
契约/Migration 影响：<versions>
旧测试 → 新测试：<mapping>
验证命令与结果：<commands/results>
```

若来源代码同时包含多个 Owner，PR 必须先拆契约，再分别落到 Module；不得为了减少 PR 数量而创建新的跨 Module 共享 `internal` 包。

## 9. 当前结论与剩余审计

本清单已覆盖 `main` 顶层生产资产和全部 29 个 `internal/aigc` 一级包，完成“选择性复用、重写、废弃、历史保留”的包级归类。它足以阻止单体目录整体回迁，并为 M1～M3 排定迁移来源。

进入某个具体迁移 PR 前仍需：

- 完整读取该来源文件及其测试，而不是只依据本包级结论；
- 对照已评审的目标 Graph Tool/跨 Module 设计；
- 确认目标 Module Migration Owner、公开契约和禁止依赖；
- 对算法/协议建立新 Golden Vector，再决定是否逐函数移植；
- 在 PR 中记录未迁移文件，避免默认“剩余文件以后自动搬”。

当前结论：**迁移资产盘点完成，具体生产迁移仍受 M0 评审门禁约束。**
