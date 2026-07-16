CREATE INDEX idx_skill_published_snapshot__published_skill_id
    ON business.skill_published_snapshot (published_at DESC, skill_id DESC);

COMMENT ON INDEX business.idx_skill_published_snapshot__published_skill_id
    IS 'Skill 公开市场按最新发布时间和公开 Skill 标识执行稳定 Keyset 分页的索引';
