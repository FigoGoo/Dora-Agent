# Dora 本地 MVP 全工具运行时设计 v1

> 状态：**Approved for Development Preview**
> Profile：`mvp_all_tools.runtime.v1preview1`
> 范围：仅本机、回环地址、独立开发数据库；不构成生产 Runtime、通用 Dispatcher 或 W2 总体设计批准。
> 目标：复用现有五条已验证 Preview Runtime，在同一进程中用一个主 ChatModelAgent 和一个 Session Lane Claim Owner 跑通基本功能。

## 1. 结论

本地 MVP 不再要求开发者一次只启动一种 Graph Tool。新增一个显式 Profile，同时开放以下能力：

1. 普通用户消息直接回复；
2. `plan_creation_spec`；
3. `analyze_materials`；
4. `plan_storyboard`；
5. `write_prompts`。

实现必须复用现有 Enqueue、PostgreSQL Repository、Receipt、Runner、Graph Tool、EventLog、Workspace/SSE 与 Business Foundation RPC。只新增两个薄适配层：

- 一个共享模型路由器，用可信 Turn Context 选择现有 source-specific Receipt Model；
- 一个共享 Scanner/Coordinator，用轮询方式调用现有 Processor 的单步处理能力。

禁止新建第二套业务状态机、通用任务表、统一大 DTO、并行的五个 Claim Loop，或把历史 `/api/aigc/**` 页面迁回主链。

## 2. 现状与问题

五条 Runtime 已分别完成隔离开发预览，且各 PostgreSQL `ClaimNext` 都先计算 Session 的全来源 HOL，再过滤自身 source。这已经提供顺序真源，但当前启动配置强制五选一，Composition Root 也会为每个 Profile 分别构造 ChatModelAgent 和 Processor worker。

直接删除互斥校验并同时启动五套 Processor 不合格，原因是：

- 会形成五个独立 Scanner owner，Wake、停止、并发预算无法统一；
- 会创建五个 ChatModelAgent，违反项目“唯一主 Agent”约束；
- 在同一 Session 的不同 source 之间产生不必要的竞态和扫描放大；
- 让能力真相继续分散在多组布尔开关里。

本设计只解决本地 MVP 的组合装配，不宣称完成通用生产 Session Lane Dispatcher。

## 3. 不做事项

本阶段明确不做：

- 不新增或合并 Agent PostgreSQL 表；
- 不修改现有 source type、Receipt schema、Graph State 或 Event schema；
- 不修改 Graph Tool 的输入、输出、幂等与 unknown-outcome 语义；
- 不引入消息总线、插件系统、动态 Tool Search、复杂意图分类器；
- 不开放生产环境、远程 PostgreSQL、远程 Redis 或远程 etcd；
- 不把静态 Tool Catalog 中尚未实现的 `generate_media`、`assemble_output` 声明为可用；
- 不删除隔离 Profile；它们继续服务现有 canonical smoke 与故障定位；
- 不把本文视为 `runner-session-lane-review-v1.md` 的整体批准。

## 4. Profile 与配置真相

### 4.1 唯一 Profile 值

三端使用同一个精确值：

| 端 | 环境变量 | 精确值 |
|---|---|---|
| Agent | `DORA_AGENT_RUNTIME_PROFILE` | `mvp_all_tools.runtime.v1preview1` |
| Business | `DORA_BUSINESS_RUNTIME_PROFILE` | `mvp_all_tools.runtime.v1preview1` |
| Frontend | `VITE_DORA_RUNTIME_PROFILE` | `mvp_all_tools.runtime.v1preview1` |

空值保持当前默认失败关闭行为。任何未知非空值启动失败，禁止静默回退。

### 4.2 与遗留开关的关系

当统一 Profile 非空时，所有隔离开关必须显式为 false；任一隔离开关同时为 true 时启动失败。Profile 在配置对象内派生以下 effective capability，而不是改写原始环境变量：

| 能力 | Agent | Business | Frontend |
|---|---:|---:|---:|
| user message | 开 | 现有 Session RPC | 开 |
| plan creation spec | 开 | 开 | 开 |
| analyze materials | 开 | 开，并启用现有 Asset Analysis Preview | 开 |
| plan storyboard | 开 | 开 | 开 |
| write prompts | 开 | 开 | 开 |
| generate media | 关，待独立预览设计实现 | 关 | 关 |
| assemble output | 关，待独立预览设计实现 | 关 | 关 |

隔离 Profile 保持原验证、端口和 smoke 行为。统一 Profile 不改变它们的 canonical 脚本。

### 4.3 本地安全边界

统一 Profile 仅在 `SERVICE_ENV=local` 生效，并要求 Agent/Business HTTP、RPC、PostgreSQL、Redis、etcd 均为回环地址。使用专用本地数据库和专用对象目录。任一端缺少相同 Profile 或 Business Probe 能力不一致，Agent 启动失败。

## 5. 一个共享主 Agent

### 5.1 Tool Registry

统一 Profile 只构造一个 `adk.ChatModelAgent`，Registry 精确包含：

- `plan_creation_spec`；
- `analyze_materials`；
- `plan_storyboard`；
- `write_prompts`。

