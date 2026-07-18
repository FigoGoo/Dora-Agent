# Workspace、Session Lane 与事件功能现状

> 文档状态：Current Implementation / 只描述当前工作树
>
> 适用 Runtime：`business/cmd/business-service`、`agent/cmd/agent-service`
>
> 更新日期：2026-07-17

## 现状

当前工作台采用“Snapshot 先加载、SSE 再增量”的同源 BFF 模式。浏览器只访问 Business 的公开路径；Business 校验 Cookie Session 和 Project/Agent Session Binding 后，为固定 Agent 路径签发短期一次性身份断言。Agent 从 PostgreSQL 一致性快照读取 Session、Message、Input、最新 Turn Card、四类创作 Preview Card、媒体 Card 和 Event 水位，并从持久 EventLog 按序投影 SSE。

Agent 已实现共享 PostgreSQL Session Lane：用户消息、CreationSpec、素材分析、Storyboard、Prompt、媒体请求和媒体终态都进入同一 Session 的有序 Input。Processor 按真正的 Head-of-Line、Lease 和 Fence 领取，不允许后到输入越过未完成前驱。Redis 或进程内 wake 只缩短延迟，PostgreSQL Scanner 是恢复真源。

当前 Workspace 新响应在媒体 Profile 关闭时使用 `session.workspace.v4`，启用媒体 Card 读取时使用 `session.workspace.v5`。V5 在 V4 基础上增加最多 16 条按 Event Seq 升序排列的 `media_previews`。

## 边界

当前公开读面只有固定 Workspace Snapshot、Event SSE 和 Tool Definition Catalog，不提供通用 Agent 反向代理或任意内部路径透传。

以下能力没有实现或不属于当前接口：

- WebSocket、GraphQL Subscription 或跨 Session 合并事件流；
- 服务端保存浏览器 Reducer/LocalStorage 状态；
- 任意 Cursor 跳读、按 Event 类型筛选或 `Last-Event-ID` Header 恢复；当前只接受规范 `after_seq`；
- 从 SSE 缓存反推业务终态；刷新必须回源 Snapshot 和各 Owner 真值；
- 完整通用 Approval/Checkpoint UI、复杂 Batch 明细或生产媒体 Job 控制台；
- 将 Tool Catalog 当作任意动态代码执行入口。

Workspace 只返回当前代码明确投影的字段，不跨库 JOIN Business Project、素材或 Asset 全量详情。

## 流程

### Snapshot 与 SSE 恢复

1. 浏览器先请求 Project Bootstrap，获得已确认的 Agent `session_id`。
2. 浏览器请求同源 Workspace 路径；Business 校验 Cookie Session，并用 ready Binding 复核该 Session 属于当前用户的 Project。
3. Business 为固定 method、canonical target、scope、user/project/session/request/nonce/expiry 签发内部断言并单次代理 Agent。
4. Agent 在 `READ ONLY, REPEATABLE READ` 一致性事务中读取 Snapshot 与 Event 高水位；事务后认证解密 Message。任一解密或投影校验失败时丢弃整个 Snapshot。
5. 浏览器以 `event_high_watermark` 作为 `after_seq` 建立 SSE。Agent 先从 PostgreSQL 补读到当前高水位，再发送 `stream.ready`。
6. 连接期间 Agent 周期轮询 EventLog 并发送连续事件，同时发送 heartbeat。断言到期、连接时长到期或客户端取消都会结束连接。
7. Cursor 过期、Event Gap 或投影非法时，Agent 发送无 `id` 的 `stream.reset` 后关闭；客户端必须重新加载 Snapshot，不能自行跳过事件。

### Session Lane

