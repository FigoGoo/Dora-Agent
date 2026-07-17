DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agent.analyze_materials_preview_run LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.analyze_materials_preview_turn_context LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.analyze_materials_preview_model_receipt LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.analyze_materials_preview_tool_receipt LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.analyze_materials_preview_projection LIMIT 1)
       OR EXISTS (
           SELECT 1 FROM agent.session_input
           WHERE source_type = 'analyze_materials_preview'
           LIMIT 1
       )
       OR EXISTS (
           SELECT 1 FROM agent.session_event_log
           WHERE event_type IN (
               'analyze_materials.preview.accepted',
               'analyze_materials.preview.completed',
               'analyze_materials.preview.partial',
               'analyze_materials.preview.failed',
               'analyze_materials.preview.runtime_failed'
           )
           LIMIT 1
       ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '55000',
            MESSAGE = 'analyze materials runtime preview contains durable data; rollback is unsafe';
    END IF;
END
$$;

DROP TRIGGER IF EXISTS trg_analyze_materials_preview_projection__immutable ON agent.analyze_materials_preview_projection;
DROP TRIGGER IF EXISTS trg_analyze_materials_preview_tool_receipt__guard ON agent.analyze_materials_preview_tool_receipt;
DROP TRIGGER IF EXISTS trg_analyze_materials_preview_model_receipt__guard ON agent.analyze_materials_preview_model_receipt;
DROP TRIGGER IF EXISTS trg_analyze_materials_preview_turn_context__immutable ON agent.analyze_materials_preview_turn_context;
DROP FUNCTION IF EXISTS agent.reject_analyze_materials_projection_mutation();
DROP FUNCTION IF EXISTS agent.guard_analyze_materials_tool_receipt_mutation();
DROP FUNCTION IF EXISTS agent.guard_analyze_materials_model_receipt_mutation();
DROP FUNCTION IF EXISTS agent.reject_analyze_materials_context_mutation();

DROP TABLE IF EXISTS agent.analyze_materials_preview_projection;
DROP TABLE IF EXISTS agent.analyze_materials_preview_tool_receipt;
DROP TABLE IF EXISTS agent.analyze_materials_preview_model_receipt;
DROP TABLE IF EXISTS agent.analyze_materials_preview_turn_context;
DROP TABLE IF EXISTS agent.analyze_materials_preview_run;

ALTER TABLE agent.session_event_log
    DROP CONSTRAINT ck_session_event_log__aggregate_type;

ALTER TABLE agent.session_event_log
    ADD CONSTRAINT ck_session_event_log__aggregate_type CHECK (
        aggregate_type IN ('session', 'session_input', 'creation_spec', 'session_turn')
    );

COMMENT ON COLUMN agent.session_event_log.aggregate_type IS '事件关联聚合类型：session、session_input、creation_spec 或 session_turn';

ALTER TABLE agent.session_event_log
    DROP CONSTRAINT ck_session_event_log__type;

ALTER TABLE agent.session_event_log
    ADD CONSTRAINT ck_session_event_log__type CHECK (event_type IN (
        'session.created',
        'session.input.accepted',
        'creation_spec.preview.completed',
        'creation_spec.preview.failed',
        'session.turn.completed',
        'session.turn.failed',
        'session.turn.recovery_pending'
    ));

COMMENT ON COLUMN agent.session_event_log.event_type IS '事件类型：会话、输入、CreationSpec Preview 或用户消息 Turn 投影';

ALTER TABLE agent.session_input
    DROP CONSTRAINT ck_session_input__source_type;

ALTER TABLE agent.session_input
    ADD CONSTRAINT ck_session_input__source_type CHECK (
        source_type IN ('user_message', 'creation_spec_preview')
    );

COMMENT ON COLUMN agent.session_input.source_type IS '可信输入来源类型：普通用户消息或 CreationSpec Preview 结构化意图';
