---
name: Eino开发实现
description: 用于指导 Go + Eino 智能体微服务代码实现、事件流、RPC client、错误处理、日志和测试。
---

# Eino开发实现

## 目标

把 Eino 智能体架构落到 Go 代码中，同时保持契约、状态、事件和测试一致。

## 使用场景

- 实现 Agent、Graph、Workflow、Tool、Skill、Middleware、RPC client 或事件流。
- 修改智能体微服务代码。
- 增加 Agent 领域 CRUD、日志或测试。

## 输入

- Eino 智能体架构设计。
- RPC 契约、AG-UI 事件协议、Agent 数据模型。
- 当前 Go 项目结构和依赖。

## 执行流程

1. 项目结构建议：按 api、application、domain、infra、runtime、tools、events、repository、tests 分层，优先贴合现有结构。
2. Agent 实现规则：Agent 负责动态推理和工具选择，不直接写业务数据库。
3. Graph 实现规则：Graph 节点输入输出明确，节点事件可观察，分支条件可测试。
4. Tool 实现规则：Tool 封装外部能力或 RPC client，输入输出 DTO 明确，错误可分类。
5. Skill / Middleware 实现规则：Skill 放可复用方法，Middleware 放横切逻辑，如日志、鉴权上下文、限流、观测。
6. RPC client 实现规则：遵守 RPC 契约、超时、重试、幂等、权限上下文和错误码映射。
7. 事件流实现规则：内部事件先标准化，再映射 AG-UI；保证 event_id、session_id、run_id、timestamp。
8. 错误处理：区分用户输入、模型、工具、RPC、系统错误，输出可恢复性。
9. 日志：记录 run_id、session_id、tool_call_id、rpc_method、latency、error_code。
10. 测试：覆盖 Agent 行为、Graph 节点、Tool、RPC mock、事件顺序和数据库 CRUD。
11. 格式化：Go 代码必须 gofmt，变更后运行 go test 或说明不能运行的原因。

## 输出

- 实现方案。
- 修改文件。
- Agent / Graph / Tool / Skill / Middleware / RPC client 影响。
- 事件流和错误处理说明。
- gofmt / go test 结果。

## 检查表

- [ ] 是否读取相关规范和契约。
- [ ] 是否避免伪造 Eino API。
- [ ] 是否遵守 AG-UI 协议。
- [ ] 是否遵守 RPC 契约。
- [ ] 是否区分 Agent DB 和业务 DB。
- [ ] 是否包含日志和测试。
- [ ] 是否运行 gofmt 和 go test。

## 注意事项

- 当前仓库没有可确认依赖时，只写架构和实现规则，不写具体 API 调用。
- Tool 不等于业务数据库访问层；业务写操作必须走 RPC。
- 事件流要能支持前端断线重连和幂等消费。
