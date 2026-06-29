# 管理端模型 / Tool / Skill 全量验收报告

日期：2026-06-29

结论：修复后通过。模型供应商、模型管理、Tool 管理、系统 Skill 的关键链路已完成回归；默认模型状态机、Skill 管理字段、Tool 管理回显、供应商操作收敛和前端交互问题均已验证。

## 范围

- 模型供应商：列表、状态筛选、新增弹窗、编辑、停用/启用、连通性测试 API、DB 落库。
- 模型管理：供应商左侧联动、资源类型筛选、新增/编辑弹窗、设为默认、停用/启用、DB 默认模型恢复。
- Tool 管理：列表、类型左侧联动、详情、影响预览、策略、计价、白名单、停用/启用、DB 落库。
- Skill 管理：系统 Skill 列表、页面式 Skill 编辑器、Markdown 模板、测试结果、发布、废弃、审核列表、审核通过/拒绝、DB 落库。
- 样式和交互：后台白色主题、左侧选择区、搜索区、下拉、弹窗、详情抽屉、状态提示、字段说明、操作合理性。

## 环境

- 管理端：`http://localhost:3100`
- 业务服务：`dora-business` / `http://localhost:19080`
- 数据库：`dora-postgres` / `dora_business`
- 浏览器：Chrome 当前登录态 `admin@dora.local`
- API 测试会话：`asess_e2e_20260629_fulltest`，测试后已撤销。

## 2026-06-29 Agent 真实运行链路 E2E 修复归档

结论：通过。已按“不是只验证接口通，而是验证后台配置能被前台用户真实使用”的口径补齐验收。脚本 `scripts/e2e-agent-runtime-config.sh` 会创建真实模型供应商、真实模型、真实 image 默认模型、真实系统 Skill 和输出元素结构，发布后用普通用户提示词触发 Agent，并同时检查 Business DB 落库和 Agent DB 运行快照。

本轮最后一次 E2E 证据：

- `run_id=run_djlndv5wagpc`
- `session_id=sess_djlndv5iodyg`
- `skill_id=sk_bb73e8aaf4f7ff6e2eed3ede5df76eb0`
- `version_id=skv_e52a5a3dc58bc34009ebc93d55024622`
- `model_id=mdl_96f26cd2c1e13941fb9e137bf20d10fa`
- `provider_id=mp_da2f4e617a27eefba8178beb97c4a5d7`
- `unique_hint=agent-e2e-skill-20260629154450`
- `trace_id=e2e-agent-runtime-20260629154450`

### 已发现并修复的问题

| 问题 | 表现 / 风险 | 根因 | 解决方案 | 回归保护 |
| --- | --- | --- | --- | --- |
| Agent 本地直连 Business RPC 不稳定 | `KITEX_REGISTRY=none` 时 Agent 无显式业务服务地址，真实运行链路可能只测到 HTTP、测不到 RPC 消费 | Agent 配置只有 `BUSINESS_SERVICE_NAME`，本地无注册中心时缺 hostport | 新增 `BUSINESS_HOSTPORTS` 配置，`BusinessGateway` 使用 `client.WithHostPorts(...)` | `config_test.go` 覆盖 `BUSINESS_HOSTPORTS` 解析；E2E 通过 Agent RPC 拉取 Skill / Model |
| 管理端创建系统 Skill 没有落 `output_elements` | 后台看似创建成功，但 Agent 拿不到 per-skill 输出元素结构，后续产物组织失真 | `adminCreateSystemSkill` 请求 DTO 未接收 `output_elements`，也未传入 `SaveSkillInput` | HTTP handler 补 `outputElementReq` / `outputElementInputs`，用户 Skill 保存和系统 Skill 创建都透传输出元素 | `TestAdminCreateSystemSkillPersistsOutputElements` 查 `skill_output_element_schemas` 落库 |
| 创建 Skill 响应缺 `latest_version_id` | 后续发布只能依赖额外查询或人工补 version id，E2E 无法自然发布刚创建的 Skill | `SaveSkill` 创建后只返回基础 `skillDTO`，没有管理端详情字段 | `SaveSkill` 返回 `skillAdminDTO(ctx, skill)`，带最近版本信息 | 同上测试校验创建响应含 `latest_version_id` |
| Agent 内部模型快照 JSON 字段大小写不稳定 | `agent_runs.model_selection_snapshot` 和确认 payload 可能出现 `ModelID/ProviderRuntimeRef`，不利于前后端/DB 契约消费 | `ModelRuntimeSnapshotDTO` 缺 JSON tag | 为快照 DTO 增加 snake_case JSON tag | E2E 查 `agent_runs.model_selection_snapshot.model_id/provider_runtime_ref/timeout_ms/runtime_parameters` |
| 用户可见确认事件泄露供应商运行引用 | `confirmation.required` 事件可能把内部 `provider_runtime_ref` 暴露给前端 | 写事件时直接放完整内部 confirmation payload | 事件与 snapshot 使用 `publicConfirmationPayload` 脱敏；DB interrupt 仍保留完整 payload 供确认后生成 | `assertNoProviderRuntimeRef` 递归扫描所有事件 payload；E2E 校验公开事件不含 `provider_runtime_ref` |

