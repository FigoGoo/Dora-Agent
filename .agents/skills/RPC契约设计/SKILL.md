---
name: RPC契约设计
description: 用于设计智能体微服务和业务微服务之间的 RPC 契约。
---

# RPC契约设计

## 目标

让智能体微服务通过稳定、可测试、可版本化的 RPC 契约调用业务系统能力。

## 使用场景

- Agent 需要读取或改变业务事实。
- 需要新增或调整业务服务 RPC。
- 出现智能体服务和业务服务边界争议。

## 输入

- 产品需求和业务规则。
- 智能体服务调用场景。
- 业务服务领域模型、权限和事务约束。

## 文档使用规则

- 开始任务先读 `AGENTS.md` 和 `docs/current/README.md`，再按任务读取 `docs/technical/README.md`、`docs/product/README.md`、`docs/contracts/README.md`、`docs/standards/README.md` 或 `docs/test/README.md`。
- 开发前若缺产品目标、技术设计、契约、数据模型、SQL 或测试口径，先补对应 `draft` 或 `active` 文档，不直接从历史归档派生实现。
- 开发前需要设计时，按 `docs/templates/` 下对应模板新建或更新 `docs/technical/**`、`docs/product/**`、`docs/contracts/**` 或 `docs/test/**`，状态先用 `draft` 或 `review`；达到当前事实源条件后再改为 `active`。
- 旧文档不再承接当前迭代、内容被字段级事实源替代、包含已完成阶段计划或与当前代码冲突时，标记为 `archived` 或 `deprecated`，移动到 `docs/archive/**` 或 `docs/releases/**`，并在当前入口写明替代文档。
- 开发中新增或改变 RPC、API、AG-UI、数据模型、SQL、配置、权限、错误码或测试夹具时，同步更新对应文档和目录 README。
- 开发完成后，在相关设计、契约或测试报告中记录实现状态、验证命令、证据路径、未执行原因、遗留风险和后续动作。
- `code-plan/**`、`docs/releases/**`、`docs/archive/**` 默认只用于历史追溯；需要复用其中结论时，先迁移到当前 active 文档。

## 执行流程

1. 定义服务名：表达业务域或能力边界。
2. 定义方法名：表达业务能力，不表达数据库表操作。
3. 定义请求 DTO：包含业务参数、权限上下文、幂等键、trace_id 和 preview / confirm 所需字段。
4. 定义响应 DTO：包含业务结果、展示摘要、确认信息或错误详情。
5. 定义错误码：区分业务错误、权限错误、幂等冲突、参数错误、系统错误。
6. 定义超时：按读写、长任务、外部依赖区分。
7. 定义重试：只对幂等读或带幂等键写操作开放。
8. 定义幂等：写操作必须有 idempotency_key。
9. 定义权限上下文：由业务服务最终校验。
10. 定义审计字段：记录操作者、租户、请求来源、trace_id 和业务动作。
11. 定义版本兼容：新增字段向后兼容，破坏性变更必须新版本。
12. 准备 mock 数据和 contract test。
13. 对需要人工确认的操作，支持 preview / confirm 模式。

## 输出

- 服务名。
- 方法名。
- 请求 DTO。
- 响应 DTO。
- 错误码。
- 业务错误和系统错误。
- 超时、重试、幂等。
- 权限上下文。
- 审计字段。
- 版本兼容。
- mock 数据。
- contract test。

## 检查表

- [ ] RPC 方法是否表达业务能力。
- [ ] 写操作是否有幂等键。
- [ ] 人工确认操作是否支持 preview / confirm。
- [ ] 权限是否由业务服务校验。
- [ ] 错误码是否稳定可测试。
- [ ] 是否准备 mock 和 contract test。

## 注意事项

- Agent 服务不能绕过业务服务直接写业务库。
- RPC 契约不是数据库表 CRUD 暴露层。
- 不要为了前端展示临时返回未定义字段。
