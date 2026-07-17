DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM agent.creation_spec_preview_tool_receipt
        WHERE business_command_ciphertext IS NOT NULL
           OR business_resend_attempts > 0
           OR stage = 'business_resend_exhausted'
        LIMIT 1
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '55000',
            MESSAGE = 'durable creation spec preview recovery data exists; rollback is unsafe';
    END IF;
END
$$;

ALTER TABLE agent.creation_spec_preview_tool_receipt
    DROP CONSTRAINT ck_creation_spec_preview_tool_receipt__result,
    DROP CONSTRAINT ck_creation_spec_preview_tool_receipt__command_bundle,
    DROP CONSTRAINT ck_creation_spec_preview_tool_receipt__stage;

ALTER TABLE agent.creation_spec_preview_tool_receipt
    DROP COLUMN business_resend_exhausted_at,
    DROP COLUMN business_last_resend_at,
    DROP COLUMN business_resend_limit,
    DROP COLUMN business_resend_attempts,
    DROP COLUMN business_command_payload_digest,
    DROP COLUMN business_command_key_version,
    DROP COLUMN business_command_ciphertext;

ALTER TABLE agent.creation_spec_preview_tool_receipt
    ALTER COLUMN stage TYPE varchar(24);

ALTER TABLE agent.creation_spec_preview_tool_receipt
    ADD CONSTRAINT ck_creation_spec_preview_tool_receipt__stage CHECK (stage IN (
        'pending', 'business_prepared', 'business_unknown', 'completed', 'failed'
    )),
    ADD CONSTRAINT ck_creation_spec_preview_tool_receipt__result CHECK (
        (stage = 'completed' AND business_request_digest IS NOT NULL AND business_content_digest IS NOT NULL AND result_ciphertext IS NOT NULL AND result_key_version IS NOT NULL AND result_digest ~ '^[0-9a-f]{64}$' AND error_code IS NULL)
        OR
        (stage = 'failed' AND result_ciphertext IS NOT NULL AND result_key_version IS NOT NULL AND result_digest ~ '^[0-9a-f]{64}$' AND error_code IS NOT NULL)
        OR
        (stage = 'pending' AND business_request_digest IS NULL AND business_content_digest IS NULL AND result_ciphertext IS NULL AND result_key_version IS NULL AND result_digest IS NULL AND error_code IS NULL)
        OR
        (stage IN ('business_prepared', 'business_unknown') AND business_request_digest IS NOT NULL AND business_content_digest IS NOT NULL AND result_ciphertext IS NULL AND result_key_version IS NULL AND result_digest IS NULL AND error_code IS NULL)
    );

COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.stage IS '执行阶段：pending、business_prepared、business_unknown、completed 或 failed';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.error_code IS '稳定失败码，仅 failed 时存在且不包含 Provider 原文';
