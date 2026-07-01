# PR-0 开发准备与 CI Gate

状态：active  
owner：文档与契约责任域 / 测试与验收责任域 / 工程基础设施责任域  
更新时间：2026-07-01  
适用范围：M7 active 拆分完成后的开发准备、CI gate、本地验证入口和后续 PR-1 到 PR-5 开发准入  
相关代码路径：`scripts/validate-active-contracts.sh`、`scripts/validate-development-ci-gate.sh`、`scripts/validate-release-full-http-smoke.sh`、`scripts/validate-release-http-service-e2e.sh`、`scripts/validate-release-browser-smoke.sh`、`tests/e2e/http/**`、`tests/reports/release-http-service-e2e-report.md`、`tests/contract/validate_json_schema_contracts.py`、`requirements/contract-gates.txt`、`Makefile`、`.github/workflows/active-contract-gates.yml`
相关契约：`docs/active/contracts/pr-roadmap.md`、`docs/active/contracts/index.md`、`docs/active/technical/release-governance.md`

## 背景

PR-1 到 PR-5 active 契约、fixture、fake provider 和 release gate 已冻结。进入业务实现前，需要先把本地和 CI 的最小验证入口固定，避免后续开发继续依赖旧 M0-M6 历史脚本或 review 文档。

## 目标

- 固定当前 active 契约总验证命令。
- 固定 PR-0 开发准备总 gate。
- 让 PR-1 到 PR-5 后续实现都能复用同一 CI 入口。
- 明确 PR-2 / PR-3 / PR-4 本地真实 PostgreSQL dry-run / down-test 已归档，PR-5 本地 service-level PostgreSQL E2E、Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke、本地 Agent + Business 双服务 HTTP smoke 和本地真实浏览器前端联动 smoke 已归档，测试环境 HTTP 服务 E2E 自动化入口和报告模板已归档，完整测试环境执行与 `status: passed` 报告仍是后续 gate。

## 非目标

- 不实现 Agent Runtime、Tool、Marketplace 或页面业务逻辑。
- 不接入真实 provider 流量。
- 不把旧 `scripts/validate-m*.sh` 作为当前重构 gate。
- 不替代 test 环境对 PR-2 / PR-3 / PR-4 migration 的发布前重放。

## Gate 分层

| Gate | 命令 | 覆盖范围 | 阻断条件 |
| --- | --- | --- | --- |
| Active Contract Gate | `scripts/validate-active-contracts.sh` | PR-1 到 PR-5 validator、正式 JSON Schema fixture validator、OpenAPI 解析、migration static guard、active 文档状态扫描 | 任一契约、fixture、OpenAPI、migration 或状态残留失败 |
| Development CI Gate | `scripts/validate-development-ci-gate.sh` | Active Contract Gate、Go 测试、用户端测试/build、管理端测试/build | 任一基础测试或构建失败 |
| Release Full HTTP Smoke | `scripts/validate-release-full-http-smoke.sh` | 本地真实 `cmd/business` + `cmd/agent` 双进程、PostgreSQL、Redis、Business HTTP login、Business Kitex RPC、Agent HTTP run 和 AG-UI replay | 进程启动、健康检查、认证、RPC、RouterDecision 或事件回放失败 |
| Release Browser Smoke | `scripts/validate-release-browser-smoke.sh` | 本地 Chrome + Vite preview 覆盖用户端 Skill 市场、创作者提交和管理端结算治理 | 前后台构建、真实 DOM、fetch 请求或幂等 key 断言失败 |
| Release HTTP Service E2E | `scripts/validate-release-http-service-e2e.sh` | 已部署测试环境的 Business / Agent health、ready、登录、Agent session/run、AG-UI replay 和报告写入 | 测试环境健康检查、认证、RouterDecision、事件回放失败，或报告不是 `status: passed` |
| Makefile 快捷入口 | `make active-contract-gate`、`make development-ci-gate`、`make release-full-http-smoke`、`make release-browser-smoke`、`make release-http-service-e2e` | 本地和测试环境快速验证；`release-http-service-e2e` 需要测试环境 URL | 同对应脚本 |
| GitHub Actions | `.github/workflows/active-contract-gates.yml` | PR 和 main/master push | 任一 job 失败 |

## 后续开发顺序

```text
PR-0 开发准备与 CI Gate
  -> PR-1 Contract Runtime 基础实现
  -> PR-2 Agent Runtime / Board / Graph
  -> PR-3 Tool / Credit / Asset
  -> PR-4 Marketplace / SkillUsage / Settlement
  -> PR-5 E2E / Fake Provider / Release Gate
```

## 责任域

| 责任域 | PR-0 职责 |
| --- | --- |
| 文档与契约责任域 | 维护 active 入口、契约索引和 gate 文档 |
| 测试与验收责任域 | 维护 contract validator、fixture 和 E2E gate |
| Agent Runtime | 保持 Go 测试通过，后续 PR-1 / PR-2 消费契约 |
| Business Service | 保持 Go 测试通过，后续 PR-3 / PR-4 消费契约 |
| 前端 / 管理端 | 保持测试和 build 通过，后续按 API / AG-UI 契约开发 |
| 运维发布责任域 | 维护 GitHub Actions、feature flag、release gate 和回滚 gate |

