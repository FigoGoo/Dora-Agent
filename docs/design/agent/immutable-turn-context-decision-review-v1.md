# Agent Immutable Turn Context P0 决策评审包 v1

> 文档状态：推荐决策候选 / Awaiting Owner Approval
>
> 评审日期：2026-07-15
>
> 适用范围：[`immutable-turn-context-design-v1.md`](./immutable-turn-context-design-v1.md) 第 12 节 10 项 P0、Session Lane 与 Eino Runner 的冻结边界
>
> 实现门禁：本文把 10 项 P0 转为可签字的推荐结论，但不代表任何一项已经 Approved。跨角色审核全部签字前，禁止创建 Turn/Context 生产 Migration、GORM Model、Repository、Helper、Runner、Graph、HTTP/IDL 或前端 Approval Action；测试专用 Canonical Corpus 也不得被生产代码导入。

## 1. 评审目的与边界

本文承接以下设计，不替代它们：

- [`Agent Immutable Turn Context 最小物理契约 v1`](./immutable-turn-context-design-v1.md)；
- [`Session Lane PostgreSQL 物理设计与升级方案 v1`](./session-lane-postgresql-design-v1.md)；
- [`Session Event Foundation 独立 Marker 设计 v1`](./session-event-foundation-marker-v1.md)；
- [`Runner Session Lane 评审 v1`](./runner-session-lane-review-v1.md)。

本文只回答“建议批准什么、由谁批准、需要什么证据”。字段名、DDL、Go DTO 和运行时注册仍以批准后的独立实现批为准。任何推荐结论都不能单独关闭 P0，也不能把 `REAL PG PRECONDITION READY` 提升为生产可运行。

## 2. 推荐决策总表

| P0 | 推荐结论 | 决策 Owner | 批准前最低证据 | 当前状态 |
|---|---|---|---|---|
| TC-P01 冻结时点 | 需要模型或 Graph 的 Input 在 Ingress/legacy apply 的同一 Agent PostgreSQL 事务中创建 Turn 与 Context；Claim、retry、resume、takeover 只加载原 Context | Agent Runtime、Agent PostgreSQL | 并发入队、配置变更、commit response lost、重放与 takeover Corpus；真实 PG 原子性 | 推荐 / 未 Approved |
| TC-P02 模型可见历史 | Agent-owned append-only Message History 承载 `user/assistant/tool` 因果事实；Context 同时冻结 Event cutoff；Summary 是 Agent History Materialization Store 持有的不可变 ref/digest | Agent History、Agent Runtime、产品、安全 | role exact-set、ToolCall/ToolResult 配对、Message/Event cutoff、Summary 来源/摘要/保留、敏感数据与恢复测试 | 推荐 / 未 Approved |
| TC-P03 Message Set | v1 采用有界、按 `message_seq ASC` 的完整 leaf 数组摘要；prefix-chain 只允许作为不兼容 v2 另行评审 | Agent PostgreSQL、数据、Agent Runtime | 空集合与 Unicode golden、全字段敏感性、上限失败关闭、legacy 重算、真实 PG 性能证据 | 推荐 / 未 Approved |
| TC-P04 Prompt Bundle | Agent 持有版本化 Prompt Registry；Context 冻结 `prompt_key@version` 与 digest，历史版本按 Context 保留期可解析 | Agent Runtime、产品、安全 | Prompt canonical、发布/撤回/保留、无 Secret、旧 Turn 可重放 | 推荐 / 未 Approved |
| TC-P05 Executable Tool Registry | Agent 启动时编译六 Tool 静态 Registry；Turn 冻结 Registry ref/digest，单次 ToolReceipt 再冻结 Definition/Schema/Graph/Prompt Pin | Agent Runtime、各 Graph Tool Owner | exact-set、启动 Compile、不可用失败关闭、per-Turn/per-Call Pin 与 Tool Reduction 测试 | 推荐 / 未 Approved |
| TC-P06 Runtime/Model/Budget | Runtime Policy、Model Route、Budget 都是 Agent-owned 不可变版本引用；Model retry/failover 只有一个 Owner；恢复不刷新预算 | Agent Runtime、财务、运维 | 默认值、墙钟/Token/费用/调用上限、Provider unknown outcome、恢复剩余预算测试 | 推荐 / 未 Approved |
| TC-P07 Access/Approval | Business Authorization 是权限真源；Agent 在事务前取得认证快照、事务内冻结 ref/digest，副作用前按冻结 Policy 重验；Continuation 绑定原 Receipt/Approval/Tool Pin | Business Authorization、Agent、安全、财务 | 跨用户/项目、资源版本变化、Decision/Consumption 幂等、过期/撤销/一次性消费测试 | 推荐 / 未 Approved |
| TC-P08 被引用事实不可变性 | Message、Context、Marker、Receipt/Authority 事实必须 append-only；EventLog 是可裁剪在线投影，独立 Marker 是永久最小证据 | Agent PostgreSQL、数据、安全、运维 | UPDATE/DELETE 拒绝、Retention/Marker、Key Rotation、anti-join 和 tamper 测试 | 推荐 / 未 Approved |
| TC-P09 Turn/Run 与 Activation | 固定 Input/Turn/Run exact-set、严格 HOL、generation gate、旧 Writer drain 与零早启；Context 损坏进入隔离/恢复，不刷新或跳过 | Agent Runtime、Agent PostgreSQL、运维 | 状态矩阵、Fence/CAS、lost wake、Scanner、Drain、stale generation 与恢复测试 | 推荐 / 未 Approved |
| TC-P10 物理与证据 | 所有长度、NULL/CHECK/索引、固定查询上限、无物理 FK、中文 COMMENT、Up/Down 与 Evidence 脱敏在实现前签字 | 数据、测试、运维、安全 | PG16 Up/upgrade/crash/race/CLI Down guard、Explain、容量与脱敏 Evidence | 推荐 / 未 Approved |

