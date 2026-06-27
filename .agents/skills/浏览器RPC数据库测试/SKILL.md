---
name: 浏览器RPC数据库测试
description: 用于端到端验证浏览器交互、AG-UI 事件、Agent API、RPC 链路、Agent 领域数据库和业务数据库。
---

# 浏览器RPC数据库测试

## 目标

沿用户路径验证前端、智能体微服务、RPC、业务微服务和数据库链路，并输出可复现缺陷。

## 使用场景

- 完成端到端测试或回归测试。
- 验证 AG-UI 事件、RPC 契约或数据库写入。
- 定位跨服务缺陷。

## 输入

- PRD、验收标准、API/RPC/AG-UI 契约。
- 测试环境、账号、数据准备和日志入口。
- Agent 领域数据库与业务数据库访问方式。

## 文档使用规则

- 开始任务先读 `AGENTS.md` 和 `docs/current/README.md`，再按任务读取 `docs/technical/README.md`、`docs/product/README.md`、`docs/contracts/README.md`、`docs/standards/README.md` 或 `docs/test/README.md`。
- 开发前若缺产品目标、技术设计、契约、数据模型、SQL 或测试口径，先补对应 `draft` 或 `active` 文档，不直接从历史归档派生实现。
- 开发前需要设计时，按 `docs/templates/` 下对应模板新建或更新 `docs/technical/**`、`docs/product/**`、`docs/contracts/**` 或 `docs/test/**`，状态先用 `draft` 或 `review`；达到当前事实源条件后再改为 `active`。
- 旧文档不再承接当前迭代、内容被字段级事实源替代、包含已完成阶段计划或与当前代码冲突时，标记为 `archived` 或 `deprecated`，移动到 `docs/archive/**` 或 `docs/releases/**`，并在当前入口写明替代文档。
- 开发中新增或改变 RPC、API、AG-UI、数据模型、SQL、配置、权限、错误码或测试夹具时，同步更新对应文档和目录 README。
- 开发完成后，在相关设计、契约或测试报告中记录实现状态、验证命令、证据路径、未执行原因、遗留风险和后续动作。
- `code-plan/**`、`docs/releases/**`、`docs/archive/**` 默认只用于历史追溯；需要复用其中结论时，先迁移到当前 active 文档。

## 执行流程

1. 浏览器功能测试：从用户入口执行完整路径，记录页面状态和关键截图。
2. AG-UI 事件测试：检查事件类型、payload、顺序、event_id、sequence、断线重连和未知事件兼容。
3. Agent API 测试：验证 session、run、message、task、interrupt、resume 接口。
4. RPC 链路测试：验证智能体服务调用业务服务的请求、响应、错误码、超时和幂等。
5. Agent 领域数据库验证：检查 session、run、message、event、tool_call、task、interrupt、artifact、memory、config 数据。
6. 业务数据库验证：只验证业务服务产生的业务事实，不要求智能体服务直接访问。
7. 错误路径：覆盖模型失败、Tool 失败、RPC 失败、权限失败、超时和用户取消。
8. 权限路径：验证租户、用户、角色和资源权限。
9. 重复提交：验证幂等键和前端重复操作。
10. 中断恢复：验证 confirmation.required、resume.accepted 和状态恢复。
11. 回归测试：复测修复项和相关链路。
12. 缺陷报告：给出复现步骤、证据、初步定位和建议责任域。
13. 测试报告：区分通过用例、失败用例、阻塞问题、非阻塞问题和上线建议。

## 输出

- 浏览器功能测试结果。
- AG-UI 事件测试结果。
- Agent API 测试结果。
- RPC 链路测试结果。
- Agent 领域数据库验证结果。
- 业务数据库验证结果。
- 缺陷报告。
- 测试报告。

## 检查表

- [ ] 是否覆盖用户主路径。
- [ ] 是否验证 AG-UI 事件顺序和 payload。
- [ ] 是否验证 RPC 链路。
- [ ] 是否区分 Agent DB 和业务 DB。
- [ ] 是否覆盖错误、权限、重复提交、中断恢复。
- [ ] 缺陷是否可复现。

## 注意事项

- 测试与验收责任域不修改契约，只报告契约不一致。
- 数据库证据要说明来源和表归属。
- 阻塞问题必须清晰说明阻塞的用户路径。
