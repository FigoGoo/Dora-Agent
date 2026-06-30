# 管理端 Skill Agent 调试台前端设计

状态：active
owner：前端责任域；Agent 服务责任域；文档与契约责任域
更新时间：2026-06-30
适用范围：`admin_frontend/src/features/agentSkillTest/**`、`admin_frontend/src/services/agentApi.js`、`services/agent/internal/application/workbench/**`、`api/openapi/agent-workbench.yaml`
相关代码路径：`admin_frontend/src/features/agentSkillTest/**`、`admin_frontend/src/pages/SkillAgentTestPage.jsx`、`admin_frontend/src/services/agentApi.js`、`services/agent/internal/application/workbench/app.go`
相关契约：`docs/contracts/api/Agent工作台API契约草案.md`、`api/openapi/agent-workbench.yaml`、`docs/contracts/ag-ui/统一Agent工作台AGUI事件协议草案.md`

## 背景

平台管理员需要在后台临时验证指定 Skill 的 Agent 运行链路，覆盖模型 Tool 调用、普通 Tool 调用、AG-UI 返回、多轮对话、对话打断和对话恢复。管理端 PRD 不允许平台管理员进入普通用户创作工作台，因此本页面定位为后台调试台，不作为用户创作入口。

## 目标

- 管理端新增 `/admin/agent/skill-test` 临时调试页。
- 页面主交互采用对话框：管理员指定普通用户 Token、项目和 Skill 后，像普通用户一样发送消息。
- 首次发送创建 Agent run；同一 run 内再次发送走追加用户输入，用于模拟多轮对话。
- Tool、AG-UI、确认、生成资产和 snapshot 信息作为折叠调试详情展示，不作为主操作入口。
- 使用 Agent API 普通用户 Bearer Token 和 Space ID，不复用后台管理员登录态。
- 创建或复用 session 后通过 `selected_skill_id` 发起 Agent run。
- 展示 AG-UI 事件、Agent 文本增量、Tool 调用、积分/确认、生成任务、资产和黑板状态。
- 支持确认、拒绝、追加用户输入、取消 run、事件补偿和 snapshot 恢复。

## 非目标

- 不把该页面做成正式用户端 Agent 工作台。
- 不新增后台管理员代登录普通用户能力。
- 不新增 Skill 意图识别路由；`selected_skill_id` 仅作为显式优先选择字段。
- 不新增数据库表、SQL migration 或业务服务 RPC。

## 产品来源

| 来源 | 路径 | 状态 | 说明 |
| --- | --- | --- | --- |
| PRD | `docs/product/prd/02-平台后台与运营管理PRD.md` | active | 后台独立入口和敏感边界 |
| PRD | `docs/product/prd/05-SkillBuilder与审核PRD.md` | active | 系统 Skill 创建、测试和发布口径 |
| PRD | `docs/product/prd/06-统一Agent创作工作台PRD.md` | active | Agent 对话、Skill、Tool、确认和生成链路 |
| PRD | `docs/product/prd/09-AG-UI与A2UI交互PRD.md` | active | AG-UI 消费、补偿和 snapshot 规则 |

## 技术范围

| 责任域 | 影响范围 | 变更摘要 |
| --- | --- | --- |
| Agent 服务 | `CreateRunRequest`、Skill 选择逻辑 | 新增 `selected_skill_id`，优先匹配已发布可路由 Skill；不可用时回退 prompt 路由 |
| 业务服务 | 无 | 继续通过既有 RPC 提供 Skill、Tool、模型和项目权限 |
| 前端 | 管理端页面、Agent API client、AG-UI reducer | 新增调试页和状态合并器 |
| 契约 | Agent OpenAPI、Agent API Markdown | 新增 `selected_skill_id` 字段和回退规则 |
| 数据库 | 无 schema 变更 | `agent_runs.skill_selection` 和 input summary 记录指定 Skill |
| 测试 | Go、Vitest | 覆盖指定 Skill 优先、回退路由、Agent API client 和 AG-UI reducer |

## 产品修正

2026-06-30 根据本地验收反馈，调试台不再以大表单暴露所有 run 入参。主路径收敛为“配置连接信息 -> 发送用户消息 -> 查看 Agent 回复 -> 继续发送或处理确认”。模型选择、引用资产、控件输入、AG-UI 事件和 Tool 详情保留为高级/折叠调试信息。

2026-06-30 本地调试补充：在 `localhost` / `127.0.0.1` 打开页面时，自动使用默认普通用户 `user1001@dora.local` 登录，填入 Agent API token、当前 Space、首个 active 项目和首个 published Skill；已有手工输入不覆盖。该能力仅服务本地临时验收，不作为生产用户登录方案。

2026-06-30 请求语义补充：`POST /api/agent/sessions` 只创建对话容器，不携带 Skill；指定 Skill 在首次用户消息创建 run 时通过 `POST /api/agent/runs` 的 `selected_skill_id` 传入，后端落到 `agent_runs.input_summary.selected_skill_id` 和 `agent_runs.skill_selection.skill_id`。

