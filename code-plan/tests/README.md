# 第一阶段服务级测试与验收设计归档目录

状态：archived
owner：测试与验收责任域
更新时间：2026-06-28
适用范围：`tests/agent/**`、`tests/business/**`、`tests/contract/**`、`tests/e2e/**`、Agent API、AG-UI schema/replay、业务 RPC、业务 HTTP API、Agent DB、业务 DB  
相关代码路径：`tests/**`、`api/thrift/**`、`api/openapi/**`、`api/agui/**`、`db/migrations/iterations/**`、`services/agent/**`、`services/business/**`  
相关设计契约：`code-plan/README.md`、`code-plan/agent/**`、`code-plan/business/**`、`docs/product/**`、`docs/standards/**`

## 归档说明

本目录是第一阶段服务级测试与验收设计归档，用于追溯 Agent 微服务、业务微服务、RPC 契约、HTTP API 契约、AG-UI 事件生产、Agent DB 和业务 DB 的历史测试计划和验收矩阵。后续新迭代不再以本目录作为当前测试事实源。

当前测试入口以 `docs/test/README.md`、`docs/contracts/**` 和 `docs/standards/测试规范.md` 为准。

## 历史测试设计结构

历史测试设计包含：

- 产品和工程事实源。
- 测试范围与非范围。
- 需求映射矩阵。
- 测试类型、测试数据和 fixture 落点。
- Agent API、AG-UI、RPC、HTTP API、Agent DB、业务 DB 的验证点。
- 权限、错误、幂等、重试、超时和断线补偿用例。
- 执行命令或后续实现命令入口。
- 通过标准、阻断标准和未执行项记录规则。

## 历史文档列表

| 顺序 | 文档 | 目标 |
| --- | --- | --- |
| 00 | [服务级测试计划与验收矩阵](./00-服务级测试计划与验收矩阵.md) | 汇总非前端、非部署范围的测试范围、fixture、主链路、错误路径、DB 验证和发布前质量门禁。 |

## 边界总则

- 测试与验收责任域不修改 RPC、OpenAPI、AG-UI 或数据库契约，只报告不一致并回派 owner。
- 数据库验证必须区分 Agent DB 和业务 DB。
- AG-UI 验证只验证事件生产、schema、sequence、replay 和 snapshot，不验证前端 reducer 或页面渲染。
- HTTP API 验证只验证服务端 DTO、状态码、错误响应、分页、权限和幂等，不验证前端页面。
- 未执行测试必须写明原因、影响范围和替代验证，不能标记为通过。

## Done Gate

- [x] 服务级测试范围排除前端开发和部署上线文档。
- [x] Agent API、AG-UI、RPC、HTTP API、Agent DB、业务 DB 均有测试入口。
- [x] 主链路、错误路径、权限路径、幂等路径、超时路径和断线补偿路径均有 fixture 设计。
- [x] 测试报告必须能定位失败 owner：Agent、业务、契约、数据库或配置。
