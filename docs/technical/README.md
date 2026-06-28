# 技术文档入口

状态：active  
owner：文档与契约责任域  
更新时间：2026-06-28  
适用范围：架构、后端、前端、主题样式、CI/CD、功能迭代等技术文档导航

## 管理原则

技术文档按“当前事实源”和“历史追溯”分开管理：

- 当前技术设计优先落在 `docs/technical/**`、`docs/contracts/**`、`docs/standards/**`。
- 第一阶段服务端开发设计保留在 `code-plan/**`，状态为历史归档，不再作为后续迭代默认入口。
- 早期架构草案保留在 `docs/architecture/**`，只在追溯设计背景时读取。
- 新增功能迭代必须创建当前迭代设计或更新现有 active 文档，不能继续往旧阶段计划追加内容。
- 后续后端、前端、契约或测试变更，如没有当前 active 设计入口，先在 `iterations/` 或对应功能目录补文档。

## 当前目录

| 目录 | 作用 | 当前关注 |
| --- | --- | --- |
| [`architecture/`](./architecture/README.md) | 当前架构设计、ADR 和跨服务边界文档入口。 | 按需关注 |
| [`backend/features/`](./backend/features/README.md) | Agent 服务和业务服务按功能点维护的后端设计入口。 | 关注 |
| [`frontend/features/`](./frontend/features/README.md) | 用户端、管理端和 Agent 工作台前端功能设计入口。 | 按需关注 |
| [`frontend/themes/`](./frontend/themes/README.md) | 用户端、管理端、Agent 工作台主题样式规范入口。 | 按需关注 |
| [`cicd/`](./cicd/README.md) | CI/CD、发布、回滚和上线验证文档入口。 | 按需关注 |
| [`iterations/`](./iterations/README.md) | 当前或即将开始的功能迭代技术文档入口。 | 关注 |

## 分类映射

| 类别 | 推荐位置 | 当前入口 |
| --- | --- | --- |
| 架构设计文档 | `docs/technical/architecture/` | [`architecture/README.md`](./architecture/README.md) |
| 后端设计文档（按功能） | `docs/technical/backend/features/` | [`backend/features/README.md`](./backend/features/README.md) |
| 后端开发规范（按职责） | `docs/standards/` | [`../standards/README.md`](../standards/README.md) |
| 前端设计文档（按功能） | `docs/technical/frontend/features/` 或 `docs/design/` | [`frontend/features/README.md`](./frontend/features/README.md)、[`../design/README.md`](../design/README.md) |
| 前端开发规范 | `docs/standards/前端编码规范.md` | [`../standards/前端编码规范.md`](../standards/前端编码规范.md) |
| 主题样式设计规范 | `docs/technical/frontend/themes/` 或 `docs/design/` | [`frontend/themes/README.md`](./frontend/themes/README.md) |
| CI/CD 发布文档 | `docs/technical/cicd/` | [`cicd/README.md`](./cicd/README.md) |
| 功能迭代文档 | `docs/technical/iterations/` 或 `docs/releases/` | [`iterations/README.md`](./iterations/README.md)；完成后沉淀到 [`../releases/README.md`](../releases/README.md) |

## 技术文档内容规范

每份技术设计文档至少包含：

- 状态、owner、更新时间、适用范围。
- 背景、目标、非目标。
- 相关产品文档、契约、代码路径和测试入口。
- 设计方案：模块、数据、流程、权限、错误、日志和边界。
- 变更影响：RPC、API、AG-UI、数据库、配置、前端和测试。
- 验收标准和验证方式。
- 过期条件：什么情况下迁移到 `docs/releases/**` 或 `docs/archive/**`。

## 归档规则

- 阶段完成后的开发计划、任务拆分、验收矩阵和阶段复核报告迁入 `docs/releases/<阶段>/`。
- 被新方案替代但仍有追溯价值的技术文档迁入 `docs/archive/technical/` 或标记 `deprecated`。
- `code-plan/**` 只保留第一阶段服务端历史设计，不承接后续功能迭代。
