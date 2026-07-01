# PR-5 E2E / Fake Provider / Release Gate 基础实现

状态：active  
owner：测试与验收责任域 / Agent Runtime / Business Service / 前端 / 管理端 / 运维发布责任域 / 文档与契约责任域  
更新时间：2026-07-01  
适用范围：PR-5 测试事实源、Fake Provider、E2E suite index、fixture、service-level PostgreSQL E2E、Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke、本地 Agent + Business 双服务 HTTP smoke、本地真实浏览器前端联动 smoke、feature flag、观测和回滚 gate 的基础校验
相关代码路径：`internal/contracts/releasegate/**`、`services/business/internal/e2e/release/**`、`services/agent/internal/e2e/release/**`、`internal/testredis/**`、`services/agent/internal/events/stream/**`、`services/business/internal/infra/repository/businesscore/marketplace.go`、`scripts/validate-release-full-http-smoke.sh`、`tests/e2e/browser/**`
相关契约：`docs/active/contracts/pr-5-e2e-fixtures-release-gates.md`、`tests/e2e/**`、`tests/fixtures/e2e/**`、`docs/active/technical/release-governance.md`

## 背景

PR-5 不新增业务字段，而是把 PR-1 到 PR-4 的契约串成可回放、可验证、可发布、可回滚的端到端事实源。进入完整测试环境 HTTP 服务 E2E 前，需要先让 fake provider 行为、suite index、fixture、release governance、本地 service-level PostgreSQL 主路径、Redis 容器事件语义、Agent / Business main 进程启动、本地双服务 HTTP 联通以及前后台真实浏览器联动具备漂移防护。

## 目标

- 提供 Fake Provider manifest 和 provider scenarios 校验。
- 提供 Fake Provider 场景执行器，验证 deterministic、async、partial、retry、terminal failure、slow callback 的幂等执行结果。
- 提供 E2E suite index 和 required fixture 覆盖校验。
- 提供 E2E fixture 依赖、用户旅程、业务状态、fake provider 行为和 release gate 校验。
- 提供 service-level E2E harness，串联 PR-2/PR-3/PR-4 migration、ToolPlan、Credit、Asset、Marketplace、SkillUsage、Settlement、listing suspended 和 provider replay 主路径。
- 提供 Agent HTTP router + Redis container E2E harness，验证 `/healthz`、`/readyz`、AG-UI replay、Redis stream dedupe、snapshot cache 和 turn lock。
- 提供 Agent 独立进程 HTTP smoke，验证真实 `cmd/agent` 二进制在 local model adapter、PostgreSQL 和 Redis runtime 下可启动并通过健康检查。
- 提供 Business 独立进程 HTTP smoke，验证真实 `cmd/business` 二进制在 PostgreSQL、Kitex 和 HTTP server 下可启动并通过健康检查。
- 提供本地 Agent + Business 双服务 HTTP smoke，验证 Business HTTP 登录、Agent 通过 Kitex 调 Business RPC、Agent HTTP session/run、RouterDecision 和 AG-UI replay。
- 提供本地真实浏览器前端联动 smoke，验证用户端 Skill 市场安装、创作者 Skill 草稿提交、管理端 settlement hold 释放和内部出账确认的 DOM / fetch / 幂等 key。
- 提供 release governance 文本 gate 校验，覆盖 feature flag、观测指标和 rollback token。

## 非目标

- 不连接真实 provider。
- 不替代完整测试环境 HTTP 服务 E2E。
- 不新增 PR-2 到 PR-4 之外的业务字段。
- 不把人工验收作为唯一 release gate。

## 实现范围

| 类型 | 文件 | 说明 |
| --- | --- | --- |
| Fake Provider | `internal/contracts/releasegate/fake_provider.go` | provider 行为、场景覆盖、场景执行和幂等规则校验 |
| E2E Gate | `internal/contracts/releasegate/e2e.go` | suite index、fixture、release governance 校验 |
| Tests | `internal/contracts/releasegate/*_test.go` | 读取 PR-5 active fixture 和 release 文档做漂移防护 |
| Service E2E | `services/business/internal/e2e/release/service_e2e_test.go` | 本地 PostgreSQL 中串联 agent/business migrations 与 PR-3/PR-4 仓储主路径 |
| Agent HTTP/Redis E2E | `services/agent/internal/e2e/release/agent_http_redis_e2e_test.go` | `httptest` 走真实 Agent HTTP router，Redis 使用 testcontainers 真容器 |
| Agent Process Smoke | `services/agent/internal/e2e/release/agent_process_smoke_test.go` | 构建并启动真实 `cmd/agent` 二进制，验证 Postgres + Redis + `/healthz` + `/readyz` |
| Business Process Smoke | `services/business/internal/e2e/release/business_process_smoke_test.go` | 构建并启动真实 `cmd/business` 二进制，验证 Postgres + Kitex + `/healthz` + `/readyz` |
| Full HTTP Service Smoke | `services/agent/internal/e2e/release/full_http_service_smoke_test.go`、`scripts/validate-release-full-http-smoke.sh` | 本地真实 `cmd/business` + `cmd/agent` 双进程，验证 Business HTTP login、Business Kitex RPC、Agent HTTP run 和 AG-UI replay |
| Browser Smoke | `scripts/validate-release-browser-smoke.sh`、`tests/e2e/browser/release-frontend-browser-smoke.mjs` | 构建用户端 / 管理端，Vite preview + 本地 Chrome 验证 PR-5 前后台联动主路径 |
| Marketplace Guard | `services/business/internal/infra/repository/businesscore/marketplace.go` | `MARKETPLACE_LISTING_SUSPENDED` 新安装守卫，已存在 installation 幂等重放不受影响 |

