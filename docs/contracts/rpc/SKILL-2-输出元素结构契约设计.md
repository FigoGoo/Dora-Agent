# SKILL-2 Skill 输出元素结构契约设计（草案）

> 状态：draft（待对齐）
> 关联缺口：`code-plan-遗漏核对-初版代码复核.md` · P0 · SKILL-2
> 边界：业务微服务（business 08，写入/校验/契约产出） × 智能体微服务（agent 12，消费）
> 契约优先：本设计先于实现，对齐后再写迁移与代码。

## 1. 背景与缺口

产品要求 Skill 必须声明「输出元素结构」，用于在黑板与资产详情中组织、展示、引用其产物，并区分草稿态与最终资产元素。当前实现这条能力链 **全缺**：

- **schema 层**：`skill_output_element_schemas` 仅 `element_type / schema_json / required`（`0007_skill_catalog_review.up.sql:67-79`），无法表达元素名称、展示位置、可编辑、可引用、草稿/最终双态。
- **写入层**：business 侧 `SkillOutputElementSchema` 仅有 model 定义，**零读写路径**——SkillBuilder 创建/更新版本时根本没有声明输出元素结构的入口（产品 `SkillBuilder产品系统设计.md:184` 未落地）。
- **契约层**：`SkillSpecResponse`（`api/thrift/business_agent_service.thrift:131-140`）不含输出元素结构 → agent 拿不到，做不到「Agent 按 Skill 输出元素结构组织产物和资产元素」（`资产与创作过程保存产品系统设计.md:247`）。
- **字典层数据源存疑（本设计新发现）**：thrift `AssetElementTypeDTO`（`thrift:466-483`）已声明 `usage_stage / draft_enabled / final_enabled / editable / referable / render_hint` 等双态属性，但 `asset_element_types` 表与 `AssetElementType` model（`models_m3.go:324-335`）**没有这些列**，迁移中亦无。即「双态只活在 thrift DTO 契约里，无 DB 数据源」，填充来源疑为默认值。

## 2. 产品与规范约束（逐条引用）

| 约束来源 | 条目 | 对本设计的含义 |
| --- | --- | --- |
| `SkillBuilder产品系统设计.md:82` | 输出元素结构需区分草稿态元素和最终资产元素 | per-skill 与字典均需表达双态 |
| `SkillBuilder产品系统设计.md:85` | 声明元素名称、类型、是否必填、是否可编辑、是否可引用和展示位置 | per-skill 字段集 |
| `SkillBuilder产品系统设计.md:61/138/196` | 只能使用平台内置固定资产元素类型；不得要求未开放能力 | 校验：element_type 必须字典内置；属性不得超字典上限 |
| `资产与创作过程保存产品系统设计.md:42` | 最终资产元素由业务微服务保存，草稿态元素由 Agent 微服务保存 | 数据主权双写 |
| `资产与创作过程保存产品系统设计.md:93` | 草稿态元素可转为最终资产元素 | 生命周期：draft → final |
| 承重墙 · 数据主权隔离 | Agent 不持久化业务事实，经 RPC 落业务库 | 草稿 agent 存、最终走 `CommitGeneratedAssetAndCharge` |
| 承重墙 · Skill=完整能力定义仅 Published 路由 | Skill 完整定义须含输出元素结构 | SkillSpecResponse 必须暴露 output_elements |
| `RPC契约规范.md` | 契约优先、非破坏演进、contract test | thrift 字段以 optional 新增；补 fixture |
| 后端补充规范 | 列表分页、避免 N+1、DTO 包装 | 批量装配输出元素，按 version_id IN 查询 |

## 3. 设计方案

### 3.1 字典层补全（前置依赖 · FP1）

让 `AssetElementTypeDTO` 已声明的双态属性获得 DB 数据源。`asset_element_types` 续号迁移加列（均给安全默认，非破坏）：

```
category        varchar(64)  NOT NULL DEFAULT 'general'
resource_type   varchar(32)  NOT NULL DEFAULT 'text'
usage_stage     varchar(32)  NOT NULL DEFAULT 'draft_and_final'
draft_enabled   boolean      NOT NULL DEFAULT true
final_enabled   boolean      NOT NULL DEFAULT true
editable        boolean      NOT NULL DEFAULT false
referable       boolean      NOT NULL DEFAULT false
sort_order      integer      NOT NULL DEFAULT 0
render_hint_json jsonb       NOT NULL DEFAULT '{}'::jsonb
```

`ListAssetElementTypes` 改为数据驱动填充（替换当前默认值来源）。**这是 per-skill 校验「不得超字典上限」的数据基础**。

### 3.2 per-skill 输出元素结构（FP2）

`skill_output_element_schemas` 续号迁移加列：

```
element_name   varchar(128) NOT NULL DEFAULT ''     -- 产品 85 元素名称
display_order  integer      NOT NULL DEFAULT 0       -- 展示顺序
display_slot   varchar(64)  NOT NULL DEFAULT 'blackboard'  -- 展示位置: blackboard / asset_detail / both
use_draft      boolean      NOT NULL DEFAULT true    -- 本 skill 该元素是否产草稿态（≤ 字典 draft_enabled）
use_final      boolean      NOT NULL DEFAULT true    -- 是否转最终资产元素（≤ 字典 final_enabled）
editable       boolean      NOT NULL DEFAULT false   -- ≤ 字典 editable
referable      boolean      NOT NULL DEFAULT false   -- ≤ 字典 referable
```