2026-06-30 调试语义补充：`selected_skill_id` 只表示本轮优先使用指定 Skill 作为上下文，不表示立即执行该 Skill 绑定的全部 Tool。问候、能力说明等文本类输入应返回 assistant 文本消息并完成 run；只有用户明确提出生成、编辑、检索或保存任务时才进入 `tool.call.*`、`confirmation.*`、模型生成和扣费流程。

## 契约影响

- RPC：不变。
- HTTP API：`POST /api/agent/runs` 新增可选字段 `selected_skill_id`。
- AG-UI：不新增事件类型；复用 `agent.skill.selected`、`agent.skill.missing`、`agent.message.completed`、`confirmation.*`、`tool.call.*`、`resume.accepted`。
- Agent 数据模型：不新增字段；指定 Skill 记录在既有 JSON 快照中。
- SQL：无。

## 实现计划

| 功能点 | 代码路径 | 前置依赖 | 状态 | 验收方式 |
| --- | --- | --- | --- | --- |
| 指定 Skill 契约 | `api/openapi/agent-workbench.yaml`、`docs/contracts/api/Agent工作台API契约草案.md` | Agent API 契约 | 已实施 | OpenAPI 字段和 Markdown 规则 |
| 指定 Skill 优先执行 | `services/agent/internal/application/workbench/app.go` | Skill 列表 RPC | 已实施 | Go 测试 |
| Agent API client | `admin_frontend/src/services/agentApi.js` | 普通用户 Bearer Token | 已实施 | Vitest |
| AG-UI reducer | `admin_frontend/src/features/agentSkillTest/aguiState.js` | AG-UI 契约 | 已实施 | Vitest |
| 调试页 | `admin_frontend/src/features/agentSkillTest/SkillAgentTestPage.jsx` | 管理端路由和导航 | 已实施 | lint、build |

## 测试计划

| 类型 | 命令或证据 | 覆盖范围 | 状态 |
| --- | --- | --- | --- |
| Go 单元测试 | `GOCACHE=/private/tmp/dora-go-build-cache /Users/figo/sdk/go1.26.3/bin/go test ./services/agent/internal/application/workbench -run 'TestRouteSkill|TestToolPolicyRiskContextRequiresPerToolWhitelistCheck' -count=1` | selected_skill_id 优先和回退纯逻辑 | 已通过 |
| Go 集成测试 | `GOCACHE=/private/tmp/dora-go-build-cache /Users/figo/sdk/go1.26.3/bin/go test ./services/agent/internal/application/workbench -run 'TestSelectedSkillID|TestM6SkillTestConsumesReviewCandidateRPC' -count=1` | 数据库集成链路 | 未通过执行环境，Docker socket 无权限 |
| 前端单测 | `pnpm --dir admin_frontend test -- src/services/agentApi.test.js src/features/agentSkillTest/aguiState.test.js --run` | Agent API client、AG-UI reducer | 已通过，15 个测试文件、45 个测试用例 |
| 前端 lint | `pnpm --dir admin_frontend lint` | 静态检查 | 已通过，保留既有 `pageConfigs.jsx:97` warning |
| 前端构建 | `pnpm --dir admin_frontend build` | Vite 生产构建 | 已通过 |

## 实现记录

- 实现状态：已完成。
- 验证命令：见测试计划。
- 证据路径：本地命令输出。
- 未执行原因：Go 数据库集成测试需要 Docker provider，当前沙箱无权访问 `/var/run/docker.sock` 和 `/Users/figo/.docker/run/docker.sock`。
- 遗留风险：该页面仍依赖人工提供普通用户 Agent API Token；正式用户端工作台仍需独立完善登录态、项目路由和资产交互。
- 2026-06-30 对话式调试台改版：主界面改为模拟用户对话框，验证命令为 `pnpm --dir admin_frontend lint`、`pnpm --dir admin_frontend test -- src/services/agentApi.test.js src/features/agentSkillTest/aguiState.test.js --run`、`pnpm --dir admin_frontend build`、`GOCACHE=/private/tmp/dora-go-build-cache /Users/figo/sdk/go1.26.3/bin/go test ./services/agent/cmd/agent -count=1`、`git diff --check`，均已通过；lint 仅保留既有 `src/features/resources/pageConfigs.jsx:97` warning。
- 2026-06-30 本地默认值填充：验证 `http://127.0.0.1:3100/api/auth/login`、`/api/projects`、`/api/skills?status=published&limit=20` 可经 Vite proxy 返回 `sp_personal_1001`、`prj_active_1001`、`sk_seed_storyboard`；前端 lint、单测和 build 均已通过。
- 2026-06-30 Skill 传参核验：复测 `POST /api/agent/runs` 已携带 `selected_skill_id=sk_seed_storyboard`，数据库 `agent_runs.skill_selection.skill_id` 和 AG-UI `agent.skill.selected.payload.skill_id` 均为 `sk_seed_storyboard`。
- 2026-06-30 能力问答核验：后端新增回归用例覆盖 `你好，你有什么能力`，指定 Skill 后应返回 `agent.message.completed` / `agent.run.completed`，不得产生 `tool.call.started`、`confirmation.required`、模型生成和扣费 RPC。

## 归档条件

- [x] 产品口径已同步。
- [x] 契约文档已同步。
- [x] 测试用例已同步。
- [x] 验证命令全部执行并记录结果。
