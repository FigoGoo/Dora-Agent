# Agent 服务端能力测试用例

状态：active  
owner：测试与验收责任域
更新时间：2026-06-28  
适用范围：智能体微服务 Agent API、TurnLoop、AG-UI 事件生产、SSE 补偿、Agent DB、RPC client、Skill 测试、模型 Tool 和跨服务主链路  
相关代码路径：`tests/agent/**`、`tests/contract/**`、`tests/e2e/service/**`、`api/openapi/agent-workbench.yaml`、`api/agui/**`、`services/agent/**`、`db/migrations/iterations/**/agent/**`  
相关契约：`docs/current/README.md`、`docs/contracts/api/Agent工作台API契约草案.md`、`docs/contracts/ag-ui/统一Agent工作台AGUI事件协议草案.md`、`docs/contracts/data/Agent领域数据模型草案.md`、`docs/standards/AG-UI事件规范.md`、`docs/standards/TurnLoop执行规范.md`、`docs/standards/Agent领域数据建模规范.md`

## 测试边界

本文件只覆盖 Agent 服务端能力。前端渲染、A2UI 组件、页面布局和浏览器操作不在范围内；AG-UI 测试只验证事件 schema、payload、顺序、重放、未知事件兼容和敏感信息边界。业务事实的最终语义由业务微服务测试验证，Agent 测试只断言 RPC client 调用、错误映射和 Agent Runtime 数据。

## 测试用例

### Agent API

| ID | 功能点 | 测试入口 | 前置数据 | 服务端断言 | 证据 |
| --- | --- | --- | --- | --- | --- |
| AG-API-001 | 创建 session 成功 | `POST /api/agent/sessions` | active project，业务 RPC `CheckProjectAccess(continue_creation)=allowed` | 先调用项目权限 RPC；创建 `agent_sessions(status=active)`，只保存 `project_id` 引用；响应 `session_id/project_id/status` | Agent API contract、Agent DB、RPC mock |
| AG-API-002 | 归档项目创建 session 拒绝 | `POST /api/agent/sessions` | `CheckProjectAccess` 返回 `PROJECT_ARCHIVED` | 返回 HTTP 409，错误码 `PROJECT_ARCHIVED`；不写 session，不启动 SSE | API contract、Agent DB |
| AG-API-003 | 查询 session 快照 | `GET /api/agent/sessions/:session_id` | active/archived 项目各一套 | view 权限通过时返回 session、latest run、snapshot 摘要；archived 项目可只读；不返回业务项目标题事实作为 Agent 真相 | API contract、Agent DB、RPC mock |
| AG-API-004 | 创建 run 成功 | `POST /api/agent/runs` | active session、active project、模型/素材输入合法 | 校验 Authorization token、session/project 匹配、项目 continue_creation、referenced assets；写 `agent_runs(pending/running)` 和 user message；响应 `run_id/stream_url/status` | API contract、Agent DB、RPC mock |
| AG-API-005 | 同 session 并发 run 拒绝 | `POST /api/agent/runs` | session 已有 running/waiting_confirmation run | 返回 `RUN_STATE_CONFLICT`；不创建第二个 active run | API contract、Agent DB |
| AG-API-006 | 引用资产权限拒绝 | `POST /api/agent/runs` | `referenced_asset_ids` 含跨空间资产 | 调用 `BatchCheckAssetAccess`，有 denied 时返回 403 或逐项拒绝摘要；不启动 TurnLoop | API contract、RPC mock |
| AG-API-007 | 创建 run 归档项目拒绝 | `POST /api/agent/runs` | project archived | 返回 HTTP 409 `PROJECT_ARCHIVED`；不创建 run、不冻结积分、不启动 SSE | API contract、Agent DB、RPC mock |
| AG-API-008 | SSE 实时事件鉴权 | `GET /api/agent/runs/:run_id/stream` | run 属于当前用户 | token、项目 view 权限通过后建立 SSE；无权限返回 401/403；heartbeat 按配置输出 | API/SSE test |
| AG-API-009 | SSE Last-Event-ID 重连 | `GET /api/agent/runs/:run_id/stream` | event store 有 sequence 1..N | 带 `Last-Event-ID` 时从下一事件续传；重复 event_id 不重复推送语义 | SSE replay test、Agent DB |
| AG-API-010 | event replay 查询 | `GET /api/agent/runs/:run_id/events` | after_sequence 指定 | 返回连续事件，默认分页 10，最大 100；缺口或超窗口时提示 snapshot fallback | API contract、Agent DB |
| AG-API-011 | 确认中断 accept | `POST /api/agent/runs/:run_id/interrupts/:interrupt_id/accept` | run waiting_confirmation，interrupt required | 校验权限、项目仍 active、幂等键；interrupt -> accepted，run -> resuming/running；输出 `confirmation.accepted` 或 `resume.accepted`；不重复冻结 | API contract、Agent DB、AG-UI fixture |
| AG-API-012 | 确认重复提交 | confirm API | 同一幂等键重复 | 返回同一确认结果；不重复调用 `FreezeCredits` | API contract、RPC call count |
| AG-API-013 | 确认过期 | confirm API | interrupt expired | 返回 `INTERRUPT_EXPIRED` 或 `RUN_STATE_CONFLICT`；run 进入 failed/cancelled 规则一致 | API contract、Agent DB |
| AG-API-014 | 拒绝中断 | `POST /api/agent/runs/:run_id/interrupts/:interrupt_id/reject` | confirmation required | interrupt -> rejected，run -> cancelled；输出 `confirmation.rejected`、`agent.run.cancelled`；如有冻结则释放 | API contract、Agent DB、AG-UI |
| AG-API-015 | 取消 run | `POST /api/agent/runs/:run_id/cancel` | running task | 停止新 Tool，正在运行 task 进入 cancel_requested/cancelled；已完成资产按规则保留，未完成释放冻结；run terminal | API contract、Agent DB、RPC mock |
| AG-API-016 | snapshot 查询 | `GET /api/agent/runs/:run_id/snapshot` | completed/running/failed/archived 项目 | 返回消息、任务、资产引用、黑板和 last_event_sequence；archived 项目 readonly_reason；不返回长期 TOS URL | API contract、Agent DB、脱敏 |
| AG-API-017 | API route parity | OpenAPI vs handler | `api/openapi/agent-workbench.yaml` | 所有设计路由存在 handler、鉴权、错误映射、幂等头要求；缺路由阻断联调 | route parity test |

