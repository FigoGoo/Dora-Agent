# Dora 共通业务规则与验收基线

> 文档状态：需求基线稿
>
> 版本：v0.1
>
> 更新日期：2026-07-13
>
> 关联文档：[用户端需求总览](user-requirements-overview.md)、[管理端需求总览](admin-requirements-overview.md)、[服务端需求总览](server-requirements-overview.md)

## 1. 文档目的与适用顺序

本文统一 Dora 用户端、管理端和服务端共同依赖的业务术语、计费与收益规则、状态机投影、非功能基线和可执行验收格式。三份需求总览不得复制出与本文不同的同名规则。

当文档存在冲突时按以下顺序处理：

1. 已由用户确认并写入本文的共通业务规则。
2. 用户端、管理端、服务端总览中的具体场景规则。
3. 后续详细 PRD 和技术设计中的实现细节。

详细设计可以补充接口、数据结构和实现方案，但不得改变本文已确认的计费、不退款、收益回收和状态语义；确需改变时必须先回到需求层评审并同步更新所有关联文档。

## 2. 统一术语

| 术语 | 统一定义 |
| --- | --- |
| Skill Invocation | Agent 在一个 Turn 中实际选择并开始执行某个确定 Skill 版本的业务调用，是调用归因、运行追踪和发布者收益归属的最小单位，但不是固定的“一次调用统一扣费”单位。 |
| Graph Tool | 暴露给主 Agent 的高层场景能力，例如素材分析、故事板规划、媒体生成和成片装配；旧文档中的“高层 Capability”统一按 Graph Tool 理解。 |
| 公开 Tool | 由平台审核并允许 Skill 通过 `@Tool` 引用的受控能力，不等同于 Graph Tool。 |
| 可计费执行 | 使用了已发布计费配置、会实际消耗模型、媒体生成或公开 Tool 能力的一次确定执行项。 |
| 技术重试 | 在同一业务幂等键、同一语义和同一冻结计费配置下，为恢复瞬时失败而进行的重试，不形成新的用户主动需求。 |
| 用户重新生成 | 用户主动发起的新业务操作，具有新的幂等语义，按新的可计费执行正常扣费。 |
| 账务冲正 | 仅用于纠正重复扣费、错扣、无对应可计费执行等平台账务差错的反向账本记录，不属于产品退款能力。 |
| 退款 | 用户因失败、取消、不满意、部分成功或申诉而要求返还已正常扣除积分的产品能力；Dora 本期不支持退款。 |

## 3. 计费与收益规则

### 3.1 已确认计费原则

1. 用户积分价格由平台维护的模型、媒体、公开 Tool 或 Graph Tool 计费配置确定，Skill 创建者、用户、客户端和模型均不得提交或覆盖可信最终积分值。
2. 每次可计费执行开始前冻结计费配置版本、用户积分、模型或能力标识、计费单位、适用 Skill Invocation、预算和幂等键；运行中改价只对后续新执行生效。
3. 本期不采用积分预占。Business Service 必须在模型或生成请求正式开始并产生平台认可的计费事实前，以原子方式校验余额并扣除积分；扣费失败时不得启动对应外部执行。
4. 同一 Graph Tool 可以包含一个或多个可计费执行。Graph Tool 完成时只汇总已经落账的扣费明细，不再追加一笔“Tool 完成费”。
5. 异步 Graph Tool 返回 `accepted` 不代表费用汇总完成；其 Operation/Batch 进入业务终态后生成最终积分汇总。
6. 技术重试、进程恢复、Checkpoint/Receipt 重放、Worker 重复唤醒和同一幂等键重投不得重复扣费。因平台技术重试产生的额外下游成本由平台承担。
7. 用户主动重新生成、重新执行或明确追加生成数量属于新的业务操作，按新的可计费执行扣费。
8. 平台不提供失败退款、取消退款、部分成功退款、体验不满意退款、申诉退款或充值退款。已正常产生的扣费不因结果失败、用户取消、结果被替代或后续未采用而返还。
9. 若平台发生重复扣费、错扣、扣费成功但权威记录证明外部执行从未开始等账务差错，必须通过不可变账本冲正纠错；冲正不改变“不支持退款”的产品规则。
10. 每笔扣费、Graph Tool 汇总、收益入账、收益冻结、收益回收和账务冲正都必须具有稳定业务引用，并可以追溯到 User、Project、Session、Turn、Skill Invocation、Graph Tool、Operation/Batch/Job 和计费配置版本中的适用对象。

计费汇总公式：

