# code-plan 遗漏核对 · 初版代码复核矩阵

> 上游：[`code-plan-遗漏审核报告.md`](./code-plan-遗漏审核报告.md)（对 code-plan **设计文档**的 51 条遗漏审核）
> 本文：把那 51 条逐条落到**初版代码**（`services/agent` + `services/business` + `api/` + `db/`）核对真实状态：已修 / 部分 / 仍缺 / 不适用
> 方法：5 路并行只读核对，每条带 `file:line` 证据，对照承重墙/接缝不变量
> 日期：2026-06-28　核对方：Claude Code（cc-yr，W1 分析先行）
> 行号以核对时 `cc-yr` 为准；逐条以最新代码复核。

## 总评

**初版实现质量明显高于 code-plan 设计阶段。** 审核报告里"契约冻结前必清"的 **7 条接缝硬冲突初版已全部收敛**（无 codegen 阻断、无联调当场崩）；多条高优先（GEN-1 生成落盘链、GEN-3 additional_input 安全重评、TURN-1 resume 事件、SKILL-1 确认规则持久化、WORK-1 企业作品越权）**已修复**。

真实仍缺集中在 8 个面：**① 业务库数据底座（软删/公共列全库缺，头号）② 安全 digest 一致性（评 A 发 B）③ 崩溃恢复/资金闭环（依赖异步队列，对应 W3 Redis）④ 企业空间越权细则 ⑤ 审计 DB 级不可变 ⑥ 可观测性体系 ⑦ Skill 输出元素双态 ⑧ 断线确认恢复**。

## 统计

| 结论 | 数量（按判定） | 含义 |
|---|---|---|
| ✅ FIXED / NA | ~33 | 初版已闭合或设计已变不适用 |
| 🟡 PARTIAL | ~14 | 有骨架但口径不全 / 仅隐式实现 / 存了没接通 |
| 🔴 MISSING | ~24 | 仍缺，需改造 |

> 接缝组（SEAM-1~7 + 2 附录）单独结论：**100% 已清**，下表不再展开，仅在末尾备忘。

---

## 🔴 P0 — 承重墙级真实缺口（最高优先）

| ID | 缺口 | 证据 | 应落 |
|---|---|---|---|
| **INFRA-1** | 业务库 63 表仅 3 表有 `deleted_at`，`updated_by` 全库为 0，`tenant_id` 仅 2 表；GORM 用裸 `*time.Time` 非 `gorm.DeletedAt`，软删各写各的 | `db/migrations/.../0001..0018` + `infra/repository/businesscore/models*.go` | business 全域 + 02 基线 |
| **GEN-5** | 评 A 发 B：安全评估 digest 在 start 阶段按当时 prompt 算，发模型时另取 `latestUserPrompt`，二者无一致性断言 → 可让"已评估"证据为一段文本背书、实际生成另一段（安全 fail-closed 旁路）| `agent workbench/app.go:1996`(评估) vs `1749/1756`(发模型) | agent 08+13 |
| **INFRA-13** | 审计/流水表无 DB 级 append-only（无 REVOKE/RULE/TRIGGER），不可变仅靠应用约定，直连 SQL 可静默篡改 | `db/.../0001`(business_audit_logs)、`0011`(work_moderation_records) | business 02 |
| **ACCT-3** | 企业积分流水 member 可见全量，未实现"owner 看全 / member 看本人"可见性分级（越权红线）| `credit/app.go:286-306` vs `listLedger ~1024-1037` | business 03+09 |
| **ACCT-1b** | 缺"绑定 Tool 不在企业白名单 → 该 Skill 不可用"禁用门（有白名单数据但无预先禁用）| `toolpolicy/app.go` + `skillcatalog/app.go` | business 03/05/08 |
| **SKILL-2** | Skill 输出元素 schema 仅 `element_type/schema_json/required`，缺草稿/最终双态（双态只活在字典表，per-skill 约束拿不到，Skill 完整定义被削）| `db/.../0007_skill_catalog_review.up.sql:67-76` | business 08 + agent 12 |
| **ACCT-8** | 平台管理员越权红线本域未对称成文/强制（admin 与 user 独立鉴权，无"管理员不得跨入业务空间归属"红线检查）| `accountspace/app.go` + `admin/app.go` | business 03/04 |