### TurnLoop 和运行状态机

| ID | 场景 | 前置数据 | 服务端断言 | 证据 |
| --- | --- | --- | --- | --- |
| AG-TURN-001 | 正常文本对话 | 无匹配 Skill，文本兜底可用 | `agent.skill.missing` 后不调用未授权 Tool；message delta/completed 完整；run completed | TurnLoop test、AG-UI fixture、Agent DB |
| AG-TURN-002 | Skill 命中 | `ListRoutableSkills` 返回 Published Skill | 保存 `agent_runs.skill_selection`，输出 `agent.skill.selected`，按 `GetPublishedSkillSpec` 执行 | TurnLoop、RPC mock |
| AG-TURN-003 | Draft/Deprecated Skill 不参与路由 | RPC fixture 含多状态 Skill | Agent 只对 Published 路由；无匹配时兜底 | TurnLoop、RPC contract |
| AG-TURN-004 | Skill confirmation_policy 加强确认 | Published spec 要求确认 | 生成 `confirmation.required`；不能绕过 credit_freeze/high_risk/business_write 平台强制确认 | TurnLoop、Agent DB |
| AG-TURN-005 | 高风险 Tool 确认 | `CheckToolExecutionPolicy.requires_confirmation=true` | run -> waiting_confirmation，写 interrupt；确认前不执行 Tool | TurnLoop、RPC call count |
| AG-TURN-006 | Tool disabled | policy RPC 返回 denied | 不执行供应商请求；输出 tool/agent failed 可理解错误；run failed 或请求改写 | TurnLoop、RPC mock |
| AG-TURN-007 | 模型选择默认解析 | user 未选模型 | 调 `ResolveDefaultModel` 和 `ResolveGenerationModelSnapshot`；保存非敏感模型快照 | TurnLoop、RPC mock、Agent DB |
| AG-TURN-008 | 模型确认后锁定 | confirmation required 后用户改模型 | 修改请求被拒绝或要求新 run；输出 `chat.controls.locked` | API/TurnLoop |
| AG-TURN-009 | 确认后模型停用 | 执行前 snapshot RPC 返回 `RESOURCE_UNAVAILABLE` | Agent 释放冻结，输出用户可见失败，run failed；不调用供应商 | TurnLoop、RPC mock |
| AG-TURN-010 | 安全评估通过 | safety evaluator 返回 passed | 保存 `agent_safety_evaluations` 脱敏证据；先输出 `safety.prompt.evaluated`，再预估积分 | TurnLoop、Agent DB、AG-UI |
| AG-TURN-011 | 安全阻断 | evaluator 返回 blocked | 输出 `safety.prompt.blocked` 和 `agent.run.failed`；不调用 Estimate/Freeze/Tool | TurnLoop、RPC call count |
| AG-TURN-012 | 安全评估失败或超时 | evaluator error/timeout | 输出 `safety.prompt.failed` 或 blocked 口径；不预估、不冻结；trace 可查 | TurnLoop、日志 |
| AG-TURN-013 | 积分不足 | Estimate 返回 insufficient | 输出 `credits.estimated` 或 `credits.insufficient`、`agent.run.failed`；不创建 interrupt，不冻结，不执行 Tool | TurnLoop、RPC call count |
| AG-TURN-014 | 确认后冻结成功 | accepted 后 Freeze 成功 | 输出 `credits.frozen`；保存 freeze_id；继续 Tool | TurnLoop、Agent DB、AG-UI |
| AG-TURN-015 | 冻结失败 | Freeze 返回业务错误 | run failed；不执行 Tool；错误事件含 support_trace_id | TurnLoop、AG-UI |
| AG-TURN-016 | 重复确认只冻结一次 | confirm 重复 | Freeze RPC call count=1 或幂等重放；Agent DB 不重复 interrupt resolved 事件 | API/TurnLoop |
| AG-TURN-017 | Tool 失败释放 | Tool adapter failed | 调 `ReleaseFrozenCredits`，输出 `tool.call.failed`、`credits.released`、`agent.run.failed` | TurnLoop、RPC mock、AG-UI |
| AG-TURN-018 | 用户取消释放 | cancel API | 停止新 Tool；未完成 line item 释放；run cancelled；task terminal | TurnLoop、Agent DB |
| AG-TURN-019 | 部分完成 | Tool 返回部分 artifact | 完成 artifact 进入保存/扣费，未完成释放；输出 partial progress 和 charged/released | TurnLoop、RPC mock |
| AG-TURN-020 | 保存资产成功扣费 | prepare slots -> upload -> commit success | `asset.save.completed` 早于 `credits.charged`；Agent DB 只保存 `asset_ref`，业务资产事实以 RPC 响应为准 | TurnLoop、Agent DB、RPC mock |
| AG-TURN-021 | 保存失败释放 | Commit 返回失败或对象上传失败 | 不发送 `credits.charged`；调用 Release；输出 `asset.save.failed` 和 failed/cancelled 终态 | TurnLoop、RPC mock |
| AG-TURN-022 | commit 前项目归档 | 二次 CheckProjectAccess 返回 archived | 停止新 Tool，释放未结算冻结，输出 `project.archived.blocked`、`credits.released`、`agent.run.cancelled` | TurnLoop、AG-UI |
| AG-TURN-023 | 追加输入恢复 | run waiting input 或 resume | 追加 message 持久化，重新 safety evaluation，继续 TurnLoop；状态闭合 | API/TurnLoop、Agent DB |
| AG-TURN-024 | resume 前权限失效 | 成员 removed 或项目 archived | 返回权限/归档错误；不恢复执行；必要时释放冻结 | API/TurnLoop、RPC mock |

