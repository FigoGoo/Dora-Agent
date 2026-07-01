# Active 测试事实源入口

状态：active  
owner：测试与验收责任域  
更新时间：2026-07-01  
适用范围：Contract-first active 化后的 fixture、contract test、E2E 和发布验收导航

## 当前 Fixture

| 类型 | 路径 | 批次 |
| --- | --- | --- |
| Router fixtures | `tests/fixtures/contracts/router/**` | PR-1 |
| AG-UI fixtures | `tests/fixtures/contracts/agui/**` | PR-1 |
| Board fixtures | `tests/fixtures/contracts/board/**` | PR-2 |
| Graph fixtures | `tests/fixtures/contracts/graph/**` | PR-2 |
| ToolPlan fixtures | `tests/fixtures/contracts/toolplan/**` | PR-3 |
| Credit fixtures | `tests/fixtures/contracts/credit/**` | PR-3 |
| Asset fixtures | `tests/fixtures/contracts/asset/**` | PR-3 |
| Marketplace fixtures | `tests/fixtures/contracts/marketplace/**` | PR-4 |
| Billing fixtures | `tests/fixtures/contracts/billing/**` | PR-4 |
| Fake provider E2E | `tests/e2e/fake-provider/**` | PR-5 |
| E2E fixtures | `tests/fixtures/e2e/**` | PR-5 |
| Full HTTP service smoke | `services/agent/internal/e2e/release/full_http_service_smoke_test.go`、`scripts/validate-release-full-http-smoke.sh` | PR-5 |
| Browser smoke | `tests/e2e/browser/**` | PR-5 |
| JSON Schema validator | `tests/contract/validate_json_schema_contracts.py` | PR-1 ~ PR-5 |

## Gate

PR-0 后，本地和 CI 的默认验证入口：

```text
make active-contract-gate
make development-ci-gate
make release-full-http-smoke
make release-browser-smoke
```

后续进入真实 DB / E2E 前仍必须补充：

- PR-2 / PR-3 / PR-4 已完成本地真实 PostgreSQL dry-run / down-test；发布前仍需在 test 环境重放。
- PR-5 已完成本地 service-level PostgreSQL E2E：`go test ./services/business/internal/e2e/release`。
- PR-5 已完成本地 Agent HTTP router + Redis container E2E：`go test ./services/agent/internal/e2e/release`。
- PR-5 已完成本地 Agent 独立进程 HTTP smoke：`go test ./services/agent/internal/e2e/release -run TestPR5AgentIndependentProcessHTTPSmoke -count=1 -v`。
- PR-5 已完成本地 Business 独立进程 HTTP smoke：`go test ./services/business/internal/e2e/release -run TestPR5BusinessIndependentProcessHTTPSmoke -count=1 -v`。
- PR-5 已完成本地 Agent + Business 双服务 HTTP smoke：`scripts/validate-release-full-http-smoke.sh` 或 `make release-full-http-smoke`。
- PR-5 已完成本地真实浏览器前端联动 smoke：`scripts/validate-release-browser-smoke.sh` 或 `make release-browser-smoke`。
- PR-5 完整测试环境 HTTP 服务 E2E 执行和测试报告仍是发布前 gate。