`analyze_materials`、`plan_storyboard`、`write_prompts` 保持 `ReturnDirectly`；`plan_creation_spec` 保持现有二次 Assistant completion 行为。最大迭代数沿用 Creation Spec 已验证预算，且不得低于 4。

普通用户消息是同一个 Agent 的无 Tool 分支，不另建 Direct Response Agent。

### 5.2 可信模型路由

共享模型路由器实现 Eino `ToolCallingChatModel`，仅从现有可信 Context 读取精确类型。一次调用必须且只能命中以下一种来源：

| Context | 委托模型 | 可见 Tool |
|---|---|---|
| Creation Spec `PreviewFrom` | 现有 Creation Spec Receipt Router | 仅 `plan_creation_spec` |
| `UserMessageRuntimeFrom` | 现有 User Message Receipt Model | 空集合 |
| `MaterialAnalysisRuntimeFrom` | 现有 Analyze Materials Receipt Router | 仅 `analyze_materials` |
| `PlanStoryboardRuntimeFrom` | 现有 Storyboard Receipt Router | 仅 `plan_storyboard` |
| `WritePromptsRuntimeFrom` | 现有 Prompt Receipt Router | 仅 `write_prompts` |

零个或多个来源同时存在都失败关闭。不得根据用户文本、Tool 参数、Prompt 内容或前端字段选择模型。

共享路由器在初始化时接收五个已经带 Receipt 能力的子模型。`WithTools` 必须保持不可变语义：每次委托追加该 source 的精确 Tool 集合，不能把完整四工具集合泄漏给子 Router。流式和非流式入口执行同一 Context 校验。

### 5.3 Runner 复用

现有五种 source-specific Runner 继续负责各自的输入转换、完整事件消费、终态验证和输出解析，但它们都接收同一个 `ChatModelAgent` 实例。Runner 对 Agent 名称的校验改为允许统一 Profile 的稳定名称；隔离 Profile 原名称继续允许。

这不是五个 Agent：多个 `adk.Runner` 只是对同一不可变 Agent 定义的 source-specific 执行适配。

## 6. 一个 Session Lane 协调器

### 6.1 单步 Processor 接口

五个现有 Processor 增加：

```go
ProcessNext(ctx context.Context) (claimed bool, err error)
```

它只执行一次 `ClaimNext`；未命中返回 `(false, nil)`，命中后同步走完原有 `process`，返回 `(true, nil)`。当前隔离 `Start/Stop/Wake` 继续存在，其 worker 循环改为复用 `ProcessNext`，避免复制状态机。

`ProcessNext` 不导出 Claim、不改变重试策略，不跨 source 调用 Repository。

### 6.2 Coordinator

新增 `agent/internal/mvpruntime`，只定义一个很小的 Handler 接口和生命周期：

```go
type Handler interface {
    ProcessNext(context.Context) (bool, error)
}
```

Coordinator 按固定顺序注册五个 Handler，使用共享的：

- 并发数；
- Poll interval；
- 有损 Wake channel；
- intake cancel、run cancel 与 drain deadline；
- round-robin 游标。

每次被唤醒或定时扫描时，从游标开始最多访问全部 Handler 一轮；命中一个工作后移动游标并继续扫描，直到完整一轮均无工作。单个 Handler 的瞬时错误只结束当前扫描轮并记录，不跳过数据库中的 HOL，不转换业务状态。

统一 Profile 下严禁调用五个 Processor 自身的 `Start`。HTTP/Session 入队服务全部注入 `Coordinator.Wake`。Composition Root 只对 Coordinator 执行一次 Start 和一次 Stop。

### 6.3 HOL 正确性

顺序仍由 PostgreSQL 真源保证：每个 source-specific `ClaimNext` 先选取每个 Session 的全来源最低 `enqueue_seq`，只有该 HOL 的 source 与自己相同时才能 Claim。

因此，轮询错误 source 只会返回空；它不能越过 HOL。随后正确 Handler Claim 同一 HOL。`running`、`retry_wait`、`quarantined` 和有效 Lease 继续阻塞后续输入。不同 Session 可由 Coordinator 的多个 worker 并行处理。

Coordinator 不缓存 Session 状态，不推断 source，不持久化游标。进程崩溃后 PostgreSQL Lease/Fence 与现有 Recovery 规则接管。

## 7. Composition Root 顺序

统一 Profile 的启动顺序固定为：

1. 加载配置并验证三端 Profile、回环地址和预算；
2. 打开 Agent PostgreSQL，验证五条现有 Runtime schema；
3. 完成 User Message legacy helper 的有界升级检查；
4. 连接 Redis、etcd、Business RPC 并验证有效能力；
5. 构造五个 source-specific Repository、Receipt Model/Tool 与 Graph；
6. 构造共享模型路由器和唯一主 ChatModelAgent；
7. 用同一 Agent 构造五个 Runner、Processor；
8. 构造 Coordinator，并把 `Coordinator.Wake` 注入全部入队 Service；
9. 注册 HTTP/RPC Handler，先监听再启动 Coordinator；
10. 服务健康后注册 etcd。

