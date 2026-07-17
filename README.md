# Dora Agent

Dora 是面向桌面 Web 的 Skill 驱动 AIGC Agent 平台。本仓库正在从历史 Demo 迁移为 `business`、`agent`、`worker` 三个独立 Go Module 的生产版工程。

## 当前仓库状态

当前工作树是三 Module 重构后的开发预览基线，不是可运行的旧后端 Demo。

| 范围 | 当前事实 | 下一门槛 |
| --- | --- | --- |
| 前端 | 真实项目列表、文本素材创建/选择/分析、六个 Tool 的统一 Profile、Workspace V5、PNG/MP4 控件与同源播放已接入；真实 Chromium 主链已通过 | 保持前端单测/构建为独立质量门禁，继续补错误恢复与可访问性 |
| Business Service | CreationSpec、素材 Evidence、Storyboard/Prompt Preview、Owner-only 项目、文本素材、媒体 Asset、内部 Prepare/Query/Finalize、Owner 内容读取与同源 BFF 已跑通 | 完整上传、通用 Asset/Evidence、RBAC、计费及生产服务认证后置 |
| Agent Service | 普通消息与六个 Graph Tool 已合入 `mvp_all_tools.runtime.v1preview1`：一个主 ChatModelAgent、一个 Coordinator、全来源 HOL、媒体 Operation/Batch/Job 和 Terminal Bridge 已跑通 | 保持生产 Catalog `unavailable`，另行补重启恢复、Fence/故障注入和生产模型门禁 |
| Business Worker | Job Claim/Lease/Heartbeat、Business Finalize 和确定性 640×360 PNG、固定 H.264/yuv420p/2s/faststart MP4 已在统一主链产生真实文件 | 生产 Provider、跨主机对象存储、压力/容灾和完整恢复矩阵后置 |
| 本地基础设施 | 2026-07-17 `make trial-basic` 已在三 Module、统一基础/媒体 Profile 和真实 Chromium 上通过；Evidence 为 `.local/smoke/trial-basic.json`，权限 `0600` | 五条 isolated smoke、三 Module 全量测试、前端全量测试/构建及完整恢复门禁继续独立执行 |

V1、V2 与 V3 的 local-only 基本功能已经形成一条可重复主链：登录、项目、文本素材、六个 Graph Tool、Worker PNG/MP4、受保护内容读取和 Workspace V5 刷新恢复均已跑通。`trial-basic` 是快速 MVP 验收，不自动包含五条 isolated smoke、三 Module 全量测试、Runtime 重启、Fence takeover、故障注入、生产 Provider、计费或 Approval。以上结论不代表生产 Catalog、完整 SMK-P0 或生产发布完成；唯一阶段排期以[功能优先开发与试跑计划](docs/requirements/full-function-smoke-development-plan.md)为准。

五条 standalone Profile 继续保持既有互斥和隔离语义，但不由 `trial-basic` 串行执行；统一 Profile 保留 QuickCreate 初始用户消息，并允许真实文本素材 Picker 与六个 Tool 在同一 Session Lane 顺序执行。静态生产 Catalog 仍未注册任何 Preview Tool。

根目录不再保留生产 `go.mod`、`go.sum`；`go.work` 只用于本地联调，CI 和发布必须在三个 Module 内以 `GOWORK=off` 独立执行。`main` 分支及旧 `internal/aigc/**` 代码只可作为历史实现参考，不得整分支恢复或直接作为当前能力验收。

## 目标 Module 布局

```text
Dora-Agent/
├── business/
│   ├── cmd/business-service/
│   └── migrations/
├── agent/
│   ├── cmd/agent-service/
│   └── migrations/
├── worker/
│   ├── cmd/business-worker/
│   └── migrations/
├── frontend/
├── docs/
└── go.work                 # 仅用于本地联调
```

- Business Service：用户、鉴权、Project、Skill、Storyboard、Asset、支付、积分、收益和管理端业务真源。
- Agent Service：Session/Input/Turn、六个 Graph Tool、Approval、Operation/Batch/Job、Continuation、EventLog 和 A2UI。
- Business Worker：只执行已持久化 Job，负责 Provider、对象存储和 Business Finalize，不选择 Skill、不决定 Prompt、不扣费或退款。

## v1 Graph Tool 白名单

主 Agent 只允许注册以下六个高层 Graph Tool，顺序与用户工具箱一致：

1. `plan_creation_spec`：流程规划。
2. `analyze_materials`：素材分析。
3. `plan_storyboard`：故事板设计。
4. `generate_media`：媒体生成。
5. `write_prompts`：提示词写法。
6. `assemble_output`：视频剪辑与装配。

