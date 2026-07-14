# Agent Eino / DeepSeek 依赖锁定评审 v1

> 评审状态：Approved（仅批准依赖锁定与兼容性测试）
>
> 评审日期：2026-07-14
>
> 适用 Module：`agent/`
>
> 不授权事项：本文不批准 Runner、ChatModel 工厂、Executable Registry、Graph Tool、Migration、HTTP Action 或任何真实模型请求。

## 1. 结论

Agent Module 精确锁定以下直接依赖：

| 依赖 | 版本 | 选择结论 |
|---|---:|---|
| `github.com/cloudwego/eino` | `v0.9.10` | Dora 统一 Eino 基线；使用经典 `*schema.Message`、`adk.ChatModelAgent` 和 `compose.Graph` |
| `github.com/cloudwego/eino-ext/components/model/deepseek` | `v0.1.6` | 已审核的经典 `model.ToolCallingChatModel` Adapter；不使用 AgenticModel |

`deepseek v0.1.6` 自身声明的 Eino 下限是 `v0.7.13`，Agent Module 通过 Go MVS 统一解析为 `v0.9.10`。兼容性测试直接在该最终版本组合上编译并执行，禁止依赖 Adapter 的旧 Eino 基线解释生产行为。

## 2. 冻结 API 面

本批只冻结后续实现所需的最小 API 面：

- `deepseek.ChatModel` 同时实现 `model.BaseChatModel` 与 `model.ToolCallingChatModel`；
- `adk.ChatModelAgentConfig`、`adk.NewChatModelAgent` 使用经典 `*schema.Message`；
- `compose.NewGraph`、`AddLambdaNode`、`AddEdge`、`Compile` 和 `Invoke` 可在 Go `1.26.3` 下工作；
- 有界 DAG 使用 `compose.WithNodeTriggerMode(compose.AllPredecessor)`；
- 本批不引用 `AgenticMessage`、DeepAgent、AgentAsTool、子 Agent、ToolsNode、Interrupt/Resume 或动态 Tool Search。

固定测试位于 `agent/internal/einocompat/compatibility_test.go`。该包只有测试文件，不被 `agent-service` 生产二进制引用，也不会读取 API Key、访问网络或注册 Runtime。

## 3. Module 与依赖边界

- 依赖只写入 `agent/go.mod` 与 `agent/go.sum`，验证必须使用 `GOWORK=off`；
- 根 `go.work` 继续只用于本地协作，不参与 Agent Module 的可构建性证明；
- Adapter 会带入 JSON Schema、DeepSeek SDK 与模板相关传递依赖；本批接受该已知体积，不把 Ollama 或其他传递包注册为 Dora Provider；
- 后续升级 Eino、Adapter 或 DeepSeek SDK 必须使用独立依赖变更重新执行本评审矩阵，业务 PR 不得隐式升级。

## 4. 许可证与供应链结论

- Eino `v0.9.10` 模块包含 `LICENSE-APACHE`；
- DeepSeek Adapter `v0.1.6` 模块源码逐文件声明 Apache License 2.0；
- DeepSeek Go SDK `v1.3.4` 为 MIT License；
- 本批直接依赖与关键传递依赖已核对许可证：Adapter 模块压缩包未携带顶层许可证文件，以逐文件 Apache-2.0 声明为依据；`godotenv v1.5.1` 的顶层 `LICENCE` 为 MIT；其余最终发布闭包仍由发布 SBOM/Notices 门禁复核；
- 发布镜像生成第三方 Notices 时必须从最终生产二进制重新生成 SBOM/许可证清单；测试闭包不能替代发布物清单。

许可证兼容结论只覆盖当前精确版本，不授权复制上游示例、README 或源码进入 Dora。

## 5. 可执行验证

依赖批必须持续通过：

```bash
cd agent
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go mod tidy
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go test ./internal/einocompat
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go test ./...
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go test -race ./...
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go vet ./...
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go build ./cmd/agent-service
```

其中兼容性测试必须证明：经典 Message 接口断言成立，最小 DAG 在 `AllPredecessor` 下可 Compile/Invoke，且测试不触发真实 DeepSeek 请求。

本批还必须用 `go version -m` 核对当前 `agent-service` 构建元数据不含 Eino 或 DeepSeek Adapter；只有后续 Runtime 评审通过并完成生产装配后，这两个模块才允许进入服务二进制。

## 6. 后续实现门禁

本评审只关闭“独立 Eino 依赖变更”门禁。以下条件仍未关闭：

1. `aigc-contract-catalog.md`、`runner-session-lane-review-v1.md` 和 `a2ui-event-action-contract-v1.md` 仍须 Approved；
2. `plan_creation_spec-design.md` 的产品、Business、Agent、财务、安全和测试决策仍须全部冻结；
3. 在上述文档 Approved 前，Tool Catalog 必须继续返回 `unavailable / DESIGN_REVIEW_PENDING`；
4. 依赖存在不等于生产 Runtime、模型配置或 Graph 已可用。

Kitex/Gin 与当前三个 Module 的编译、测试没有回归；Eino Runtime 实际装配后的 Telemetry Callback、Trace 上下文和中间件顺序仍属于 Runner 集成评审，不由本依赖批关闭。