### 2.1 引用、发布、解析与保留 Owner

“Agent Runtime”不能作为多个引用对象的模糊总 Owner。v1 推荐按下表分离；同一团队可以承担多个角色，但证据和签字仍需逐对象给出：

| 对象 | 发布与 canonical/digest Owner | 运行时解析 Owner | 历史保留与撤回 Owner |
|---|---|---|---|
| Session Skill Snapshot | Agent Session/Skill Snapshot Owner | Agent Context Loader | Agent Session/Skill Snapshot Owner |
| Prompt Bundle | Agent Prompt Registry Owner | Agent Prompt Resolver | Agent Prompt Registry Owner；撤回不得删除仍被 Context 引用的版本 |
| Executable Tool Registry | Agent Tool Registry Owner；单 Tool Definition/Graph Pin 由对应 Graph Tool Owner | Agent Tool Registry Resolver | Agent Tool Registry Owner与各 Graph Tool Owner |
| Runtime Policy | Agent Runner Policy Owner | Agent Runner Policy Resolver | Agent Runner Policy Owner |
| Model Route | Agent Model Gateway Owner | Agent Model Route Resolver | Agent Model Gateway Owner |
| Budget Snapshot | Agent Budget Policy Owner；默认值和费用规则由产品/财务签字 | Agent Budget Enforcer | Agent Budget Policy Owner与财务审计 Owner |
| Access Scope Snapshot | Business Authorization 发布认证 Decision；Agent Access Snapshot Store 负责 canonical/digest | Agent Authorization Adapter；副作用前由 Business Authorization 再验证 | Business 保留权限 Decision；Agent 保留被 Context 引用的 Snapshot |
| Business Resource/Quote | 对应 Business Domain 持有资源版本、Quote、计费与业务结果真源 | Agent Business Adapter；副作用前回 Business 再验证 | 对应 Business Domain |
| Approval Decision/Consumption | Agent Approval Store 持有 Decision/Consumption Receipt 和父 Receipt Pin；Business 按跨 Module 契约校验其证据 | Agent Continuation/Tool Adapter | Agent Approval Store |
| Operation/Batch/Job | Agent Operation Store 持有执行状态与 current truth；Business/Worker 只返回各自负责的业务事实和 Worker terminal evidence | Agent Graph/Operation Adapter | Agent Operation Store |
| History Summary | Agent History Materialization Store 持有算法版本、来源 cutoff、ref/digest | Agent Context Loader | Agent History Materialization Store；不得早于引用 Context 删除 |

