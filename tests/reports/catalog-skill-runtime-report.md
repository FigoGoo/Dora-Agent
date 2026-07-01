# M3 配置能力技术基线报告

状态：已通过 `go test -count=1 ./...`，并由 `scripts/validate-catalog-skill-runtime.sh` 作为冻结门禁复核
日期：2026-06-28
范围：业务 06/07/08、业务 10 元素类型只读字典子集、Agent 02/08/12/13、Agent 04 token 鉴权、Agent 07 中 `ResolveAuthContextFromToken` 与 Skill / Tool / Model / PlatformDictionary RPC 子集；不含前端、部署上线文档和 M4 积分资产闭环。

## 已执行验证

- `scripts/validate-toolchain-contract-baseline.sh`：由 `scripts/validate-catalog-skill-runtime.sh` 串行执行。
- `scripts/validate-engineering-baseline.sh`：由 `scripts/validate-catalog-skill-runtime.sh` 串行执行。
- `scripts/validate-account-agent-http.sh`：由 `scripts/validate-catalog-skill-runtime.sh` 串行执行。
- `go test -count=1 ./...`：已单独执行通过，并由 `scripts/validate-catalog-skill-runtime.sh` 再执行。
- `scripts/validate-catalog-skill-runtime.sh`：M3 冻结门禁入口。
- `rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan services internal`：由 `scripts/validate-catalog-skill-runtime.sh` 串行执行。

## 验收覆盖

- 业务 RPC：`AccountSpaceService.ResolveAuthContextFromToken`、`SkillCatalogService.ListRoutableSkills`、`SkillCatalogService.GetPublishedSkillSpec`、`SkillCatalogService.GetReviewCandidateSkillSpec`、`SkillCatalogService.SaveSkillTestResult`、`ToolCapabilityService.CheckToolExecutionPolicy`、`ModelConfigService.ListAvailableGenerationModels`、`ModelConfigService.ResolveDefaultModel`、`ModelConfigService.ResolveGenerationModelSnapshot`、`PlatformDictionaryService.ListAssetElementTypes` 不再返回 M3 范围内的 `NOT_IMPLEMENTED`。
- 业务 HTTP：`/api/models/generation`、`/api/tools/bindable`、`/api/skills/**`、`/api/admin/models/**`、`/api/admin/tools/**`、`/api/admin/skills/**`、`/api/asset-element-types`、`/api/admin/asset-element-types` 与 OpenAPI route parity 校验。
- Agent API：Authorization token 通过业务 RPC 解析，Agent 不信任 `X-Actor-User-Id` / `X-Space-Id` 作为身份事实；SSE 支持 `Last-Event-ID` replay 和 heartbeat；`GetRun`、`ListMessages`、`ReplayEvents`、`Snapshot`、`CancelRun` 均按 `view` 调用业务项目权限，且检查响应体 `allowed=false`。
- Agent Run 输入：`referenced_assets` / `control_inputs` 完成入参校验，并写入 `agent_runs.input_summary` 的资产引用、控制输入、安全目标和生成计划摘要。
- AG-UI Runtime：Agent 运行时事件只写入 `api/agui/agent-workbench-events.schema.json` 已定义 canonical 类型；事件 DTO 顶层字段包含 `event_id`、`sequence`、`trace_id`、`session_id`、`project_id`、`space_id`、`actor_user_id`、`timestamp`、`component`、`payload`。
- Agent Runtime：`runtime/eino`、`runtime/turnloop`、`runtime/skill`、`runtime/tool`、`runtime/safety`、`runtime/modeltool`、`runtime/memory`、`runtime/skilltest`、`domain/event`、`events/agui`、`events/stream` 包结构存在、可编译，并由 TurnLoop / modeltool / skilltest 单测覆盖核心行为。
- Agent TurnLoop 基线：创建 run 后加载 Published Skill、Tool policy、默认模型快照、元素字典，写入 `safety.prompt.evaluating` / `safety.prompt.evaluated` 事件和安全证据；`agent_runs.skill_selection` 保存 skill scope、命中/兜底原因、tool refs digest、运行空间和计费账户作用域；用户可见 `generation.progress` 不暴露 `provider_runtime_ref`。
- 追加输入/确认/取消：追加输入执行安全重评并创建 safety evidence；确认恢复和追加输入通过 `ResumeTurn` 推进到 running；取消通过 `CancelRun` 决策后持久化 cancelled。
- Skill Test：`SaveSkillTestResult` 使用 `request_meta.idempotency_key=skill_test:<test_run_id>` 与 request hash 区分 replay/conflict；`SafetyEvidenceDTO(scene=skill_test)` 缺失、过期、trace 不匹配或状态不一致会拒绝；`GetReviewCandidateSkillSpec` 对非管理员执行 owner/可见性校验。
- Skill 输出校验：`runtime/skilltest` 校验 3 个样例、安全阻断、必需元素、未知元素类型、usage stage 和 render hint。
- fixture/seed：M3 RPC fixture 覆盖 token 成功/跨空间、Skill、Tool、Model、Dictionary、SkillTest；业务 seed 包含模型、Tool、Published Skill、Skill confirmation policy、3 个 Skill 测试样例和 14 个 active 资产元素类型。
- schema/migration：`skill_versions.confirmation_policy_json` 已由 0014 增量 migration 落库；`skill_test_runs.idempotency_key/request_hash` 已由 0015 增量 migration 落库；`AssetElementTypeDTO` 对齐 `resource_type/status/usage_stage/draft_enabled/final_enabled/editable/referable/render_hint/schema_hint_json/render_hint_json`。

## 未执行项

未执行项：无（M3 范围内）。

## 范围外后续项

Credit / Asset / AssetCreditCommit RPC、真实供应商生成、TOS 上传、资产保存、积分预估冻结扣费释放属于 M4，不在本报告中标记为通过。

## 阻塞问题

无。