```text
单个 Graph Tool 用户积分 = Σ 该 Graph Tool 下去重后的可计费执行积分
单个 Skill Invocation 用户积分 = Σ 直接或通过 Graph Tool 归属于该 Invocation 的去重后可计费执行积分
单个 Turn 用户积分 = Σ 该 Turn 下所有 Invocation 和平台原生可计费执行的去重后积分
```

### 3.2 计费真值表

| 场景 | 是否扣用户积分 | 是否生成发布者收益积分 | 后续处理 |
| --- | --- | --- | --- |
| 参数、权限、预算或安全校验在可计费执行开始前失败 | 否 | 否 | 返回可操作错误，不调用模型、Provider 或公开 Tool。 |
| 余额不足，原子扣费失败 | 否 | 否 | 不启动该执行，进入等待用户充值或失败状态。 |
| 扣费成功且模型、Provider 或公开 Tool 已正式开始执行 | 是 | 待执行结果确认 | 即使后续失败、取消、超时、结果被替代或用户未采用，也不退款；发布者收益按合格明细判定。 |
| 用户在可计费执行开始前取消 | 否 | 否 | 幂等取消，终止尚未开始的执行。 |
| 用户在可计费执行开始后取消 | 是 | 仅已成功的合格明细 | 尽力取消下游；已扣积分不退款，已产生副作用继续收尾或核对。 |
| 同一业务幂等键的技术重试、恢复或重复回调 | 不新增 | 不新增 | 重放原扣费和原收益结果。 |
| 用户主动重新生成或追加生成 | 是 | 按收益规则处理 | 作为新业务操作创建新扣费记录。 |
| Graph Tool 部分成功 | 按已开始的可计费执行逐项扣费 | 仅成功的合格明细 | 汇总成功、失败和已扣费明细，不退款。 |
| 外部请求是否开始无法确认 | 暂不新增扣费 | 暂不新增 | 进入 `reconciling`，先按原幂等键查询权威回执，再决定落账或结束。 |
| Graph Tool 或 Batch 到达终态 | 不二次扣费 | 汇总合格收益 | 只汇总去重后的已有扣费明细。 |
| 重复扣费、错扣或无对应可计费执行 | 原扣费冲正 | 对应收益冲正 | 写追加式冲正记录，不修改或删除原账本。 |
| Skill 自测、发布者自买、刷量或风险流量 | 按平台测试/风控规则扣用户积分 | 否或冻结后冲正 | 保留原始事实和风险处置记录，不形成可回收收益。 |

### 3.3 发布者收益积分与五折回收

1. 用户消费积分账户与发布者收益积分账户必须分离。
2. 发布者收益积分由平台收益规则根据有效的已发布 Skill Invocation、归属明确的已扣费明细和风险结果生成；收益积分不得由发布者设置，不得直接等同于用户总扣费或 Provider 成本。
3. “合格收益明细”必须同时满足：对应用户扣费为 `charged` 且未冲正、执行项成功产生有效结果、唯一归属于已发布 Skill Invocation、不是 Skill 自测或发布者自买，并通过刷量和风险校验。部分成功只计算成功子项；失败、取消或结果未知的子项不生成收益，但一个已成功结果后续被用户放弃或替代不撤销其正常收益。
4. 收益规则版本在 Skill Invocation 开始时冻结。后续改价、改规则或 Skill 更新不得改写历史收益。
5. Skill 自测、发布者自买、刷量、虚假账号、异常流量和未通过风控观察期的调用不生成可回收收益。
6. 收益积分先进入“待结算”，通过结算周期和风控后进入“可用”；存在申诉、风险或异常时可以冻结，确认违规或账务错误时使用收益冲正记录。
7. 平台按五折回收可用收益积分，回收系数固定为 `50%`：

```text
回收结算基数 = 本次合格可用收益积分 × 50%
```

8. 回收申请按整批可用收益积分计算；涉及最小积分或最小货币单位舍入时，不足一个最小结算单位的余数继续保留在收益账户，不得直接丢弃。
9. 五折只作用于发布者收益积分回收，不改变用户消费积分扣费数值，不允许把消费积分直接提现。
10. 收益积分转消费积分、收益回收和法币打款是不同账务动作，必须分别记录；具体积分兑付币种、法币面值、税费、最低回收门槛和打款渠道在详细财务设计中确认。

### 3.4 账务状态

