# M1 双服务基础设施验收报告

报告日期：2026-06-27

## 结论

M1 双服务基础设施基线通过。Agent 与 Business 服务骨架、配置加载、结构化日志、HTTP health、Business Kitex skeleton、Agent Runtime repository、Business 幂等与审计基础能力、PostgreSQL migration up/down 与 seed 集成测试均已真实执行。

本报告已按 M1 整改收口更新：业务幂等以 `tenant_id + scope + idempotency_key` 为唯一语义，审计模型按 `code-plan/business/02` 的 `audit_id/operator_type/operator_id/tenant_id/business_action/metadata_summary` 落库；Agent run/interrupt 状态机按 `code-plan/agent/03` 的 `waiting_confirmation/resuming/cancelled` 与 `required/accepted/rejected/expired/resolved` 落实现。

本次继续补齐 M1 语义门禁：Agent 安全证据结果只接受 `passed/blocked/failed`，拒绝旧 `allow`；业务 request hash 使用 canonical JSON，忽略 `request_id/trace_id/X-Client-Request-ID` 等观测字段并纳入 tenant/space/actor 上下文；幂等 Begin 覆盖并发同 key 场景，唯一冲突收敛为 `processing/replay/conflict` 语义。

M1 未实现登录、空间、项目、资产、积分、Skill、Tool、Agent Runtime 主链路、SSE/AG-UI publisher、RPC client 等 M2+ 功能；这些 RPC/endpoint 只验证为可注册、可构造并返回明确 `NOT_IMPLEMENTED`，不计为业务功能通过。

## 执行环境

| 项目 | 结果 |
| --- | --- |
| Go | `go version go1.26.3 darwin/arm64` |
| thriftgo | `thriftgo 0.4.5` |
| kitex | `v0.16.2` |
| Docker | Testcontainers PostgreSQL 集成测试已执行通过 |

## 执行命令

```bash
export GOROOT=/Users/figo/sdk/go1.26.3
export GOPATH=/Users/figo/go
export PATH="$GOROOT/bin:$GOPATH/bin:$PATH"

go mod tidy
go test ./...
scripts/validate-engineering-baseline.sh
```

## 验收结果

| 检查项 | 状态 | 说明 |
| --- | --- | --- |
| M0 基线 | passed | `scripts/validate-engineering-baseline.sh` 已先执行 `scripts/validate-toolchain-contract-baseline.sh`。 |
| Go toolchain | passed | 固定 `go1.26.3 darwin/arm64`。 |
| gofmt dry check | passed | `services internal tests` 下 Go 文件无未格式化项。 |
| Go 全量测试 | passed | `go test ./...` 通过。 |
| SQL up/down 成对 | passed | `db/migrations/iterations` 下 13 组 up/down 成对。 |
| 外键约束扫描 | passed | `db/migrations api code-plan services internal` 未命中阻断关键词。 |
| 配置键覆盖 | passed | Agent 23 个、Business 33 个 M1 服务配置键均被非测试源文件读取或使用。 |
| M1 语义对齐 | passed | `scripts/validate-engineering-baseline.sh` 校验业务幂等 tenant 唯一键、canonical request hash、并发同 key 测试、审计模型字段、Agent 状态机和安全证据枚举。 |
| Agent HTTP health | passed | `/healthz`、`/readyz` ready/unready 单元测试通过。 |
| Business HTTP health | passed | `/healthz`、`/readyz` ready/unready 和 trace header 单元测试通过。 |
| Agent 日志 | passed | JSON 输出包含 `service`、`env`、`trace_id`，敏感字段脱敏。 |
| Business 日志 | passed | JSON 输出包含 `service`、`env`、`trace_id`，敏感字段脱敏。 |
| Business Kitex skeleton | passed | 9 个 M0 service 全部注册；未实现 handler 返回 `NOT_IMPLEMENTED`。 |
| Agent DB 集成 | passed | Testcontainers PostgreSQL 执行 migration up/down、核心表存在、无外键、session/run/event/interrupt/artifact/safety/runtime config repository 场景通过；覆盖 run 等待确认、恢复、取消、interrupt accepted/resolved、旧安全结果拒绝。 |
| Business DB 集成 | passed | Testcontainers PostgreSQL 执行 migration up/down、seed、幂等 proceed/replay/conflict/processing、跨 tenant 同 key 互不影响、并发同 key 不泄漏唯一约束错误、audit 写入通过。 |
| 边界检查 | passed | Agent 测试库不含业务事实表；Business 测试库不含 Agent runtime 表。 |

## 未执行项

| 项目 | 原因 | 影响 | Owner |
| --- | --- | --- | --- |
| 真实 etcd 注册联调 | M1 测试环境允许 `KITEX_REGISTRY=none`，不依赖本机 etcd。 | 不影响 M1 本地基线；服务联调阶段需接入真实 etcd。 | 主控 Codex / 后端服务 owner |
| M2+ 业务功能验收 | 不属于 M1 范围。 | 后续 M2/B1/B2/B3+ 切片按功能单独验收。 | 对应业务切片 owner |
| Agent Runtime 主链路验收 | 不属于 M1 范围。 | 后续 A3-A8 切片按 Agent 任务链路验收。 | Go Eino 智能体微服务架构工程师 |

## 阻塞问题

无。

## 后续使用方式

- M2 可直接消费 `services/business` 的配置、日志、HTTP health、幂等、审计和 DB 基线。
- A3/A5 可直接消费 `services/agent` 的配置、日志、HTTP health、Agent Runtime repository 与 Business Kitex 生成代码。
- B1/B2 可从 `services/business/internal/transport/rpc` 的 `NOT_IMPLEMENTED` skeleton 替换具体 handler/application 实现。
- 后续修改 migration、配置键、repository 或契约时必须重新执行 `scripts/validate-engineering-baseline.sh`。