停止顺序：先摘除 etcd，停止 HTTP/RPC 新输入，再停止 Coordinator intake，等待在途执行；超出统一 shutdown deadline 才取消 run context，最后关闭外部连接。

任一步失败都不得留下已启动的 source-specific worker。

## 8. Business 与前端装配

Business 统一 Profile 只派生并注册现有四个 Preview BFF/Foundation 能力，不新增 RPC DTO。Probe 必须同时返回 Storyboard 与 Write Prompts 的既有精确 Profile；Creation Spec 与 Analyze Materials 继续通过已有启动门禁和请求契约验证。

Frontend 统一 Profile 只派生正式 `ProjectWorkspacePage` 中已有的四个 Tool 入口和普通消息入口。不得注册或回退到 DEV-only `AigcWorkspacePage`、`projectMocks.js` 或 `/api/aigc/**`。

当某能力的后端探针/HTTP 返回失败时，前端展示稳定错误，不伪造成功卡片，不用本地 Mock 降级。

## 9. 失败、恢复与可观测性

- 模型路由 Context 不匹配：终止当前 source 执行，沿现有 Runtime failure/Receipt 路径收口；
- 某 Handler Claim 失败：保留数据库状态，下一个 Poll 重试；
- Wake 丢失：Poll scanner 恢复；
- Runner/Tool/Business 响应未知：完全沿用 source-specific Receipt 与 query-only recovery；
- Lease 丢失：旧 Fence 结果不得提交；
- Coordinator 停机超时：取消共享 run context，不修改权威终态；
- Business Profile 不一致：启动失败，不开放半套能力；
- 未知 Profile：启动失败，不静默回退隔离 Runtime。

日志只记录 Profile、source、稳定 ID、状态、耗时与错误码；不得记录 Prompt、完整模型输出、Secret 或受保护素材。

建议最小指标：Coordinator 扫描轮次、各 source claim 数/空扫数/错误数、HOL age、Wake 合并数、在途数与 drain 耗时。source 是固定低基数标签。

## 10. 测试门禁

### 10.1 单元与契约

- [ ] Agent/Business 未知 Profile、非 local、非回环、Profile 与隔离开关同时启用均失败；
- [ ] Profile 派生的 capability 集合精确，静态未实现 Tool 保持关闭；
- [ ] 共享模型路由五个合法 Context 分别只看到精确 Tool；
- [ ] 零 Context、双 Context、伪造用户文本均失败关闭；
- [ ] 共享主 Agent Registry 恰好四项且只构造一次；
- [ ] 五个 Processor 的 `ProcessNext` 复用原处理路径；
- [ ] Coordinator round-robin、Wake 丢失、poll、并发、重复启动、drain/cancel 通过；
- [ ] 同 Session 跨五 source 的输入严格按 `enqueue_seq` 完成；
- [ ] 不同 Session 可并行；`retry_wait/quarantined/running` 不被越过；
- [ ] 隔离 Profile 的既有测试与五条 canonical smoke 全部回归。

### 10.2 本地集成验收

在专用本地数据库中，单次登录和单个项目至少完成：

1. 普通消息得到真实 EventLog/SSE 回复；
2. Creation Spec 形成候选或明确终态；
3. 文本素材进入 Analyze Materials 并生成分析卡；
4. 同一项目执行 Storyboard；
5. 同一项目执行 Write Prompts；
6. 浏览器硬刷新和 SSE 断线重连后仍从 PostgreSQL 快照恢复；
7. Agent 进程重启后未完成输入按 Lease/Fence 恢复；
8. 五条 canonical isolated smoke 继续通过。

生成 PNG/MP4 属于下一阶段的独立本地媒体预览设计；其 Profile 组合、六工具入口和内部传输已在 [MVP 六工具媒体扩展 V1](./mvp-six-tools-media-extension-v1-design.md) 批准，只有该纵切真实跑通后才能宣称完成。

## 11. 实施切片

按以下顺序提交，任何切片都可独立回滚：

1. Agent/Business/Frontend Profile 配置与失败关闭测试；
2. 共享模型路由器、四 Tool 主 Agent 与 Runner 兼容测试；
3. 五个 Processor `ProcessNext` 重构及原 Profile 回归；
4. Coordinator 与 Bootstrap 统一装配；
5. 统一 Profile 前端开关与开发配置示例；
6. 新 `mvp-all-tools-runtime-smoke`，最后纳入一键 `trial-basic`。

回滚只需清空三端统一 Profile；隔离 Profile 与数据库 schema 不变，不需要 Down migration。

## 12. 审核结论

本文批准的只有 `mvp_all_tools.runtime.v1preview1` 的本地组合实现。它建立在五条现有 Preview Runtime、PostgreSQL HOL、Receipt-first 与 Fence 语义之上，并以最少适配层消除多 Agent、多 Scanner 与能力开关分散。

批准不覆盖生产开放、W2 通用 Session Lane、Approval、计费、真实 Provider、通用 Dispatcher 或未实现 Graph Tool。实现若需要新增数据库表、改变跨 Module DTO、放宽回环约束或复制状态机，必须暂停并重新评审。