## 开发注意事项

1. 新实现不得读取 review 文档作为字段级事实源。
2. 涉及字段、状态、错误码、事件或 SQL 时，先更新 canonical contract 和 fixture。
3. PR-1 到 PR-5 后续实现必须保留 `scripts/validate-active-contracts.sh` 通过。
4. PR-2 / PR-3 / PR-4 真实 PostgreSQL dry-run / down-test 证据已归档；发布前仍需在 test 环境重放。
5. PR-5 完整测试环境 HTTP 服务 E2E 执行并归档 `status: passed` 报告前，禁止真实 provider 流量。

## Done Gate

- [x] Active Contract Gate 脚本存在。
- [x] Development CI Gate 脚本存在。
- [x] Release Full HTTP Smoke 脚本存在。
- [x] Release Browser Smoke 脚本存在。
- [x] Release HTTP Service E2E 脚本存在。
- [x] 正式 JSON Schema fixture validator 接入 Active Contract Gate 和 CI。
- [x] Makefile 快捷入口存在。
- [x] GitHub Actions workflow 存在。
- [x] `scripts/validate-active-contracts.sh` 本地通过。
- [x] `scripts/validate-development-ci-gate.sh` 本地通过。
- [ ] GitHub Actions 在远端 PR 中通过。
- [x] PR-2 / PR-3 / PR-4 真实 PostgreSQL dry-run / down-test 证据已由 repository integration tests 归档。
- [x] PR-5 本地 service-level PostgreSQL E2E 证据已由 `services/business/internal/e2e/release` 归档。
- [x] PR-5 本地 Agent HTTP router + Redis container E2E 证据已由 `services/agent/internal/e2e/release` 归档。
- [x] PR-5 本地 Agent 独立进程 HTTP smoke 证据已由 `services/agent/internal/e2e/release` 归档。
- [x] PR-5 本地 Business 独立进程 HTTP smoke 证据已由 `services/business/internal/e2e/release` 归档。
- [x] PR-5 本地 Agent + Business 双服务 HTTP smoke 证据已由 `services/agent/internal/e2e/release` 归档。
- [x] PR-5 本地真实浏览器前端联动 smoke 证据归档。
- [x] PR-5 测试环境 HTTP 服务 E2E 自动化入口和报告模板归档。
- [ ] PR-5 完整测试环境 HTTP 服务 E2E 执行证据和 `status: passed` 报告归档。

## 本地验证记录

2026-07-01 已执行：

```bash
scripts/validate-active-contracts.sh
scripts/validate-development-ci-gate.sh
scripts/validate-release-full-http-smoke.sh
scripts/validate-release-browser-smoke.sh
python3 tests/contract/validate_json_schema_contracts.py
make active-contract-gate
make development-ci-gate
make release-full-http-smoke
make release-browser-smoke
go test ./services/agent/internal/infra/repository
go test ./services/business/internal/infra/repository/businesscore
go test ./services/business/internal/e2e/release
go test ./services/agent/internal/e2e/release
go test ./services/agent/internal/e2e/release -run TestReleaseFullHTTPServiceE2ESmoke -count=1 -v
go test ./services/agent/internal/e2e/release -run TestReleaseAgentIndependentProcessHTTPSmoke -count=1 -v
go test ./services/business/internal/e2e/release -run TestReleaseBusinessIndependentProcessHTTPSmoke -count=1 -v
```

结果：

```text
active contract gate passed
development CI gate passed
release browser smoke passed
release full HTTP service smoke passed
json schema contract validation ok
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository
ok github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore
ok github.com/FigoGoo/Dora-Agent/services/business/internal/e2e/release
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/e2e/release
PASS TestReleaseFullHTTPServiceE2ESmoke
PASS TestReleaseAgentIndependentProcessHTTPSmoke
PASS TestReleaseBusinessIndependentProcessHTTPSmoke
```

完整测试环境 HTTP 服务 E2E 未纳入默认本地验证记录，执行时需要：

```bash
RELEASE_BUSINESS_BASE_URL=https://business.test.example.com \
RELEASE_AGENT_BASE_URL=https://agent.test.example.com \
make release-http-service-e2e
```

默认报告路径为 `tests/reports/release-http-service-e2e-report.md`；当前状态为 `pending_environment`。

## 风险

| 风险 | 影响 | 缓解方式 |
| --- | --- | --- |
| 旧 M0-M6 脚本仍被误用 | 重新引入旧事实源和旧状态口径 | 当前 gate 明确为 active contract、development CI、release smoke 和 release HTTP service E2E |
| CI runner 工具链与本地不同 | PR 在远端失败 | workflow 固定 Python、Go、Node、pnpm 安装入口 |
| test 环境尚未重放 migration gate | 发布环境兼容问题延后暴露 | 发布前必须重放 PR-2 / PR-3 / PR-4 dry-run / down-test |
| fake provider 与真实 provider 差异 | 真实流量失败率上升 | 真实 provider 只允许在 PR-5 完整测试环境 HTTP 服务 E2E gate 通过并归档报告后灰度 |