1. 各写入口先用稳定请求/命令键 AppendOnce 创建 Input 和对应上下文。
2. Coordinator 从 PostgreSQL 读取全局真正 HOL，只允许一个 Handler 领取该 Session 的最早可处理 Input。
3. Claim 写入 Lease/Fence；运行中按预算续租。确定性失败、可重试失败和外部结果未知分别进入不同状态。
4. Tool/Model/外部命令先持久化 Intent/Receipt，再执行可能有副作用的调用；响应未知时保持 `recovery_pending` 并用原键查询。
5. 终态事务写 Input、Run/Receipt、Projection 和 Event；提交后 wake 失败不会回滚。

### 媒体终态回桥

Worker 通过 Agent 发布的 PostgreSQL Function 提交当前 Fence 的媒体终态。Agent 在同一事务更新 Job/Batch/Operation 并写 Terminal Outbox；Bridge 再以 Event ID AppendOnce 写 `media_job_preview_terminal` Input。Terminal Processor 只读取冻结 Result 并确定性生成 Card/Event，不恢复原媒体 Graph，也不调用模型。

## 接口与状态

### 同源公开接口

| 方法与路径 | 当前行为 | 认证要求 |
| --- | --- | --- |
| `GET /api/v1/agent/sessions/:session_id/workspace` | 返回一致性 Workspace Snapshot；拒绝 Query | Business Session + Session Binding |
| `GET /api/v1/agent/sessions/:session_id/events?after_seq=N` | 按 Cursor 补读并持续输出 SSE | Business Session + Session Binding + 流并发预算 |
| `GET /api/v1/agent/sessions/:session_id/tools` | 返回固定六项 Tool Definition Catalog 投影 | Business Session + Session Binding |

当前写入口包括：

| 方法与路径 | Input Source |
| --- | --- |
| `POST /api/v1/agent/sessions/:session_id/creation-spec-previews` | `creation_spec_preview` |
| `POST /api/v1/agent/sessions/:session_id/analyze-materials-previews` | `analyze_materials_preview` |
| `POST /api/v1/agent/sessions/:session_id/plan-storyboard-previews` | `plan_storyboard_preview` |
| `POST /api/v1/agent/sessions/:session_id/write-prompts-previews` | `write_prompts_preview` |
| `POST /api/v1/agent/sessions/:session_id/generate-media-previews` | `generate_media_preview_request` |
| `POST /api/v1/agent/sessions/:session_id/assemble-output-previews` | `assemble_output_preview_request` |

这些写接口均要求 Session + CSRF，并受对应 Runtime Feature Flag 保护。当前 `user_message` Input 来自 Project Quick Create 的首提示词和受控的旧输入升级路径；代码没有注册“向现有 Session 继续发送普通用户消息”的公开 POST。Workspace 会统一读取已经存在的 Message、Input 和最新 Turn Card。

### Workspace 主要字段

`session.workspace.v4/v5` 当前包含：Session、按序 Messages、按 enqueue_seq 排序的 Inputs、nullable `creation_spec_preview`、`latest_turn_output`、`analyze_materials_preview`、`plan_storyboard_preview`、`write_prompts_preview`、Event 高水位和最小可重放序号；V5 额外包含 `media_previews`。

### 当前状态与事件

| 对象 | 当前值 |
| --- | --- |
| Agent Session | `active`、`archived` |
| Session Input | `pending`、`claimed`、`running`、`retry_wait`、`recovery_pending`、`resolved`、`dead` |
| Input Source | `user_message`、`creation_spec_preview`、`analyze_materials_preview`、`plan_storyboard_preview`、`write_prompts_preview`、`generate_media_preview_request`、`assemble_output_preview_request`、`media_job_preview_terminal` |
| SSE 控制事件 | `stream.ready`、`stream.reset`、heartbeat comment |

持久业务事件当前覆盖 `session.created`、`session.input.accepted`、CreationSpec completed/failed、用户 Turn completed/failed/recovery_pending、Analyze Materials accepted/completed/partial/failed/runtime_failed、Storyboard accepted/completed/failed/runtime_failed、Prompt accepted/completed/failed/runtime_failed、Media accepted/completed/failed/runtime_failed。

## 数据 Owner

