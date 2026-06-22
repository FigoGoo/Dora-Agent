# Go 服务端编码规范

状态：active  
owner：主控 Codex 汇总维护  
适用范围：Go 智能体微服务和业务微服务  

## gofmt

- 所有 Go 文件必须通过 gofmt。
- import 由 gofmt/goimports 或项目既有工具整理。

## 技术栈

- RPC 使用 Kitex。
- IDL 使用 Thrift。
- HTTP 使用 Gin；业务服务仅在确需直接暴露 HTTP 时使用。
- DB 使用 PostgreSQL。
- DAO 使用 GORM。
- Migration 使用 golang-migrate。
- 配置/服务发现使用 etcd。
- 日志接入火山引擎日志服务。
- 测试使用 Go testing + testify。
- 具体操作要求见 `docs/standards/后端技术栈与操作规范.md`。

## context

- RPC、数据库、模型调用、外部 HTTP、长任务步骤必须传递 context。
- context 中可携带 trace_id、tenant_id、user_id 等请求级信息。
- 不在 context 中塞入大型业务对象。

## error handling

- 错误要保留原因并映射到稳定错误码。
- 对外返回业务可理解错误，对内日志保留排障信息。
- 不吞掉错误；无法处理时向上返回。

## timeout

- RPC、数据库、外部依赖和模型调用必须有超时策略。
- 长任务不得无限阻塞请求线程。

## transaction

- 业务数据库写操作必须明确事务边界。
- 事务内只放必须原子执行的操作。
- 外部 RPC 或模型调用一般不放在数据库事务内。

## repository

- repository 只负责数据访问，不承载业务规则。
- 智能体服务 repository 只访问 Agent 领域数据库。
- 业务服务 repository 只访问业务数据库。

## application

- application 层编排用例、事务、权限、RPC 调用和领域服务。
- 不把 HTTP/RPC transport 细节泄露到 domain。

## domain

- domain 表达业务概念、状态和规则。
- 业务 domain 只存在于业务服务内。
- Agent domain 表达 session、run、message、task、interrupt、artifact、memory 等 Runtime 概念。

## rpc

- RPC server 位于业务服务，RPC client 位于智能体服务。
- RPC 方法表达业务能力，不表达表 CRUD。
- 写操作必须有幂等设计。

## test

- 单元测试覆盖 domain、application、Tool、Graph 节点和错误分支。
- 集成测试覆盖 repository、RPC client/server 和关键链路。
- 契约测试覆盖 DTO、错误码、超时、幂等和版本兼容。
