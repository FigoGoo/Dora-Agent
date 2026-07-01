# PR-1 Contract Runtime 基础实现

状态：active  
owner：Agent Runtime / Business Service / 文档与契约责任域 / 测试与验收责任域  
更新时间：2026-07-01  
适用范围：PR-1 字段级契约在 Go 运行时的共享类型、校验、digest、状态枚举、RouterDecision 和 AG-UI Envelope  
相关代码路径：`internal/contracts/pr1/**`  
相关契约：`docs/active/contracts/pr-1-contract-index-state-router-agui.md`、`api/schemas/common/**`、`api/schemas/router/**`、`api/agui/**`

## 背景

PR-1 已冻结 Contract Index、StateEnum、RouterDecision 和 AG-UI Envelope。进入业务实现前，需要先提供 Go 运行时可复用的基础契约包，让后续 Agent Runtime、业务服务 adapter 和测试不再散落手写字段。

## 目标

- 提供 digest 和 ID 的基础校验。
- 提供 PR-1 StateEnumRegistry 的 Go 常量与校验。
- 提供 RouterDecision Go 类型和守卫校验。
- 提供 AG-UI Envelope Go 类型、构造函数、`dedupe_key` 规则和 fixture 序列校验。
- 使用 active schema / fixture 回归测试防止实现漂移。

## 非目标

- 不实现 Board / Graph / Tool / Marketplace 业务流程。
- 不改写旧工作台事件流的全部输出。
- 不生成最终 OpenAPI / Thrift 代码。
- 不接入真实 provider、Redis worker 或数据库迁移执行。

## 实现范围

| 类型 | 文件 | 说明 |
| --- | --- | --- |
| Common | `internal/contracts/pr1/common.go` | `sha256:` digest、canonical digest、ID 校验 |
| State | `internal/contracts/pr1/state.go` | PR-1 状态枚举和 `IsValidState` |
| Router | `internal/contracts/pr1/router.go` | RouterDecision 类型和 `ValidateRouterDecision` |
| AG-UI | `internal/contracts/pr1/agui.go` | AG-UI Envelope 类型、构造、`ValidateAGUIEnvelope`、`ValidateAGUISequence` |
| Tests | `internal/contracts/pr1/*_test.go` | 读取 active schema / fixtures 做漂移防护 |

## 开发注意事项

1. 后续 PR 不得在业务代码中重新定义同名状态枚举。
2. RouterDecision 默认 `safe_to_execute=false`，执行必须经过后续 Graph / CostDisclosure gate。
3. AG-UI `dedupe_key` 固定为 `run_id:event_type:seq`。
4. 同一 run 的 PR-1 fixture 序列必须从 1 连续递增。
5. 旧工作台事件兼容层后续单独迁移，不在 PR-1 大改。

## Done Gate

- [x] `internal/contracts/pr1` 包存在。
- [x] StateEnumRegistry Go 常量与 JSON Schema 对齐测试通过。
- [x] RouterDecision fixture 校验通过。
- [x] AG-UI fixture 校验通过。
- [x] `go test ./internal/contracts/pr1` 通过。
- [x] Agent Runtime RunStatus 已通过 `services/agent/internal/domain/state` 复用 PR-1 状态常量；`pending` / `resuming` 保留为 Agent 本地恢复与兼容扩展态，并由单测约束不得误入 PR-1 契约。

## 验证命令

```bash
go test ./internal/contracts/pr1
go test ./services/agent/internal/domain/state
make active-contract-gate
make pr0-ci-gate
```

## 本地验证记录

2026-07-01 已执行：

```bash
go test ./internal/contracts/pr1
go test ./services/agent/internal/domain/state
make pr0-ci-gate
```

结果：

```text
ok github.com/FigoGoo/Dora-Agent/internal/contracts/pr1
ok github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/state
PR-0 CI gate passed
```