### AG-UI 事件和 SSE 补偿

| ID | 功能点 | 测试入口 | 服务端断言 | 证据 |
| --- | --- | --- | --- | --- |
| AG-AGUI-001 | 公共事件结构 | schema 校验 | 每个事件含 `event_id/type/session_id/run_id/project_id/space_id/actor_user_id/sequence/timestamp/trace_id/payload`，`additionalProperties=false` | JSON schema test |
| AG-AGUI-002 | canonical type | AG-UI fixture | 新实现只生产 `agent.run.*`、`agent.message.*`、`confirmation.*` 等 canonical `type`；alias 仅作为消费兼容测试 | schema/replay |
| AG-AGUI-003 | sequence 连续 | 单 run 事件数组 | 同一 run sequence 从 1 单调递增；缺口触发补偿，不继续合并 | replay test、Agent DB |
| AG-AGUI-004 | event_id 幂等 | 重复事件重放 | 相同 event_id 语义不变；replay 不生成新 event_id | replay test |
| AG-AGUI-005 | unknown event 兼容 | fixture 注入未知 type | replay/consumer 测试记录 debug，不改变状态，不崩溃 | replay test |
| AG-AGUI-006 | 敏感信息边界 | 全事件 payload scan | 不出现系统 Prompt、完整组装 Prompt、供应商原始响应、API Key、内部成本、TOS 签名 URL、完整用户隐私原文 | schema + payload scan |
| AG-AGUI-007 | 正常生成事件序列 | TurnLoop success fixture | safety -> estimated -> confirmation -> frozen -> tool -> generation -> asset.save -> workspace -> charged -> snapshot -> completed 顺序完整 | replay fixture |
| AG-AGUI-008 | 安全阻断序列 | safety blocked fixture | `safety.prompt.blocked` 后无 `credits.estimated/frozen/tool.call.started`，最终 `agent.run.failed` | replay fixture |
| AG-AGUI-009 | 积分不足序列 | insufficient fixture | 预估后不确认、不冻结、不执行 Tool；错误 payload 用户可理解 | replay fixture |
| AG-AGUI-010 | 拒绝确认序列 | confirmation rejected fixture | `confirmation.rejected` -> `agent.run.cancelled`；如未冻结不释放，如已冻结按状态释放 | replay fixture |
| AG-AGUI-011 | 保存失败序列 | asset save failed fixture | `asset.save.failed` -> `credits.released` -> `agent.run.failed`；错误含 retryable/support_trace_id | replay fixture |
| AG-AGUI-012 | 项目归档序列 | project archived fixture | `project.archived.blocked`、`credits.released`、`agent.run.cancelled`、`process.snapshot.saved` 顺序符合契约 | replay fixture |
| AG-AGUI-013 | Last-Event-ID 补偿 | SSE + replay API | 根据 event_id 找到 sequence 后续传；找不到则按 snapshot fallback | SSE test |
| AG-AGUI-014 | after_sequence 补偿 | `/runs/:run_id/events` API | `after_sequence=N` 返回 N+1 起连续事件，分页默认 10，最大 100 | API contract |
| AG-AGUI-015 | snapshot fallback | event 缺口超窗口 | 返回 `snapshot_id/snapshot_version/last_event_sequence` 和不可补偿标识；snapshot 可恢复 | API/Agent DB |
| AG-AGUI-016 | 逐事件 payload 必填 | 所有事件类型 fixture | `agent.run.*`、thinking、message、skill、controls、safety、credits、confirmation、tool、generation、asset、workspace、snapshot、project archived、failed payload 必填字段齐全 | schema test |