| 对象 | 权威状态 |
| --- | --- |
| 用户积分扣费 | `uncharged → charging → charged`；账务差错时 `charged → reversed` |
| Graph Tool 费用汇总 | `collecting → completed/partial_completed/failed`，汇总状态不得反向改变底层已扣费事实 |
| 发布者收益积分 | `pending → available/frozen/reversed`；`available → frozen/recovered/reversed`；`frozen → available/reversed` |
| 收益回收申请 | `submitted → risk_review → finance_review → paying → completed/rejected/pay_failed` |

正常业务流程不存在 `reserved` 或 `refunded` 状态。执行状态与账务状态必须分离；运行失败不等于未扣费，已扣费也不等于运行成功。

## 4. 统一状态机与用户投影

### 4.1 状态使用原则

1. 服务端保存细粒度权威状态，用户端和管理端只做确定性中文投影，不得各自创建新的业务状态。
2. Project 生命周期与最近运行摘要分离；Skill 聚合与 Skill Version 状态分离；Skill Invocation 执行与账务状态分离。
3. 终态对象不得原地重置。人工重放、重新生成、重新提交审核或创建新公开快照必须创建新的业务记录并关联原记录。
4. 所有非法迁移、同版本并发覆盖和过期 Worker/Agent 提交必须失败关闭，并保留稳定错误码和审计记录。

### 4.2 权威状态与用户投影

| 对象/状态轴 | 服务端权威状态 | 用户端/管理端统一投影 |
| --- | --- | --- |
| Project 生命周期 | `active / archived / trash / deleted` | 使用中 / 已归档 / 回收站 / 已删除 |
| Project 最近运行摘要 | `idle / queued / running / waiting_user / waiting_async / succeeded / partial_failed / failed / cancelled` | 空闲 / 排队中 / 进行中 / 等待我处理 / 等待异步结果 / 已完成 / 部分失败 / 失败 / 已取消 |
| Skill Version | `draft / testing / submitted / reviewing / published / rejected / suspended / offline / deprecated` | 草稿 / 测试中 / 待审核 / 审核中 / 已发布 / 已驳回 / 已暂停 / 已下架 / 已废弃 |
| Skill Invocation 执行 | `created / running / waiting_user / waiting_async / succeeded / partial_failed / failed / cancelled` | 已创建 / 运行中 / 等待我处理 / 等待异步结果 / 成功 / 部分失败 / 失败 / 已取消 |
| Skill Invocation 账务 | `uncharged / charging / charged / partially_charged / reversed` | 未扣费 / 扣费中 / 已扣费 / 部分扣费 / 已冲正 |
| Graph Tool | `created / running / waiting_user / accepted / completed / partial_failed / failed / cancelled` | 已创建 / 运行中 / 等待我处理 / 已接受异步执行 / 已完成 / 部分失败 / 失败 / 已取消 |
| Generation Job | `pending / running / retry_wait / reconciling / succeeded / dead / cancelled` | 排队中 / 生成中 / 等待重试 / 结果核对中 / 成功 / 失败 / 已取消 |
| Generation Batch | `waiting_jobs / finalizing / reconciling / cancelling / completed / partial_failed / failed / cancelled` | 处理中 / 收尾中 / 结果核对中 / 取消中 / 已完成 / 部分失败 / 失败 / 已取消 |
| Approval | `pending / approved / rejected / expired / cancelled` | 待确认 / 已同意 / 已拒绝 / 已过期 / 已取消 |
| Featured Work | `draft / submitted / reviewing / published / rejected / suspended / offline` | 草稿 / 待审核 / 审核中 / 已发布 / 已驳回 / 已暂停 / 已下架 |
| Announcement | `draft / scheduled / published / expired / withdrawn` | 草稿 / 已排期 / 展示中 / 已结束 / 已撤回 |
| 发布者收益积分 | `pending / available / frozen / recovered / reversed` | 待结算 / 可用 / 冻结 / 已回收 / 已冲正 |

### 4.3 关键恢复规则

- `waiting_user` 必须关联正式 Approval、明确待处理动作和过期时间；用户自然语言中的“确认”不能替代正式审批。
- `waiting_async` 或 `accepted` 必须关联 Operation/Batch，不维持跨分钟的 Agent/Graph 调用栈。
- `reconciling` 表示外部副作用结果未知，必须先查询权威回执，禁止直接重试、重复扣费或伪造成功/取消。
- `dead` 是 Worker 不可自动恢复的终态；人工重放必须创建新 Job，并关联原 Job、操作人和原因。
- 已经开始的可计费执行即使最终进入 `failed`、`dead` 或 `cancelled`，对应正常扣费仍保持 `charged`；只有账务差错才能进入 `reversed`。