## 3. 十项推荐结论

### 3.1 TC-P01：在 Ingress 事务冻结，不在 Claim 冻结

推荐需要 Turn 的 Input 在首次可见前完成以下顺序：事务外解析不可变 Published Ref 和 Business Authorization Snapshot；事务内锁定 Session/Counter，重验 ref/digest，创建 Message/Input/Turn/Context/Receipt/Marker/Event；提交后才发送非权威 Wake。

- backlog 等待期间发布的新 Prompt、Tool、Model、Budget 或权限配置只影响后续新 Input；
- 同一 Input 的技术重试、Lease takeover、Runner resume 和 Checkpoint recovery 复用同一 Context；
- Claim 只允许验证和读取，禁止重新选择“当前配置”；
- 跨 Module RPC 不得发生在持锁事务内；RPC 返回的认证快照必须有稳定 ref、版本和 digest，事务内可本地验证；
- commit response lost 只能查询原 Receipt/Turn/Context，不能创建第二套冻结事实。

这样才能保证“可 Claim”与“已有完整 Context”同时成立，并避免队列等待时间改变业务语义。

### 3.2 TC-P02：模型历史是强类型因果事实，不是 Prompt 拼接日志

推荐将 Agent-owned `session_message` 扩展为 append-only 的模型历史真源，v1 至少支持 `user/assistant/tool` 三类稳定角色，并明确：

- Assistant ToolCall 与 Tool Result 必须通过稳定 call ID、ToolReceipt 和因果序号配对；缺一、乱序、跨 Turn 或摘要不符均失败关闭；
- System Prompt 由 Prompt Bundle 注入，Session Skill 由 Skill Snapshot 注入，Approval Continuation 由父 Receipt/Pin 注入，不伪造为 `user` 消息；
- Continuation 直接调用原 pinned Graph，不产生新的模型历史；
- Context 在同一 Ingress 事务中从锁定的 Event Counter 冻结 `event_cutoff_seq`；模型历史、Summary 和事件派生证据都不得读取该 cutoff 之后的 Event。EventLog 不是模型对话历史，只有经过批准的 History Materialization 才能进入模型输入；
- Summary 由 Agent History Materialization Store 持有，只能是带 Message/Event 来源 cutoff、算法版本、不可变 ref/digest 和保留期的 snapshot，不能删除或覆盖原始历史真源；
- 不认识的 role、content schema、tool call schema 或 summary schema 禁止进入 Runner。

### 3.3 TC-P03：Message Set v1 选择完整有界数组摘要

推荐 v1 对 `message_seq <= cutoff` 的完整、稠密、按序 leaf 数组计算 domain-separated digest。选择它是因为当前尚无 prefix 字段、回填算法、空链 golden、跨版本切换和数据库不变量；此时引入链式摘要会把未验证的性能优化写入永久事实。

- v1 必须设置固定 `max_messages` 和总字节上限；超限且没有 Approved Summary Snapshot 时失败关闭；
- 空集合、单消息、Unicode、Assistant ToolCall、Tool Result 和最大边界都要有 golden；
- prefix-chain 若未来获批，必须使用新的 schema/domain/version 和前向 Migration，禁止同一 Context 版本混用两种算法；
- legacy Helper 只能调用同一 canonical 实现重算，禁止 SQL 拼 JSON 或信任旧行已有摘要。

### 3.4 TC-P04：Prompt Bundle 由 Agent 版本化 Registry 持有

推荐 Prompt Registry 随 Agent 发布并支持 append-only 版本：Context 冻结 `prompt_key@version` 与 canonical digest，运行时只按精确版本加载。历史版本的保留期至少覆盖所有引用它的非终态 Turn、重试/审计窗口和合规要求。

Prompt Bundle 可包含 System Prompt、模板和安全策略引用，但不包含 Provider Secret。Skill 内容继续由 Session Skill Snapshot 持有；Summary、临时恢复提示和 Provider 参数不能暗中改写 Prompt Bundle 语义。

### 3.5 TC-P05：静态六 Tool Registry 与单次调用 Pin 分层

