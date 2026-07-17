DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM business.prompt_preview_command_receipt LIMIT 1)
        OR EXISTS (SELECT 1 FROM business.prompt_preview_draft LIMIT 1) THEN
        RAISE EXCEPTION 'Prompt Preview 表仍包含业务数据，拒绝自动回滚'
            USING ERRCODE = '55000';
    END IF;
END
$$;

DROP TABLE business.prompt_preview_command_receipt;
DROP TABLE business.prompt_preview_draft;
