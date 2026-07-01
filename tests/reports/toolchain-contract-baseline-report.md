# M0 技术基线验收报告

状态：passed  
owner：浏览器、RPC 与数据库测试工程师  
更新时间：2026-06-27  
适用范围：Go module、Kitex 生成代码、RPC 契约、业务 OpenAPI、Agent OpenAPI、AG-UI schema、contract fixture、M0 本地验证  

## 结论修订

此前 `scripts/validate-toolchain-contract-baseline.sh` 的轻量通过结论只证明技术基线可运行，不能证明契约冻结完成；该结论已撤回。

本报告以升级后的严格 M0 门禁为准：校验 code-plan 对齐的 RPC 关键字段、业务 OpenAPI 写操作幂等规则、Agent OpenAPI 命名 DTO、AG-UI schema 分支覆盖、领域级 RPC fixture、HTTP 状态码 fixture、业务 seed 覆盖、无数据库级外键关键词和 Kitex 生成代码编译。

## 测试范围

- 智能体微服务：未实现业务逻辑；验证 Agent OpenAPI、AG-UI schema 与 fixture。
- RPC：验证 Thrift IDL、Kitex 生成代码、业务 RPC fixture 与 code-plan 关键字段一致。
- 业务微服务：未实现业务逻辑；验证业务 HTTP OpenAPI、业务 RPC/HTTP fixture、测试 seed。
- Agent 领域数据库：验证 migration 扫描门禁。
- 业务数据库：验证 migration 扫描门禁。
- 前端：本轮不涉及。

## 测试环境

- 环境：macOS 本地开发环境。
- Go：`go version go1.26.3 darwin/arm64`。
- GOROOT：`/Users/figo/sdk/go1.26.3`。
- GOPATH：`/Users/figo/go`。
- thriftgo：`0.4.5`。
- Kitex：`v0.16.2`。

## 生成代码结果

| 项目 | 结果 | 证据 |
| --- | --- | --- |
| Go module | passed | `go.mod` 使用 `module github.com/FigoGoo/Dora-Agent`、`go 1.26`、`toolchain go1.26.3`。 |
| Kitex 生成 | passed | 执行 `kitex -module github.com/FigoGoo/Dora-Agent -record api/thrift/business_agent_service.thrift`。 |
| 生成记录 | passed | `kitex-all.sh` 已生成并可执行。 |
| 生成目录 | passed | `kitex_gen/dora/api/businessagent/**` 共 30 个生成文件。 |
| 编译 | passed | `go test ./...` 通过，当前仅生成代码包，无测试文件。 |

## 契约验证结果

| 契约 | 结果 | 证据 |
| --- | --- | --- |
| RPC code-plan 对齐 | passed | `scripts/validate-toolchain-contract-baseline.sh` 校验 `GeneratedAssetObjectInput`、`GeneratedAssetUploadSlot`、`CommittedAssetRefDTO`、`skill_scope_filter`、`account_id` 等关键字段，并阻断旧 DTO。 |
| 业务 OpenAPI | passed | `api/openapi/business-api.yaml` 解析通过，101 个 operation 均有命名 200 response；所有 POST/PATCH/PUT/DELETE 均有 `Idempotency-Key`、命名 request body 和必填 `request_hash`。 |
| Agent OpenAPI | passed | `api/openapi/agent-workbench.yaml` 解析通过，12 个 operation 无 `additionalProperties: true` 泛型兜底；消息、事件回放和 snapshot 使用命名 DTO。 |
| OpenAPI 占位名 | passed | `rg -n "JsonBody\|ApiResponse\|PageResponse" api/openapi` 无输出。 |
| AG-UI schema 与 fixture | passed | AG-UI schema 覆盖 40 个 payload 分支；`python3 tests/agent/agui/validate_fixtures.py` 通过 9 个 fixture，含 `additional_input_resume_safety.json`。 |
| 业务 RPC fixture | passed | `python3 tests/contract/validate_fixtures.py` 通过 16 个原子 RPC fixture 和 38 个领域场景，覆盖 accountspace、project、skill、tool、model、credit、asset。 |
| 业务 HTTP fixture | passed | `python3 tests/contract/validate_fixtures.py` 通过 11 个 HTTP fixture，覆盖 200、401、403、404、409、422、500。 |
| 业务 seed | passed | `tests/business/seed` 覆盖初始管理员、个人空间、企业空间、归档项目、跨空间资产、公开作品、兑换码绑定、低余额账号。 |
| SQL 门禁 | passed | `rg -n "FOREIGN KEY\|REFERENCES" db/migrations api code-plan` 无输出；无数据库级外键或引用约束关键字。 |

## 执行命令

```bash
export GOROOT=/Users/figo/sdk/go1.26.3
export GOPATH=/Users/figo/go
export PATH="$GOROOT/bin:$GOPATH/bin:$PATH"

kitex -module github.com/FigoGoo/Dora-Agent -record api/thrift/business_agent_service.thrift
go mod tidy
go test ./...
python3 tests/agent/agui/validate_fixtures.py
python3 tests/contract/validate_fixtures.py
scripts/validate-toolchain-contract-baseline.sh
```

## 阻塞问题

| 问题 | 影响路径 | 处理结果 |
| --- | --- | --- |
| RPC 契约事实源不一致 | A5 RPC client、B1 RPC server | 已按 code-plan 调整 Thrift、fixture 并重新生成 Kitex。 |
| AG-UI schema 未覆盖 canonical payload | Agent event publisher、SSE replay、QA fixture | 已补 40 个 payload 分支、9 个 fixture 和 schema 覆盖门禁。 |
| Contract fixture 覆盖不足 | B1 服务联调、RPC contract test | 已补七个领域目录和 38 个领域场景，并按 IDL 方法覆盖验证。 |
| Agent OpenAPI 泛型兜底 | Agent API 开发和回放补偿 | 已替换为命名 DTO。 |
| 业务 HTTP 幂等规则缺口 | 业务写操作、preview 操作、幂等冲突 | 已强制所有写操作携带 `Idempotency-Key` 和必填 `request_hash`。 |
| 测试报告过早通过 | M0 Done Gate | 已撤回轻量结论，本报告仅记录真实执行结果。 |

## 回归风险

- 后续修改 `api/thrift/business_agent_service.thrift` 后必须重新执行 Kitex 生成并提交 `kitex_gen/**`。
- 后续新增业务 HTTP operation 时必须增加命名 request / response DTO，写操作必须带 `Idempotency-Key` 和必填 `request_hash`。
- 后续新增 AG-UI event 时必须同步 schema 分支、payload required 表和最小 fixture。
- 后续新增 RPC 方法时必须补充 contract fixture；validator 会直接扫描 IDL 方法覆盖。

## 结论

M0 严格技术基线通过。当前交付物可支撑后续 A1/B1/A5 消费，但进入下一阶段前必须以 Git 提交形成冻结点。
