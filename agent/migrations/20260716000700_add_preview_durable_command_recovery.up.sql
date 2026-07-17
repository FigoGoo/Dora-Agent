DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM agent.creation_spec_preview_tool_receipt
        WHERE stage IN ('business_prepared', 'business_unknown')
        LIMIT 1
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '55000',
            MESSAGE = 'open creation spec preview commands require manual reconciliation before durable-command recovery upgrade';
    END IF;
END
$$;

ALTER TABLE agent.creation_spec_preview_tool_receipt
    DROP CONSTRAINT ck_creation_spec_preview_tool_receipt__result;

ALTER TABLE agent.creation_spec_preview_tool_receipt
    DROP CONSTRAINT ck_creation_spec_preview_tool_receipt__stage;

ALTER TABLE agent.creation_spec_preview_tool_receipt
    ALTER COLUMN stage TYPE varchar(32);

ALTER TABLE agent.creation_spec_preview_tool_receipt
    ADD COLUMN business_command_ciphertext bytea NULL,
    ADD COLUMN business_command_key_version varchar(64) NULL,
    ADD COLUMN business_command_payload_digest char(64) NULL,
    ADD COLUMN business_resend_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN business_resend_limit integer NULL,
    ADD COLUMN business_last_resend_at timestamptz NULL,
    ADD COLUMN business_resend_exhausted_at timestamptz NULL;

ALTER TABLE agent.creation_spec_preview_tool_receipt
    ADD CONSTRAINT ck_creation_spec_preview_tool_receipt__stage CHECK (stage IN (
        'pending',
        'business_prepared',
        'business_unknown',
        'business_resend_exhausted',
        'completed',
        'failed'
    )),
    ADD CONSTRAINT ck_creation_spec_preview_tool_receipt__command_bundle CHECK (
        (
            business_command_ciphertext IS NULL
            AND business_command_key_version IS NULL
            AND business_command_payload_digest IS NULL
            AND business_resend_attempts = 0
            AND business_resend_limit IS NULL
            AND business_last_resend_at IS NULL
            AND business_resend_exhausted_at IS NULL
        )
        OR
        (
            business_command_ciphertext IS NOT NULL
            AND business_command_key_version IS NOT NULL
            AND business_command_payload_digest ~ '^[0-9a-f]{64}$'
            AND business_resend_limit BETWEEN 1 AND 20
            AND business_resend_attempts BETWEEN 0 AND business_resend_limit
            AND (
                (business_resend_attempts = 0 AND business_last_resend_at IS NULL)
                OR (business_resend_attempts > 0 AND business_last_resend_at IS NOT NULL)
            )
        )
    ),
    ADD CONSTRAINT ck_creation_spec_preview_tool_receipt__result CHECK (
        (
            stage = 'completed'
            AND business_request_digest IS NOT NULL
            AND business_content_digest IS NOT NULL
            AND result_ciphertext IS NOT NULL
            AND result_key_version IS NOT NULL
            AND result_digest ~ '^[0-9a-f]{64}$'
            AND error_code IS NULL
        )
        OR
        (
            stage = 'failed'
            AND result_ciphertext IS NOT NULL
            AND result_key_version IS NOT NULL
            AND result_digest ~ '^[0-9a-f]{64}$'
            AND error_code IS NOT NULL
        )
        OR
        (
            stage = 'pending'
            AND business_request_digest IS NULL
            AND business_content_digest IS NULL
            AND business_command_ciphertext IS NULL
            AND result_ciphertext IS NULL
            AND result_key_version IS NULL
            AND result_digest IS NULL
            AND error_code IS NULL
        )
        OR
        (
            stage IN ('business_prepared', 'business_unknown')
            AND business_request_digest IS NOT NULL
            AND business_content_digest IS NOT NULL
            AND business_command_ciphertext IS NOT NULL
            AND result_ciphertext IS NULL
            AND result_key_version IS NULL
            AND result_digest IS NULL
            AND error_code IS NULL
            AND business_resend_exhausted_at IS NULL
        )
        OR
        (
            stage = 'business_resend_exhausted'
            AND business_request_digest IS NOT NULL
            AND business_content_digest IS NOT NULL
            AND business_command_ciphertext IS NOT NULL
            AND business_resend_attempts = business_resend_limit
            AND business_resend_exhausted_at IS NOT NULL
            AND result_ciphertext IS NULL
            AND result_key_version IS NULL
            AND result_digest IS NULL
            AND error_code = 'CREATION_SPEC_SAVE_RECOVERY_EXHAUSTED'
        )
    );

COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.business_command_ciphertext IS '完整 Business Save Draft 稳定命令的 DRAE v1 AEAD 密文；不包含易变 Lease Owner 或 Fence';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.business_command_key_version IS 'Business Save Draft 命令密文使用的 Agent 内容密钥版本';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.business_command_payload_digest IS 'Business Save Draft 命令规范明文的 SHA-256 摘要，用于认证解密与防替换';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.business_resend_attempts IS '权威 not_found 后已在 PostgreSQL 原子预留的同键重发次数';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.business_resend_limit IS 'Prepare 时从版本化 Preview Runtime 配置冻结的同键重发上限';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.business_last_resend_at IS '最近一次原子预留同键重发预算的数据库 UTC 时间';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.business_resend_exhausted_at IS '最终查询仍未收敛且自动重发预算耗尽的数据库 UTC 时间；非空时停止自动 Claim';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.stage IS '执行阶段：pending、business_prepared、business_unknown、business_resend_exhausted、completed 或 failed';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.error_code IS '稳定失败或恢复阻塞码；重发耗尽时只标记可观察恢复阻塞，不表示业务失败';
