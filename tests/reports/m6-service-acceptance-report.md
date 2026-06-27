# M6 服务级验收报告

## 范围

M6 覆盖非前端、非部署范围的服务级收口：RPC、HTTP、AG-UI、DB、fixture、测试报告和跨服务主链路门禁。事实源以 `code-plan/README.md`、`code-plan/tests/00-服务级测试计划与验收矩阵.md`、`code-plan/agent/14-测试验收场景与边界验证设计.md`、`code-plan/business/14-数据库迁移种子测试数据与验收场景设计.md`、`code-plan/business/15-业务AgentService-RPC服务端契约实现设计.md` 为准。

## 执行环境

- Go: `/Users/figo/sdk/go1.26.3`，`go version go1.26.3 darwin/arm64`
- GOPATH: `/Users/figo/go`
- 本地依赖: Testcontainers PostgreSQL、fake/local TOS、fake/local 模型供应商
- 证据日志: `/tmp/dora-validate-m6.log`

## 验证命令

| 命令 | 结果 | 证据 |
| --- | --- | --- |
| `go version` | passed | `go version go1.26.3 darwin/arm64` |
| `scripts/validate-m0.sh` | passed | M0 strict baseline 在 `scripts/validate-m6.sh` 中串行通过 |
| `scripts/validate-m1.sh` | passed | M1 config、DB、repository、Go tests 在总门禁中通过 |
| `scripts/validate-m2.sh` | passed | M2 HTTP/RPC/Agent API targeted tests 和 full Go tests 通过 |
| `scripts/validate-m3.sh` | passed | M3 semantic source checks、fixture、Go tests 通过 |
| `scripts/validate-m4.sh` | passed | M4 资产积分闭环、AG-UI fixture、contract fixture 通过 |
| `scripts/validate-m5.sh` | passed | M5 作品公开/通知 route parity、fixture、Go tests 通过 |
| `go test -count=1 ./...` | passed | M6 full Go tests 在总门禁中通过 |
| `scripts/validate-m6.sh` | passed | 退出码 0，尾部输出 `M6 service acceptance baseline passed` |
| `rg -n "FOREIGN KEY\|REFERENCES" db/migrations api code-plan services` | passed | M6 no database-level FK 扫描通过 |

## RPC

RPC 账本已覆盖 18 个服务：`AccountSpaceService`、`EnterpriseService`、`AdminService`、`UserAdminService`、`ProjectService`、`ProjectAssetService`、`AssetService`、`CreditService`、`AssetCreditCommitService`、`SkillCatalogService`、`ToolCapabilityService`、`ModelConfigService`、`PlatformDictionaryService`、`WorkService`、`WorkShareService`、`FeaturedWorkAdminService`、`PublicContentService`、`NotificationService`。

`scripts/validate-m6.sh` 会解析 Thrift 并要求方法集合与 M6 账本完全一致，同时校验 Kitex 生成目录、`RegisterAll` 注册、handler 方法和 `tests/contract/validate_fixtures.py` 的 per-method fixture 覆盖。

## HTTP

HTTP 验收通过 M2-M5 route parity 和 M6 关键 route 检查兜底：身份/项目、后台用户状态、模型/Tool/Skill、积分/资产、作品公开、后台下架、通知列表/已读/跳转。写操作仍由既有中间件和应用测试覆盖 `Idempotency-Key`、canonical request hash、actor/admin 维度隔离和审计。

## AG-UI

AG-UI runtime 事件类型全部在 `api/agui/agent-workbench-events.schema.json` canonical 集合内。fixture 验证覆盖 event_id、sequence、trace_id、Last-Event-ID replay、gap 补偿、unknown event 和 snapshot fallback。

## DB

DB 验收覆盖 Agent/Business migration up/down 成对、seed 可执行、无数据库级外键、业务库不出现 Agent runtime 表、Agent 库不出现业务事实表，关键事务回滚由 Go 集成测试覆盖。

## 未执行项

本轮 M6 门禁未记录到跳过命令。若后续环境缺少 Docker/Testcontainers 或 fake adapter，必须重新执行 `scripts/validate-m6.sh` 后再判定冻结点有效。

## 当前阻塞记录

本轮验证未发现阻断项。历史执行中发现并修复了两处门禁误报：Kitex 转义方法 `SaveSkillTestResult_` 的识别、生产代码边界扫描误包含 `_test.go`；最终脚本已重新完整执行并通过。
