# M2 身份项目能力技术基线报告

状态：已通过 `scripts/validate-m2.sh` 真实验证
日期：2026-06-27
范围：业务 03/04/05，Agent 04/05/07；不含前端和部署上线文档。

## 已执行验证

- `scripts/validate-m0.sh`：由 `scripts/validate-m2.sh` 串行执行。
- `scripts/validate-m1.sh`：由 `scripts/validate-m2.sh` 串行执行。
- `go test -count=1 ./...`：由 `scripts/validate-m2.sh` 串行执行。
- `scripts/validate-m2.sh`：M2 冻结门禁入口。
- `rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan services internal`：由 `scripts/validate-m2.sh` 串行执行。

## 验收覆盖

- 业务 HTTP：登录、当前空间、身份切换、企业创建和成员列表、项目创建/更新/归档、项目封面归属校验、`base_updated_at` 冲突、管理员登录/强制轮换/用户禁用。
- 业务 RPC：`ResolveCurrentSpaceContext`、`CheckProjectAccess`，含跨空间、项目归档、项目只读和禁用用户错误。
- Agent API：session 创建、run 创建、SSE stream、追加输入、interrupt accept/reject、同 session active run 冲突、取消、事件 replay、归档只读 snapshot。
- 契约一致性：`scripts/validate-m2.sh` 校验 Agent OpenAPI 与 Gin route parity、Business Project PATCH OpenAPI 与 Gin route parity。
- DB 边界：业务 DB 与 Agent DB 仍由各自 Testcontainers migration 测试验证。
- 幂等/审计：业务写操作通过 `Idempotency-Key`、request hash 和审计表写入测试覆盖。

## 未执行项

未执行项：无。

## 阻塞问题

无。