推荐启动时编译固定顺序的六 Tool Registry：`plan_creation_spec`、`analyze_materials`、`plan_storyboard`、`generate_media`、`write_prompts`、`assemble_output`。Context 冻结本 Turn 可见 Registry 的 ref/digest；ToolReceipt 逐次冻结 Tool key、Definition/Schema、Graph version 和 Prompt pin。

目录展示状态不是可执行授权。任一 Tool 未 Approved、Compile 失败、Schema/digest 不符或运行期不可用时失败关闭；Tool Reduction 只能在已冻结 Registry 内缩小集合，不能新增 Tool、改 Definition 或扩权。发布新版本不改变旧 Turn 的 Pin。

### 3.6 TC-P06：策略、路由和预算各自版本化，恢复不重置

推荐由 Agent 分别维护 Runtime Policy、Model Route 和 Budget Snapshot 的 immutable ref/digest。Model retry/failover 只由模型层拥有，Runner 不再叠加第二套盲重试。

- Budget 在 Ingress 冻结调用、Token、费用、Operation、迭代和墙钟上限；
- Queue delay 是独立 SLO，不消耗执行墙钟；执行墙钟从首个合法 Run `running` 开始，takeover/resume 继续使用剩余额度；
- Provider 返回 unknown outcome 且不可查询时进入隔离/人工处置，禁止用 failover 盲发；
- cancel/retry/failover 不能刷新 Context、切换为更宽预算或重新起算 deadline。

### 3.7 TC-P07：Business 授权真源、Agent 冻结证据、执行前再验证

推荐 Business Authorization 对用户、项目和资源权限负责；Agent 保存不可变 Access Scope Snapshot ref/digest，而不复制 Business 用户或 Project 模型。副作用前按冻结 Policy 重验当前资源版本、权限、Quote/Approval/Operation 条件；重验失败不改变已冻结 Context，只阻断动作并产生稳定证据。

Approval Continuation 必须绑定父 ToolReceipt、原 request semantic digest、Approval Decision/Consumption 和原 Tool Pin。自然语言“确认”、前端可编辑字段或模型参数都不能替代 Approval；Continuation 不重新过模型，也不能改变原动作参数。

### 3.8 TC-P08：引用事实先不可变，再允许 Runner 读取

推荐在任何 Context Writer/Runner 启用前，数据库层拒绝 Message、Context、Marker 以及已冻结 Receipt/Authority 的 UPDATE/DELETE，包括 no-op UPDATE。EventLog 同样必须拒绝任何 UPDATE；DELETE 只能由带 Marker/generation/fence 的受控 Retention 事务执行。Key Rotation 只能作用于受控密文存储和 Keyring，不能原地改变 semantic digest。

EventLog 继续作为有水位的在线投影；`session.created/session.input.accepted` 由独立 Marker 保存永久最小证据。Retention 只有在 exact Marker、Counter、generation 和事务条件全部匹配时才能裁剪 Event。跨表关系使用逻辑引用、同事务校验和 anti-join，不创建物理 FK 或 cascade。

### 3.9 TC-P09：状态 exact-set、严格 HOL 与分层 Activation

推荐固定以下 v1 状态集合：

- Input：`pending/claimed/running/retry_wait/quarantined/resolved/dead`；
- Turn：`created/running/completed/failed/cancelled`；
- Run：`created/running/recovery_pending/completed/failed/cancelled`。

未知状态、Context 损坏、Fence 不明或下游结果未知映射到 `quarantined/recovery_pending`，不得转 `dead` 放行 HOL。Activation 必须按 Foundation、Marker/Context/Legacy Verify、Lane Capability、Processor、Claim generation 分层；旧 Writer 未排空、blocker 非零或 generation stale 时 Claim/Wake/Run 均为零。

Eino Runner 只接收已完整加载并重算通过的 frozen Context。`SessionValues`、Checkpoint 或进程内缓存不是权威真源；Cancel/Retry/Failover 不得刷新 Context。严格 HOL 下不启用 TurnLoop 新消息抢占，后续消息等待当前 Input 达到已批准的终态。

### 3.10 TC-P10：先冻结物理边界和证据模板，再写生产 DDL