### Agent 领域数据库

| ID | 功能点 | 测试入口 | 服务端断言 | 证据 |
| --- | --- | --- | --- | --- |
| AG-DB-001 | Agent migration 禁止外键 | SQL lint | agent migration 不出现 `FOREIGN KEY`、`REFERENCES`；session_id/run_id/project_id 仅普通字段和索引 | SQL lint |
| AG-DB-002 | Agent 表清单 | schema scan | 存在 sessions/runs/messages/events/tool_calls/tasks/interrupts/artifacts/safety_evaluations/memories/runtime_configs | schema test |
| AG-DB-003 | Agent DB 不保存业务事实 | schema/payload scan | 不保存积分余额、积分流水、最终资产事实、作品公开状态、企业成员、业务权限主数据 | schema lint |
| AG-DB-004 | session 状态机 | repository/domain test | active -> archived/expired，archived -> active 需权限校验；非法跳转拒绝 | unit/integration |
| AG-DB-005 | run 状态机 | repository/domain test | pending -> running -> waiting_confirmation/resuming/completed/failed/cancelled；非法跳转拒绝 | unit/integration |
| AG-DB-006 | task 状态机 | repository/domain test | pending/running/cancel_requested/partial/completed/failed/timeout/cancelled 流转合法 | unit/integration |
| AG-DB-007 | interrupt 状态机 | repository/domain test | required -> accepted/rejected/expired -> resolved；重复 accept/reject 幂等或冲突明确 | unit/integration |
| AG-DB-008 | event 幂等写 | event repository | 同 event_id 重复写不重复；同 run sequence 唯一且连续策略可检查 | repository test |
| AG-DB-009 | 消息分页恢复 | message repository | `session_id` 按 created_at 分页，默认 10；不保存系统 Prompt | repository + payload scan |
| AG-DB-010 | 事件补偿查询 | event repository | `run_id, sequence` 索引查询，after_sequence 分页；避免逐条关联查询 | repository/query count |
| AG-DB-011 | Tool 调用记录 | tool_call repository | 保存公开摘要、status、耗时、错误码；不保存完整原始参数或供应商响应 | repository、脱敏 |
| AG-DB-012 | artifact/asset ref | artifact repository | 草稿 artifact 可保存元素摘要；最终资产只保存 `business_ref_id=asset_id` 和脱敏展示摘要 | repository、边界 |
| AG-DB-013 | safety evaluation | safety repository | 保存 evidence id、scene、result、digest、policy/evidence version、expiry、trace；不保存策略细节、评分、完整 prompt | repository、脱敏 |
| AG-DB-014 | runtime config 快照 | runtime config repository | 配置版本可查询、可审计、可回放；run 使用配置 snapshot | repository |
| AG-DB-015 | migration up/down | local DB | agent migration up/down 可执行，索引存在 | migration test |