## 5. 详细设计阶段必须交付的产物

以下内容按用户决定延后到详细设计阶段，不在需求总览中提前固定具体表、字段、RPC 方法或 Event Schema：

1. 跨 Module 数据所有权、Migration Owner、写入方、读取方和一致性矩阵。
2. Business、Agent、Worker 之间的 Thrift DTO、Event、Job Payload、Outbox/Inbox 和版本兼容策略。
3. 注册登录、实名、支付、收益回收、打款、账号注销、申诉和数据保留的详细 PRD。
4. 每个 Agent-facing Graph Tool 的独立设计文档、Graph State、Node/Branch、HITL、幂等、预算和状态机。
5. 模型、媒体和公开 Tool 的具体计费配置 Schema，以及积分兑付法币的财务规则。

上述产物虽然延后到设计阶段，但属于开始对应功能实现前的强制评审门禁。任何 Module 不得因设计尚未完成而默认直连其他 Module 数据库、复用其他 Module 的 `internal` 包或自行解释计费规则。

## 6. v1 量化非功能基线

以下指标是 v1 默认验收下限。外部模型和 Provider 自身耗时不计入平台同步接口延迟，但平台必须在限定时间内返回已接受、等待或失败状态。

| 维度 | v1 基线 |
| --- | --- |
| 可用性 | 用户端和管理端核心 API 月可用性不低于 `99.9%`；积分扣费、收益账本和可靠任务受理接口不低于 `99.95%`。 |
| 同步接口延迟 | 不含外部 Provider 的读接口 `P95 ≤ 500ms`、写接口 `P95 ≤ 800ms`；快速创建 Project `P95 ≤ 1s` 返回 `project_id`。 |
| 交互反馈 | 用户提交操作后 `200ms` 内出现本地反馈；持久化输入接受后 `P95 ≤ 2s` 返回首个 SSE 状态事件。 |
| 断线恢复 | SSE 重连携带最后确认序号后，`P95 ≤ 3s` 完成缺口回放并进入实时流；超过在线事件窗口时必须回源完整读模型。 |
| 可靠唤醒 | Redis 唤醒丢失或不可用时，PostgreSQL 扫描应使已到期 Agent Input/Job 在 `P95 ≤ 30s` 内重新进入处理。 |
| 幂等 | 同一幂等键并发或重放 `100` 次，只允许产生一个业务事实、一次正常扣费和一份发布者收益。 |
| 容量 | 上线前压测容量不得低于已批准峰值预测的 `2 倍`；Session Lane、Worker Pool、Claim Batch、Provider、模型和公开 Tool 并发必须分别有硬上限。 |
| 可恢复性 | PostgreSQL 权威数据目标 `RPO ≤ 5min`、核心服务目标 `RTO ≤ 30min`；至少每季度完成一次恢复演练。 |
| 优雅退出 | Agent/Worker 收到退出信号后 `5s` 内停止接收或 Claim 新任务，并在配置的最长 `60s` Drain 窗口内保存可恢复状态。 |
| 数据保留 | 普通应用日志默认 `30 天`、Trace 默认 `7 天`、在线 EventLog 默认 `30 天`；账务和审计记录至少 `7 年`或遵循更长法定要求。 |
| Checkpoint/Approval | 终态 Run 的技术 Checkpoint 默认保留 `7 天`；普通用户确认默认 `24 小时`过期，具体高风险动作可以配置更短时限。 |
| 安全 | Secret、完整 Prompt、完整 Tool/Provider Payload、Checkpoint 和 Reasoning 不进入普通日志或 Trace；高敏查看和导出 `100%` 记录审计。 |
| 无障碍 | 桌面 Web 关键创作、支付、审批和管理流程达到 WCAG 2.1 AA 的键盘、焦点、语义和对比度要求。 |
| 财务对账 | 每日自动对账覆盖 `100%` 扣费、冲正、收益和回收记录；任何不平记录在发现后 `5min` 内告警并阻止相关重复结算。 |

## 7. 可执行验收规范

### 7.1 用例格式

每条验收用例必须具有稳定 ID，并包含：

- `Given`：初始数据、账号、权限、配置版本、余额和对象状态。
- `When`：唯一业务动作、幂等键及必要并发/故障条件。
- `Then`：权威状态、用户投影、账本/事件数量、禁止发生的副作用和时限。
- `Evidence`：API/RPC 响应、数据库权威记录、EventLog、审计日志、指标或 UI 结果。