推荐批准包必须同时包含：字段长度与 exact CHECK、固定查询/批次上限、索引和 Explain、append-only trigger、中文 COMMENT、无物理 FK、容量增长、Up/upgrade/Down、crash/race/CLI migration、generation/Drain 和脱敏 Evidence。

Evidence 只记录安全 ID、状态、数量、reason code、generation、digest 和耗时；禁止输出正文、密文、Payload、DSN、Token、密钥或 Provider Secret。任何依赖本次 expanded contract 的 Marker/Authority/Ledger/Turn/Context/Run/新状态或新字段业务事实存在时，Down 必须在首个 DDL 前失败并保持版本/dirty/数据不变；Migration 005 已存在的 Session/Foundation cohort 本身不阻断 pristine expand-only Down。

## 4. 明确拒绝的替代方案

以下方案不进入 v1：

1. 在 Claim 或 Runner 启动时读取“当前配置”并覆盖原 Context；
2. 用 Eino `SessionValues`、Checkpoint、Redis 或进程内缓存作为 Turn Context 真源；
3. 把 System Prompt、Approval Continuation 或 Tool Result 伪造成 User Message；
4. 在没有 prefix 字段、golden 和 legacy 算法时直接上链式摘要；
5. 将目录 `available`、模型参数或前端字段当作 Tool/权限/Approval 授权；
6. 重试时重置墙钟、费用、Token、调用或 Operation 预算；
7. 为便利查询创建跨表物理 FK、cascade 或允许 no-op UPDATE；
8. 用纯 Go Corpus、sqlmock 或一次成功启动替代真实 PostgreSQL 16 升级/崩溃/Down 证据；
9. 在共享 R01/R02/R03/R08 未 Approved 前实现任一真实 Graph；
10. 先实现 `write_prompts` 作为捷径。首个真实 Graph 仍应是 `plan_creation_spec`，对应 `SMK-009`，并且只能在共享门禁关闭后开始。

## 5. 审批与实现顺序

1. Agent Runtime、PostgreSQL/Data、Business Authorization、安全、财务/产品、运维/SRE、测试分别对第 2 节职责签字；
2. 用独立 test-only Corpus 冻结 Message Set、Context canonical、exact-set、边界和稳定拒绝原因；
3. 通过设计评审后，以独立提交实现 append-only 基础、Marker/Authority/Legacy Helper、Turn/Context DDL 与 Repository；
4. 从真实 Migration 005 的 V1/V2/empty cohort 执行 PG16 forward Up、Helper、Verify、race/crash 和 CLI Down guard；
5. 证据 Approved 后才接 Lane Capability/Processor/Scanner，并确保旧 Writer drain 与零早启；
6. 最后装配 Eino Runner/Checkpoint/Model 与静态 Tool Registry；
7. 共享 R01/R02/R03/R08 全部关闭后，才进入 `plan_creation_spec` / `SMK-009` 的首个真实 Graph 切片。

## 6. 签字清单

- [ ] Agent Runtime：P01/P02/P04/P05/P06/P09 与 Eino 边界；
- [ ] Agent PostgreSQL/Data：P01/P03/P08/P09/P10 与容量、索引、锁顺序；
- [ ] Business Authorization：P07 的权限真源、Access Snapshot 与再验证；
- [ ] 安全：Prompt/Message/Access/Approval 数据分类、不可变、Key Rotation 与脱敏；
- [ ] 产品/财务：Budget、Quote、Approval、费用与 unknown-outcome 人工处置；
- [ ] 运维/SRE：generation、Drain、Retention、Scanner、Down/dirty 恢复和告警；
- [ ] 测试：Corpus、PG16 upgrade/crash/race/CLI matrix 与 Evidence 防空跑；
- [ ] 前端/A2UI：Event/Continuation 展示、刷新重连、未知类型、阻断状态与无伪授权入口；
- [ ] 跨 Module Contract Owner：所有 RPC/Event/Receipt/Approval exact version 与兼容策略。

当前结论：**10 项均已有推荐决策，但全部仍为 Awaiting Owner Approval；本文不授权生产实现。当前唯一允许的开发切片是独立 test-only Canonical Corpus 与评审证据。**