### 真实链路断言

Business DB 层：

- `model_providers`：新供应商为 `active`，`provider_code` 与本轮唯一值一致，`config_json.timeout_ms=12345`。
- `models`：新模型为 `active image`，`model_code` 与本轮唯一值一致，`route_config_json.e2e_marker=runtime_config`。
- `default_models`：`image/global/active` 指向刚创建的模型，脚本退出后恢复原默认模型。
- `skills/skill_versions`：系统 Skill 已发布，`published_version_id` 指向刚创建版本，`route_hints.keyword` 等于本轮唯一 hint。
- `skill_output_element_schemas`：发布版本含 1 个 `image_ref` 输出元素，`use_draft/use_final=true`，`display_slot=asset_detail`。
- `skill_test_cases`：发布前补齐 3 条 active 测试用例，满足发布门槛。

Agent 用户运行层：

- 普通用户登录后创建 session/run，prompt 为 `请使用 <unique_hint> 帮我生成一张可爱的产品图方案`。
- `agent.skill.selected` 事件必须命中本轮 `skill_id`，且 `matched_reason=route_hint:keyword`、`route_hints.keyword=<unique_hint>`。
- `generation.progress(model_snapshot_resolved)` 必须指向本轮 `model_id`。
- `agent_runs.skill_selection` 必须包含本轮 `skill_id`、`output_elements_count=1` 和 `image_ref`。
- `agent_runs.model_selection_snapshot` 必须包含本轮 `model_id`、`provider_runtime_ref=<provider_code>:<model_code>`、`timeout_ms=12345` 和 `runtime_parameters.e2e_marker=runtime_config`。
- `agent_interrupts.confirmation_payload` 内部保存完整模型快照和输出元素，供确认后生成/扣费继续使用；用户可见事件不泄露 `provider_runtime_ref`。

### 验证命令

| 命令 | 结果 |
| --- | --- |
| `bash -n scripts/e2e-agent-runtime-config.sh` | 通过 |
| `go test ./services/agent/internal/infra/config ./services/agent/internal/infra/rpc ./services/agent/internal/application/workbench ./services/agent/internal/api/http -count=1` | 通过 |
| `go test ./services/business/internal/application/skillcatalog ./services/business/internal/transport/http ./services/business/internal/application/modelconfig -count=1` | 通过 |
| `scripts/e2e-agent-runtime-config.sh` | 通过，输出上述 `run_id/model_id/skill_id/provider_id` 证据 |

### 后续读取提醒

- 以后验收管理端模型供应商、模型管理、系统 Skill 是否“真的管用”，必须跑 `scripts/e2e-agent-runtime-config.sh` 或等价链路，不能只看 HTTP 200。
- 验收口径必须同时覆盖三层：后台配置写入 Business DB、Agent 通过 Business RPC 取到配置、用户 prompt 触发 Agent run 后在 Agent DB 留下运行快照。
- 公开 AG-UI / HTTP 事件只允许暴露前端需要字段；完整 `provider_runtime_ref` 只能留在服务端 DB interrupt payload 中用于后续确认执行。
- 本次修复未偏离方向：没有新增产品能力，只把管理端配置打通到用户侧 Agent 运行消费，并补充防回归脚本和契约测试。

## 2026-06-29 Tool 管理注册入口修复归档

