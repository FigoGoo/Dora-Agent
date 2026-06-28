-- Dora business service migration 0021 (down)
-- 回滚 SKILL-2 FP2 per-skill 输出元素结构字段。
ALTER TABLE skill_output_element_schemas
  DROP COLUMN IF EXISTS element_name,
  DROP COLUMN IF EXISTS display_order,
  DROP COLUMN IF EXISTS display_slot,
  DROP COLUMN IF EXISTS use_draft,
  DROP COLUMN IF EXISTS use_final,
  DROP COLUMN IF EXISTS editable,
  DROP COLUMN IF EXISTS referable;
