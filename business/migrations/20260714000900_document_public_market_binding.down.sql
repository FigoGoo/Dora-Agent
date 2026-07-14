DO $migration$
BEGIN
    -- SHARE 与 QuickCreate INSERT 所需的 ROW EXCLUSIVE 冲突；两张表按写入顺序加锁，
    -- 确保历史检查和旧注释恢复之间不会提交新的 public-market Resolution。
    LOCK TABLE business.project_session_skill_resolution,
        business.project_session_skill_resolution_item IN SHARE MODE;

    IF EXISTS (
        SELECT 1
        FROM business.project_session_skill_resolution AS resolution_header
        JOIN business.project_session_skill_resolution_item AS resolution_item
          ON resolution_item.resolution_id = resolution_header.id
        WHERE resolution_header.owner_user_id <> resolution_item.publisher_user_id
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'cannot rollback public market binding documentation migration while public-market history exists'
            USING ERRCODE = '55000';
    END IF;

    -- COMMENT 与 guard 保持在同一个 DO 语句的隐式事务中；异常会原子回滚且不会留下打开的事务。
    EXECUTE $comment$
        COMMENT ON COLUMN business.project_session_skill_resolution_item.publisher_user_id
            IS '发布者用户标识，W1 owner-private 等于项目所有者'
    $comment$;

    EXECUTE $comment$
        COMMENT ON COLUMN business.project_session_skill_resolution_item.permission_snapshot_digest
            IS 'owner-private 权限快照 Canonical JSON 的 SHA-256 摘要'
    $comment$;
END
$migration$;