## 开发注意事项

1. PR-5 gate 只允许 fake provider 覆盖真实 provider 前置验证；真实 provider 流量必须等待后续完整测试环境 HTTP 服务 gate。
2. 所有 required E2E fixture 必须被 suite index 引用，且必须包含 `Fixture Gate`。
3. E2E fixture 的 `contract_references` 只能指向 `tests/fixtures/contracts/**`。
4. 发布必须默认关闭新 feature flag，并能独立回滚 `agent_runtime_v2`、`tool_generation_v2`、`marketplace_v2`。
5. 回滚必须保留 AG-UI replay、按 `dedupe_key` 去重、释放未完成冻结，并生成审计。

## Done Gate

- [x] `internal/contracts/releasegate` 包存在。
- [x] fake provider required behaviors 覆盖校验通过。
- [x] fake provider scenarios 可执行且幂等。
- [x] E2E suite index 覆盖 required fixtures 校验通过。
- [x] E2E fixtures 的依赖、用户旅程、业务状态和 release gate 校验通过。
- [x] service-level PostgreSQL E2E 串联 PR-2/PR-3/PR-4 主路径通过。
- [x] Agent HTTP router + Redis container E2E 串联 `/healthz`、`/readyz`、AG-UI replay、stream dedupe、cache 和 lock 通过。
- [x] Agent 独立进程 HTTP smoke 串联真实 `cmd/agent` 二进制、PostgreSQL、Redis、`/healthz` 和 `/readyz` 通过。
- [x] Business 独立进程 HTTP smoke 串联真实 `cmd/business` 二进制、PostgreSQL、Kitex、`/healthz` 和 `/readyz` 通过。
- [x] 本地 Agent + Business 双服务 HTTP smoke 串联 Business HTTP login、Business Kitex RPC、Agent HTTP run 和 AG-UI replay 通过。
- [x] 本地真实浏览器前端联动 smoke 串联用户端 Skill 市场、创作者提交、管理端 settlement release / payout 页面通过。
- [x] release governance feature flag、观测和回滚 token 校验通过。
- [ ] 后续接入完整测试环境 HTTP 服务 E2E 并归档测试报告。

## 验证命令

```bash
go test ./internal/contracts/releasegate
go test ./services/agent/internal/e2e/release ./services/agent/internal/events/stream ./internal/testredis
go test ./services/agent/internal/e2e/release -run TestPR5AgentIndependentProcessHTTPSmoke -count=1 -v
go test ./services/business/internal/e2e/release -run TestPR5BusinessIndependentProcessHTTPSmoke -count=1 -v
go test ./services/agent/internal/e2e/release -run TestPR5FullHTTPServiceE2ESmoke -count=1 -v
go test ./internal/contracts/releasegate ./services/business/internal/infra/repository/businesscore ./services/business/internal/e2e/release
go test ./internal/contracts/toolasset ./internal/contracts/skillmarket ./internal/contracts/releasegate ./services/business/internal/e2e/release
scripts/validate-release-full-http-smoke.sh
scripts/validate-release-browser-smoke.sh
make release-full-http-smoke
make release-browser-smoke
make active-contract-gate
make development-ci-gate
```

## 本地验证记录

2026-07-01 已执行：

```bash
go test ./internal/contracts/releasegate
go test ./services/agent/internal/e2e/release ./services/agent/internal/events/stream ./internal/testredis
go test ./services/agent/internal/e2e/release -run TestPR5AgentIndependentProcessHTTPSmoke -count=1 -v
go test ./services/business/internal/e2e/release -run TestPR5BusinessIndependentProcessHTTPSmoke -count=1 -v
go test ./services/agent/internal/e2e/release -run TestPR5FullHTTPServiceE2ESmoke -count=1 -v
go test ./internal/contracts/releasegate ./services/business/internal/infra/repository/businesscore ./services/business/internal/e2e/release
go test ./internal/contracts/toolasset ./internal/contracts/skillmarket ./internal/contracts/releasegate ./services/business/internal/e2e/release
scripts/validate-release-full-http-smoke.sh
scripts/validate-release-browser-smoke.sh
make release-full-http-smoke
make release-browser-smoke
make active-contract-gate
make development-ci-gate
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/internal/contracts/releasegate
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/e2e/release
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/events/stream
PASS TestPR5AgentIndependentProcessHTTPSmoke
PASS TestPR5BusinessIndependentProcessHTTPSmoke
PASS TestPR5FullHTTPServiceE2ESmoke
ok github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore
ok github.com/FigoGoo/Dora-Agent/services/business/internal/e2e/release
PR-5 full HTTP service smoke passed
PR-5 frontend browser smoke passed
active contract gate passed
PR-0 CI gate passed
```