`required / schema_json / UNIQUE(version_id, element_type)` 沿用既有。SkillBuilder 保存/提交版本时写入并校验（FP2 补写入路径）。

### 3.3 契约暴露 SkillSpecResponse（FP3）

thrift 新增强类型 DTO 并在 `SkillSpecResponse` 以 **optional** 字段挂载（非破坏，agent 渐进消费）：

```thrift
struct SkillOutputElementDTO {
  1: required string element_type,
  2: required string element_name,
  3: required bool   required,
  4: required bool   use_draft,
  5: required bool   use_final,
  6: required bool   editable,
  7: required bool   referable,
  8: optional i32    display_order,
  9: optional string display_slot,
  10: optional string schema_json,
}
// SkillSpecResponse 追加：
//   9: optional list<SkillOutputElementDTO> output_elements,
```

`GetPublishedSkillSpec` 按 `version_id` 批量装配（一次 `IN` 查询，避免 N+1）。`ReviewCandidateSkillSpecResponse` 可同样补 `output_elements` 供审核态校验，与既有 `expected_elements_json` 并存（前者是结构声明，后者是测试期望）。

### 3.4 校验规则（business 写入时强制）

1. `element_type` 必须存在于 `asset_element_types` 且 `status=active`（产品：只能用平台内置固定类型）。
2. `use_draft ≤ 字典.draft_enabled`，`use_final ≤ 字典.final_enabled`，`editable ≤ 字典.editable`，`referable ≤ 字典.referable`（不得要求未开放能力）。
3. `use_draft` 与 `use_final` 至少一个为真。
4. 同一 `version_id` 下 `element_type` 唯一（既有约束）。
5. 必填输出元素须能被测试样例覆盖（衔接 `SkillBuilder:185/186` 输出结构测试，落 `SaveSkillTestResult`）。

### 3.5 数据主权与双态生命周期映射

| 态 | 存储归属 | 落点 | 依据 |
| --- | --- | --- | --- |
| 草稿态元素（use_draft） | 智能体微服务 | `agent_artifacts` / 草稿 | 产品 42；承重墙数据主权 |
| 最终资产元素（use_final） | 业务微服务 | `asset_elements`，经 `CommitGeneratedAssetAndCharge.final_elements`（已有 `GeneratedAssetElementInput`，thrift:420-425） | 产品 42/93 |

Skill 输出元素结构是上述两套存储的**唯一声明源**；草稿→最终转化时以 `element_type` 对齐。

## 4. 实现计划（按功能点拆分提交）

| FP | 范围 | owner | 产出 |
| --- | --- | --- | --- |
| FP1 | 字典层双态属性落库 + 数据驱动 | business 08 | 迁移、model、`ListAssetElementTypes`、fixture |
| FP2 | per-skill 输出元素结构 schema + 写入/校验 | business 08 | 迁移、model、SkillBuilder 写入、校验、单测 |
| FP3 | 契约暴露 + 装配 | thrift（agent 提出 / business 确认）+ business 08 | thrift DTO、`GetPublishedSkillSpec` 装配、contract fixture |
| FP4 | agent 消费输出元素组织产物 | agent 12 | TurnLoop/产物组织、草稿落 `agent_artifacts`、最终走 RPC |

迁移续号：接 `0020_audit_append_only` → FP1=`0021_*`、FP2=`0022_*`（单源迁移目录，版本递增）。

## 5. 待确认事项（对齐点）

1. **字典数据源**：`AssetElementTypeDTO` 双态字段当前填充来源（疑默认值）——确认 FP1 先补字典数据层是否为前置共识。
2. **契约风格**：`output_elements` 用强类型 `list<SkillOutputElementDTO>`（本草案推荐）还是 `output_elements_json` 字符串，与现有 `*_json` 风格的取舍。
3. **agent owner 确认**：thrift 变更按「Go Eino 工程师提出、业务工程师确认」流程，需 agent 侧确认消费契约与草稿态 `agent_artifacts` 落点。
4. **审核态范围**：`ReviewCandidateSkillSpecResponse` 是否同步补 `output_elements`（建议补，便于发布前校验）。

## 6. 约束追溯矩阵

| 设计点 | 遵循约束 |
| --- | --- |
| 3.1 字典补全 | 产品 61/138；承重墙 Skill 完整定义；契约-schema 一致性 |
| 3.2 per-skill 字段 | 产品 82/85 |
| 3.3 SkillSpec 暴露 | 承重墙 Skill 完整能力定义；RPC 契约非破坏演进 |
| 3.4 校验 | 产品 61/138/196；fail-closed |
| 3.5 数据主权 | 产品 42/93；承重墙数据主权隔离 |
| 4 分功能点 | 开发流程规范 · 按功能点提交；后端补充规范 · 避免 N+1/分页/DTO |
