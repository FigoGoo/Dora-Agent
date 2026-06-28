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