结论：通过。后端 `POST /api/admin/tools` 本身可注册 Tool，但管理端 Tool 页面缺少“注册 Tool”入口，导致系统 Skill 编辑器提示“先在 Tool 管理中注册名称和作用说明”时，管理员没有前端路径可走。

## 2026-06-30 后台配置到用户 Agent 全链路复核索引

新增归档：`tests/reports/admin-agent-full-flow-2026-06-30.md`

结论：后端真实消费链路通过，用户前台产品链路未闭环。`scripts/e2e-admin-agent-full-flow.sh` 已覆盖后台新增 Tool、模型供应商、模型、默认模型、系统 Skill、输出元素，并用普通用户 Agent prompt 验证 `agent.skill.selected`、`tool.call.started/completed`、默认模型快照和确认 payload。浏览器核对显示当前 `frontend` 仍停在首页登录意图弹窗，“登录并继续”未接登录/工作台/Agent run，因此用户端 UI 不能完成真实创作链路。

### 已发现并修复的问题

| 问题 | 表现 / 证据 | 根因 | 解决方案 | 回归保护 |
| --- | --- | --- | --- | --- |
| Tool 管理没有注册入口 | Tool 页只能列表、策略、计价、白名单、启停；系统 Skill 编辑器却提示先在 Tool 管理注册 | `pageConfigs.tools.create` 缺失，前端没有表单映射 `POST /api/admin/tools` | Tool 页新增“注册 Tool”弹窗，覆盖基础信息、Schema、执行策略、计价和注册原因 | `pageConfigs.test.jsx` 覆盖 create 配置和提交体字段 |
| Tool 注册原因未进入注册变更记录 | API 请求可传 `reason`，但 `tool_policy_change_records.after_json.reason` 为空 | HTTP handler 未接 `reason`，`RegisterToolInput` 和应用层 change record 未保存 | handler 接收 `reason/request_hash`，应用层把 `reason` 写入 `tool.register` 的 `after_json` | `TestAdminRegisterToolPersistsReasonAndPolicies` 覆盖注册响应、策略计价和 reason 落库 |

### 真实链路验证

- 通过后台账号调用 `POST /api/admin/tools` 注册唯一 Tool。
- `GET /api/admin/tools?page_size=100` 能回显同一个 `tool_key`。
- DB 断言 `tool_definitions`、`tool_policies`、`tool_pricing_policies` 各自有本轮 active 记录。
- DB 断言 `tool_policy_change_records.after_json.reason` 等于本轮注册原因。

最后一次本地 smoke 证据：

- `tool_key=e2e_ui_register_tool_20260629155628:builtin`
- `pricing_policy_id=tool_price_5460fd9f8548c53e69ee59c166594355`
- `reason=验证 Tool 管理注册入口 20260629155628`

### 验证命令

| 命令 | 结果 |
| --- | --- |
| `pnpm --dir admin_frontend test -- pageConfigs.test.jsx --run` | 通过，13 个测试文件、41 个测试用例 |
| `pnpm --dir admin_frontend test -- --run` | 通过，13 个测试文件、41 个测试用例 |
| `go test ./services/business/internal/transport/http -run TestAdminRegisterToolPersistsReasonAndPolicies -count=1` | 通过 |
| `go test ./services/business/internal/application/toolpolicy ./services/business/internal/transport/http -count=1` | 通过 |
| 本地 `POST /api/admin/tools` + `GET /api/admin/tools` + DB 三表和 change record 查询 | 通过 |

### 后续读取提醒

- Tool 管理页必须保留注册入口；否则系统 Skill 编辑器中“先注册 Tool”的产品路径会断。
- Tool 注册只登记元信息、Schema、策略、计价和审计原因，不代表创建运行时执行器；运行时执行器仍由平台工程侧接入。
- 后续改 Tool 注册字段时，要同步 `api/openapi/business-api.yaml`、`handlers_m3.go`、`toolpolicy.RegisterToolInput`、`pageConfigs.tools.create` 和前后端测试。

## 2026-06-29 二次本地启动巡检修复归档

提交：`0b4eb1e fix: harden admin resource smoke issues`
分支：`cc-yr`
结论：未偏离目标。本次只围绕截图红框中的 `系统 Skill`、`模型供应商`、`模型管理`、`Tool 管理` 做本地启动、页面巡检、API smoke 和缺陷修复。