## 🟠 P1 — 高危：可用性 / 资金 / 联调断点

| ID | 缺口 | 证据 | 应落 / 关联 |
|---|---|---|---|
| **GEN-6** | 生成全链同步跑在确认 HTTP 请求内，无异步 worker / 重启恢复 / 对账；崩在 Freeze 后 commit 前 → 冻结悬挂至 `expires_at` | `api/http/workbench_handlers.go:146`→`app.go:973`→`1701`(全程同步)；全仓无 worker/recover | agent 05/13 · **W3 Redis 队列+对账** |
| **TURN-8** | `SnapshotResponse` 缺 `interrupt` 字段 → 断线在待确认态恢复不出确认面板，run 卡死 | `agent workbench/app.go:636-645,1136-1139` | agent 04 对齐 10 |
| **GEN-7** | `EstimateGenerationCredits` 唯一未用幂等卫的写 RPC，确认前重复预估插孤儿 `credit_estimates` | `credit/app.go:308-339,891`(无 guard.Begin) | business 09 |
| **INFRA-9** | Agent 运行期配置加载链断开：`agent_runtime_configs` 表 + `GetActiveRuntimeConfig` 都在，但运行期从不调用，`configVersion` 仅作字符串标签（配置化只有壳）| `config.go` + `main.go:47` + 仅测试调用 loader | agent 11/03 |
| **TURN-7** | reject 直接终结 run 为 failed，无"改模型→取消确认→重估"回退闭环 | `agent workbench/app.go:1003-1016`；state.go 无 waiting_confirmation→running 边 | agent 05 |
| **ACCT-4** | 移除成员仅置 status=removed + 审计，未对该成员运行中的 active run/task 做终止/拒绝处置 | `accountspace/app.go:828-880` | business 03 + agent 05 |

## 🟡 P2 — 中危：体系化缺口

| ID | 缺口 | 证据 | 应落 |
|---|---|---|---|
| INFRA-3 | 业务侧无任何指标定义（counter/histogram/gauge）| services/business 全域 | business 02/14 |
| INFRA-5 | 跨服务仅手工透传明文 `trace_id` 字符串，非 W3C traceparent/OTel context（OTel 仅 indirect 依赖）| `business_gateway.go:599-604` + `go.mod` | agent 11 + business 02 |
| INFRA-6 | 业务错误码扁平 ~19 值，未对齐 7 类领域分类 | `pkg/errors/error.go:10-30` | business 02 |
| INFRA-7 | agent 错误模型扁平 Code（~10），无 category 维度；`error_type` 仅 2 临时值 | `apperror/error.go:10-21` | agent 11 |
| INFRA-10 | 配置实际仅 默认→文件→env 三层，etcd 不是配置源（仅供 Kitex registry），缺 etcd 非敏感加载层 | `infra/config/config.go:113` + `bootstrap/app.go` | business 01/15 + agent 11 |
| INFRA-11 | 业务状态机无成文"流转矩阵"（常量内联）；非法跳转靠隐式守卫（work/credit 有部分测试）| `work/app.go:30-36` + `credit/app.go:394-411` | business 各域+14 |
| TURN-6 | `chat.controls.locked` 已定义但 services/agent 无 emit 点（仅测试白名单）；required 锁 vs accepted 锁未辨析 | 草案:115,124；代码无 emit | agent 06/05 |
| TURN-10 | `payload_schema_version` 存 DB 且 emit 硬编码，但未进 AG-UI 信封 schema，旧前端降级读法未定义 | `models.go:84` + `api/agui/...schema.json:6-19` | agent 06 |
| WORK-3 | 作品 category 作自由字符串写入，无"内置单选+active 字典"校验（字典表存在但应用层不查）| `work/app.go:286-356,358-465` | business 12 |
| WORK-4 | 无"最后一个 active 管理员"计数守护，DisableAdmin 仅拦 self → 可锁死 | `admin/app.go:429-472` | business 04 |
| WORK-5 | 后台 7 模块 owner 归属无声明/枚举 | `admin/app.go` 全域 | business 04+各域 |
| SKILL-9 | 二级兜底仅静默回退文本模型，无"不推销/不建议建 Skill"成文规则或测试 | `skill/router.go:43` + `workbench/app.go:2158-2168` | agent 08 |
| SKILL-10 | 运行期确实逐个复检 Tool 策略，但 tool_bindings 无"需运行期逐个复检白名单"显式标注（靠隐式）| `workbench/app.go:1380-1394` | agent 08 |

