-- Dora business service migration 0021
-- Owner: 业务微服务后端工程师 (W1 · SKILL-2 FP2)
-- Scope: skill_output_element_schemas 补 per-skill 输出元素结构字段——名称/展示位置/草稿·最终双态/可编辑/可引用。
-- 约束: 产品 SkillBuilder产品系统设计.md:82(草稿/最终双态)·85(名称/可编辑/可引用/展示位置);
--       契约 docs/contracts/rpc/SKILL-2-输出元素结构契约设计.md §3.2; 禁外键(跨表一致性走应用校验)。
-- 演进: 全部 ADD COLUMN IF NOT EXISTS + 安全默认，对既有行非破坏(既有结构维持原行为)。
-- 双态上限: use_draft/use_final/editable/referable 写入时须 ≤ asset_element_types 字典上限(应用层 FP2 校验)。

ALTER TABLE skill_output_element_schemas
  ADD COLUMN IF NOT EXISTS element_name  varchar(128) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS display_order integer      NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS display_slot  varchar(64)  NOT NULL DEFAULT 'blackboard',
  ADD COLUMN IF NOT EXISTS use_draft     boolean      NOT NULL DEFAULT true,
  ADD COLUMN IF NOT EXISTS use_final     boolean      NOT NULL DEFAULT true,
  ADD COLUMN IF NOT EXISTS editable      boolean      NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS referable     boolean      NOT NULL DEFAULT false;
