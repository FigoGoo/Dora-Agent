# 前端功能设计文档入口

状态：active
owner：前端责任域；文档与契约责任域
更新时间：2026-06-29
适用范围：用户端、管理端和 Agent 工作台的前端功能设计文档

## 当前状态

用户端首页已进入首版前端实现；其他用户端、管理端和 Agent 工作台页面仍以 `docs/design/**` 作为页面、体验、视觉和线框输入。正式开发新页面前，应按功能补充前端功能设计，并与 API、AG-UI、主题样式和测试口径对齐。

## 当前文档

| 文档 | 状态 | 适用范围 |
| --- | --- | --- |
| [`2026-06-29-管理端功能API接入矩阵.md`](./2026-06-29-管理端功能API接入矩阵.md) | active | 管理端功能点、`/api/admin/**` API 接入矩阵和验收索引 |
| [`2026-06-29-用户端首页前端设计.md`](./2026-06-29-用户端首页前端设计.md) | active | DORAIGC 用户端首页第一版视觉、主题 token、静态交互和验证记录。 |
| [`2026-06-30-管理端前端基础架构设计.md`](./2026-06-30-管理端前端基础架构设计.md) | active | `admin_frontend` 管理端工程目录、路由、Provider、页面入口、服务层和基础模块边界。 |

## 何时新增

- 新增页面、路由、组件、状态管理、API 集成、SSE/WebSocket 或 AG-UI 消费逻辑。
- 修改用户端或管理端交互状态、错误展示、权限空态、加载态或确认流程。
- 前端需要消费新的 API 字段、AG-UI 事件或主题 token。

## 必须关联

| 类型 | 入口 |
| --- | --- |
| 产品与页面范围 | `docs/product/README.md`、`docs/design/README.md` |
| API/AG-UI 契约 | `docs/contracts/README.md` |
| 前端编码规范 | `docs/standards/前端编码规范.md` |
| 主题样式 | `docs/technical/frontend/themes/README.md` |
| 测试用例 | `docs/test/README.md` |

## 文档命名

推荐格式：`YYYY-MM-DD-客户端-功能名前端设计.md`，例如 `2026-07-01-用户端-Agent工作台前端设计.md`。
