DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM business.storyboard_preview_command_receipt LIMIT 1)
        OR EXISTS (SELECT 1 FROM business.storyboard_preview_draft LIMIT 1) THEN
        RAISE EXCEPTION 'Storyboard Preview 表仍包含业务数据，拒绝自动回滚'
            USING ERRCODE = '55000';
    END IF;
END
$$;

DROP TABLE business.storyboard_preview_command_receipt;
DROP TABLE business.storyboard_preview_draft;