## 🔵 P3 — 低危：口径精化 / 显式化防回归

| ID | 缺口 | 证据 |
|---|---|---|
| GEN-4 | safety evidence 过期只硬失败+全额释放，无重评续跑（fail-closed 成立，缺可用性）| `assetcommit/app.go:566-573` + `agent app.go:1922-1937` |
| GEN-8 | commit 端缺"部分 artifact 缺元素"的部分结算+定额释放，当前全回滚+全释放（不漏释放但不精确）| `assetcommit/app.go:321,264-268` |
| WORK-7 | `AdminUserDetailDTO.spaces[]` 为 `[]map[string]string` 弱类型，非字段级白名单 | `admin/app.go:142-147` |
| WORK-9 | `business_action` 共 43 码散落字面量，与 PRD 11 模块大体对齐但无中心枚举 | services/business 散落 |
| WORK-10 | 点赞 `like_count` 用 `GREATEST(...,0)` 保不为负 + 唯一约束防重复，但无防刷频控、无成文不变量 | `work/app.go:945-953` + `0011/0018` |
| WORK-8 | 管理员不能改用户密码（实现保证：无端点写 password_hash），但红线未成文 | `admin/app.go:285-343` |
| INFRA-2 | agent 侧第 11 表 safety_evaluations 已入迁移+边界测试；业务侧 14 表白名单未成文、无对标断言 | `repository_integration_test.go` vs `idempotency_integration_test.go` |
| INFRA-4 | 结构化 logger 仅保证 service/env/trace_id 三字段，其余散落各 payload，无统一强制字段集 | `observability/logger.go:28,36` |
| INFRA-14 | `ListAssetElementTypes` 默认 50（字典例外正确），但 fixture 未标"默认 50≠通用默认 10"差异 | `tests/contract/fixtures/.../list_asset_element_types_success.json` |
| ACCT-6 | 无显式未登录访客边界（所有入口要求 auth.UserID）| `accountspace/app.go` 全域 |
| ACCT-7 | 无"未登录→登录回原意图"承接字段/流程（pending_intent/return_to 缺）| services/business 全域 |
| TURN-4 | 排序靠 sequence 已确立，但"timestamp 仅展示、不作排序"显式规则未复述 | 草案:244 vs 86-87 |

---

## ✅ 已闭合（FIXED/NA）— 证明初版进步

- **接缝硬冲突全清**：SEAM-1 `credit_account_scope`+`_id` 双字段、SEAM-2 `safety_evidence` required、SEAM-3（NA，改用 PERMISSION_DENIED 错误码）、SEAM-4 `ResolveGenerationModelSnapshot`、SEAM-5 归 `SkillCatalogService`、SEAM-6 `final_elements` 强类型 struct、SEAM-7 `schema_hint_json`+`sort_order:i32`；附录 `skill_scope` 映射成立、`ResolveCurrentSpaceContext` 命名统一。
- **高优先已修**：GEN-1（落盘链 PrepareObjects→上传 TOS→Commit 完整）、GEN-2、GEN-3（additional_input 前 `recordPromptSafetyEvaluation` 强制重评）、TURN-1（emit `confirmation.accepted`/`resume.accepted`）、SKILL-1（`confirmation_policy_json` 全链落地、专用 skill_confirmation interrupt）、SKILL-3（14 类种子 seed 含 draft/final_enabled）、SKILL-4/5/6/7/8、WORK-1（`getOwnedWorkTx` 校验 active enterprise member，被移出原作者挡住）、WORK-2（专用 `work_public_taken_down` 通知）、WORK-6、WORK-8、ACCT-1a/2/5、INFRA-12（幂等 `(tenant_id,scope,key)` 隔离）、TURN-2/5/9/11、TURN-3(NA)。

