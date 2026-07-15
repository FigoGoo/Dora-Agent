# Dora Agent

Dora 是面向桌面 Web 的 Skill 驱动 AIGC Agent 平台。本仓库正在从历史 Demo 迁移为 `business`、`agent`、`worker` 三个独立 Go Module 的生产版工程。

## 当前仓库状态

当前 `master` 是重构基线，不是可运行的旧后端 Demo。

| 范围 | 当前事实 | 下一门槛 |
| --- | --- | --- |
| 前端 | `frontend/` 已接入真实登录/退出、QuickCreate、Project Workspace、Snapshot→SSE、Owner Skill Builder、Reviewer 管理入口、最多 16 项 Published+Active Skill 选择、匿名 Skill 市场列表/详情，以及跨发布者 Market 预选与登录恢复；W0.5/W1 Chromium 主路径通过 | 继续推进治理页面、Executable Tool Registry 与后续业务纵切 |
| Business Service | `business/` 已具备真实 Auth/Session、Project QuickCreate/Bootstrap、Skill 草稿/审核/不可变发布、Reviewer/Governor 动态权限、治理状态机、匿名 Market 白名单读取、Public Market Permission v2 跨 Owner Binding、加密 Outbox/Dispatcher、Agent Workspace BFF 与内部身份签名 | 完整管理员角色/数据范围 RBAC 仍留在 W5；下一业务门槛是 W2 Agent Runtime/Graph Tool |
| Agent Service | `agent/` 已具备 Session/Input/Event Transport、幂等 Ensure RPC、Workspace Snapshot、PostgreSQL 补读优先 SSE、内部身份验签和 Nonce 防重放；尚未注册六个 Graph Tool | 完成对应中文设计评审后，从 Agent Runtime/首个 Graph Tool 纵切推进 |
| Business Worker | `worker/` 已可独立构建，具备依赖探针、执行器资源边界、健康检查与优雅退出；尚未消费 Job | Job/Event/Finalize 契约评审通过后实现 Claim/Lease/Heartbeat |
| 本地基础设施 | `go.work`、三 Schema Migration、PostgreSQL/Redis/etcd Compose、跨服务 Probe、Schema Catalog 契约测试，以及 W0/W0.5/W1 真实 API/浏览器 Smoke 已建立；W1 同次发布 Foundation canonical 与 Governance、Market、Public Market Binding 三个独立 sidecar，共四份 exact-set Evidence | 把脱敏 Evidence 与故障注入矩阵纳入持续 CI |

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

任何 Tool 开发前都必须先完成对应的 `docs/design/agent/graphtool/<tool_key>-design.md` 中文设计并通过评审。

## 开发入口

- [项目开发计划（Canonical，当前状态与唯一执行顺序）](docs/requirements/project-development-plan.md)
- [项目协作指引](AGENTS.md)
- [服务端开发规范 Skill](.agents/skills/dora-server-development/SKILL.md)
- [用户端需求总览](docs/requirements/user-requirements-overview.md)
- [管理端需求总览](docs/requirements/admin-requirements-overview.md)
- [服务端需求总览](docs/requirements/server-requirements-overview.md)
- [Graph Tool 功能需求总览](docs/requirements/graph-tool-requirements-overview.md)
- [支付与积分充值需求总览](docs/requirements/payment-requirements-overview.md)
- [共通业务规则与验收基线](docs/requirements/common-requirements-baseline.md)
- [全功能冒烟开发推进计划（详细里程碑、SMK-P0 与长期 backlog）](docs/requirements/full-function-smoke-development-plan.md)
- [Graph Tool 详细设计索引](docs/design/agent/graphtool/README.md)
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

成功运行会同时发布权限为 `0600`、共用相同 run/source digest 的四份 Evidence：

| 文件 | Schema | assertions |
| --- | --- | --- |
| `.local/smoke/w1-skill-foundation-evidence.json` | Foundation canonical v3 | 47 项，其中 42 项布尔门禁 |
| `.local/smoke/w1-skill-governance-evidence.json` | Governance v1 | 5 项布尔闭集 |
| `.local/smoke/w1-skill-market-evidence.json` | Market v2 | 6 项布尔闭集 |
| `.local/smoke/w1-skill-market-binding-evidence.json` | Public Market Binding v1 | 7 项布尔闭集 |

三个 sidecar 不扩容 Foundation canonical。该命令已覆盖 Creator/Reviewer/Consumer Chromium 主链、Governor 暂停/恢复/offline、游客 Market 列表/详情、21 条 keyset、登录预选期间九类数据库事实零增量、跨 Owner Permission v2、治理锁竞争与陈旧选择失败关闭、真实 owner-private/public-market mixed、幂等重放与旧 Session 冻结；不表示治理页面、Graph Tool 可执行或完整 ADM-RBAC 已实现。

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