任何 Tool 只能实现其 `docs/design/agent/graphtool/<tool_key>-design.md` 顶部明确批准的开发 Profile。同步四 Tool 已批准 standalone Development Preview 与 `mvp_all_tools.runtime.v1preview1` 统一装配；`generate_media`、`assemble_output` 只批准 `media.runtime.v3preview1` 的本地确定性 PNG/固定 MP4。真实 Provider、生产 Evidence/MaterialAnalysis、稳定 Revision/Slot、Active/Approval、计费、TOS 或生产 Catalog 均未获授权，不能据此宣称生产可用。

## 开发入口

- [项目协作指引](AGENTS.md)
- [服务端开发规范 Skill](.agents/skills/dora-server-development/SKILL.md)
- [用户端需求总览](docs/requirements/user-requirements-overview.md)
- [管理端需求总览](docs/requirements/admin-requirements-overview.md)
- [服务端需求总览](docs/requirements/server-requirements-overview.md)
- [Graph Tool 功能需求总览](docs/requirements/graph-tool-requirements-overview.md)
- [支付与积分充值需求总览](docs/requirements/payment-requirements-overview.md)
- [共通业务规则与验收基线](docs/requirements/common-requirements-baseline.md)
- [功能优先开发与试跑计划（唯一排期口径）](docs/requirements/full-function-smoke-development-plan.md)
- [Graph Tool 详细设计索引](docs/design/agent/graphtool/README.md)
- [V2 通用 user_message Runtime 最小 Profile 设计](docs/design/agent/user-message-runtime-v2-design.md)
- [MVP All Tools Runtime V1](docs/design/agent/mvp-all-tools-runtime-v1-design.md)
- [Media Runtime V3 Preview](docs/design/agent/media-runtime-v3-preview-design.md)
- [MVP 六工具媒体扩展 V1](docs/design/agent/mvp-six-tools-media-extension-v1-design.md)
- [Media Runtime V3 跨 Module 契约](docs/design/cross-module/media-runtime-v3-preview-contract.md)
- [AIGC 跨 Module 契约目录](docs/design/cross-module/aigc-contract-catalog.md)
- [Foundation RPC v1 契约](docs/design/cross-module/foundation-rpc-v1.md)
- [三 Module 持久化基础 v1](docs/design/cross-module/persistence-foundation-v1.md)
- [W0 身份与工作台契约 v1](docs/design/cross-module/w0-identity-workspace-contract-v1.md)
- [W0.5 Workspace Transport 契约 v1](docs/design/cross-module/w05-workspace-transport-contract-v1.md)
- [W1-E Skill Market 公开读取 v1](docs/design/business/w1-skill-market-read-v1.md)
- [W1-F Public Market Binding v1](docs/design/business/w1-public-market-binding-v1.md)
- [前端接入基础 v1](docs/design/frontend/integration-foundation-v1.md)
- [全功能冒烟工程设计](docs/design/testing/full-function-smoke-engineering-design.md)
- [`main` 分支 AIGC 迁移资产清单](docs/design/migration/main-branch-aigc-asset-inventory.md)

`docs/aigc-*-design.md` 保留了历史实现与目标设计信息，开发时必须按当前分支代码核验“当前实现”，并把旧路径映射到三 Module 新结构。

## 前端本地运行

```bash
cd frontend
npm install
npm run dev
```

校验命令：

```bash
cd frontend
npm test
npm run build
```

也可从仓库根目录执行：

```bash
make check-frontend
```

开发代理默认把 `/api/**` 转发到 Business `18081`，浏览器不直连 Agent；`/api/aigc/**` 仅用于尚未迁移的历史页面，不能作为新接口接入方式。本地覆盖值参考 `frontend/.env.example`。登录、退出、QuickCreate 和正式 Workspace 已使用真实 Business API。

## 快速 MVP 主链验收

本地 PostgreSQL、Redis、etcd 已在 `.env.example` 对应端口可用后，执行：

```bash
make GO=/Users/figo/sdk/go1.26.3/bin/go \
  W0_ENV_FILE=.env.example trial-basic
```

该命令重置三个专用测试数据库，从当前工作树构建并启动 Business、Agent、Worker、Vite 与 Chromium，使用唯一基础 Profile `mvp_all_tools.runtime.v1preview1` 和媒体 Profile `media.runtime.v3preview1`，验证登录、项目、文本素材、六个 Graph Tool、Worker 真实 PNG/MP4、Owner 保护的 `200/206/416` 内容读取、Workspace V5 硬刷新恢复、受控进程清理和执行期间源码无变化。全部断言通过后才发布权限为 `0600` 的 `.local/smoke/trial-basic.json`。