---

## 与后续工作线关联

- **W3 Redis** 直接救 **GEN-6**（异步 worker + 崩溃恢复 + 冻结对账清扫）；GEN-7/ACCT-4 也受益于队列化处置。
- **W4 测试** 优先覆盖本表 MISSING 项（尤其 P0/P1），并补 runtime/safety、TurnLoop 24 场景、管理端全线的自动化。
- **W2 闭环** 核对时以本表为断点清单逐域走查。

## 建议改造批次（每批改造前按改造铁律进 plan 对齐、逐条标注约束）

1. **批 A 数据底座先行**：INFRA-1（公共列+软删基线）→ 改一次触达全库，越早做返工面越小；连带 INFRA-13（审计 append-only）、INFRA-12 已 FIXED 可作范式。
2. **批 B 安全/越权红线**：GEN-5（评 A 发 B digest 断言）+ ACCT-3/ACCT-1b/ACCT-8（企业越权细则）。
3. **批 C 确认恢复闭环**：TURN-8（snapshot interrupt 字段）+ TURN-7/TURN-6（确认锁定/回退）。
4. **批 D 配合 W3**：GEN-6/GEN-7/ACCT-4（崩溃恢复+幂等+active run 处置）。
5. **批 E 可观测体系**：INFRA-3/5/6/7（指标/trace/错误分类）。
6. **批 F 收尾**：P2 剩余 + P3。

> 接缝组无待办。SEAM-3 仅需把设计文档措辞同步为"PERMISSION_DENIED 错误码"，不影响代码。

---

## 批 A 完成记录（2026-06-28 · INFRA-1 / INFRA-13）✅

提交：`64235b1`(0019 公共列 + 0020 append-only) · `54a8c8c`(0020 补 skill_review_records) · `5171b53`(model 软删 52 struct)
验证：`go test ./...` 全绿（agent 8 包 + business 20 包，testcontainer 跑完整 migration up/down）。

- **INFRA-1 ✅ 已修**：续号迭代 `0019` 为 53 张可变业务表补 `created_by/updated_by/deleted_at`；model 层 52 个 struct 启用 `gorm.DeletedAt` 自动软删（User/Asset/Work 由裸 `*time.Time` 死字段激活）。
- **INFRA-13 ✅ 已修**：`0020` 对 **10 张** append-only 审计/流水表加 `BEFORE UPDATE OR DELETE` 触发器（DB 级不可篡改，任何角色含 superuser 都拦）。

范围决策（逐条可追溯）：
- `tenant_id` 未全表加——沿用 `space_id` 为隔离键，避免冗余。
- `created_by/updated_by` 仅 schema 加列预留，model **不映射**——与现有领域操作者字段（`CreatedByAdminID` 等）冗余；operator 回填留子切片。
- append-only 10 张表不软删、不映射 DeletedAt；`skill_review_records` 经代码核实为 insert-only 事件流，与 `work_moderation_records` 对称纳入。

遗留（后续子切片，不阻塞）：
- **created_by/updated_by operator 回填**：贯穿各写路径从 auth context 取操作者。
- **软删唯一约束复用评估**：`email/account_no/各 _no` 等自然键软删后仍占用，若需"软删后复用"须补 partial unique index（`WHERE deleted_at IS NULL`）。当前业务多用 `status` 而非软删，暂不阻塞。
- 明细/价格表已纳入软删（可软删停用），如需改 append-only 另议。

## 批 B 完成记录（2026-06-28 · 安全/越权红线 + SKILL-2）✅