### 已修复问题

| 问题 | 表现 / 证据 | 根因 | 解决方案 | 回归保护 |
| --- | --- | --- | --- | --- |
| Tool 详情直接展示英文枚举 | 详情抽屉显示 `active`、`medium`、`model_generation`、`asset` 等 | 通用详情组件按原值渲染后端枚举 | `ResourceListPage.jsx` 增加常见详情枚举中文映射，嵌套对象和顶层详情都按字段名转换 | `ResourceListPage.test.jsx` 覆盖 `status/tool_type/risk_level/charge_mode/billing_unit` 中文展示 |
| Tool 列表计费单位仍显示 `asset/call` | Tool 管理列表显示 `asset · 12 积分`、`call · 3 积分` | Tool 计费单位复用了模型计费单位映射，缺少 `asset` 等 Tool 单位 | `pageConfigs.jsx` 增加 `toolBillingUnitOptions` 和 `toolBillingUnitLabel`，Tool 列表显示 `按资产/按调用` | `pageConfigs.test.jsx` 覆盖 Tool 计价列中文展示 |
| 模型供应商 PATCH 会丢旧字段 | 只传 `status` 或只改名时，存在丢失 `provider_code/base_url/config/secret_ref_status` 的风险 | 后端 `SaveProvider` 把 PATCH 当全量保存；HTTP handler 在 PATCH 时会自动生成缺省 `provider_code` 并构造空 `config` | 应用层更新旧供应商时先读取现有行，未传字段保留旧值；HTTP handler 仅创建时自动生成 `provider_code`，且只有显式传 `config/secret_key_ref` 时才覆盖配置 | `app_admin_contract_test.go` 覆盖只改状态/只改名称保留旧字段；`m3_integration_test.go` 覆盖 HTTP PATCH 保留密钥状态 |
| 供应商编辑表单缺少 `provider_code` | 管理员编辑供应商时无法看见/保留供应商编码 | 前端编辑 action 字段未包含 `provider_code` | `pageConfigs.jsx` 在模型供应商编辑字段中补 `provider_code` | `pageConfigs.test.jsx` 覆盖编辑字段必须包含 `provider_code` |

### 本次未作为缺陷处理的 smoke 假阴性

- 系统 Skill 测试结果保存第一次返回 `SAFETY_EVIDENCE_INVALID`，根因是 smoke payload 使用了 `{}` 或字段不完整的 `safety_evidence_json`，不是业务接口缺陷。
- 合法契约：`Idempotency-Key` 必须是 `skill_test:<test_run_id>`；`safety_evidence_json.scene=skill_test`、`target_type=skill_test_prompt`、`target_ref_id` 等于 `test_run_id`（传 `test_case_id` 时等于 `test_case_id`）、`source_run_id=test_run_id`、`trace_id` 与 `X-Trace-Id` 对齐、`expires_at` 在未来。
- 后续本地 smoke 不应再用空安全证据；若字段不满足上述契约，返回 `SAFETY_EVIDENCE_INVALID` 是预期行为。

### 验证证据

命令验证：

| 命令 | 结果 |
| --- | --- |
| `pnpm test -- --run`（目录：`admin_frontend/`） | 13 个测试文件、40 个测试用例通过 |
| `pnpm build`（目录：`admin_frontend/`） | 通过，Vite 生产构建成功 |
| `go test ./services/business/internal/application/modelconfig ./services/business/internal/transport/http -count=1` | 通过 |
| `git diff --check` | 通过，无空白错误 |

本地页面 / API 验证：

- 四个页面 `/admin/skills/system`、`/admin/models/providers`、`/admin/models`、`/admin/tools` 均可加载，无浏览器 console error。
- 模型供应商 API smoke：新增、只改 `status`、只改名称后，`provider_code/base_url/secret_ref_status` 均保持正确。
- 模型管理 API smoke：新增文本模型成功。
- Tool 管理 API smoke：`impact-preview` 和 `whitelist` 保存成功。
- 系统 Skill API smoke：使用合法安全证据保存测试结果成功，返回 `saved=true`。
- Tool 管理页面刷新后列表显示 `按资产 · 12 积分`、`按调用 · 3 积分`，详情抽屉显示 `状态 启用`、`风险等级 中风险`、`Tool 类型 模型生成 Tool`。