### RPC client 和业务 DTO 映射

| ID | 功能点 | 测试入口 | 服务端断言 | 证据 |
| --- | --- | --- | --- | --- |
| AG-RPC-001 | Authorization token 解析 | BusinessGateway | Agent API 不信任前端 user/space，先调业务 token/space 解析；token 失败终止 | RPC mock、API test |
| AG-RPC-002 | Auth/RequestMeta mapper | rpc mapper unit | `source=agent_service`、trace_id、idempotency_key、space/user 正确映射；不把 X-Client-Request-ID 写入 RequestMeta | unit |
| AG-RPC-003 | 错误映射 | error mapper | 业务错误映射到 Agent domain error 和 AG-UI user_message；系统错保留 support_trace_id | unit/contract |
| AG-RPC-004 | 超时映射 | rpc mock timeout | 业务 timeout 返回可重试标记，TurnLoop 按策略处理，不无限等待 | RPC mock |
| AG-RPC-005 | `NOT_IMPLEMENTED` 扫描 | 全量 Agent-facing RPC | 当前服务范围内的 RPC client/server fixture 不返回 `NOT_IMPLEMENTED` | RPC smoke |
| AG-RPC-006 | Skill DTO 映射 | `ListRoutableSkills/GetPublishedSkillSpec` | memory_policy、confirmation_policy、output_element_schema、tool_bindings 原样映射；不丢字段 | RPC contract |
| AG-RPC-007 | Tool policy DTO 映射 | `CheckToolExecutionPolicy` | risk_level、requires_confirmation、charge_mode、timeout_ms、retry/cancel policy 正确进入 TurnLoop | RPC contract |
| AG-RPC-008 | Model snapshot DTO 映射 | `ResolveGenerationModelSnapshot` | provider_ref 非敏感，pricing_snapshot_id 必须匹配；不读取 API Key | RPC contract、脱敏 |
| AG-RPC-009 | Credit DTO 映射 | Estimate/Freeze/Charge/Release | `credit_account_scope`、estimate/line item/freeze/charged/released 字段完整；Agent 不自行计算单价 | RPC contract |
| AG-RPC-010 | Asset DTO 映射 | BatchCheck/Prepare/Commit | object key 只接受业务签发，asset refs 写回 artifact；跨空间 denied 被阻断 | RPC contract |
| AG-RPC-011 | Dictionary DTO 映射 | ListAssetElementTypes | active element type、draft/final/render hint、schema_version 正确进入输出校验 | RPC contract |
| AG-RPC-012 | 依赖方向 | static import check | runtime/domain 不直接依赖 infra GORM/业务 DB；TurnLoop 通过 gateway 接口调用业务 RPC | static test |

