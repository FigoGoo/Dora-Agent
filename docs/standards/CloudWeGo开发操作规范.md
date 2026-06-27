# CloudWeGo 开发操作规范

状态：active  
owner：文档与契约责任域
适用范围：Kitex、Thrift、etcd 服务发现相关开发  
更新时间：2026-06-25

## 目标

确保 Kitex、Thrift、etcd 的使用遵循 CloudWeGo 官方文档，避免自造 RPC 约定和代码生成流程。

## 官方文档优先

开发前优先查阅 CloudWeGo 官方文档：

- Kitex Getting Started：https://www.cloudwego.io/docs/kitex/getting-started/
- Kitex 环境准备：https://www.cloudwego.io/docs/kitex/getting-started/prerequisite/
- Kitex 代码生成：https://www.cloudwego.io/docs/kitex/tutorials/code-gen/code_generation/
- Kitex etcd 服务发现：https://www.cloudwego.io/docs/kitex/tutorials/third-party/service_discovery/etcd/
- Kitex Client Options：https://www.cloudwego.io/docs/kitex/tutorials/options/client_options/

如本文与官方文档冲突，以官方文档为准，并更新本文。

## IDL 目录

建议目录：

```text
api/thrift/
  common/
  business/
  agent/
```

- `common/` 放通用错误码、分页、权限上下文、审计上下文。
- `business/` 放业务服务 RPC。
- `agent/` 仅在需要对外定义智能体服务 RPC 时使用。

## Thrift 规则

- service 表达业务能力，不表达表 CRUD。
- 请求 DTO 必须包含必要业务参数、权限上下文、trace_id。
- 请求 DTO 和响应 DTO 必须按领域和业务场景划分，不复用业务大对象或 ORM 对象。
- 列表查询方法必须包含分页字段，默认 10 条每页，并在 IDL 或契约文档中说明上限和排序语义。
- 写操作必须包含 idempotency_key。
- 需要人工确认的操作支持 preview / confirm 模式。
- 字段新增保持向后兼容；破坏性变更走新版本。

## 代码生成

Kitex 代码生成依赖 thriftgo 和 kitex 命令，安装和参数以官方文档为准。

典型准备命令：

```bash
go install github.com/cloudwego/thriftgo@latest
go install github.com/cloudwego/kitex/tool/cmd/kitex@latest
```

典型生成动作：

```bash
kitex -module <go_module> -service <service_name> <path/to/service.thrift>
```

要求：

- 生成命令写入 PR 描述。
- 生成代码目录由项目结构统一约定。
- 生成代码不得手动修改。
- IDL 变更必须同步 contract test。

## Kitex Server

- 业务服务责任域负责 Kitex server。
- server 注册到 etcd。
- server handler 只做 RPC transport 转换，业务规则进入 application/domain。
- 错误码稳定，避免直接暴露内部错误。

## Kitex Client

- Agent 服务责任域负责 Kitex client。
- client 通过 etcd resolver 发现业务服务。
- client 调用必须设置 context timeout。
- client 错误必须映射为 Agent 可理解错误，不吞掉业务错误。

## etcd 服务发现

- 使用 CloudWeGo Kitex etcd registry/resolver 扩展。
- 本地 endpoint 来自 `.env.local`。
- 线上 endpoint 来自环境变量或安全配置。
- 服务名和 key 命名必须稳定。

## 检查表

- [ ] 是否先查 CloudWeGo 官方文档。
- [ ] 是否有 Thrift IDL。
- [ ] 是否按官方流程生成代码。
- [ ] 是否没有手改生成代码。
- [ ] Kitex server/client 责任域是否明确。
- [ ] 是否通过 etcd 注册发现。
- [ ] 是否有 contract test。
