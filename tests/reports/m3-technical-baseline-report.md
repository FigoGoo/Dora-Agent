# M3 配置能力技术基线报告

状态：已通过 `go test -count=1 ./...`，并由 `scripts/validate-m3.sh` 作为冻结门禁复核
日期：2026-06-27
范围：业务 06/07/08、业务 10 元素类型只读字典子集、Agent 02/08/12/13、Agent 04 token 鉴权、Agent 07 中 `ResolveAuthContextFromToken` 与 Skill / Tool / Model / PlatformDictionary RPC 子集；不含前端、部署上线文档和 M4 积分资产闭环。

## 已执行验证

- `scripts/validate-m0.sh`：由 `scripts/validate-m3.sh` 串行执行。
- `scripts/validate-m1.sh`：由 `scripts/validate-m3.sh` 串行执行。
- `scripts/validate-m2.sh`：由 `scripts/validate-m3.sh` 串行执行。
- `go test -count=1 ./...`：已单独执行通过，并由 `scripts/validate-m3.sh` 再执行。
- `scripts/validate-m3.sh`：M3 冻结门禁入口。
- `rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan services internal`：由 `scripts/validate-m3.sh` 串行执行。

## 验收覆盖

- 业务 RPC：`AccountSpaceService.ResolveAuthContextFromToken`、`SkillCatalogService.ListRoutableSkills`、`SkillCatalogService.GetPublishedSkillSpec`、`SkillCatalogService.GetReviewCandidateSkillSpec`、`SkillCatalogService.SaveSkillTestResult`、`ToolCapabilityService.CheckToolExecutionPolicy`、`ModelConfigService.ListAvailableGenerationModels`、`ModelConfigService.ResolveDefaultModel`、`ModelConfigService.ResolveGenerationModelSnapshot`、`PlatformDictionaryService.ListAssetElementTypes` 不再返回 M3 范围内的 `NOT_IMPLEMENTED`。
- 业务 HTTP：`/api/models/generation`、`/api/tools/bindable`、`/api/skills/**`、`/api/admin/models/**`、`/api/admin/tools/**`、`/api/admin/skills/**`、`/api/asset-element-types`、`/api/admin/asset-element-types` 与 OpenAPI route parity 校验。
- Agent API：Authorization token 通过业务 RPC 解析，Agent 不信任 `X-Actor-User-Id` / `X-Space-Id` 作为身份事实；SSE 支持 `Last-Event-ID` replay 和 heartbeat。
- Agent Runtime：`runtime/eino`、`runtime/turnloop`、`runtime/skill`、`runtime/tool`、`runtime/safety`、`runtime/modeltool`、`runtime/skilltest`、`events/stream` 包结构存在并可编译。
- Agent TurnLoop 基线：创建 run 后加载 Published Skill、Tool policy、默认模型快照、元素字典，写入安全 passed 证据，并明确输出 M4 积分资产 deferred 状态。
- fixture/seed：M3 RPC fixture 覆盖 token 成功/跨空间、Skill、Tool、Model、Dictionary、SkillTest；业务 seed 包含模型、Tool、Published Skill、3 个 Skill 测试样例和 14 个 active 资产元素类型。

## 未执行项

未执行项：无（M3 范围内）。

## 范围外后续项

Credit / Asset / AssetCreditCommit RPC、真实供应商生成、TOS 上传、资产保存、积分预估冻结扣费释放属于 M4，不在本报告中标记为通过。

## 阻塞问题

无。