### Skill 测试运行和输出元素校验

| ID | 场景 | 前置数据 | 服务端断言 | 证据 |
| --- | --- | --- | --- | --- |
| AG-SKILLTEST-001 | 创建独立测试 run | 业务 `RunSkillTest` 返回 test_run_id | 使用独立 `test_run_id`，不写正式用户 session，不扣用户积分 | Agent test、Agent DB |
| AG-SKILLTEST-002 | 获取待测 spec | `GetReviewCandidateSkillSpec` 成功 | 只用于测试候选版本；不进入线上路由；spec 脱敏 | RPC mock |
| AG-SKILLTEST-003 | 少于 3 个样例 | 业务或 Agent 校验失败 | 状态 `rejected`，不执行 Tool；可回传 SaveSkillTestResult | Agent test |
| AG-SKILLTEST-004 | 安全阻断 | skill_test safety blocked | 状态 `blocked`，保存 safety_evidence_id，actual_elements 不作为 passed | Agent + RPC |
| AG-SKILLTEST-005 | 输出缺必填元素 | output_element_schema required | 状态 `failed`，`missing_required[]` 非空；不发布 passed 结果 | Agent test |
| AG-SKILLTEST-006 | 非法元素类型 | 元素类型不在字典 active | 状态 `failed`，`invalid_types[]` 非空 | Agent test |
| AG-SKILLTEST-007 | 阶段不匹配 | draft/final enabled 不匹配 | `draft_enabled=false` 不能作过程态，`final_enabled=false` 不能作最终资产元素 | Agent test |
| AG-SKILLTEST-008 | 高风险/业务写入 Tool 隔离 | 测试 spec 绑定高风险或业务写入 Tool | 测试模式 preview 或隔离 adapter；不得改变业务事实；高风险不可测时稳定失败 | Agent test、业务 DB |
| AG-SKILLTEST-009 | 测试通过回传 | 所有元素合法、安全 passed | 调 `SaveSkillTestResult(status=passed)`，safety_evidence_json 合法且 scene=skill_test | RPC contract |
| AG-SKILLTEST-010 | 重复回传 | 同 test_run_id 幂等键 | 相同 hash 重放，不重复保存；不同 hash 返回 `IDEMPOTENCY_CONFLICT` | RPC contract |

### 模型 Tool 适配器和任务

