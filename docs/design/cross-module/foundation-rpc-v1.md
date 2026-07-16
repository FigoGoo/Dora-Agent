# Foundation RPC v1 契约

> 文档状态：Frozen / v1，可进入 M1.2 实现
>
> 契约标识：`foundation.rpc.v1`
>
> 评审日期：2026-07-14
>
> IDL Owner：Business Module

## 1. 目的与边界

Foundation RPC v1 用于验证三个独立 Go Module 开始业务开发前必须成立的基础链路：单一 Thrift IDL、Kitex 生成代码、Business RPC Runtime、etcd 服务发现、Agent Client 超时、请求关联和优雅摘除。

本契约只包含无业务副作用的 `Probe`。它不查询或修改用户、Project、Skill、积分、Asset、Session、Graph、Operation、Job 或 Worker Attempt，也不能作为调用方鉴权、业务授权、Liveness 或外部流量入口。

[AIGC 跨 Module 契约目录](aigc-contract-catalog.md) 继续保持 `Draft / v1alpha1`。Foundation RPC v1 的冻结不代表其中的计费、Approval、Job、Event 或 Graph Tool 契约已经通过评审。

## 2. Owner 与生成规则

| 项目 | 决定 |
|---|---|
| IDL 源 | `business/api/thrift/foundation/v1/foundation.thrift` |
| IDL Owner | Business Module |
| Provider | `business/cmd/business-service` |
| Consumer | `agent/cmd/agent-service` |
| Worker | 本版本不调用；不能为满足联调虚构 RPC 服务 |
| 生成输出 | Business 与 Agent 各自在本 Module 保存生成代码 |
| 生成器 | Kitex `v0.16.2`、thriftgo `v0.4.5` |
| 服务名 | `dora.business.foundation.v1` |
| etcd Prefix | `/dora/services/dora.business.foundation.v1/` |

两个 Module 的生成代码来自同一 IDL，但使用各自 Module Path 生成，不互相 import，也不创建第四个共享 Go Module。生成代码禁止手工修改；`scripts/generate-foundation-rpc.sh` 是唯一生成入口。

## 3. RPC 定义

```text
BusinessFoundationServiceV1.Probe(FoundationProbeRequestV1)
  → FoundationProbeResponseV1
  throws FoundationServiceExceptionV1
```

### 3.1 请求

| 字段号 | 字段 | 约束 |
|---|---|---|
| 1 | `schema_version` | 必须精确等于 `foundation.rpc.v1` |
| 2 | `request_id` | 必须是调用方生成的 UUIDv7；同一次有限重试保持不变 |
| 3 | `caller_service` | 必填稳定服务名；最大 128 字节 |
| 4 | `caller_version` | 必填构建版本；最大 128 字节 |
| 5 | `sent_at_unix_ms` | UTC Unix 毫秒；必须大于零 |

### 3.2 响应

| 字段号 | 字段 | 约束 |
|---|---|---|
| 1 | `schema_version` | 固定为 `foundation.rpc.v1` |
| 2 | `request_id` | 必须原样回显请求 ID，用于跨服务证据关联 |
| 3 | `service_name` | 固定为 Business Runtime 稳定服务名 |
| 4 | `service_version` | Business 构建版本 |
| 5 | `environment` | Business 运行环境 |
| 6 | `instance_id` | 实际处理请求的 Business 实例 |
| 7 | `received_at_unix_ms` | Business 接收请求的 UTC Unix 毫秒时间 |

响应不得包含主机内部路径、数据库信息、Secret、调用栈或业务数据。

### 3.3 稳定错误

| 错误码 | Retryable | 语义 |
|---|---|---|
| `INVALID_ARGUMENT` | false | 请求为空、版本错误、ID 非 UUIDv7、字符串越界或时间非法 |
| `INTERNAL` | false | 无法构造安全响应；外部只返回固定中文信息 |

etcd 暂无实例、连接失败、请求超时和连接中断属于 Client Transport Error，不伪装成业务错误。Agent 只在启动预算内有限重试只读 `Probe`；超时后保持未就绪并失败启动。

## 4. 服务发现契约

Business 在 HTTP 与 RPC Listener 都绑定成功后，用同一进程所有的 etcd 租约发布两个 Endpoint。Foundation RPC 记录使用以下 JSON：

```json
{
  "service": "dora.business.foundation.v1",
  "instance_id": "business-local-1",
  "address": "host.docker.internal:19081",
  "version": "dev",
  "registered_at": "2026-07-14T00:00:00Z"
}
```

- `address` 必须是其他实例可访问的 Advertised Address，拒绝回环和通配地址。
- `foundation-smoke` 的 Runtime 在宿主机启动；若模板使用 `host.docker.internal`，脚本会发布默认路由的非回环 IPv4，避免把容器访问宿主机的别名误用于宿主机自访问。
- 所有记录绑定租约；KeepAlive 中断必须使 Business 退出 Readiness。
- Agent Resolver 只解析固定 Prefix 和有类型 JSON，忽略格式错误记录；没有合法实例时返回发现错误。
- 进程退出先置为未就绪并撤销租约，再停止 RPC/HTTP。
- etcd 记录不是鉴权声明，也不保存业务配置。

## 5. Runtime 与安全约束

1. Business RPC 使用独立监听地址和显式 Read/Write、连接空闲、退出超时。
2. Agent Client 使用显式 Connect Timeout、RPC Timeout 和 Startup Probe Timeout；框架自动重试保持关闭。
3. `Probe` Handler 执行严格 DTO 校验，不访问 PostgreSQL、Redis、外部网络或其他业务 Service。
4. 普通日志只允许记录 Request ID、调用方服务、处理实例、状态和耗时，不记录完整 Payload。
5. 本地环境允许明文 Thrift；生产发布前必须增加服务身份认证、传输 TLS 和网络最小权限。Foundation `Probe` 成功不能替代这些安全门禁。

## 6. 兼容性

- 已发布字段号不得复用；新增字段只能使用新编号并保持旧消费者可读。
- 删除字段、改变字段语义、服务名、etcd Prefix 或错误语义必须发布新主版本。
- Consumer 必须拒绝未知 `schema_version`，不能静默回退。
- IDL、生成代码、Handler、Client Mapper 和契约测试必须处于同一变更。

## 7. 验收

- [x] Business、Agent、Worker Owner 与非目标明确；Worker 不注册虚假 RPC。
- [x] IDL Source、版本、字段号、服务名和发现 Prefix 冻结。
- [x] 读请求无业务副作用，允许由 Agent 在单一启动预算内有限重试。
- [x] Request ID、版本校验、错误码和日志脱敏明确。
- [x] 明文只限本地；生产认证/TLS 未完成时不得宣称生产安全。
- [x] Business/Agent 生成代码可重复且无差异。
- [x] Handler/Mapper/Resolver 契约测试通过。
- [x] 本地真实 etcd + Kitex Probe 与优雅摘除冒烟通过。

评审结论：Foundation RPC v1 通过 M1.2 工程实现门禁；通过范围仅限本文，不解除 AIGC 契约和六个 Graph Tool 的独立评审门禁。
