DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM business.creation_spec_command_receipt LIMIT 1)
        OR EXISTS (SELECT 1 FROM business.creation_spec LIMIT 1) THEN
        RAISE EXCEPTION 'CreationSpec Preview 表仍包含业务数据，拒绝自动回滚'
            USING ERRCODE = '55000';
    END IF;
END
$$;

DROP TABLE business.creation_spec_command_receipt;
DROP TABLE business.creation_spec;
