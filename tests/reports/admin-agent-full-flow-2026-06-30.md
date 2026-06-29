# 后台 Skill / 模型 / Tool 到用户 Agent 链路验收报告

日期：2026-06-30

结论：后端真实消费链路通过，用户前台产品链路未闭环。后台新增系统 Skill、模型供应商、模型管理、Tool 管理后，普通用户通过 Agent API 发起复杂提示词，可以命中本轮新增 Skill，并在 Agent 运行记录中消费到本轮 Tool 策略、默认模型快照和输出元素结构。但当前 `frontend` 仍是首页/登录意图页，不能从前台 UI 完成登录、进入工作台、创建 session/run。

## 本轮复杂场景

- Tool：注册 `storyboard_extract_<suffix>:builtin`，中风险、免确认、超时 `47000ms`、0 积分，用于故事板分镜提取。
- 模型供应商：新增 `full_flow_provider_<suffix>`，`config.timeout_ms=23456`。
- 模型：新增 image 模型 `full-flow-image-<suffix>`，`route_config.e2e_marker=admin_agent_full_flow`。
- 默认模型：临时把 `image/global/active` 默认模型切到本轮新增模型，脚本退出后恢复原默认模型。
- 系统 Skill：新增并发布公开 Skill，Markdown 中引用本轮 Tool，并配置 `image_ref` 输出元素，`use_draft/use_final=true`。
- 用户提示词：普通用户创建 Agent session/run，prompt 包含唯一 hint `agent-full-flow-<suffix>`。

最后一次通过证据：

- `run_id=run_djloavhwao7c`
- `session_id=sess_djloavhgh87c`
- `skill_id=sk_a7e310a5b6fb41f1ba895460ec114eb2`
- `version_id=skv_db450a69946dac602ea583dcdde71dc4`
- `tool_key=storyboard_extract_20260629162757:builtin`
- `model_id=mdl_705b1b53568d7ca4d916bc8d45f090e8`
- `provider_id=mp_d9310ba7f2c4cfe9908496d3f9e1f33f`
- `unique_hint=agent-full-flow-20260629162757`
- `trace_id=e2e-admin-agent-full-20260629162757`

## 交互流程图

```mermaid
flowchart TD
  A["后台 Tool 管理：注册 Tool"] --> B["Business DB: tool_definitions / tool_policies / tool_pricing_policies"]
  C["后台模型供应商：新增供应商"] --> D["Business DB: model_providers"]
  E["后台模型管理：新增 image 模型并设为默认"] --> F["Business DB: models / model_prices / default_models"]
  G["后台系统 Skill：Markdown 引用 Tool + output_elements"] --> H["Business DB: skills / skill_versions / skill_tool_bindings / skill_output_element_schemas"]
  H --> I["发布 Skill: 需要 3 条 active 测试用例"]
  B --> J["用户 Agent run"]
  F --> J
  I --> J
  J --> K["Agent RPC: ListRoutableSkills / GetPublishedSkillSpec"]
  K --> L["Agent 事件: agent.skill.selected"]
  K --> M["Agent RPC: CheckToolExecutionPolicy"]
  M --> N["Agent 事件: tool.call.started / tool.call.completed"]
  J --> O["Agent RPC: ResolveDefaultModel / ResolveGenerationModelSnapshot"]
  O --> P["Agent DB: model_selection_snapshot"]
  K --> Q["Agent DB: skill_selection.output_elements"]
  P --> R["Agent interrupt: credit_generation_confirmation"]
  Q --> R
  S["当前用户前台 homepage"] -. "只能保存 loginIntent" .-> T["登录弹窗"]
  T -. "未接登录/工作台/Agent run" .-> U["断点：前台 UI 无法完成真实用户链路"]
```

## 已验证通过的流转

