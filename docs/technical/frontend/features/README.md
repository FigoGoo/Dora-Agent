# 前端功能设计文档入口

状态：active  
owner：前端责任域；文档与契约责任域  
更新时间：2026-06-28  
适用范围：用户端、管理端和 Agent 工作台的前端功能设计文档

## 当前状态

当前尚未进入前端实现阶段。已有 `docs/design/**` 是页面、体验、视觉和线框的设计输入；正式前端开发前，应按功能补充前端功能设计，并与 API、AG-UI、主题样式和测试口径对齐。

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