提交：`80a35f1`(GEN-5) · `513ae4a`(ACCT-3) · `6892e07`(ACCT-1b) · `04c2b4f`(ACCT-8) · SKILL-2：`9dfe953`/`ea79a91`(设计) + `32c8cce`(FP1) `72750f7`(FP2) `142c2a7`(FP3a)
验证：各条独立 testcontainer 测试通过；business `go build ./...` 全绿。

- **GEN-5 ✅**：发模型前断言 prompt 与安全证据 `EvaluatedObjectDigest` 一致，不一致 fail-closed（杜绝评 A 发 B）。
- **ACCT-3 ✅**：企业积分流水按角色分级——owner 看全量、member 仅本人 project 流水（`project.owner_user_id` 关联过滤，无 schema 改动）。
- **ACCT-1b ✅**：绑定被企业/空间白名单显式禁用 Tool 的 Skill 不可路由；批量两查（deny 规则 + bindings）避免 N+1。
- **ACCT-8 ✅（成文+固化，不改行为）**：admin 与 user 两套独立鉴权本已结构性隔离，补安全规范对称红线 + `GetUserSummary` 注释 + 测试固化（防回归）。
- **SKILL-2 🟡 business 侧已落（FP1–FP3a），FP3b/FP4 待**：
  - FP1 ✅ `assetdict` 字典上限批量读取（复用 `schema_json` 内嵌，无迁移）。
  - FP2 ✅ `0021` per-skill 输出元素结构 schema + `SaveSkill` 写入/字典上限校验。
  - FP3a ✅ thrift 声明 `SkillOutputElementDTO` + `SkillSpecResponse.output_elements`；application `GetPublishedSkillSpec` 装配。
  - FP3b ⏳ 待 codegen 环境：`kitex` 重新生成 kitex_gen + RPC handler 映射 + contract fixture。
  - FP4 ⏳ agent 12：按 `output_elements` 组织产物，草稿落 `agent_artifacts`、最终走 `CommitGeneratedAssetAndCharge`。
  - 设计：`docs/contracts/rpc/SKILL-2-输出元素结构契约设计.md`。

范围决策：
- ACCT-3 用 project 关联过滤而非加 user 维度列（`CreditLedgerEntry` 无 user 维度但有 `project_id`）。
- ACCT-8 现状无 bug、与承重墙一致，故只成文+固化，不新增 admin 跨入通道（未来受控跨入留待裁决）。
- SKILL-2 FP1 经实现期核查退化：字典双态属性已由 `asset_element_types.schema_json` 内嵌承载，取消表列迁移（外科手术式改动）。

遗留（后续，不阻塞）：
- SKILL-2 FP3b（codegen + RPC 映射 + fixture）、FP4（agent 消费）。
- `ReviewCandidateSkillSpecResponse` 是否同步补 `output_elements`（设计 §5 待确认 4，建议补）。

## 批 C 完成记录（2026-06-28 · 确认恢复闭环）✅

提交：`7de584c`（TURN-8/7/6/10）
验证：`go test ./services/agent/internal/application/workbench ./services/agent/internal/api/http ./services/agent/internal/runtime/turnloop` 通过；`python3 tests/agent/agui/validate_fixtures.py` 通过。

- **TURN-8 ✅ 已修**：`SnapshotResponse` 增加 `interrupt`，断线恢复时从 `agent_interrupts` 的 required interrupt + 最近 `confirmation.required` 事件恢复确认面板；`confirmation_payload` 仅暴露前端可用字段，过滤模型快照、安全证据、预估详情、供应商运行引用、Prompt 和密钥引用。
- **TURN-6 ✅ 已修**：创建 `confirmation.required` 后服务端同步 emit `chat.controls.locked`，锁定 `model_selection`、`control_inputs`、`referenced_assets`，明确 required 态即锁控件。
- **TURN-7 ✅ 已修**：拒绝确认不再把 run 标为 failed；改为 `interrupt -> rejected`、run -> `cancelled`，emit `confirmation.rejected` + `agent.run.cancelled`，释放 active-run 占用，用户改模型/参数后用新 run 重新预估。
- **TURN-10 ✅ 已修**：AG-UI `EventDTO` 暴露 `payload_schema_version`；schema 将其列为可选信封字段并定义历史缺省 `2026-06-27`，避免旧 fixture/历史事件重放被破坏。