| ID | 场景 | 前置数据 | 服务端断言 | 证据 |
| --- | --- | --- | --- | --- |
| AG-TOOL-001 | 同步成功 | mock provider sync success | 转换为 completed task 和 artifact；输出 tool/generation/artifact 事件 | adapter test、AG-UI |
| AG-TOOL-002 | 异步成功 | submit/poll mock | task created -> submitted -> running -> completed；轮询 obey timeout/retry | adapter test、Agent DB |
| AG-TOOL-003 | 用户取消异步任务 | cancel API | task -> cancel_requested/cancelled；不再提交新请求；未完成释放积分 | adapter/TurnLoop |
| AG-TOOL-004 | 供应商超时 | mock timeout | task timeout，错误 user_message 可见，未完成释放 | adapter/TurnLoop |
| AG-TOOL-005 | 部分完成 | provider partial | completed artifact 保存并扣费，未完成释放；task partial/completed 状态合法 | adapter/TurnLoop |
| AG-TOOL-006 | 供应商鉴权错误 | provider auth failure | 映射 `PROVIDER_CONFIG_MISSING` 或 `provider_auth_error`，不重试，日志不含密钥 | adapter、日志 |
| AG-TOOL-007 | 限流重试 | provider rate limit | 按 retry_policy 退避，超过次数 failed；trace 贯通 | adapter test |
| AG-TOOL-008 | 参数非法 | provider invalid argument | 不冻结或释放冻结，返回用户可修改参数错误 | adapter/TurnLoop |
| AG-TOOL-009 | 供应商产物落 TOS | provider URL/base64/stream | 先 `PrepareGeneratedAssetObjects`，上传到业务 object key，再 commit；不把 provider 临时 URL 当业务资产 URL | adapter、RPC mock |
| AG-TOOL-010 | TOS 上传失败 | TOS mock failure | 不调用 CommitGeneratedAssetAndCharge；释放冻结；事件 `asset.save.failed` | adapter/TurnLoop |
| AG-TOOL-011 | 日志脱敏 | 所有 adapter | 日志不含原始请求、原始响应、API Key、完整 Prompt、签名 URL | log scan |

### 黑板、快照和 Memory

| ID | 场景 | 前置数据 | 服务端断言 | 证据 |
| --- | --- | --- | --- | --- |
| AG-SNAPSHOT-001 | 正常快照保存 | running/completed run | snapshot 包含消息摘要、任务、黑板、asset refs、last_event_sequence；输出 `process.snapshot.saved` | Agent DB、AG-UI |
| AG-SNAPSHOT-002 | Last-Event-ID 缺口恢复 | event replay 不可补偿 | snapshot 可恢复到最新可展示状态；返回 schema_version | API/Agent DB |
| AG-SNAPSHOT-003 | 资产权限失效 | asset ref 后成员 removed | snapshot 不返回预览 URL，保留 hidden/permission summary | API/RPC mock |
| AG-SNAPSHOT-004 | 项目归档只读 | archived project | snapshot 可读，继续创作 API 阻断；readonly_reason 正确 | API/Agent DB |
| AG-BLACKBOARD-001 | 黑板更新 | Skill 输出 draft elements | 保存 element/storyline 摘要，输出 `workspace.blackboard.updated`；不把业务资产事实写入 Agent DB | Agent DB、AG-UI |
| AG-BLACKBOARD-002 | workspace assets 更新 | asset refs 变化 | 输出 `workspace.assets.updated`，只含业务 asset_id 引用和脱敏摘要，不含长期 URL | AG-UI、payload scan |
| AG-MEM-001 | session summary Memory | memory_policy 默认开启 | 只保存 session summary，不含业务事实和敏感原文 | Agent DB、脱敏 |
| AG-MEM-002 | user/space preference 授权有效 | memory_policy 含 user/space scope | 有授权才写摘要偏好；无授权降级且不报错 | Agent DB、authorization mock |
| AG-MEM-003 | Memory 撤销 | 用户撤销授权 | 新 run 不检索旧偏好，不写 user/space preference；历史保留按策略处理 | Agent DB |
| AG-MEM-004 | snapshot schema 升级 | 旧 snapshot | 按 schema_version 兼容读取，缺省字段有安全默认值 | repository test |

### 可观测性和质量门禁

| ID | 功能点 | 测试入口 | 服务端断言 | 证据 |
| --- | --- | --- | --- | --- |
| AG-OBS-001 | trace 贯通 | API -> TurnLoop -> RPC -> Event -> DB | 同一 trace_id 出现在 API log、RPC meta、Agent DB、AG-UI event、错误 support_trace_id | integration、log |
| AG-OBS-002 | 日志字段 | 运行主链路 | 日志含 service/env/tenant/user/space/project/session/run/tool/task/interrupt/event/rpc/error/latency，敏感字段脱敏 | log scan |
| AG-OBS-003 | 错误分类 | user/permission/safety/credit/tool/asset/system | domain error 分类稳定，AG-UI failed payload 含 error_code/user_message/retryable/support_trace_id | unit + AG-UI |
| AG-OBS-004 | provider config missing | 外部模型未配置 | 返回明确配置错误，不使用隔离测试 adapter 冒充生产真实调用；测试报告标注 | adapter test |
| AG-OBS-005 | AG-UI schema/fixture 门禁 | schema + fixture | 新增事件必须同步 schema、最小 fixture、publisher 单测；缺一阻断 | fixture lint |
| AG-OBS-006 | Agent DB migration 门禁 | migration + model | 新字段先 migration/model/repository，再使用；up/down 可执行 | migration test |
| AG-OBS-007 | RPC fixture 完整性 | business RPC fixtures | 当前服务范围所需 fixture 覆盖正常、权限、业务错误、幂等冲突、超时 | fixture lint |
| AG-OBS-008 | 报告真实性 | 测试报告 | 未执行项不得写通过；mock-only、配置缺失、外部依赖未接入必须标明 | report audit |