同一需求 ID 必须能够追踪到用户端、管理端、服务端、详细设计和测试用例。编号前缀统一使用：

```text
USR-  用户端
ADM-  管理端
SRV-  服务端
BILL- 共通计费
AGT-  Agent Runtime
WRK-  Worker
NFR-  非功能
```

### 7.2 共通必测用例

下表是可直接转为自动化或人工测试的场景定义。每次验收执行都必须附带对应 Evidence：用户端场景至少包含 UI/无障碍结果、API 响应和关键事件；管理端场景至少包含 API 响应、权限判定和审计记录；计费与收益场景至少包含幂等键、不可变账本、Graph Tool 汇总和下游回执；Agent/Worker 场景至少包含权威状态、Lease/Fence、Receipt/EventLog 和外部调用次数；非功能场景必须附压测、监控或恢复演练报告。只有 UI 截图、只有接口成功码或只有数据库快照均不能单独作为通过证据。

| ID | Given / When | Then |
| --- | --- | --- |
| BILL-001 | Given 余额充足且计费版本已冻结；When 同一生成幂等键并发提交 100 次 | 只启动一个业务执行，只产生一笔扣费和一份收益归属，其余返回原结果。 |
| BILL-002 | Given 余额不足；When 发起可计费生成 | 原子扣费失败，不调用 Provider，不产生 Asset、收益或负余额。 |
| BILL-003 | Given 已成功扣费且 Provider 已开始；When 用户取消或执行最终失败 | 已开始的执行停止或收尾，原扣费不退款，运行状态与账务状态分别展示；失败或取消子项不生成发布者收益，已经成功的合格子项除外。 |
| BILL-004 | Given Graph Tool 包含多个执行项；When 部分成功、部分失败并进入终态 | 汇总积分等于去重后的已扣费执行之和，不追加 Tool 完成费，不对失败项退款。 |
| BILL-005 | Given 扣费成功后进程崩溃；When 使用相同幂等键恢复 | 查询并重放原扣费，不产生第二笔扣费或第二份收益。 |
| BILL-006 | Given 存在重复扣费或错扣证据；When 财务执行受控纠错 | 原账本不修改，新增唯一冲正记录，相关收益同步冲正并完整审计。 |
| BILL-007 | Given 发布者有合格可用收益积分；When 提交回收 | 按 50% 系数计算结算基数，余数保留，重复提交不重复扣减收益或打款。 |
| AGT-001 | Given Session 已有未决输入；When 两个新输入并发到达 | 输入先持久化并按 Session 串行处理，后续输入不得越过前序输入。 |
| AGT-002 | Given Runner 已冻结模型或 Tool Receipt；When 投影失败、进程重启或 Resume | 重放冻结结果，不重新调用模型、Graph Tool 或产生新扣费。 |
| AGT-003 | Given 存在待用户确认 Approval；When 重复批准、拒绝、过期后批准或资源版本变化 | 仅第一个合法决定生效，其余确定性返回原结果、过期或版本冲突。 |
| AGT-004 | Given Graph Tool 已派发异步 Operation；When Agent Run 结束或服务重启 | Operation 继续由持久化状态推进，不依赖原 Graph 调用栈；终态通过可信 Continuation 回流。 |
| WRK-001 | Given 同一 Job 被重复唤醒且多个 Worker 竞争；When Claim 执行 | 只有有效 Lease/Fencing Owner 可以提交状态，Provider 副作用和扣费最多一次。 |
| WRK-002 | Given Provider 请求结果未知；When Worker 恢复 | Job 进入 `reconciling` 并按原幂等键核对，不立即重跑 Provider 或重复扣费。 |
| WRK-003 | Given Redis 不可用；When Job 已到 `available_at` | PostgreSQL 轮询在 30 秒 P95 内恢复 Claim，Redis 恢复后不重复执行。 |
| WRK-004 | Given Worker 正在运行 Job；When 收到退出信号 | 5 秒内停止 Claim，Drain 期间继续 Heartbeat，超时后留下可由新 Lease 接管的权威状态。 |
| NFR-001 | Given 已批准峰值负载；When 以 2 倍峰值执行压测 | 核心延迟、错误率、并发上限和数据库连接池均满足本文件基线，无无界队列或 goroutine。 |
| NFR-002 | Given PostgreSQL 备份和故障场景；When 执行恢复演练 | RPO 不超过 5 分钟、RTO 不超过 30 分钟，账务、Session、Job 和 EventLog 可以一致恢复。 |
