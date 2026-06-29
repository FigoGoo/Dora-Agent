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
