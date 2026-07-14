# 前端接入基础 v1

> 状态：Implemented / v1
>
> 更新日期：2026-07-14
>
> 范围：统一 API Client、认证会话快照、错误映射、SSE 游标重连和本地服务代理

## 1. 目标与边界

本基础层让现有前端在领域 HTTP 契约逐项冻结后能够按页面纵向替换 Mock，而不再由每个页面各自实现 `fetch`、登录布尔值、错误解析或 EventSource 生命周期。

本轮不创建或冻结登录、Project、Session、Graph Tool 等服务端领域 API，不把历史 `/api/aigc/**` 路径声明为目标生产契约，也不批量替换现有业务 Mock。登录弹窗当前仍是交互占位：它只更新内存中的展示会话，不调用真实认证接口；正式登录、退出和会话恢复必须等待 Business 鉴权契约评审。

## 2. 已实现能力

### 2.1 JSON API Client

- 默认使用 `credentials: include`，认证凭据预期由同源 HttpOnly Cookie 承载；
- JSON 请求统一补充 `Accept`，非 FormData 请求补充 `Content-Type`；
- 保留调用方的幂等键和其他 Header；
- 将 `{code,message,request_id,details,retryable}` 或嵌套 `error` Envelope 映射为 `ApiError`；
- 只有可选资源的 `404` 返回 `null`，其他失败保持失败关闭；
- `401` 只广播最小会话失效事件，不传播响应正文或保存 Token。

### 2.2 认证会话

- `AuthSessionProvider` 是全站唯一登录态快照，页面不再各自维护登录布尔值；
- 浏览器只保存安全展示字段，不把 Access Token 写入 LocalStorage/SessionStorage；
- API Client 收到 `401` 后会话立即回到 `anonymous`；
- 正式认证接口接入后，由成功的 session bootstrap/login/logout 调用更新 Provider，不改变页面消费方式。

### 2.3 SSE 恢复

- 首次连接和重连均使用同源 Cookie；
- 从协议 `seq` 或 SSE `Last-Event-ID` 推进最后确认游标；
- 传输失败后关闭旧 EventSource，按带抖动的有界指数退避创建新连接；
- 重连 URL 携带 `after_seq`，由服务端 EventLog 回放缺口；
- 页面切换或会话切换会永久关闭旧连接并取消待执行重连，旧回调不得污染新会话。

### 2.4 本地代理与 CI

- `/api/aigc/**` 暂时代理到 Agent `18082`，其他 `/api/**` 默认代理到 Business `18081`；
- 代理目标可通过 `frontend/.env.local` 覆盖，不包含 Secret；
- 根 Makefile 提供 `make check-frontend`；CI 使用 Node 24 执行 `npm ci`、测试和构建。

## 3. 验收

- [x] API Client 覆盖 Cookie、FormData、结构化错误、404、401 和非法响应测试；
- [x] 认证会话覆盖登录快照与 401 失效测试；
- [x] SSE 覆盖游标、重连、退避、旧回调隔离和非法事件测试；
- [x] 现有工作台 API/SSE 已接入基础层；
- [x] 前端全量单元/组件测试通过；
- [x] Vite 生产构建通过；
- [ ] 正式 Business 鉴权 API 接入；
- [ ] 按已冻结领域契约逐页移除业务 Mock；
- [ ] 与真实 Agent EventLog 完成 `USR-WORKSPACE-001 / SRV-READ-001` 端到端验收。

未完成项属于后续领域纵切，不影响本基础层 v1 的工程验收，也不得被表述为真实业务 API 已可用。
