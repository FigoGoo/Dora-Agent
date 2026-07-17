DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agent.creation_spec_preview_run LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.creation_spec_preview_model_receipt LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.creation_spec_preview_tool_receipt LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.creation_spec_preview_projection LIMIT 1)
       OR EXISTS (
           SELECT 1 FROM agent.session_event_log
		   WHERE source_kind = 'creation_spec_preview'
		      OR event_type IN ('creation_spec.preview.completed', 'creation_spec.preview.failed')
           LIMIT 1
       )
       OR EXISTS (
           SELECT 1 FROM agent.session_input
           WHERE source_type = 'creation_spec_preview'
           LIMIT 1
       )
       OR EXISTS (
           SELECT 1 FROM agent.session_message
           WHERE source_kind = 'creation_spec_preview'
           LIMIT 1
       ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '55000',
            MESSAGE = 'creation spec preview runtime contains durable data; rollback is unsafe';
    END IF;
END
$$;

DROP TABLE IF EXISTS agent.creation_spec_preview_projection;
DROP TABLE IF EXISTS agent.creation_spec_preview_tool_receipt;
DROP TABLE IF EXISTS agent.creation_spec_preview_model_receipt;
DROP TABLE IF EXISTS agent.creation_spec_preview_run;

ALTER TABLE agent.session_event_log
    DROP CONSTRAINT ck_session_event_log__aggregate_type;
ALTER TABLE agent.session_event_log
    ADD CONSTRAINT ck_session_event_log__aggregate_type CHECK (
        aggregate_type IN ('session', 'session_input')
    );
COMMENT ON COLUMN agent.session_event_log.aggregate_type IS '事件关联聚合类型：session 或 session_input';

ALTER TABLE agent.session_event_log
    DROP CONSTRAINT ck_session_event_log__schema_version;
ALTER TABLE agent.session_event_log
    ADD CONSTRAINT ck_session_event_log__schema_version CHECK (
        schema_version IN ('session.event.v1')
    );
COMMENT ON COLUMN agent.session_event_log.schema_version IS '前端投影事件 Schema 版本，W0 固定为 session.event.v1';

ALTER TABLE agent.session_event_log
    DROP CONSTRAINT ck_session_event_log__type;
ALTER TABLE agent.session_event_log
    ADD CONSTRAINT ck_session_event_log__type CHECK (
        event_type IN ('session.created', 'session.input.accepted')
    );
COMMENT ON COLUMN agent.session_event_log.event_type IS '事件类型，W0 仅允许 session.created 与 session.input.accepted';

DROP INDEX agent.idx_session_input__claim;
CREATE INDEX idx_session_input__claim
    ON agent.session_input (status, available_at, session_id, enqueue_seq)
    WHERE status IN ('pending', 'retry_wait');

ALTER TABLE agent.session_input
    DROP CONSTRAINT ck_session_input__status;
ALTER TABLE agent.session_input
    ADD CONSTRAINT ck_session_input__status CHECK (status IN (
        'pending', 'claimed', 'running', 'retry_wait', 'resolved', 'dead'
    ));
COMMENT ON COLUMN agent.session_input.status IS '输入状态：pending、claimed、running、retry_wait、resolved 或 dead；W0 仅写 pending';

ALTER TABLE agent.session_input
    DROP CONSTRAINT ck_session_input__source_type;
ALTER TABLE agent.session_input
    ADD CONSTRAINT ck_session_input__source_type CHECK (
        source_type IN ('user_message')
    );
COMMENT ON COLUMN agent.session_input.source_type IS '可信输入来源类型，W0 只允许 user_message；恢复、审批和批次继续结果必须经后续设计和前向 Migration 扩展';

ALTER TABLE agent.session_message
    DROP CONSTRAINT ck_session_message__source_kind;
ALTER TABLE agent.session_message
    ADD CONSTRAINT ck_session_message__source_kind CHECK (
        source_kind IN ('ensure_project_session')
    );
COMMENT ON COLUMN agent.session_message.source_kind IS '稳定消息来源类型，W0 固定 ensure_project_session';
