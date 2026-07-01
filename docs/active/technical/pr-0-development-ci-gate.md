# PR-0 开发准备与 CI Gate

状态：active  
owner：文档与契约责任域 / 测试与验收责任域 / 工程基础设施责任域  
更新时间：2026-07-01  
适用范围：M7 active 拆分完成后的开发准备、CI gate、本地验证入口和后续 PR-1 到 PR-5 开发准入  
相关代码路径：`scripts/validate-active-contracts.sh`、`scripts/validate-pr0-ci-gate.sh`、`scripts/validate-pr5-browser-smoke.sh`、`Makefile`、`.github/workflows/active-contract-gates.yml`
相关契约：`docs/active/contracts/pr-roadmap.md`、`docs/active/contracts/index.md`、`docs/active/technical/release-governance.md`

## 背景

PR-1 到 PR-5 active 契约、fixture、fake provider 和 release gate 已冻结。进入业务实现前，需要先把本地和 CI 的最小验证入口固定，避免后续开发继续依赖旧 M0-M6 历史脚本或 review 文档。

## 目标

- 固定当前 active 契约总验证命令。
- 固定 PR-0 开发准备总 gate。
- 让 PR-1 到 PR-5 后续实现都能复用同一 CI 入口。
- 明确 PR-2 / PR-3 / PR-4 本地真实 PostgreSQL dry-run / down-test 已归档，PR-5 本地 service-level PostgreSQL E2E、Agent HTTP router + Redis container E2E、Agent / Business 独立进程 HTTP smoke 和本地真实浏览器前端联动 smoke 已归档，完整测试环境 HTTP 服务 E2E 仍是后续 gate。

## 非目标

- 不实现 Agent Runtime、Tool、Marketplace 或页面业务逻辑。
- 不接入真实 provider 流量。
- 不把旧 `scripts/validate-m*.sh` 作为当前重构 gate。
- 不替代 test 环境对 PR-2 / PR-3 / PR-4 migration 的发布前重放。

## Gate 分层

| Gate | 命令 | 覆盖范围 | 阻断条件 |
| --- | --- | --- | --- |
| Active Contract Gate | `scripts/validate-active-contracts.sh` | PR-1 到 PR-5 validator、OpenAPI 解析、migration static guard、active 文档状态扫描 | 任一契约、fixture、OpenAPI、migration 或状态残留失败 |
| PR-0 CI Gate | `scripts/validate-pr0-ci-gate.sh` | Active Contract Gate、Go 测试、用户端测试/build、管理端测试/build | 任一基础测试或构建失败 |
| PR-5 Browser Smoke | `scripts/validate-pr5-browser-smoke.sh` | 本地 Chrome + Vite preview 覆盖用户端 Skill 市场、创作者提交和管理端结算治理 | 前后台构建、真实 DOM、fetch 请求或幂等 key 断言失败 |
| Makefile 快捷入口 | `make active-contract-gate`、`make pr0-ci-gate`、`make pr5-browser-smoke` | 本地开发快速验证 | 同对应脚本 |
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
5. PR-5 完整测试环境 HTTP 服务 E2E 环境执行前，禁止真实 provider 流量。

## Done Gate

- [x] Active Contract Gate 脚本存在。
- [x] PR-0 CI Gate 脚本存在。
- [x] PR-5 Browser Smoke 脚本存在。
- [x] Makefile 快捷入口存在。
- [x] GitHub Actions workflow 存在。
- [x] `scripts/validate-active-contracts.sh` 本地通过。
- [x] `scripts/validate-pr0-ci-gate.sh` 本地通过。
- [ ] GitHub Actions 在远端 PR 中通过。
- [x] PR-2 / PR-3 / PR-4 真实 PostgreSQL dry-run / down-test 证据已由 repository integration tests 归档。
- [x] PR-5 本地 service-level PostgreSQL E2E 证据已由 `services/business/internal/e2e/pr5` 归档。
- [x] PR-5 本地 Agent HTTP router + Redis container E2E 证据已由 `services/agent/internal/e2e/pr5` 归档。
- [x] PR-5 本地 Agent 独立进程 HTTP smoke 证据已由 `services/agent/internal/e2e/pr5` 归档。
- [x] PR-5 本地 Business 独立进程 HTTP smoke 证据已由 `services/business/internal/e2e/pr5` 归档。
- [x] PR-5 本地真实浏览器前端联动 smoke 证据归档。
- [ ] PR-5 完整测试环境 HTTP 服务 E2E 证据归档。

## 本地验证记录

2026-07-01 已执行：

```bash
scripts/validate-active-contracts.sh
scripts/validate-pr0-ci-gate.sh
scripts/validate-pr5-browser-smoke.sh
make active-contract-gate
make pr0-ci-gate
make pr5-browser-smoke
go test ./services/agent/internal/infra/repository
go test ./services/business/internal/infra/repository/businesscore
go test ./services/business/internal/e2e/pr5
go test ./services/agent/internal/e2e/pr5
go test ./services/agent/internal/e2e/pr5 -run TestPR5AgentIndependentProcessHTTPSmoke -count=1 -v
go test ./services/business/internal/e2e/pr5 -run TestPR5BusinessIndependentProcessHTTPSmoke -count=1 -v
```

结果：

```text
active contract gate passed
PR-0 CI gate passed
PR-5 frontend browser smoke passed
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository
ok github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore
ok github.com/FigoGoo/Dora-Agent/services/business/internal/e2e/pr5
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/e2e/pr5
PASS TestPR5AgentIndependentProcessHTTPSmoke
PASS TestPR5BusinessIndependentProcessHTTPSmoke
```

## 风险

| 风险 | 影响 | 缓解方式 |
| --- | --- | --- |
| 旧 M0-M6 脚本仍被误用 | 重新引入旧事实源和旧状态口径 | PR-0 明确当前 gate 为 active contract / PR-0 CI gate |
| CI runner 工具链与本地不同 | PR 在远端失败 | workflow 固定 Python、Go、Node、pnpm 安装入口 |
| test 环境尚未重放 migration gate | 发布环境兼容问题延后暴露 | 发布前必须重放 PR-2 / PR-3 / PR-4 dry-run / down-test |
| fake provider 与真实 provider 差异 | 真实流量失败率上升 | 真实 provider 只允许在 PR-5 完整测试环境 HTTP 服务 E2E gate 后灰度 |