范围决策：
- snapshot 恢复不回传完整内部确认 payload，仅回传前端重建确认面板所需的脱敏字段。
- reject 的“重估闭环”采用新 run 重新预估，而不是在同一 run 内新增 waiting_confirmation -> running -> waiting_confirmation 复杂分支；与当前状态机、API 契约和 fixture 的 `cancelled` 语义一致。
- `payload_schema_version` 先做可选信封字段；新服务事件必带，旧事件缺失按 `2026-06-27` 解析。

## 批 D 子切片记录（2026-06-28 · GEN-7）✅

提交：`33b835e`（GEN-7）
验证：`go test ./services/business/internal/application/credit` 通过。

- **GEN-7 ✅ 已修**：`EstimateGenerationCredits` 接入业务幂等卫，scope=`credit.estimate_generation`；同 key 同请求 replay 返回原 `estimate_id`，不再重复插入 `credit_estimates` / 孤儿预估；同 key 不同模型/数量/tool items/安全证据 digest 返回 `IDEMPOTENCY_CONFLICT`。

范围决策：
- 仅修复核对项点名的生成预估写 RPC；`EstimateToolCredits` 暂不扩大行为面。
- replay 从 `credit_estimates` + `credit_estimate_items` 还原 DTO，保持第一次预估结果对调用方稳定。

## 批 D 子切片记录（2026-06-28 · INFRA-9）✅

提交：`4607b04`（INFRA-9）
验证：`go test ./services/agent/internal/application/workbench ./services/agent/internal/api/http` 通过。

- **INFRA-9 ✅ 已修**：创建 run 时优先读取 `agent_runtime_configs` 中 `config_key=agent.default` 的 active 版本，并写入 `agent_runs.runtime_config_version`；无 active 配置时才回退构造参数 `configVersion`。

范围决策：
- 本切片先打通运行期配置加载链路和版本落库，不解释 `content` 中的策略参数；策略解释器留给后续 W2/W3 功能闭环。

## 批 F 子切片记录（2026-06-28 · WORK-4）✅

提交：`1305d44`（WORK-4）
验证：`go test ./services/business/internal/application/admin` 通过。

- **WORK-4 ✅ 已修**：`DisableAdmin` 在事务内锁定目标管理员并检查除目标外仍存在 active 管理员；若目标是最后一个 active 管理员，返回 `STATE_CONFLICT`，避免后台账号全部锁死。

范围决策：
- 保持“不能禁用当前会话 admin”的原有入口保护；新增最后 active 管理员守护作为领域不变量，覆盖未来批处理或内部调用绕过当前会话保护的风险。

## 批 F 子切片记录（2026-06-28 · WORK-3）✅

提交：`e168bf4`（WORK-3）· `711a48b`（测试同步）
验证：`go test ./services/business/internal/application/work` 通过。

- **WORK-3 ✅ 已修**：`CreateWork` / `UpdateWork` 对非空 `category` 校验 `work_categories.category_key` 且必须为 active；非法自由字符串返回 `INVALID_ARGUMENT`。

范围决策：
- 空 category 仍允许，保持存量作品兼容；第一版 active 字典以 seed/migration 中的 `work_categories` 为准。

## 批 E 子切片记录（2026-06-28 · INFRA-6 / INFRA-7）✅

提交：`aba4465`（INFRA-6/7）
验证：`go test ./services/business/internal/pkg/errors ./services/agent/internal/apperror ./services/business/internal/transport/http ./services/agent/internal/api/http` 通过；`python3 tests/agent/agui/validate_fixtures.py` 通过。

- **INFRA-6 ✅ 已修**：业务错误模型增加 `Category` 维度与 `CategoryForCode` 映射，覆盖 validation/auth/permission/not_found/state/idempotency/dependency/internal；HTTP 错误响应新增 `error.category`，保留原 `error.code`。
- **INFRA-7 ✅ 已修**：Agent 错误模型增加同口径 `Category` 维度；HTTP 错误响应新增 `error.category`。