### 服务级 Agent 主链路

| ID | 主链路 | 测试步骤 | 服务端断言 | 证据 |
| --- | --- | --- | --- | --- |
| AG-E2E-001 | 登录后创建项目并创建 session | 业务登录 -> 项目创建 -> Agent session | 权限上下文正确，session 落 Agent DB，业务 DB 只保存项目事实 | service E2E、DB |
| AG-E2E-002 | 创建 run 并打开 SSE | 创建 run -> SSE -> event replay | run 状态、事件 sequence、Last-Event-ID、snapshot fallback 可验证 | service E2E、AG-UI |
| AG-E2E-003 | 安全评估通过后积分确认 | user input -> safety -> estimate -> confirmation | 安全证据脱敏，预估在安全后，确认 payload 锁定 | service E2E |
| AG-E2E-004 | 用户确认后冻结并生成 | confirm -> freeze -> tool task | 幂等确认，freeze_id 保存，task 状态推进 | service E2E |
| AG-E2E-005 | 生成完成保存资产并扣费 | tool completed -> prepare/upload/commit -> charge | 业务签发 object key，保存成功后扣费，Agent DB 保存 asset_ref | service E2E、业务 DB、Agent DB |
| AG-E2E-006 | 直接平台 Tool 成功扣费 | EstimateTool -> Freeze -> ChargeToolUsage | 独立 Tool 只扣实际完成 item，重复 item 不重复扣 | service E2E |
| AG-E2E-007 | 保存失败或取消 | cancel/save failed | 冻结释放、run terminal、错误 AG-UI、snapshot 保存 | service E2E |
| AG-E2E-008 | 项目运行中归档 | run running -> project archived -> next check | 停新 Tool，释放未结算冻结，`project.archived.blocked` | service E2E |
| AG-E2E-009 | SSE 断线恢复 | 断开 -> Last-Event-ID -> replay/snapshot | 去重、sequence 缺口、snapshot fallback 全覆盖 | service E2E |
| AG-E2E-010 | Skill 发布前测试 | business test run -> Agent sandbox -> SaveSkillTestResult | 3 样例、安全证据、输出元素校验、测试结果回传 | service E2E |
| AG-E2E-011 | 模型供应商错误 | provider auth/rate limit/timeout/partial | 错误分类、重试、部分完成、释放、日志脱敏 | service E2E |

## 风险和待确认

- 真实模型供应商和 TOS 未配置时，必须使用 mock/隔离 adapter 覆盖服务端逻辑，并在报告中说明未覆盖真实外部调用。
- SSE 鉴权、Last-Event-ID、event replay、snapshot fallback 和同 session active run 策略已由 Agent API 与 AG-UI 契约冻结；实现报告需补充执行证据。

## 验收标准

- Agent API、TurnLoop、AG-UI、Agent DB、RPC client、Skill 测试和模型 Tool 均有 contract 或集成用例。
- 所有主链路同时断言 run、task、interrupt、event 和必要 RPC side effect。
- AG-UI fixture 覆盖正常、安全阻断、积分不足、确认拒绝、补充输入恢复、部分完成、保存失败、项目归档、SSE 重连和 unknown event。
- Agent DB 与业务 DB 边界有 schema 和数据断言。
- 本文件不包含页面渲染、浏览器点击或 A2UI 组件视觉测试。