### 后续读取提醒

- 后续继续验收管理端四页时，先读取本节，避免重复把上述已修问题当作新缺陷。
- 供应商 PATCH 的既定口径是“部分更新，未传字段保留旧值”；不得恢复为全量覆盖。
- 枚举展示的既定口径是“管理端可见 UI 使用中文标签，后端仍保留枚举值作为契约值”。

## 2026-06-29 修复回归

修复项：

- 默认模型：重复设置默认模型不再触发唯一约束；当前默认模型不允许停用，返回 `STATE_CONFLICT`。
- 模型管理：默认模型行只保留“编辑”，非默认启用模型才显示“设为默认/停用”；资源类型和状态已中文化。
- 模型供应商：行级操作收敛为“编辑/启停”，不再展示“连通性测试”；密钥小眼睛不再触发 Chrome 密码管理弹窗。
- Tool 管理：左侧类型选择区与模型管理一致；停用 Tool 仍回显真实策略、计价和影响 Skill 名称。
- 系统 Skill：列表返回并展示 `latest_version_id`、`active_test_case_count`，发布/废弃操作按状态和测试用例数量收敛。
- 全局交互：搜索区按钮、下拉层级、强提示弹窗、表单校验提示、枚举中文显示完成回归。

自动化验证：

- `/Users/figo/sdk/go1.26.3/bin/go test ./services/business/internal/application/modelconfig ./services/business/internal/application/skillcatalog ./services/business/internal/application/toolpolicy ./services/business/internal/transport/http`
- `pnpm --dir admin_frontend test -- --run`
- `python3 tests/contract/test_admin_openapi_contract.py`

HTTP / DB 回归：

- `GET /api/admin/models/providers` 返回 7 个供应商。
- `GET /api/admin/models` 返回 12 个模型；重复 `POST /api/admin/models/default` 返回 200。
- 对当前文本默认模型调用 `POST /api/admin/models/{model_id}/status` 停用返回 409。
- `GET /api/admin/tools` 返回 4 个 Tool；`POST /api/admin/tools/{tool_key}/impact-preview` 返回 `affected_skills`。
- `GET /api/admin/skills/system` 返回 4 条系统 Skill，包含最近版本和测试用例数量字段。
- DB 校验 active 默认模型：`image/global = 1`，`text/global = 1`。
- 本轮临时管理员和临时 session 已删除。

浏览器检查：

- Chrome 进入 `http://localhost:3100/admin` 后，Tool 管理左侧类型区、搜索区、下拉菜单显示正常且无点击穿透。
- 模型管理左侧供应商与右侧模型列表联动正常；默认模型行无“停用/设为默认”按钮。
- 模型供应商列表只展示“编辑/启停”；编辑弹窗的密钥小眼睛点击后只切换自有显示状态。
- 系统 Skill 列表展示“最近版本”和“测试用例 n/3”；新建页为页面式 Markdown 编辑器，标签单行展示。

## API / RPC / DB 结果

通过项：

- `GET /api/admin/models/providers`、`GET /api/admin/models`、`GET /api/admin/tools`、`GET /api/admin/skills/system`、`GET /api/admin/skills/reviews` 均返回 200。
- 模型供应商创建、编辑、连通性测试通过；临时供应商 `e2e_provider_20260629_full_mqyjhh7z` 已停用。
- 模型创建、编辑、首次设为默认通过；临时模型 `e2e-text-20260629_full_mqyjhh7z` 已停用。
- Tool 注册、影响预览、策略更新、计价更新、白名单保存、停用/启用通过；临时 Tool `e2e_tool_20260629_skilltool_mqyjjyhj:e2e_type` 已停用。
- Skill 创建、Markdown 解析、Tool 绑定、测试结果保存通过。
- 补齐 3 条 active `skill_test_cases` 后，Skill 发布和废弃通过。
- Skill 审核通过、审核拒绝 API 通过；UI 拒绝操作也能完成。

关键 DB 证据：