范围决策：
- 不重命名现有错误码，不改变 HTTP status；分类作为向后兼容新增字段，旧前端继续按 `code` 读取。

## 批 F 子切片记录（2026-06-28 · P3 显式化防回归）✅

提交：`e513a17`（TURN-4 / INFRA-14 / WORK-8 / WORK-10）
验证：`go test ./services/business/internal/application/work ./services/business/internal/application/admin` 通过；`python3 tests/contract/validate_fixtures.py` 通过；`git diff --check` 通过。

- **TURN-4 ✅ 已修**：AG-UI 前端渲染规则明确 `timestamp` 仅用于展示和排障，不得作为排序依据、补偿游标或缺口判定条件；排序/合并仍以同一 `run_id` 内的 `sequence` 为准。
- **INFRA-14 ✅ 已修**：`ListAssetElementTypes` fixture 改为缺省 `page_size`，并在 RPC 契约与 assertion 中标明该字典读取例外默认 50、最大 100，区别于通用列表默认 10。
- **WORK-8 ✅ 已固化**：安全规范补“管理员不得设置/重置/改写业务用户密码”；`ConfirmSetUserStatus` 防回归测试验证后台用户状态流不改写 `business_users.password_hash`。
- **WORK-10 ✅ 已固化**：业务数据模型成文“同公开作品同用户唯一反应行、重复点赞/取消点赞幂等、`like_count` 不为负”；应用测试覆盖先取消、重复点赞、重复取消和唯一行不变量。

范围决策：
- 本切片不引入防刷频控；WORK-10 原缺口中的频控属于产品/风控策略，不用隐式限流冒充闭环。
- 不新增管理员改用户密码入口；WORK-8 只固化红线并验证现有状态治理流程不会误触密码字段。

## 批 F 子切片记录（2026-06-28 · P2 显式化/类型化收口）✅

提交：`0ff2f65`（WORK-5 / WORK-7 / INFRA-11 / SKILL-9 / SKILL-10）
验证：`go test ./services/business/internal/application/admin ./services/agent/internal/application/workbench` 通过；`python3 tests/contract/validate_fixtures.py` 通过；`git diff --check` 通过。

- **WORK-5 ✅ 已修**：后台模块 owner 归属收敛为 `AdminModuleOwners` 中心枚举，覆盖平台管理员、用户管理、系统 Skill、Skill 审核、模型供应商、模型、Tool、积分发放、兑换码、精选作品和审计日志，并声明 owner domain 与 audit scope。
- **WORK-7 ✅ 已修**：`AdminUserDetailDTO` 的空间、企业成员、审计引用占位从 `[]map[string]string` 改为强类型空白名单 DTO，继续保持 ACCT-8 红线：管理通道不展开业务归属明细。
- **INFRA-11 ✅ 已成文**：业务数据模型补主要领域状态流转矩阵，明确用户、管理员、企业成员、Skill、Tool、项目、积分冻结、兑换码、作品的允许流转和禁止/前置条件。
- **SKILL-9 ✅ 已固化**：文本兜底 payload 明确 `fallback_mode=text_model` 且 `recommend_create_skill=false`，文档要求不得在兜底中推销或建议创建 Skill。
- **SKILL-10 ✅ 已固化**：Tool 绑定声明和运行期策略复检分离成文；Agent 发送 Tool policy RPC 时携带 `runtime_whitelist_check=required_per_tool` 风险上下文，测试固化逐 Tool 复检语义。

范围决策：
- 不新增后台 RBAC；第一版仍是单一平台管理员角色，本切片只声明模块归属，便于审计和后续 RBAC 演进。
- INFRA-11 先固化矩阵口径，不改业务状态机行为；已存在的状态守卫由各 application 测试继续覆盖。

## 批 F 子切片记录（2026-06-28 · WORK-9）✅

