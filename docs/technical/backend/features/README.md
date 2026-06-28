# 后端功能设计文档入口

状态：active  
owner：文档与契约责任域  
更新时间：2026-06-28  
适用范围：Agent 服务和业务服务按功能点维护的后端设计文档

## 当前状态

第一阶段服务端开发已经完成，历史设计保留在 `docs/releases/phase-01-server/README.md` 和 `code-plan/**`。后续后端新增、修改或重构功能时，不继续追加到 `code-plan/**`，应在本目录新增功能设计或更新当前 active 契约、规范和测试文档。

## 何时新增

- 新增业务功能、Agent 能力、RPC 能力、HTTP API、AG-UI 事件或数据库表。
- 修改权限、幂等、事务、错误码、分页、审计、日志或配置语义。
- 当前实现与契约或测试口径不一致，需要先形成设计口径。

## 必须关联

| 类型 | 入口 |
| --- | --- |
| 产品范围 | `docs/product/README.md`、对应 PRD 或产品迭代文档 |
| 技术约束 | `docs/standards/README.md` |
| RPC/API/AG-UI/SQL | `docs/contracts/README.md` |
| 测试用例 | `docs/test/README.md` |
| 迭代记录 | `docs/technical/iterations/README.md` |

## 文档命名

推荐格式：`YYYY-MM-DD-功能名后端设计.md`。

每份文档至少写清：状态、owner、更新时间、适用范围、相关代码路径、相关契约、背景、目标、非目标、方案、数据与事务、错误码、测试和过期条件。

