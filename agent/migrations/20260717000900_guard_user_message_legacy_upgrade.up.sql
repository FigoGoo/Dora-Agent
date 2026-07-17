ALTER TABLE agent.session_user_message_upgrade_ledger
    ADD COLUMN upgrade_generation bigint NOT NULL DEFAULT 1,
    ADD COLUMN version bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT ck_session_user_message_upgrade_ledger__generation CHECK (upgrade_generation = 1),
    ADD CONSTRAINT ck_session_user_message_upgrade_ledger__version CHECK (version > 0);

COMMENT ON COLUMN agent.session_user_message_upgrade_ledger.upgrade_generation IS 'Preview Legacy Helper 升级代次，当前固定为 1';
COMMENT ON COLUMN agent.session_user_message_upgrade_ledger.version IS 'Ledger 阶段 CAS 版本，从 1 开始严格递增';

CREATE INDEX idx_session_user_message_upgrade_ledger__stage_input
    ON agent.session_user_message_upgrade_ledger (stage, input_id);

CREATE FUNCTION agent.reject_session_command_receipt_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'session command receipt is immutable';
END;
$$;

CREATE TRIGGER trg_session_command_receipt__immutable
BEFORE UPDATE OR DELETE ON agent.session_command_receipt
FOR EACH ROW EXECUTE FUNCTION agent.reject_session_command_receipt_mutation();

CREATE FUNCTION agent.reject_session_message_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'session message is immutable';
END;
$$;

CREATE TRIGGER trg_session_message__immutable
BEFORE UPDATE OR DELETE ON agent.session_message
FOR EACH ROW EXECUTE FUNCTION agent.reject_session_message_mutation();

CREATE FUNCTION agent.guard_user_message_upgrade_ledger_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'user message upgrade ledger cannot be deleted';
    END IF;
    IF NEW.input_id IS DISTINCT FROM OLD.input_id
       OR NEW.session_id IS DISTINCT FROM OLD.session_id
       OR NEW.turn_id IS DISTINCT FROM OLD.turn_id
       OR NEW.context_digest IS DISTINCT FROM OLD.context_digest
       OR NEW.upgrade_generation IS DISTINCT FROM OLD.upgrade_generation
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'user message upgrade ledger identity is immutable';
    END IF;
    IF NEW.version <> OLD.version + 1 OR NEW.updated_at <= OLD.updated_at THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'user message upgrade ledger version must increase';
    END IF;
    IF NOT ((OLD.stage = 'prepared' AND NEW.stage = 'applied')
            OR (OLD.stage = 'applied' AND NEW.stage = 'verified')) THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'user message upgrade ledger transition is invalid';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_session_user_message_upgrade_ledger__guard
BEFORE UPDATE OR DELETE ON agent.session_user_message_upgrade_ledger
FOR EACH ROW EXECUTE FUNCTION agent.guard_user_message_upgrade_ledger_mutation();