这是快速开发反馈，不是完整质量或发布门禁。五条 standalone isolated smoke 仍按各自命令运行；三 Module 的全量 `verify/test/vet/race/build`、前端 `test/build`、Runtime 重启恢复、Fence takeover、故障注入、生产 Provider、计费和 Approval 也必须按变更范围单独验证。P1 完整 Evidence 口径见[全功能冒烟工程设计](docs/design/testing/full-function-smoke-engineering-design.md)。

## W0.5 Workspace Transport 本地运行

以下命令验证真实 PostgreSQL、Redis、etcd、Business、Agent、Vite 与 Chromium 上的 W0/W0.5 主链路。它覆盖登录、QuickCreate、Ensure RPC、Workspace Snapshot/SSE、硬刷新和退出撤销，但不代表 Graph Tool、模型执行、Worker Job、支付或完整管理员 RBAC 已实现。

```bash
cp .env.example .env.local

# 首次安装版本化 Migration CLI。
GOBIN="$PWD/.local/tools" go install -tags postgres \
  github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.0

make local-up
MIGRATE_BIN="$PWD/.local/tools/migrate" make migrate-up
make foundation-smoke

# API/RPC/数据库/Workspace/SSE 黑盒断言。
make GO=/Users/figo/sdk/go1.26.3/bin/go W0_ENV_FILE=.env.example w05-smoke

# 在上述断言前增加前端单测、构建和真实 Chromium Driver。
make GO=/Users/figo/sdk/go1.26.3/bin/go W0_ENV_FILE=.env.example w05-browser-smoke
```

W1 Skill Foundation、Reviewer 浏览器链和 Governor HTTP/数据库治理链使用同一完整门禁：

```bash
make GO=/Users/figo/sdk/go1.26.3/bin/go W0_ENV_FILE=.env.example w1-smoke
```

成功运行会在 `.local/smoke/w1-evidence-releases/<run_id>/` 生成权限为 `0600`、共用相同 run/source digest 的五份 Evidence，并仅通过 `.local/smoke/w1-evidence-releases/current.json` 的一次原子 rename 对外提交整组结果：

| 文件 | Schema | assertions |
| --- | --- | --- |
| `<run_id>/w1-skill-foundation-evidence.json` | Foundation canonical v3 | 47 项，其中 42 项布尔门禁 |
| `<run_id>/w1-skill-governance-evidence.json` | Governance v1 | 5 项布尔闭集 |
| `<run_id>/w1-skill-market-evidence.json` | Market v2 | 6 项布尔闭集 |
| `<run_id>/w1-skill-market-binding-evidence.json` | Public Market Binding v1 | 7 项布尔闭集 |
| `<run_id>/w1-skill-republish-session-isolation-evidence.json` | Republish Session Isolation v1 | 33 项布尔闭集 |

四个 sidecar 不扩容 Foundation canonical。消费者必须先校验 `current.json` 的 run/source digest、五个文件 SHA-256 和 Schema，再读取同一 release；固定顶层 Evidence 文件已废止。该命令已覆盖 Creator/Reviewer/Consumer Chromium 主链、Governor 暂停/恢复/offline、游客 Market 列表/详情、21 条 keyset、登录预选期间九类数据库事实零增量、跨 Owner Permission v2、治理锁竞争与陈旧选择失败关闭、真实 owner-private/public-market mixed、同一 Skill A→B 修订血缘、幂等重放与旧/新 Session 冻结；完整 `SMK-006` 仍待 Skill 测试调用与失败矩阵，也不表示治理页面、Graph Tool 可执行或完整 ADM-RBAC 已实现。

009 Down 不是无条件的注释回退：运维需先停止并 drain 新 QuickCreate v2 写入；Down 会在同一事务原子加锁复核，存在跨 Owner public-market Resolution 历史时以 SQLSTATE `55000` 拒绝。

Foundation Thrift 代码只能从 Business Owner 的单一 IDL 生成。修改 IDL 后执行：

```bash
make foundation-rpc-tools
make generate-foundation-rpc
make check-generated
```

独立 Module 校验：

```bash
make verify
make vet
make race
make build
make check-database-contracts
```

基础设施默认使用 PostgreSQL `15432`、Redis `16379`、etcd `12379`；Business HTTP 使用 `18081`，Agent HTTP 使用 `18082`，Business Foundation RPC 使用 `19081`，避免与历史 Demo 占用的标准端口冲突。W0.5 已覆盖受控断网、跨 Owner、Cursor reset 后完整回源与旧连接隔离；尚未完成的业务域和全功能冒烟口径以推进计划为准。

`.env.example` 只为本地 V1 试跑显式设置 `DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=true`。共享或生产环境必须保持关闭；该开关只开放 `plan_creation_spec.v1preview1` Draft 路径，不改变六 Tool 生产 Catalog 的 `unavailable` 结论。

