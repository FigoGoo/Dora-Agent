DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agent.media_preview_request LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.media_preview_operation LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.media_preview_batch LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.media_preview_job LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.media_preview_dispatch_outbox LIMIT 1)
       OR EXISTS (SELECT 1 FROM agent.media_preview_terminal_outbox LIMIT 1)
       OR EXISTS (
            SELECT 1 FROM agent.session_input
            WHERE source_type IN (
                'generate_media_preview_request',
                'assemble_output_preview_request',
                'media_job_preview_terminal'
            )
            LIMIT 1
       )
       OR EXISTS (
            SELECT 1 FROM agent.session_event_log
            WHERE event_type IN (
                'media.preview.accepted',
                'media.preview.completed',
                'media.preview.failed',
                'media.preview.runtime_failed'
            )
            LIMIT 1
       ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '55000',
            MESSAGE = 'media runtime v3 preview contains durable data; rollback is unsafe';
    END IF;
END
$$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'dora_worker_app') THEN
        REVOKE SELECT ON agent.media_job_preview_v1_claimable FROM dora_worker_app;
        REVOKE EXECUTE ON FUNCTION agent.media_job_preview_v1_claim(uuid, text, uuid, uuid, integer) FROM dora_worker_app;
        REVOKE EXECUTE ON FUNCTION agent.media_job_preview_v1_renew(uuid, text, uuid, bigint, integer) FROM dora_worker_app;
        REVOKE EXECUTE ON FUNCTION agent.media_job_preview_v1_schedule_retry(uuid, text, uuid, bigint, integer, text) FROM dora_worker_app;
        REVOKE EXECUTE ON FUNCTION agent.media_job_preview_v1_mark_reconciling(uuid, text, uuid, bigint, text) FROM dora_worker_app;
        REVOKE EXECUTE ON FUNCTION agent.media_job_preview_v1_commit_terminal(uuid, text, uuid, bigint, uuid, text, text, text, jsonb) FROM dora_worker_app;
        REVOKE EXECUTE ON FUNCTION agent.media_job_preview_v1_get(uuid) FROM dora_worker_app;
    END IF;
END
$$;

DROP FUNCTION IF EXISTS agent.media_job_preview_v1_get(uuid);
DROP FUNCTION IF EXISTS agent.media_job_preview_v1_commit_terminal(uuid, text, uuid, bigint, uuid, text, text, text, jsonb);
DROP FUNCTION IF EXISTS agent.media_job_preview_v1_mark_reconciling(uuid, text, uuid, bigint, text);
DROP FUNCTION IF EXISTS agent.media_job_preview_v1_schedule_retry(uuid, text, uuid, bigint, integer, text);
DROP FUNCTION IF EXISTS agent.media_job_preview_v1_renew(uuid, text, uuid, bigint, integer);
DROP FUNCTION IF EXISTS agent.media_job_preview_v1_claim(uuid, text, uuid, uuid, integer);
DROP VIEW IF EXISTS agent.media_job_preview_v1_claimable;

DROP TABLE IF EXISTS agent.media_preview_terminal_outbox;
DROP TABLE IF EXISTS agent.media_preview_dispatch_outbox;
DROP TABLE IF EXISTS agent.media_preview_job;
DROP TABLE IF EXISTS agent.media_preview_batch;
DROP TABLE IF EXISTS agent.media_preview_operation;
DROP TABLE IF EXISTS agent.media_preview_request;
DROP FUNCTION IF EXISTS agent.media_preview_v1_jsonb_object_key_count(jsonb);

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
        'session.turn.recovery_pending',
        'analyze_materials.preview.accepted',
        'analyze_materials.preview.completed',
        'analyze_materials.preview.partial',
        'analyze_materials.preview.failed',
        'analyze_materials.preview.runtime_failed',
        'plan_storyboard.preview.accepted',
        'plan_storyboard.preview.completed',
        'plan_storyboard.preview.failed',
        'plan_storyboard.preview.runtime_failed',
        'write_prompts.preview.accepted',
        'write_prompts.preview.completed',
        'write_prompts.preview.failed',
        'write_prompts.preview.runtime_failed'
    ));

COMMENT ON COLUMN agent.session_event_log.event_type IS '事件类型：会话、输入、CreationSpec Preview、用户消息 Turn、Analyze Materials Preview、Plan Storyboard Preview 或 Write Prompts Preview 投影';

ALTER TABLE agent.session_input
    DROP CONSTRAINT ck_session_input__source_type;

ALTER TABLE agent.session_input
    ADD CONSTRAINT ck_session_input__source_type CHECK (
        source_type IN (
            'user_message',
            'creation_spec_preview',
            'analyze_materials_preview',
            'plan_storyboard_preview',
            'write_prompts_preview'
        )
    );

COMMENT ON COLUMN agent.session_input.source_type IS '可信输入来源类型：普通用户消息、CreationSpec Preview、Analyze Materials Preview、Plan Storyboard Preview 或 Write Prompts Preview 结构化意图';
