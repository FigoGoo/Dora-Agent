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

## 文档使用规则

- 开始任务先读 `AGENTS.md` 和 `docs/current/README.md`，再按任务读取 `docs/technical/README.md`、`docs/product/README.md`、`docs/contracts/README.md`、`docs/standards/README.md` 或 `docs/test/README.md`。
- 开发前若缺产品目标、技术设计、契约、数据模型、SQL 或测试口径，先补对应 `draft` 或 `active` 文档，不直接从历史归档派生实现。
- 开发前需要设计时，按 `docs/templates/` 下对应模板新建或更新 `docs/technical/**`、`docs/product/**`、`docs/contracts/**` 或 `docs/test/**`，状态先用 `draft` 或 `review`；达到当前事实源条件后再改为 `active`。
- 旧文档不再承接当前迭代、内容被字段级事实源替代、包含已完成阶段计划或与当前代码冲突时，标记为 `archived` 或 `deprecated`，移动到 `docs/archive/**` 或 `docs/releases/**`，并在当前入口写明替代文档。
- 开发中新增或改变 RPC、API、AG-UI、数据模型、SQL、配置、权限、错误码或测试夹具时，同步更新对应文档和目录 README。
- 开发完成后，在相关设计、契约或测试报告中记录实现状态、验证命令、证据路径、未执行原因、遗留风险和后续动作。
- `code-plan/**`、`docs/releases/**`、`docs/archive/**` 默认只用于历史追溯；需要复用其中结论时，先迁移到当前 active 文档。

## 执行流程

1. 项目结构建议：按 api、application、domain、infra、runtime、tools、events、repository、tests 分层，优先贴合现有结构。
2. Agent 实现规则：Agent 负责动态推理和工具选择，不直接写业务数据库。
3. Graph 实现规则：Graph 节点输入输出明确，节点事件可观察，分支条件可测试。
4. Tool 实现规则：Tool 封装外部能力或 RPC client，输入输出 DTO 明确且按领域和业务场景划分，错误可分类。
5. Skill / Middleware 实现规则：Skill 放可复用方法，Middleware 放横切逻辑，如日志、鉴权上下文、限流、观测。
6. RPC client 实现规则：遵守 RPC 契约、超时、重试、幂等、权限上下文和错误码映射。
7. 事件流实现规则：内部事件先标准化，再映射 AG-UI；保证 event_id、session_id、run_id、timestamp。
8. 错误处理：区分用户输入、模型、工具、RPC、系统错误，输出可恢复性。
9. 日志：记录 run_id、session_id、tool_call_id、rpc_method、latency、error_code。
10. 数据访问：Agent 领域库建表不创建数据库级外键约束；列表查询分页，默认 10 条每页；尽量避免在 `for` 循环中逐条查询数据库或 RPC。
11. 测试：覆盖 Agent 行为、Graph 节点、Tool、RPC mock、事件顺序和数据库 CRUD。
12. 格式化：Go 代码必须 gofmt，变更后运行 go test 或说明不能运行的原因。

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
- [ ] 是否没有创建数据库级外键约束。
- [ ] 列表查询是否分页且默认 10 条每页。
- [ ] 是否避免 `for` 循环逐条查询数据库或 RPC。
- [ ] DTO 是否按领域和业务场景拆分。
- [ ] 是否包含日志和测试。
- [ ] 是否运行 gofmt 和 go test。

## 注意事项

- 当前仓库没有可确认依赖时，只写架构和实现规则，不写具体 API 调用。
- Tool 不等于业务数据库访问层；业务写操作必须走 RPC。
- 事件流要能支持前端断线重连和幂等消费。
