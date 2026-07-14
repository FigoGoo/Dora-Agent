COMMENT ON COLUMN business.project_session_skill_resolution_item.publisher_user_id
    IS '冻结的 Skill 权威所有者（Publisher）用户标识，允许与项目所有者不同，不设置数据库物理外键约束';

COMMENT ON COLUMN business.project_session_skill_resolution_item.permission_snapshot_digest
    IS '权限快照 Canonical JSON 的 SHA-256 摘要，可来自 v1 owner-private 或 v2 public-market';