| 数据 | Owner | 当前规则 |
| --- | --- | --- |
| Project 与 Agent Session Binding | Business PostgreSQL | BFF 每次代理前执行资源级授权 |
| Session、Message、Input、Lease/Fence、Run/Receipt | Agent PostgreSQL | Session Lane 唯一处理真源 |
| Workspace Projection、EventLog、Event Seq | Agent PostgreSQL | Snapshot 与 SSE 共用持久事实 |
| 创作草稿、素材、Asset | Business PostgreSQL | Workspace 只保存安全引用或 Card，不成为这些对象的 Owner |
| Worker Attempt/Artifact/Finalize Observation | Worker PostgreSQL | 不直接进入 Workspace；终态经 Agent 契约回桥 |
| wake | 进程内信号或 Redis 非权威通知 | 丢失后由 PostgreSQL Scanner 恢复 |

前端 Reducer 和浏览器内存不是权威 Owner；刷新或 Reset 必须重新读取 Snapshot。

## 错误与安全

- Business BFF 只代理 allowlist 路径，禁用环境代理、Redirect、自动业务重试和无界响应读取。
- 内部身份断言绑定 canonical target 和 scope，带短有效期与一次性 Nonce；Agent 校验 method、target、session、user、project 和请求标识。
- Workspace 无 Query；SSE 只接受唯一 `after_seq`，拒绝重复 Query、负数、超前/溢出 Cursor 和 `Last-Event-ID`。
- SSE 有用户/Session 连接数预算、批量上限、轮询周期、heartbeat、单帧写 Deadline 和最大连接时长。
- Snapshot 在一致性事务中读取；Message 逐条做 AEAD 和 Digest 校验，任一失败返回整体不可用，不跳过或返回占位正文。
- Event 必须从 `after_seq+1` 连续递增；Gap、过期 Cursor、未知 Schema 或非法 Payload 触发 `stream.reset`，不会推进客户端 Cursor。
- Workspace 隐藏 UserID、Lease Owner、Fence、Attempt、密文、内部 Source Payload、SQL 和堆栈。
- Session 不存在与不属于当前用户统一为 404；内部身份无效和依赖不可用使用不同稳定错误码。

## 测试

当前仓库包含：

- Business Agent Proxy 的固定路径、身份断言、SSE 逐帧代理、响应上限、Redirect 和错误透传测试；
- Agent Workspace Snapshot、Message 解密、Input exact-set、Event 连续性、Cursor Reset、SSE Writer 和连接预算测试；
- Session Lane、HOL、Lease/Fence、Retry/Recovery、各 Preview Processor 和 Terminal Bridge 的单元/集成测试；
- 前端 Snapshot Reducer、SSE Event Reducer、Reset/重连和 Schema fail-closed 测试；
- `make w05-smoke` / `make w05-browser-smoke`：Snapshot、SSE 和同源 BFF；
- `make user-message-runtime-smoke`、`make analyze-materials-runtime-smoke`、`make plan-storyboard-runtime-smoke`、`make write-prompts-runtime-smoke`：各独立 Runtime；
- `make trial-basic`：六工具、媒体终态、硬刷新恢复和 Range 播放的一条本地 MVP 链路。

这些入口是当前验证面；某次提交是否通过仍以该提交上的实际执行结果为准。

## 生产差距

当前 Event 传输采用 PostgreSQL poll-first SSE，没有专用消息总线、跨区域 Fan-out、长期归档查询或运营重放控制台。Redis 仅可作为 wake，不能提供可靠性或顺序。

当前内部身份与 Preview Runtime 主要面向本地/受控环境，生产部署仍需要 TLS、服务身份、Nonce 存储容量治理、Gateway 限流和完整可观测性。Workspace 只提供安全聚合 Card，没有通用 Approval UI、复杂 Batch/Job 明细、跨 Session 搜索或长期事件审计产品。Tool Catalog 与代码内可执行 Registry 仍是分离边界，不能从 Skill 文本或前端状态动态执行任意 Tool。
