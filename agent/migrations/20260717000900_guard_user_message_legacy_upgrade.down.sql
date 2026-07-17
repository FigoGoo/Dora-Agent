DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agent.session_user_message_upgrade_ledger LIMIT 1) THEN
        RAISE EXCEPTION 'user message upgrade ledger contains durable data; rollback is unsafe';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS trg_session_user_message_upgrade_ledger__guard
    ON agent.session_user_message_upgrade_ledger;
DROP FUNCTION IF EXISTS agent.guard_user_message_upgrade_ledger_mutation();
DROP TRIGGER IF EXISTS trg_session_command_receipt__immutable
    ON agent.session_command_receipt;
DROP FUNCTION IF EXISTS agent.reject_session_command_receipt_mutation();
DROP TRIGGER IF EXISTS trg_session_message__immutable
    ON agent.session_message;
DROP FUNCTION IF EXISTS agent.reject_session_message_mutation();
DROP INDEX IF EXISTS agent.idx_session_user_message_upgrade_ledger__stage_input;

ALTER TABLE agent.session_user_message_upgrade_ledger
    DROP CONSTRAINT IF EXISTS ck_session_user_message_upgrade_ledger__version,
    DROP CONSTRAINT IF EXISTS ck_session_user_message_upgrade_ledger__generation,
    DROP COLUMN IF EXISTS version,
    DROP COLUMN IF EXISTS upgrade_generation;