- 当前文本默认模型已恢复：`mdl_deepseek_v4_fast / price_auto_0f81cdc9a2d18c6e9c4c1f163dadb04c / active`。
- 临时模型供应商：`mp_f9ec1d8347892997bd882e5046c18ec8 / disabled`。
- 临时模型：`mdl_87a60ffa76d6c95d046e2cfeffb07158 / disabled`。
- 临时 Tool：`e2e_tool_20260629_skilltool_mqyjjyhj:e2e_type / disabled`。
- Skill 审核记录包含 `publish/approved` 和 `reject/rejected`。
- 临时管理端 session：`asess_e2e_20260629_fulltest / revoked`。

## 首轮发现的问题

以下问题为首轮全量测试发现项，本轮已按上方“修复回归”完成处理或按最新产品口径调整。

P0：

- 默认模型状态机有两个问题。第一，当前默认模型可以被 `POST /api/admin/models/{model_id}/status` 禁用，接口返回 200，导致默认模型可能指向 disabled 模型。第二，多次设置默认模型会触发 `default_models_resource_type_scope_status_key` 唯一约束冲突，`POST /api/admin/models/default` 返回 500；日志显示 active 行更新为 inactive 时与已有 inactive 行冲突。

P1：

- Skill Key 自动生成对中文名称不稳定。`E2E 审核通过 Skill ...` 和 `E2E 审核拒绝 Skill ...` 生成了相同 `skill_key`，后端直接触发数据库唯一约束并返回 500，而不是返回可理解的 409/字段错误。
- Skill 发布按钮在没有 3 条 active 测试用例时可直接点击，后端返回 `STATE_CONFLICT: at least 3 active skill test cases are required`。管理端当前没有测试用例维护入口，管理员不知道如何满足发布条件。
- Skill 审核拒绝后，`skill_versions.status` 回到 `draft`，但 `skills.status` 仍保留 `submitted`。列表会显示不一致状态。
- Skill 审核拒绝弹窗没有“拒绝原因”输入框，只显示 version id；API 支持 `reason`，但 UI 没有采集。
- 模型供应商页面没有“连通性测试”入口，但后端已有 `/connectivity-test` API。

P2：

- Tool 策略弹窗没有可靠回显当前策略。列表中临时 Tool 是禁用/高风险策略，打开策略弹窗却显示“低风险”、空 JSON，容易误保存覆盖。
- Tool 影响预览只展示 Skill ID，不展示 Skill 名称、状态、发布版本或影响动作，风险判断成本高。
- 系统 Skill 列表对已发布/已废弃状态仍显示“发布/废弃”操作，操作可见性没有按状态收敛。
- 模型列表默认行仍显示“设为默认”，冗余且容易误点。
- 模型供应商新增弹窗的“密钥引用”输入框会触发 Chrome 密码管理器自动填充随机密码，语义不对。
- 多个页面仍直接展示英文枚举：`active`、`disabled`、`submitted`、`published`、`deprecated`、`public`、`text`、`image`、`music`、`call`、`asset`、`medium`、`low`。需要统一映射为中文标签。

## UI / 交互评估

- 模型管理和 Tool 管理的左侧选择区已采用相近结构，整体比之前清晰；左侧字体权重也足够。
- 模型新增弹窗已有分区和字段说明，基础信息、计费配置、运行绑定、高级路由参数比较可填写。
- Skill 编辑器改为页面后空间充足；标签是单行输入，Markdown 模板包含中文标签、Tool 引用和 AG-UI 引用，方向正确。
- Tool 页面没有新增按钮，符合当前“Tool 只注册，不由管理端新增运行时执行器”的产品口径。
- 仍需加强状态标签、枚举翻译、操作显隐、错误强提示和审核原因采集。

## 建议修复顺序

1. 修复默认模型状态机：禁止禁用当前默认模型；调整 `default_models` 历史行状态或唯一约束设计，避免多次设默认 500。
2. 修复 Skill Key 生成和冲突错误码；中文名称应生成稳定且唯一的 key，冲突应返回 409/字段错误。
3. 补齐 Skill 测试用例管理入口，或发布弹窗明确展示缺失用例数量和创建入口。
4. 修复 Skill 审核拒绝状态同步，并在拒绝弹窗采集原因。
5. 补模型供应商连通性测试按钮，Tool 策略弹窗按当前值回显。
6. 全局枚举中文化，按状态隐藏不适用操作。
