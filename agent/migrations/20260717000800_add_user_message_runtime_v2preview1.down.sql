DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agent.session_user_message_turn LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.session_user_message_turn_context LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.session_user_message_run LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.session_user_message_model_receipt LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.session_user_message_output_receipt LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.session_user_message_output_projection LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.session_user_message_upgrade_ledger LIMIT 1)
       OR EXISTS (
           SELECT 1 FROM agent.session_event_log
           WHERE event_type IN ('session.turn.completed', 'session.turn.failed', 'session.turn.recovery_pending')
           LIMIT 1
       ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '55000',
            MESSAGE = 'user message runtime preview contains durable data; rollback is unsafe';
    END IF;
END
$$;

ALTER TABLE agent.session_event_log
    DROP CONSTRAINT ck_session_event_log__aggregate_type;
ALTER TABLE agent.session_event_log
    ADD CONSTRAINT ck_session_event_log__aggregate_type CHECK (
        aggregate_type IN ('session', 'session_input', 'creation_spec')
    );
COMMENT ON COLUMN agent.session_event_log.aggregate_type IS '事件关联聚合类型：session、session_input 或 creation_spec';

ALTER TABLE agent.session_event_log
    DROP CONSTRAINT ck_session_event_log__type;
ALTER TABLE agent.session_event_log
    ADD CONSTRAINT ck_session_event_log__type CHECK (event_type IN (
        'session.created',
        'session.input.accepted',
        'creation_spec.preview.completed',
        'creation_spec.preview.failed'
    ));
COMMENT ON COLUMN agent.session_event_log.event_type IS '事件类型：会话创建、输入接受或 CreationSpec Preview 完成/失败终态';

DROP TRIGGER IF EXISTS trg_session_user_message_output_receipt__guard ON agent.session_user_message_output_receipt;
DROP TRIGGER IF EXISTS trg_session_user_message_model_receipt__guard ON agent.session_user_message_model_receipt;
DROP FUNCTION IF EXISTS agent.guard_user_message_output_receipt_mutation();
DROP FUNCTION IF EXISTS agent.guard_user_message_model_receipt_mutation();
DROP TRIGGER IF EXISTS trg_session_user_message_turn_context__immutable ON agent.session_user_message_turn_context;
DROP FUNCTION IF EXISTS agent.reject_user_message_context_mutation();

DROP TABLE IF EXISTS agent.session_user_message_upgrade_ledger;
DROP TABLE IF EXISTS agent.session_user_message_output_projection;
DROP TABLE IF EXISTS agent.session_user_message_output_receipt;
DROP TABLE IF EXISTS agent.session_user_message_model_receipt;
DROP TABLE IF EXISTS agent.session_user_message_run;
DROP TABLE IF EXISTS agent.session_user_message_turn_context;
DROP TABLE IF EXISTS agent.session_user_message_turn;