提交：`e4a44e1`（WORK-9）
验证：`go test ./services/business/internal/pkg/auditlog ./services/business/internal/application/accountspace ./services/business/internal/application/admin ./services/business/internal/application/project ./services/business/internal/application/work` 通过；`git diff --check` 通过。

- **WORK-9 ✅ 已修**：`business_action` 收敛到 `services/business/internal/pkg/auditlog` 中心枚举，覆盖账号/企业/后台/项目/作品当前写入 `business_audit_logs` 的 action；应用层审计写入点改用常量，测试固化无重复、命名口径和关键 action 覆盖。

范围决策：
- 仅收敛 `business_audit_logs.business_action`，不把 idempotency `Scope`、Tool policy `change_type` 等相邻字符串混入同一枚举，避免不同语义被中心常量误绑。

## 批 E 子切片记录（2026-06-28 · INFRA-4）✅

提交：`de013df`（INFRA-4）
验证：`go test ./services/business/internal/infra/logger ./services/business/internal/transport/http` 通过；`git diff --check` 通过。

- **INFRA-4 ✅ 已固化**：业务结构化日志字段集收敛到 `services/business/internal/infra/logger` 常量与字段清单，基础字段为 `service/env`，HTTP 请求日志必带 `trace_id/request_id/method/path/status/latency_ms`；请求中间件将 `request_id` 放入 context，router 测试解析 JSON 日志验证字段全集。

范围决策：
- 本切片先固化业务 HTTP 入口公共字段，不展开全链路 OTel/W3C traceparent；后者仍归 INFRA-5。

## 批 A 子切片记录（2026-06-28 · INFRA-2）✅

提交：`3f9a8d9`（INFRA-2）
验证：`go test ./services/business/internal/infra/repository/businesscore` 通过；`git diff --check` 通过。

- **INFRA-2 ✅ 已固化**：业务 schema baseline 增加 testcontainer 迁移断言，53 张公共列表白名单必须存在 `created_by/updated_by/deleted_at`，10 张 append-only 表白名单必须存在 `trg_append_only` 触发器；测试同时锁定白名单数量，防止新增表绕过公共列/不可变性口径。

范围决策：
- `skill_review_records` 是唯一显式重叠例外：迁移事实为公共列已补齐，且按 `0020` 作为 Skill 审核事件流禁止 UPDATE/DELETE。

## 批 F 子切片记录（2026-06-28 · ACCT-6 / ACCT-7）✅

提交：`5473e27`（ACCT-6 / ACCT-7）
验证：`go test ./services/business/internal/transport/http` 通过；`python3 tests/contract/validate_fixtures.py` 通过；`git diff --check` 通过。

- **ACCT-6 ✅ 已固化**：HTTP 入口对 `UNAUTHENTICATED` 统一返回登录边界 details，匿名访问公开读不受影响，匿名点赞等需登录动作保持 401。
- **ACCT-7 ✅ 已固化**：401 details 增加 `login_required=true`、`return_to` 和 `pending_intent`，匿名点赞集成测试锁定登录后回原路径/原动作的承接字段；business-api fixture 同步字段契约。

范围决策：
- 本切片只定义业务 API 的登录承接契约，不实现前端 LoginModal 或登录后自动重放动作。

## 批 E 子切片记录（2026-06-28 · INFRA-3）✅

提交：随本切片提交（INFRA-3）
验证：`go test ./services/business/internal/infra/metrics ./services/business/internal/transport/http ./services/business/internal/bootstrap` 通过；`git diff --check` 通过。

- **INFRA-3 ✅ 已修**：业务侧新增内置 metrics registry，覆盖 counter/gauge/histogram 三类指标；HTTP 入口记录 `business_http_requests_total`、`business_http_inflight_requests`、`business_http_request_duration_ms` 并暴露 `/metrics` 文本出口；bootstrap 持有共享 registry。

范围决策：
- 本切片先建立业务指标基线和可抓取出口，不引入外部 Prometheus/OTel exporter 配置；跨服务 trace/OTel 仍归 INFRA-5。