通用首消息 Scheme A Development Preview 使用 `DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=true` 与固定 Profile `user_message.runtime.v2preview1`。它只允许 `DORA_ENV=local`，并与 `DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED` 互斥；启用时必须先把旧 Preview Processor 关闭，避免两个 source-filtered Claim loop 同时竞争 Session Lane。

方案 A 的 canonical 本地 Trial 使用同一个根命令：

```bash
# 只校验编排、真实浏览器用例、Feature Flag、Evidence 与 cleanup 的静态契约。
make test-user-message-runtime-smoke

# 重建并启动宿主机 Business/Agent/Vite，直连现有宿主机端口
# PostgreSQL 15432、Redis 16379、etcd 12379，然后执行真实 Chromium。
make GO=/Users/figo/sdk/go1.26.3/bin/go \
  W0_ENV_FILE=.env.example user-message-runtime-smoke
```

该命令不执行 Docker socket/Compose readiness 检查；它只通过宿主机端口等待现有中间件，强制覆盖为 `DORA_AGENT_PLAN_SPEC_PREVIEW_ENABLED=false`、`DORA_AGENT_USER_MESSAGE_RUNTIME_ENABLED=true`，并在成功时原子发布权限 `0600` 的 `.local/smoke/user-message-runtime-trial-evidence.json`。Evidence Schema 固定为 `user_message_runtime.trial_evidence.v1`，只含安全 ID、摘要、计数和布尔断言，不含登录信息、原始用户输入、Cookie、密钥、DSN 或密文。脚本成功与失败均有界停止自身启动的 Playwright、Vite、Agent 和 Business 进程，并要求 Business/Agent etcd service prefix 在执行期间持续只包含本轮精确实例、关闭后恢复为空。最终源码 Run `20260716T202111Z-58305` 已在专用 `dora_agent_test` 产生 32 项全真断言，Legacy Ledger 为 `verified / generation=1 / version=3`；Evidence source digest 为 `sha256:7b11d556defb379de05a04ff4e9b808618b784abc0244b3e75ceaeb9044d79ad`，文件 SHA-256 为 `sha256:7d2b1c0d0db69695e4eea2f54e1ffcacb6f01294fcf116f3ce8c1c312f363ab1`。

Analyze Materials 单 Tool Development Preview 使用独立命令：

```bash
# 静态校验：直连端口、Go Helper、互斥开关、Vite/Chromium、Evidence 与 cleanup。
make test-analyze-materials-runtime-smoke

# 重建宿主机 Runtime，直连现有 PostgreSQL/Redis/etcd 端口并执行 Chromium。
make GO=/Users/figo/sdk/go1.26.3/bin/go \
  W0_ENV_FILE=.env.example analyze-materials-runtime-smoke
```

该命令不调用 Docker CLI、Socket、Compose、`psql` 或 `redis-cli`；数据库/Redis 权威断言由 `localsmoke` Go Helper 完成。最终 Run `20260716T215049Z-39824` 的 22/22 断言均为真，source digest 为 `sha256:1f853003f9b21c8514a8178aa1e65986cecb5973f1ee07c840702388167a96a3`，Evidence SHA-256 为 `sha256:d7e5c0e4a475f2c918195e32114b7f53df83504eedb81982d6a8380571dcdff0`，文件位于 `.local/smoke/analyze-materials-runtime-v2.json` 且权限为 `0600`。

Plan Storyboard 单 Tool Development Preview 使用独立命令：

```bash
# 静态校验：直连端口、互斥 Profile、Chromium、Evidence、源码零漂移与 cleanup。
make test-plan-storyboard-runtime-smoke

# 重建宿主机 Runtime，直连现有 PostgreSQL/Redis/etcd 端口并执行 Chromium。
make GO=/Users/figo/sdk/go1.26.3/bin/go \
  W0_ENV_FILE=.env.example plan-storyboard-runtime-smoke
```

该命令不调用 Docker CLI、Socket、Compose、`psql`、`redis-cli` 或 `etcdctl`。它先以 Storyboard Profile 创建空 Session Lane，再独占切换 CreationSpec Profile 准备可信 Draft，最后切回 exact-loopback Storyboard Profile，由 Chromium 验证表单、accepted/terminal SSE、Card、硬刷新和 Agent 受控重启恢复。最终源码 Run `20260717T010209Z-81125` 的 16/16 断言均为真，source digest 为 `sha256:3e7f04b585d6001ec6de341293f25a8e83256dfc8a370022f5240e3e0460d9f5`，Evidence SHA-256 为 `sha256:f2d915247536c6f0fd18417225194f0f5592d88519d7edeb518a0d3cf87d5fa7`，文件位于 `.local/smoke/plan-storyboard-runtime-v2.json` 且权限为 `0600`；静态生产 Catalog 仍保持 `unavailable`。