| 流转 | 验证方式 | 结果 |
| --- | --- | --- |
| Tool 后台注册和策略落库 | `POST /api/admin/tools` + DB 查 `tool_definitions/tool_policies/tool_pricing_policies/tool_policy_change_records` | 通过 |
| Skill Markdown 引用 Tool 后写绑定 | DB 查 `skill_tool_bindings.tool_name/tool_type` | 通过 |
| Skill 输出元素结构落库 | DB 查 `skill_output_element_schemas.image_ref` | 通过 |
| Skill 发布门槛 | 脚本写入 3 条 active `skill_test_cases` 后发布 | 通过 |
| Agent Skill 路由 | 事件 `agent.skill.selected` 命中本轮 `skill_id`，`matched_reason=route_hint:keyword` | 通过 |
| Agent Tool 策略消费 | 事件 `tool.call.started` 包含本轮 Tool、`policy_allowed=true`、`timeout_ms=47000` | 通过 |
| Agent 默认模型消费 | 事件 `generation.progress(model_snapshot_resolved)` 指向本轮 `model_id` | 通过 |
| Agent 运行快照 | `agent_runs.skill_selection` 含 `tool_refs_count=1/output_elements_count=1`；`model_selection_snapshot` 含本轮 provider/model runtime 信息 | 通过 |
| 确认 payload | `agent_interrupts.confirmation_payload` 含模型快照和 `image_ref` 输出元素 | 通过 |
| 后台 UI 入口 | 浏览器核对 `/admin/tools`、`/admin/models/providers`、`/admin/models`、`/admin/skills/system/new` | 通过 |

## 发现的问题与解决方案

| 问题 | 影响 | 当前状态 | 解决方案 |
| --- | --- | --- | --- |
| 用户前台不能真实发起 Agent run | 用户在首页输入 prompt 后只打开“登录后继续创作”弹窗，无法登录、进入工作台或调用 `/api/agent/sessions`、`/api/agent/runs` | 未修，产品链路缺口 | W2 需要补用户端登录态、工作台路由、session/run 创建、事件流展示、确认继续流程 |
| `LoginModal` 的“登录并继续/注册账号”按钮无行为 | 点击后仍停留弹窗，URL 不变，用户无法继续刚才 prompt | 未修，产品链路缺口 | 给登录按钮接入 `/api/auth/login` 或跳转真实登录页，登录成功后恢复 intent 并创建 Agent session/run |
| 管理端系统 Skill 新建页不支持一等配置 `output_elements` | 后台 UI 可以写 Markdown Tool 引用，但输出元素结构本轮仍通过 API 脚本传入；纯 UI 无法完整配置输出元素 | 未修，管理端体验缺口 | 在系统 Skill 编辑器增加输出元素结构编辑控件，字段对齐 `element_type/use_draft/use_final/display_slot/schema_json` |
| Tool 管理注册只创建元信息和治理策略 | Agent 可以做策略校验和独立 Tool 计费，但没有证明存在真实 Tool 执行器 | 非缺陷，边界需明确 | 文档和 UI 已提示“只注册 Tool 元信息和治理策略，不创建或部署运行时执行器”；真正执行器接入应单列任务 |
| 高风险或需确认 Tool 会提前中断 | 如果 Tool 配置 `requires_confirmation=true` 或 `risk_level=high`，Agent 会创建 `risk_confirmation` interrupt 并返回，不会继续走默认模型快照 | 非缺陷，流程分支 | 主链路验收用免确认 Tool；另补单测/脚本覆盖风险确认分支 |
| 验证脚本内联 JSON 被 shell 花括号展开破坏 | 初次脚本发送的 schema 变成 `"type":"object"`，导致接口返回 `json field must be a valid object` / `schema_json must be json` | 已修 | `scripts/e2e-admin-agent-full-flow.sh` 改为把 JSON Schema 放入变量，再通过 `jq --arg` 注入 |

## 验证命令

| 命令 | 结果 |
| --- | --- |
| `bash -n scripts/e2e-admin-agent-full-flow.sh` | 通过 |
| `scripts/e2e-admin-agent-full-flow.sh` | 通过，输出 `run_id=run_djloavhwao7c` |
| `curl http://127.0.0.1:19080/readyz` | `{"service":"business","status":"ready"}` |
| `curl http://127.0.0.1:18080/readyz` | `{"service":"agent","status":"ready"}` |
| 浏览器打开 `http://localhost:3100/admin` | 已登录后台，四个能力配置入口存在 |
| 浏览器打开 `http://127.0.0.1:3200` 并提交 prompt | 只出现登录意图弹窗，未进入 Agent 工作台 |

## 后续读取提醒

- 以后验收“后台配置是否真的管用”，必须跑 `scripts/e2e-admin-agent-full-flow.sh` 或等价链路，不能只看后台 API 200。
- 后端已证明配置能被 Agent 消费；当前最大缺口是用户端工作台闭环，不应再把它归因到模型/Skill/Tool 后台配置。
- 后续修用户前台时，至少要补：登录后恢复 prompt intent、创建 Agent session/run、订阅 run events、展示 confirmation interrupt、确认后继续生成与资产提交。
