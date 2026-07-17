ALTER TABLE agent.session_input
    DROP CONSTRAINT ck_session_input__source_type;

ALTER TABLE agent.session_input
    ADD CONSTRAINT ck_session_input__source_type CHECK (
        source_type IN (
            'user_message',
            'creation_spec_preview',
            'analyze_materials_preview',
            'plan_storyboard_preview',
            'write_prompts_preview',
            'generate_media_preview_request',
            'assemble_output_preview_request',
            'media_job_preview_terminal'
        )
    );

COMMENT ON COLUMN agent.session_input.source_type IS '可信输入来源类型：五条基础本地预览、Generate Media 请求、Assemble Output 请求或媒体 Job 终态';

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
        'write_prompts.preview.runtime_failed',
        'media.preview.accepted',
        'media.preview.completed',
        'media.preview.failed',
        'media.preview.runtime_failed'
    ));

COMMENT ON COLUMN agent.session_event_log.event_type IS '事件类型：基础会话、五条基础预览或 media.runtime.v3preview1 的 accepted/completed/failed 投影';

CREATE FUNCTION agent.media_preview_v1_jsonb_object_key_count(p_value jsonb)
RETURNS integer
LANGUAGE sql
IMMUTABLE
STRICT
PARALLEL SAFE
SET search_path = pg_catalog
AS $$
    SELECT count(*)::integer
    FROM pg_catalog.jsonb_object_keys(p_value)
$$;

COMMENT ON FUNCTION agent.media_preview_v1_jsonb_object_key_count(jsonb) IS '返回 JSONB 对象的直接键数量，供 media.runtime.v3preview1 严格结构约束复用';
REVOKE ALL ON FUNCTION agent.media_preview_v1_jsonb_object_key_count(jsonb) FROM PUBLIC;

CREATE TABLE agent.media_preview_request (
    request_id uuid NOT NULL,
    session_id uuid NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    idempotency_key uuid NOT NULL,
    request_digest char(64) NOT NULL,
    tool_key varchar(32) NOT NULL,
    intent_schema_version varchar(64) NOT NULL,
    intent_digest char(64) NOT NULL,
    intent jsonb NOT NULL,
    input_id uuid NOT NULL,
    turn_id uuid NOT NULL,
    run_id uuid NOT NULL,
    tool_call_id uuid NOT NULL,
    accepted_event_id uuid NOT NULL,
    terminal_event_id uuid NOT NULL,
    deadline_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_media_preview_request PRIMARY KEY (request_id),
    CONSTRAINT uq_media_preview_request__idempotency UNIQUE (session_id, idempotency_key),
    CONSTRAINT uq_media_preview_request__input UNIQUE (input_id),
    CONSTRAINT uq_media_preview_request__turn UNIQUE (turn_id),
    CONSTRAINT uq_media_preview_request__run UNIQUE (run_id),
    CONSTRAINT uq_media_preview_request__tool_call UNIQUE (tool_call_id),
    CONSTRAINT uq_media_preview_request__accepted_event UNIQUE (accepted_event_id),
    CONSTRAINT uq_media_preview_request__terminal_event UNIQUE (terminal_event_id),
    CONSTRAINT ck_media_preview_request__tool CHECK (
        (tool_key = 'generate_media' AND intent_schema_version = 'generate_media.intent.v3preview1')
        OR (tool_key = 'assemble_output' AND intent_schema_version = 'assemble_output.intent.v3preview1')
    ),
    CONSTRAINT ck_media_preview_request__digests CHECK (
        request_digest ~ '^[0-9a-f]{64}$' AND intent_digest ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT ck_media_preview_request__intent CHECK (jsonb_typeof(intent) = 'object'),
    CONSTRAINT ck_media_preview_request__deadline CHECK (deadline_at > created_at)
);

COMMENT ON TABLE agent.media_preview_request IS '两类媒体 typed ingress 的 first-write-wins Intent 与稳定执行身份；Session Input 仍是处理状态真源';
COMMENT ON COLUMN agent.media_preview_request.request_id IS 'Business 身份断言绑定且作为 Session Input source_id 的请求 UUIDv7';
COMMENT ON COLUMN agent.media_preview_request.session_id IS '目标 Agent Session UUIDv7 逻辑引用';
COMMENT ON COLUMN agent.media_preview_request.user_id IS 'Business 已认证 Owner 用户 UUIDv7';
COMMENT ON COLUMN agent.media_preview_request.project_id IS 'Business 已绑定项目 UUIDv7';
COMMENT ON COLUMN agent.media_preview_request.idempotency_key IS 'Session 内 first-write-wins UUIDv7';
COMMENT ON COLUMN agent.media_preview_request.request_digest IS 'tool_key 与 canonical Intent 的稳定请求 SHA-256';
COMMENT ON COLUMN agent.media_preview_request.tool_key IS '仅 generate_media 或 assemble_output';
COMMENT ON COLUMN agent.media_preview_request.intent_schema_version IS '与 tool_key 精确绑定的 Intent 版本';
COMMENT ON COLUMN agent.media_preview_request.intent_digest IS '规范化 Intent JSON 的 SHA-256 摘要';
COMMENT ON COLUMN agent.media_preview_request.intent IS '仅含资源引用、摘要、目标键与固定输出 Profile 的严格 JSON';
COMMENT ON COLUMN agent.media_preview_request.input_id IS '全局 Session Lane Input UUIDv7';
COMMENT ON COLUMN agent.media_preview_request.turn_id IS '技术恢复复用的稳定 Turn UUIDv7';
COMMENT ON COLUMN agent.media_preview_request.run_id IS 'Lease takeover 复用的稳定 Run UUIDv7';
COMMENT ON COLUMN agent.media_preview_request.tool_call_id IS 'deterministic dispatcher 必须原样使用的 Tool Call UUIDv7';
COMMENT ON COLUMN agent.media_preview_request.accepted_event_id IS 'Graph accepted 投影 AppendOnce UUIDv7';
COMMENT ON COLUMN agent.media_preview_request.terminal_event_id IS 'Graph early-failed/runtime-failed 互斥投影 UUIDv7';
COMMENT ON COLUMN agent.media_preview_request.deadline_at IS '首次入队冻结且后续不得延长的媒体请求 UTC Deadline';
COMMENT ON COLUMN agent.media_preview_request.created_at IS '首次入队数据库 UTC 时间';

CREATE TABLE agent.media_preview_operation (
    operation_id uuid NOT NULL,
    tool_call_id uuid NOT NULL,
    scope_digest char(64) NOT NULL,
    tool_key varchar(32) NOT NULL,
    output_profile varchar(64) NOT NULL,
    session_id uuid NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    input_id uuid NOT NULL,
    turn_id uuid NOT NULL,
    run_id uuid NOT NULL,
    planned_batch_id uuid NOT NULL,
    planned_job_id uuid NOT NULL,
    planned_dispatch_event_id uuid NOT NULL,
    preparation_request_id uuid NOT NULL,
    preparation_command_id uuid NOT NULL,
    preparation_request_digest char(64) NULL,
    preparation_request jsonb NULL,
    preparation_id uuid NULL,
    preparation_response_digest char(64) NULL,
    preparation_response jsonb NULL,
    dispatch_digest char(64) NULL,
    failure_code varchar(64) NULL,
    recovery_reason_code varchar(64) NULL,
    status varchar(24) NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    accepted_at timestamptz NULL,
    completed_at timestamptz NULL,
    CONSTRAINT pk_media_preview_operation PRIMARY KEY (operation_id),
    CONSTRAINT uq_media_preview_operation__tool_call UNIQUE (tool_call_id),
    CONSTRAINT uq_media_preview_operation__batch UNIQUE (planned_batch_id),
    CONSTRAINT uq_media_preview_operation__job UNIQUE (planned_job_id),
    CONSTRAINT uq_media_preview_operation__dispatch_event UNIQUE (planned_dispatch_event_id),
    CONSTRAINT uq_media_preview_operation__prepare_command UNIQUE (preparation_command_id),
    CONSTRAINT ck_media_preview_operation__tool CHECK (
        (tool_key = 'generate_media' AND output_profile = 'png_640x360.v1')
        OR (tool_key = 'assemble_output' AND output_profile = 'mp4_h264_640x360_2s.v1')
    ),
    CONSTRAINT ck_media_preview_operation__digests CHECK (
        scope_digest ~ '^[0-9a-f]{64}$'
        AND (preparation_request_digest IS NULL OR preparation_request_digest ~ '^[0-9a-f]{64}$')
        AND (preparation_response_digest IS NULL OR preparation_response_digest ~ '^[0-9a-f]{64}$')
        AND (dispatch_digest IS NULL OR dispatch_digest ~ '^[0-9a-f]{64}$')
    ),
    CONSTRAINT ck_media_preview_operation__status CHECK (
        status IN ('preparing', 'recovery_pending', 'accepted', 'running', 'completed', 'failed')
    ),
    CONSTRAINT ck_media_preview_operation__version CHECK (version > 0),
    CONSTRAINT ck_media_preview_operation__preparation CHECK (
        (
            preparation_request_digest IS NULL
            AND preparation_request IS NULL
            AND preparation_id IS NULL
            AND preparation_response_digest IS NULL
            AND preparation_response IS NULL
        )
        OR (
            preparation_request_digest ~ '^[0-9a-f]{64}$'
            AND jsonb_typeof(preparation_request) = 'object'
            AND (
                (
                    preparation_id IS NULL
                    AND preparation_response_digest IS NULL
                    AND preparation_response IS NULL
                )
                OR (
                    preparation_id IS NOT NULL
                    AND preparation_response_digest ~ '^[0-9a-f]{64}$'
                    AND jsonb_typeof(preparation_response) = 'object'
                )
            )
        )
    ),
    CONSTRAINT ck_media_preview_operation__terminal CHECK (
        (status NOT IN ('completed', 'failed') AND completed_at IS NULL)
        OR (status IN ('completed', 'failed') AND completed_at IS NOT NULL)
    ),
    CONSTRAINT ck_media_preview_operation__time CHECK (
        updated_at >= created_at
        AND (accepted_at IS NULL OR accepted_at >= created_at)
        AND (completed_at IS NULL OR completed_at >= created_at)
    )
);

COMMENT ON TABLE agent.media_preview_operation IS 'media.runtime.v3preview1 的一 Tool Call 一 Operation 权威状态与 Prepare/Dispatch 恢复回执';
COMMENT ON COLUMN agent.media_preview_operation.operation_id IS 'Operation UUIDv7 主键，同时作为 Prepare first-write-wins 命令作用域';
COMMENT ON COLUMN agent.media_preview_operation.tool_call_id IS '可信 Session Lane 冻结的唯一 Tool Call UUIDv7';
COMMENT ON COLUMN agent.media_preview_operation.scope_digest IS 'Tool Pin、Source Ref、固定输出规格与可信范围的 canonical SHA-256';
COMMENT ON COLUMN agent.media_preview_operation.tool_key IS '仅 generate_media 或 assemble_output';
COMMENT ON COLUMN agent.media_preview_operation.output_profile IS '仅批准的固定 PNG 或 MP4 输出剖面';
COMMENT ON COLUMN agent.media_preview_operation.session_id IS 'Agent Session 逻辑引用，不创建物理外键';
COMMENT ON COLUMN agent.media_preview_operation.user_id IS 'Business 已认证用户逻辑引用';
COMMENT ON COLUMN agent.media_preview_operation.project_id IS 'Business 已认证项目逻辑引用';
COMMENT ON COLUMN agent.media_preview_operation.input_id IS '媒体请求 Session Input 逻辑引用';
COMMENT ON COLUMN agent.media_preview_operation.turn_id IS '媒体请求稳定 Turn UUIDv7';
COMMENT ON COLUMN agent.media_preview_operation.run_id IS '媒体请求稳定 Run UUIDv7';
COMMENT ON COLUMN agent.media_preview_operation.planned_batch_id IS 'Ensure Operation 首写时预分配且 Dispatch 重放复用的单 Batch UUIDv7';
COMMENT ON COLUMN agent.media_preview_operation.planned_job_id IS 'Ensure Operation 首写时预分配且 Dispatch 重放复用的单 Job UUIDv7';
COMMENT ON COLUMN agent.media_preview_operation.planned_dispatch_event_id IS 'Ensure Operation 首写时预分配的 Dispatch Outbox Event UUIDv7';
COMMENT ON COLUMN agent.media_preview_operation.preparation_request_id IS 'Prepare/Query 追踪复用的稳定 Request UUIDv7';
COMMENT ON COLUMN agent.media_preview_operation.preparation_command_id IS 'Business Prepare 首写生效命令的 UUIDv7';
COMMENT ON COLUMN agent.media_preview_operation.preparation_request_digest IS '发起 Business Prepare 前冻结的 exact request SHA-256';
COMMENT ON COLUMN agent.media_preview_operation.preparation_request IS '不含 Prompt、绝对路径、URL、Secret 的 Prepare exact JSON';
COMMENT ON COLUMN agent.media_preview_operation.preparation_id IS 'Business 权威预留回执 UUIDv7';
COMMENT ON COLUMN agent.media_preview_operation.preparation_response_digest IS 'Business Prepare 权威响应 exact JSON SHA-256';
COMMENT ON COLUMN agent.media_preview_operation.preparation_response IS '包含相对 Object Key 且仅供 Agent/Worker 恢复的受控响应';
COMMENT ON COLUMN agent.media_preview_operation.dispatch_digest IS '单 Batch/Job/Outbox 原子派发 exact command SHA-256';
COMMENT ON COLUMN agent.media_preview_operation.failure_code IS '终态白名单错误码，不保存内部诊断';
COMMENT ON COLUMN agent.media_preview_operation.recovery_reason_code IS '原键 Query 收敛前冻结的低基数恢复原因';
COMMENT ON COLUMN agent.media_preview_operation.status IS 'Operation 状态：preparing、recovery_pending、accepted、running、completed 或 failed';
COMMENT ON COLUMN agent.media_preview_operation.version IS '状态变化的单调乐观锁版本';
COMMENT ON COLUMN agent.media_preview_operation.created_at IS 'Operation 首写数据库 UTC 时间';
COMMENT ON COLUMN agent.media_preview_operation.updated_at IS 'Operation 最近状态变化数据库 UTC 时间';
COMMENT ON COLUMN agent.media_preview_operation.accepted_at IS 'Dispatch 原子提交的数据库 UTC 时间';
COMMENT ON COLUMN agent.media_preview_operation.completed_at IS 'completed 或 failed 终态数据库 UTC 时间';

CREATE INDEX idx_media_preview_operation__recovery
    ON agent.media_preview_operation (status, updated_at, operation_id)
    WHERE status IN ('preparing', 'recovery_pending');

CREATE TABLE agent.media_preview_batch (
    batch_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    status varchar(16) NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    started_at timestamptz NULL,
    completed_at timestamptz NULL,
    CONSTRAINT pk_media_preview_batch PRIMARY KEY (batch_id),
    CONSTRAINT uq_media_preview_batch__operation UNIQUE (operation_id),
    CONSTRAINT ck_media_preview_batch__status CHECK (status IN ('accepted', 'running', 'completed', 'failed')),
    CONSTRAINT ck_media_preview_batch__version CHECK (version > 0),
    CONSTRAINT ck_media_preview_batch__terminal CHECK (
        (status IN ('completed', 'failed') AND completed_at IS NOT NULL)
        OR (status NOT IN ('completed', 'failed') AND completed_at IS NULL)
    ),
    CONSTRAINT ck_media_preview_batch__time CHECK (
        updated_at >= created_at
        AND (started_at IS NULL OR started_at >= created_at)
        AND (completed_at IS NULL OR completed_at >= created_at)
    )
);

COMMENT ON TABLE agent.media_preview_batch IS 'media.runtime.v3preview1 固定一 Operation 一 Batch 的权威投影';
COMMENT ON COLUMN agent.media_preview_batch.batch_id IS 'Ensure Operation 首写预分配的 Batch UUIDv7';
COMMENT ON COLUMN agent.media_preview_batch.operation_id IS 'Operation 逻辑引用，不创建物理外键';
COMMENT ON COLUMN agent.media_preview_batch.status IS 'Batch 状态：accepted、running、completed 或 failed';
COMMENT ON COLUMN agent.media_preview_batch.version IS 'Batch 状态变化的单调版本';
COMMENT ON COLUMN agent.media_preview_batch.created_at IS 'Dispatch 事务数据库 UTC 时间';
COMMENT ON COLUMN agent.media_preview_batch.updated_at IS 'Batch 最近状态变化数据库 UTC 时间';
COMMENT ON COLUMN agent.media_preview_batch.started_at IS 'Job 首次合法 Claim 数据库 UTC 时间';
COMMENT ON COLUMN agent.media_preview_batch.completed_at IS 'Batch 终态数据库 UTC 时间';

CREATE TABLE agent.media_preview_job (
    job_id uuid NOT NULL,
    batch_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    session_id uuid NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    job_type varchar(24) NOT NULL,
    definition_version varchar(64) NOT NULL,
    scope_digest char(64) NOT NULL,
    output_profile varchar(64) NOT NULL,
    source_ref jsonb NOT NULL,
    target jsonb NOT NULL,
    artifact_request_digest char(64) NOT NULL,
    priority smallint NOT NULL DEFAULT 0,
    status varchar(24) NOT NULL,
    available_at timestamptz NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    attempt_id uuid NULL,
    claim_request_id uuid NULL,
    lease_owner varchar(200) NULL,
    lease_expires_at timestamptz NULL,
    fence bigint NOT NULL DEFAULT 0,
    retry_count integer NOT NULL DEFAULT 0,
    last_error_code varchar(64) NULL,
    reconciliation_reason_code varchar(64) NULL,
    result_schema_version varchar(64) NULL,
    result_digest char(64) NULL,
    result jsonb NULL,
    terminal_event_id uuid NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    started_at timestamptz NULL,
    completed_at timestamptz NULL,
    deadline_at timestamptz NOT NULL,
    CONSTRAINT pk_media_preview_job PRIMARY KEY (job_id),
    CONSTRAINT uq_media_preview_job__operation UNIQUE (operation_id),
    CONSTRAINT uq_media_preview_job__batch UNIQUE (batch_id),
    CONSTRAINT uq_media_preview_job__terminal_event UNIQUE (terminal_event_id),
    CONSTRAINT ck_media_preview_job__type CHECK (
        (job_type = 'generate_png' AND definition_version = 'generate_media.v3preview1' AND output_profile = 'png_640x360.v1')
        OR (job_type = 'assemble_mp4' AND definition_version = 'assemble_output.v3preview1' AND output_profile = 'mp4_h264_640x360_2s.v1')
    ),
    CONSTRAINT ck_media_preview_job__digests CHECK (
        scope_digest ~ '^[0-9a-f]{64}$'
        AND artifact_request_digest ~ '^[0-9a-f]{64}$'
        AND (result_digest IS NULL OR result_digest ~ '^[0-9a-f]{64}$')
    ),
    CONSTRAINT ck_media_preview_job__source_ref CHECK (
        jsonb_typeof(source_ref) = 'object'
        AND (
            (
                job_type = 'generate_png'
                AND agent.media_preview_v1_jsonb_object_key_count(source_ref) = 6
                AND source_ref->>'source_type' = 'prompt_preview'
                AND source_ref ?& ARRAY['source_id','source_version','source_digest','target_local_key','target_digest']
                AND NOT (source_ref ? 'source_object_key')
                AND source_ref->>'source_digest' ~ '^[0-9a-f]{64}$'
                AND source_ref->>'target_digest' ~ '^[0-9a-f]{64}$'
                AND length(source_ref->>'target_local_key') BETWEEN 1 AND 128
            )
            OR (
                job_type = 'assemble_mp4'
                AND agent.media_preview_v1_jsonb_object_key_count(source_ref) = 5
                AND source_ref->>'source_type' = 'image_asset'
                AND source_ref ?& ARRAY['source_id','source_version','source_digest','source_object_key']
                AND NOT (source_ref ? 'target_local_key')
                AND NOT (source_ref ? 'target_digest')
                AND source_ref->>'source_digest' ~ '^[0-9a-f]{64}$'
                AND length(source_ref->>'source_object_key') BETWEEN 1 AND 512
                AND source_ref->>'source_object_key' !~ '(^/|\\|(^|/)\.\.(/|$)|//)'
            )
        )
    ),
    CONSTRAINT ck_media_preview_job__target CHECK (
        jsonb_typeof(target) = 'object'
        AND agent.media_preview_v1_jsonb_object_key_count(target) = 4
        AND target ?& ARRAY['asset_id','asset_version','preparation_id','staging_object_key']
        AND length(target->>'staging_object_key') BETWEEN 1 AND 512
        AND target->>'staging_object_key' !~ '(^/|\\|(^|/)\.\.(/|$)|//)'
    ),
    CONSTRAINT ck_media_preview_job__priority CHECK (priority BETWEEN -100 AND 100),
    CONSTRAINT ck_media_preview_job__status CHECK (
        status IN ('pending', 'running', 'retry_wait', 'reconciling', 'succeeded', 'dead')
    ),
    CONSTRAINT ck_media_preview_job__attempt CHECK (
        attempt_count >= 0 AND retry_count >= 0 AND fence >= 0
        AND ((attempt_id IS NULL) = (claim_request_id IS NULL))
        AND ((lease_owner IS NULL) = (lease_expires_at IS NULL))
    ),
    CONSTRAINT ck_media_preview_job__lease CHECK (
        (status IN ('running', 'reconciling') AND attempt_id IS NOT NULL AND claim_request_id IS NOT NULL
            AND length(lease_owner) > 0 AND lease_expires_at IS NOT NULL AND fence > 0)
        OR (status NOT IN ('running', 'reconciling') AND lease_expires_at IS NULL)
    ),
    CONSTRAINT ck_media_preview_job__terminal CHECK (
        (
            status NOT IN ('succeeded', 'dead')
            AND result_schema_version IS NULL AND result_digest IS NULL AND result IS NULL
            AND terminal_event_id IS NULL AND completed_at IS NULL
        )
        OR (
            status IN ('succeeded', 'dead')
            AND result_schema_version = 'media_job.preview.result.v1'
            AND result_digest ~ '^[0-9a-f]{64}$'
            AND jsonb_typeof(result) = 'object'
            AND terminal_event_id IS NOT NULL AND completed_at IS NOT NULL
        )
    ),
    CONSTRAINT ck_media_preview_job__time CHECK (
        updated_at >= created_at AND deadline_at > created_at
        AND available_at >= created_at
        AND (started_at IS NULL OR started_at >= created_at)
        AND (completed_at IS NULL OR completed_at >= created_at)
    )
);

COMMENT ON TABLE agent.media_preview_job IS 'Agent-owned media preview 单 Job、Worker Attempt/Lease/Fence 与冻结终态权威记录';
COMMENT ON COLUMN agent.media_preview_job.job_id IS 'Ensure Operation 首写预分配的 Job UUIDv7';
COMMENT ON COLUMN agent.media_preview_job.batch_id IS '单 Batch 逻辑引用，不创建物理外键';
COMMENT ON COLUMN agent.media_preview_job.operation_id IS '单 Operation 逻辑引用，不创建物理外键';
COMMENT ON COLUMN agent.media_preview_job.session_id IS '终态返回原 Session Lane 的逻辑引用';
COMMENT ON COLUMN agent.media_preview_job.user_id IS 'Business Owner 校验使用的用户逻辑引用';
COMMENT ON COLUMN agent.media_preview_job.project_id IS 'Business Owner 校验使用的项目逻辑引用';
COMMENT ON COLUMN agent.media_preview_job.job_type IS '仅 generate_png 或 assemble_mp4';
COMMENT ON COLUMN agent.media_preview_job.definition_version IS '仅批准的 generate_media.v3preview1 或 assemble_output.v3preview1';
COMMENT ON COLUMN agent.media_preview_job.scope_digest IS 'Graph 冻结的执行范围 SHA-256';
COMMENT ON COLUMN agent.media_preview_job.output_profile IS '固定 PNG 或 MP4 输出剖面';
COMMENT ON COLUMN agent.media_preview_job.source_ref IS '封闭 source_ref JSON，不含 Prompt、URL、绝对路径或 Secret';
COMMENT ON COLUMN agent.media_preview_job.target IS 'Business 生成的 Asset/Preparation/相对 staging key 封闭 JSON';
COMMENT ON COLUMN agent.media_preview_job.artifact_request_digest IS 'Worker 确定性 Artifact 请求 SHA-256';
COMMENT ON COLUMN agent.media_preview_job.priority IS '固定低范围领取优先级';
COMMENT ON COLUMN agent.media_preview_job.status IS 'Job 状态：pending、running、retry_wait、reconciling、succeeded 或 dead';
COMMENT ON COLUMN agent.media_preview_job.available_at IS '数据库时钟下下一次可领取时间';
COMMENT ON COLUMN agent.media_preview_job.attempt_count IS '成功取得新 Fence 的 Attempt 总数';
COMMENT ON COLUMN agent.media_preview_job.attempt_id IS '当前或最近一次 Worker Attempt UUIDv7';
COMMENT ON COLUMN agent.media_preview_job.claim_request_id IS '当前 Claim first-write-wins UUIDv7';
COMMENT ON COLUMN agent.media_preview_job.lease_owner IS '当前或最近 Worker 稳定低基数身份';
COMMENT ON COLUMN agent.media_preview_job.lease_expires_at IS '当前 Lease 数据库过期时间，非运行态为空';
COMMENT ON COLUMN agent.media_preview_job.fence IS '每次新 Claim 单调加一的隔离令牌';
COMMENT ON COLUMN agent.media_preview_job.retry_count IS '已证明无未知副作用后的调度重试次数';
COMMENT ON COLUMN agent.media_preview_job.last_error_code IS '最近一次稳定白名单错误码';
COMMENT ON COLUMN agent.media_preview_job.reconciliation_reason_code IS 'Unknown Outcome 对账原因码';
COMMENT ON COLUMN agent.media_preview_job.result_schema_version IS '终态固定 media_job.preview.result.v1';
COMMENT ON COLUMN agent.media_preview_job.result_digest IS '终态 exact Result JSON SHA-256';
COMMENT ON COLUMN agent.media_preview_job.result IS '成功 Asset Ref 或失败 error_code 的严格联合';
COMMENT ON COLUMN agent.media_preview_job.terminal_event_id IS 'commit_terminal AppendOnce 的终态 Event UUIDv7';
COMMENT ON COLUMN agent.media_preview_job.created_at IS 'Dispatch 事务数据库 UTC 时间';
COMMENT ON COLUMN agent.media_preview_job.updated_at IS 'Job 最近状态变化数据库 UTC 时间';
COMMENT ON COLUMN agent.media_preview_job.started_at IS '首次合法 Claim 数据库 UTC 时间';
COMMENT ON COLUMN agent.media_preview_job.completed_at IS 'succeeded 或 dead 终态数据库 UTC 时间';
COMMENT ON COLUMN agent.media_preview_job.deadline_at IS 'Ingress 冻结且 Worker 不得延长的执行 Deadline';

CREATE INDEX idx_media_preview_job__claim
    ON agent.media_preview_job (available_at, priority DESC, job_id)
    WHERE status IN ('pending', 'retry_wait');

CREATE INDEX idx_media_preview_job__expired_lease
    ON agent.media_preview_job (lease_expires_at, job_id)
    WHERE status IN ('running', 'reconciling');

CREATE TABLE agent.media_preview_dispatch_outbox (
    event_id uuid NOT NULL,
    job_id uuid NOT NULL,
    schema_version varchar(64) NOT NULL,
    payload_digest char(64) NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    delivered_at timestamptz NULL,
    CONSTRAINT pk_media_preview_dispatch_outbox PRIMARY KEY (event_id),
    CONSTRAINT uq_media_preview_dispatch_outbox__job UNIQUE (job_id),
    CONSTRAINT ck_media_preview_dispatch_outbox__schema CHECK (schema_version = 'media_job.preview.dispatch.v1'),
    CONSTRAINT ck_media_preview_dispatch_outbox__digest CHECK (payload_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_media_preview_dispatch_outbox__payload CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT ck_media_preview_dispatch_outbox__time CHECK (delivered_at IS NULL OR delivered_at >= created_at)
);

COMMENT ON TABLE agent.media_preview_dispatch_outbox IS 'Media Job Dispatch 事务内 AppendOnce 的可丢失唤醒 Outbox';
COMMENT ON COLUMN agent.media_preview_dispatch_outbox.event_id IS 'Operation 首写预分配的 Dispatch Event UUIDv7';
COMMENT ON COLUMN agent.media_preview_dispatch_outbox.job_id IS '同事务创建的唯一 Job 逻辑引用';
COMMENT ON COLUMN agent.media_preview_dispatch_outbox.schema_version IS '固定 media_job.preview.dispatch.v1';
COMMENT ON COLUMN agent.media_preview_dispatch_outbox.payload_digest IS '最小 Dispatch Payload exact JSON SHA-256';
COMMENT ON COLUMN agent.media_preview_dispatch_outbox.payload IS '只含 Job ID/Profile 的最小唤醒 Payload';
COMMENT ON COLUMN agent.media_preview_dispatch_outbox.created_at IS 'Dispatch 事务数据库 UTC 时间';
COMMENT ON COLUMN agent.media_preview_dispatch_outbox.delivered_at IS '可选 Redis 唤醒成功时间，丢失不影响 PostgreSQL 扫描';

CREATE INDEX idx_media_preview_dispatch_outbox__pending
    ON agent.media_preview_dispatch_outbox (created_at, event_id)
    WHERE delivered_at IS NULL;

CREATE TABLE agent.media_preview_terminal_outbox (
    event_id uuid NOT NULL,
    session_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    batch_id uuid NOT NULL,
    job_id uuid NOT NULL,
    tool_key varchar(32) NOT NULL,
    terminal_status varchar(16) NOT NULL,
    result_schema_version varchar(64) NOT NULL,
    result_digest char(64) NOT NULL,
    result jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    delivered_at timestamptz NULL,
    lane_input_id uuid NULL,
    CONSTRAINT pk_media_preview_terminal_outbox PRIMARY KEY (event_id),
    CONSTRAINT uq_media_preview_terminal_outbox__job UNIQUE (job_id),
    CONSTRAINT uq_media_preview_terminal_outbox__lane_input UNIQUE (lane_input_id),
    CONSTRAINT ck_media_preview_terminal_outbox__tool CHECK (tool_key IN ('generate_media', 'assemble_output')),
    CONSTRAINT ck_media_preview_terminal_outbox__status CHECK (terminal_status IN ('succeeded', 'failed')),
    CONSTRAINT ck_media_preview_terminal_outbox__schema CHECK (result_schema_version = 'media_job.preview.result.v1'),
    CONSTRAINT ck_media_preview_terminal_outbox__digest CHECK (result_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_media_preview_terminal_outbox__result CHECK (jsonb_typeof(result) = 'object'),
    CONSTRAINT ck_media_preview_terminal_outbox__delivery CHECK (
        (delivered_at IS NULL AND lane_input_id IS NULL)
        OR (delivered_at IS NOT NULL AND lane_input_id IS NOT NULL AND delivered_at >= occurred_at)
    )
);

COMMENT ON TABLE agent.media_preview_terminal_outbox IS 'Worker 当前 Fence 终态与 Job/Batch/Operation 同事务 AppendOnce 的 Session Lane Outbox';
COMMENT ON COLUMN agent.media_preview_terminal_outbox.event_id IS 'Worker 提供并由 commit_terminal 幂等复用的终态 Event UUIDv7';
COMMENT ON COLUMN agent.media_preview_terminal_outbox.session_id IS 'Terminal Bridge 目标 Session 逻辑引用';
COMMENT ON COLUMN agent.media_preview_terminal_outbox.operation_id IS '终态 Operation 逻辑引用';
COMMENT ON COLUMN agent.media_preview_terminal_outbox.batch_id IS '终态 Batch 逻辑引用';
COMMENT ON COLUMN agent.media_preview_terminal_outbox.job_id IS '终态 Job 逻辑引用且每 Job 仅一条';
COMMENT ON COLUMN agent.media_preview_terminal_outbox.tool_key IS '原始 generate_media 或 assemble_output';
COMMENT ON COLUMN agent.media_preview_terminal_outbox.terminal_status IS '仅 succeeded 或 failed';
COMMENT ON COLUMN agent.media_preview_terminal_outbox.result_schema_version IS '固定 media_job.preview.result.v1';
COMMENT ON COLUMN agent.media_preview_terminal_outbox.result_digest IS '严格终态 Result JSON SHA-256';
COMMENT ON COLUMN agent.media_preview_terminal_outbox.result IS '不含 stderr、路径或 Secret 的成功/失败严格联合';
COMMENT ON COLUMN agent.media_preview_terminal_outbox.occurred_at IS 'commit_terminal 数据库 UTC 时间';
COMMENT ON COLUMN agent.media_preview_terminal_outbox.delivered_at IS 'Terminal Bridge AppendOnce 成功后的数据库 UTC 时间';
COMMENT ON COLUMN agent.media_preview_terminal_outbox.lane_input_id IS 'Bridge 创建或重放的 media_job_preview_terminal Input UUIDv7';

CREATE INDEX idx_media_preview_terminal_outbox__pending
    ON agent.media_preview_terminal_outbox (session_id, occurred_at, event_id)
    WHERE delivered_at IS NULL;

CREATE VIEW agent.media_job_preview_v1_claimable
WITH (security_barrier = true)
AS
SELECT
    job_record.job_id,
    CASE
        WHEN job_record.status IN ('running', 'reconciling') THEN job_record.lease_expires_at
        ELSE job_record.available_at
    END AS available_at,
    job_record.priority
FROM agent.media_preview_job AS job_record
WHERE (
        job_record.status IN ('pending', 'retry_wait')
        AND job_record.available_at <= clock_timestamp()
    )
    OR (
        job_record.status IN ('running', 'reconciling')
        AND job_record.lease_expires_at <= clock_timestamp()
    );

COMMENT ON VIEW agent.media_job_preview_v1_claimable IS 'Worker 只读最小可领取 Job ID/时间/优先级视图，包含到期待领与已过期 Lease';
COMMENT ON COLUMN agent.media_job_preview_v1_claimable.job_id IS 'Worker 可传入 claim 函数的候选 Job UUIDv7';
COMMENT ON COLUMN agent.media_job_preview_v1_claimable.available_at IS '待领可用时间或运行态 Lease 过期时间';
COMMENT ON COLUMN agent.media_job_preview_v1_claimable.priority IS '固定低范围领取优先级';

CREATE FUNCTION agent.media_job_preview_v1_claim(
    p_job_id uuid,
    p_worker_id text,
    p_attempt_id uuid,
    p_claim_request_id uuid,
    p_lease_ttl_ms integer
) RETURNS TABLE (
    schema_version text,
    job_id uuid,
    batch_id uuid,
    operation_id uuid,
    session_id uuid,
    user_id uuid,
    project_id uuid,
    job_type text,
    definition_version text,
    scope_digest text,
    output_profile text,
    source_ref jsonb,
    target jsonb,
    artifact_request_digest text,
    attempt_id uuid,
    fence bigint,
    lease_expires_at timestamptz,
    created_at timestamptz,
    deadline_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, agent
AS $$
DECLARE
    v_now timestamptz := clock_timestamp();
    v_job agent.media_preview_job%ROWTYPE;
BEGIN
    IF p_job_id IS NULL
       OR p_worker_id IS NULL OR length(btrim(p_worker_id)) NOT BETWEEN 1 AND 200
       OR p_attempt_id IS NULL OR substring(p_attempt_id::text FROM 15 FOR 1) <> '7'
       OR p_claim_request_id IS NULL OR substring(p_claim_request_id::text FROM 15 FOR 1) <> '7'
       OR p_lease_ttl_ms NOT BETWEEN 1000 AND 300000 THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid media preview claim arguments';
    END IF;

    SELECT job_record.*
    INTO v_job
    FROM agent.media_preview_job AS job_record
    WHERE job_record.job_id = p_job_id
    FOR UPDATE;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    IF v_job.claim_request_id = p_claim_request_id THEN
        IF v_job.attempt_id <> p_attempt_id OR v_job.lease_owner <> p_worker_id
           OR v_job.status NOT IN ('running', 'reconciling') THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'media preview claim idempotency conflict';
        END IF;
    ELSE
        IF NOT (
            (v_job.status IN ('pending', 'retry_wait') AND v_job.available_at <= v_now)
            OR (v_job.status IN ('running', 'reconciling') AND v_job.lease_expires_at <= v_now)
        ) THEN
            RETURN;
        END IF;

        UPDATE agent.media_preview_job AS job_record
        SET status = 'running',
            attempt_count = job_record.attempt_count + 1,
            attempt_id = p_attempt_id,
            claim_request_id = p_claim_request_id,
            lease_owner = p_worker_id,
            lease_expires_at = v_now + make_interval(secs => p_lease_ttl_ms::double precision / 1000.0),
            fence = job_record.fence + 1,
            updated_at = v_now,
            started_at = COALESCE(job_record.started_at, v_now),
            reconciliation_reason_code = NULL
        WHERE job_record.job_id = p_job_id;

        UPDATE agent.media_preview_batch AS batch_record
        SET status = 'running', version = batch_record.version + 1,
            updated_at = v_now, started_at = COALESCE(batch_record.started_at, v_now)
        WHERE batch_record.batch_id = v_job.batch_id AND batch_record.status = 'accepted';

        UPDATE agent.media_preview_operation AS operation_record
        SET status = 'running', version = operation_record.version + 1, updated_at = v_now
        WHERE operation_record.operation_id = v_job.operation_id AND operation_record.status = 'accepted';
    END IF;

    RETURN QUERY
    SELECT
        'agent.media_job.preview.v1'::text,
        job_record.job_id,
        job_record.batch_id,
        job_record.operation_id,
        job_record.session_id,
        job_record.user_id,
        job_record.project_id,
        job_record.job_type::text,
        job_record.definition_version::text,
        job_record.scope_digest::text,
        job_record.output_profile::text,
        job_record.source_ref,
        job_record.target,
        job_record.artifact_request_digest::text,
        job_record.attempt_id,
        job_record.fence,
        job_record.lease_expires_at,
        job_record.created_at,
        job_record.deadline_at
    FROM agent.media_preview_job AS job_record
    WHERE job_record.job_id = p_job_id;
END;
$$;

COMMENT ON FUNCTION agent.media_job_preview_v1_claim(uuid, text, uuid, uuid, integer) IS '按 Job ID、Claim UUIDv7、数据库时钟和过期 Lease 原子取得更高 Fence；同键重放同一 Envelope';

CREATE FUNCTION agent.media_job_preview_v1_renew(
    p_job_id uuid,
    p_worker_id text,
    p_attempt_id uuid,
    p_fence bigint,
    p_lease_ttl_ms integer
) RETURNS TABLE (status text, lease_expires_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, agent
AS $$
DECLARE
    v_now timestamptz := clock_timestamp();
BEGIN
    IF p_lease_ttl_ms NOT BETWEEN 1000 AND 300000 OR p_fence < 1 THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid media preview renew arguments';
    END IF;
    RETURN QUERY
    UPDATE agent.media_preview_job AS job_record
    SET lease_expires_at = v_now + make_interval(secs => p_lease_ttl_ms::double precision / 1000.0),
        updated_at = v_now
    WHERE job_record.job_id = p_job_id
      AND job_record.status IN ('running', 'reconciling')
      AND job_record.lease_owner = p_worker_id
      AND job_record.attempt_id = p_attempt_id
      AND job_record.fence = p_fence
      AND job_record.lease_expires_at > v_now
    RETURNING 'renewed'::text, job_record.lease_expires_at;
END;
$$;

COMMENT ON FUNCTION agent.media_job_preview_v1_renew(uuid, text, uuid, bigint, integer) IS '仅当前 running/reconciling Attempt/Fence 可按数据库时钟续租，零行等价 LEASE_LOST';

CREATE FUNCTION agent.media_job_preview_v1_schedule_retry(
    p_job_id uuid,
    p_worker_id text,
    p_attempt_id uuid,
    p_fence bigint,
    p_retry_delay_ms integer,
    p_error_code text
) RETURNS TABLE (status text, available_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, agent
AS $$
DECLARE
    v_now timestamptz := clock_timestamp();
BEGIN
    IF p_retry_delay_ms NOT BETWEEN 1 AND 600000 OR p_fence < 1
       OR p_error_code !~ '^[A-Z][A-Z0-9_]{0,63}$' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid media preview retry arguments';
    END IF;
    RETURN QUERY
    UPDATE agent.media_preview_job AS job_record
    SET status = 'retry_wait',
        retry_count = job_record.retry_count + 1,
        available_at = v_now + make_interval(secs => p_retry_delay_ms::double precision / 1000.0),
        lease_owner = NULL,
        lease_expires_at = NULL,
        last_error_code = p_error_code,
        updated_at = v_now
    WHERE job_record.job_id = p_job_id
      AND job_record.status = 'running'
      AND job_record.lease_owner = p_worker_id
      AND job_record.attempt_id = p_attempt_id
      AND job_record.fence = p_fence
      AND job_record.lease_expires_at > v_now
    RETURNING job_record.status::text, job_record.available_at;
END;
$$;

COMMENT ON FUNCTION agent.media_job_preview_v1_schedule_retry(uuid, text, uuid, bigint, integer, text) IS '仅已证明无 Unknown Side Effect 的当前 Fence 可进入 retry_wait，零行等价 LEASE_LOST';

CREATE FUNCTION agent.media_job_preview_v1_mark_reconciling(
    p_job_id uuid,
    p_worker_id text,
    p_attempt_id uuid,
    p_fence bigint,
    p_reason_code text
) RETURNS TABLE (status text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, agent
AS $$
DECLARE
    v_now timestamptz := clock_timestamp();
BEGIN
    IF p_fence < 1 OR p_reason_code !~ '^[A-Z][A-Z0-9_]{0,63}$' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid media preview reconciliation arguments';
    END IF;
    RETURN QUERY
    UPDATE agent.media_preview_job AS job_record
    SET status = 'reconciling', reconciliation_reason_code = p_reason_code, updated_at = v_now
    WHERE job_record.job_id = p_job_id
      AND job_record.status = 'running'
      AND job_record.lease_owner = p_worker_id
      AND job_record.attempt_id = p_attempt_id
      AND job_record.fence = p_fence
      AND job_record.lease_expires_at > v_now
    RETURNING job_record.status::text;
END;
$$;

COMMENT ON FUNCTION agent.media_job_preview_v1_mark_reconciling(uuid, text, uuid, bigint, text) IS 'Finalize 或终态响应未知时按当前 Fence 进入 reconciling，禁止当普通 retry';

CREATE FUNCTION agent.media_job_preview_v1_commit_terminal(
    p_job_id uuid,
    p_worker_id text,
    p_attempt_id uuid,
    p_fence bigint,
    p_terminal_event_id uuid,
    p_terminal_status text,
    p_result_schema_version text,
    p_result_digest text,
    p_result jsonb
) RETURNS TABLE (
    job_status text,
    batch_status text,
    operation_status text,
    terminal_event_id uuid
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, agent
AS $$
DECLARE
    v_now timestamptz := clock_timestamp();
    v_job agent.media_preview_job%ROWTYPE;
    v_tool_key text;
    v_job_status text;
    v_aggregate_status text;
BEGIN
    IF p_fence < 1
       OR p_terminal_event_id IS NULL OR substring(p_terminal_event_id::text FROM 15 FOR 1) <> '7'
       OR p_terminal_status NOT IN ('succeeded', 'failed')
       OR p_result_schema_version <> 'media_job.preview.result.v1'
       OR p_result_digest !~ '^[0-9a-f]{64}$'
       OR jsonb_typeof(p_result) <> 'object'
       OR NOT (
            (
                p_terminal_status = 'succeeded'
                AND p_result->>'schema_version' = 'media_job.preview.result.v1'
                AND p_result->>'status' = 'succeeded'
                AND agent.media_preview_v1_jsonb_object_key_count(p_result) = 4
                AND p_result ?& ARRAY['asset_ref','finalization_receipt_id']
                AND jsonb_typeof(p_result->'asset_ref') = 'object'
                AND agent.media_preview_v1_jsonb_object_key_count(p_result->'asset_ref') = 7
                AND p_result->'asset_ref'->>'status' = 'ready'
                AND p_result->'asset_ref'->>'content_digest' ~ '^[0-9a-f]{64}$'
                AND (p_result->'asset_ref'->>'size_bytes')::bigint > 0
            )
            OR (
                p_terminal_status = 'failed'
                AND p_result->>'schema_version' = 'media_job.preview.result.v1'
                AND p_result->>'status' = 'failed'
                AND agent.media_preview_v1_jsonb_object_key_count(p_result) = 3
                AND p_result->>'error_code' ~ '^[A-Z][A-Z0-9_]{0,63}$'
            )
       ) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid media preview terminal result';
    END IF;

    SELECT job_record.*
    INTO v_job
    FROM agent.media_preview_job AS job_record
    WHERE job_record.job_id = p_job_id
    FOR UPDATE;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT operation_record.tool_key
    INTO v_tool_key
    FROM agent.media_preview_operation AS operation_record
    WHERE operation_record.operation_id = v_job.operation_id
      AND operation_record.planned_job_id = v_job.job_id;

    IF NOT FOUND OR NOT (
        (v_job.job_type = 'generate_png' AND v_tool_key = 'generate_media')
        OR (v_job.job_type = 'assemble_mp4' AND v_tool_key = 'assemble_output')
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid media preview terminal job tool binding';
    END IF;

    IF p_terminal_status = 'succeeded' AND NOT COALESCE((
        p_result->'asset_ref'->'asset_id' = v_job.target->'asset_id'
        AND p_result->'asset_ref'->'version' = v_job.target->'asset_version'
        AND (
            (
                v_job.job_type = 'generate_png'
                AND p_result->'asset_ref'->>'media_kind' = 'image'
                AND p_result->'asset_ref'->>'mime_type' = 'image/png'
            )
            OR (
                v_job.job_type = 'assemble_mp4'
                AND p_result->'asset_ref'->>'media_kind' = 'video'
                AND p_result->'asset_ref'->>'mime_type' = 'video/mp4'
            )
        )
    ), FALSE) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'invalid media preview terminal asset binding';
    END IF;

    IF v_job.terminal_event_id IS NOT NULL THEN
        IF v_job.terminal_event_id <> p_terminal_event_id
           OR v_job.result_schema_version <> p_result_schema_version
           OR v_job.result_digest <> p_result_digest
           OR v_job.result <> p_result
           OR (v_job.status = 'succeeded') <> (p_terminal_status = 'succeeded') THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'media preview terminal idempotency conflict';
        END IF;
    ELSE
        IF v_job.status NOT IN ('running', 'reconciling')
           OR v_job.lease_owner <> p_worker_id
           OR v_job.attempt_id <> p_attempt_id
           OR v_job.fence <> p_fence
           OR v_job.lease_expires_at <= v_now THEN
            RETURN;
        END IF;

        v_job_status := CASE WHEN p_terminal_status = 'succeeded' THEN 'succeeded' ELSE 'dead' END;
        v_aggregate_status := CASE WHEN p_terminal_status = 'succeeded' THEN 'completed' ELSE 'failed' END;

        UPDATE agent.media_preview_job AS job_record
        SET status = v_job_status,
            result_schema_version = p_result_schema_version,
            result_digest = p_result_digest,
            result = p_result,
            terminal_event_id = p_terminal_event_id,
            lease_owner = NULL,
            lease_expires_at = NULL,
            updated_at = v_now,
            completed_at = v_now
        WHERE job_record.job_id = p_job_id;

        UPDATE agent.media_preview_batch AS batch_record
        SET status = v_aggregate_status, version = batch_record.version + 1,
            updated_at = v_now, completed_at = v_now
        WHERE batch_record.batch_id = v_job.batch_id
          AND batch_record.status IN ('accepted', 'running');

        UPDATE agent.media_preview_operation AS operation_record
        SET status = v_aggregate_status, version = operation_record.version + 1,
            failure_code = CASE WHEN p_terminal_status = 'failed' THEN p_result->>'error_code' ELSE NULL END,
            updated_at = v_now, completed_at = v_now
        WHERE operation_record.operation_id = v_job.operation_id
          AND operation_record.status IN ('accepted', 'running');

        INSERT INTO agent.media_preview_terminal_outbox (
            event_id, session_id, operation_id, batch_id, job_id, tool_key,
            terminal_status, result_schema_version, result_digest, result, occurred_at
        ) VALUES (
            p_terminal_event_id, v_job.session_id, v_job.operation_id, v_job.batch_id, v_job.job_id, v_tool_key,
            p_terminal_status, p_result_schema_version, p_result_digest, p_result, v_now
        );
    END IF;

    RETURN QUERY
    SELECT job_record.status::text, batch_record.status::text, operation_record.status::text, job_record.terminal_event_id
    FROM agent.media_preview_job AS job_record
    JOIN agent.media_preview_batch AS batch_record ON batch_record.batch_id = job_record.batch_id
    JOIN agent.media_preview_operation AS operation_record ON operation_record.operation_id = job_record.operation_id
    WHERE job_record.job_id = p_job_id;
END;
$$;

COMMENT ON FUNCTION agent.media_job_preview_v1_commit_terminal(uuid, text, uuid, bigint, uuid, text, text, text, jsonb) IS '当前 Fence 原子更新 Job/Batch/Operation 并 AppendOnce Terminal Outbox；同 event/digest 重放，异义冲突';

CREATE FUNCTION agent.media_job_preview_v1_get(
    p_job_id uuid
) RETURNS TABLE (
    job_status text,
    attempt_id uuid,
    fence bigint,
    lease_owner text,
    lease_expires_at timestamptz,
    result_schema_version text,
    result_digest text,
    terminal_event_id uuid
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, agent
AS $$
    SELECT
        job_record.status::text,
        job_record.attempt_id,
        job_record.fence,
        job_record.lease_owner::text,
        job_record.lease_expires_at,
        job_record.result_schema_version::text,
        job_record.result_digest::text,
        job_record.terminal_event_id
    FROM agent.media_preview_job AS job_record
    WHERE job_record.job_id = p_job_id
$$;

COMMENT ON FUNCTION agent.media_job_preview_v1_get(uuid) IS '按 Job UUID 只读查询 Worker Unknown Outcome 收敛所需的最小权威状态';

REVOKE ALL ON agent.media_job_preview_v1_claimable FROM PUBLIC;
REVOKE ALL ON FUNCTION agent.media_job_preview_v1_claim(uuid, text, uuid, uuid, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION agent.media_job_preview_v1_renew(uuid, text, uuid, bigint, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION agent.media_job_preview_v1_schedule_retry(uuid, text, uuid, bigint, integer, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION agent.media_job_preview_v1_mark_reconciling(uuid, text, uuid, bigint, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION agent.media_job_preview_v1_commit_terminal(uuid, text, uuid, bigint, uuid, text, text, text, jsonb) FROM PUBLIC;
REVOKE ALL ON FUNCTION agent.media_job_preview_v1_get(uuid) FROM PUBLIC;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'dora_worker_app') THEN
        EXECUTE format('GRANT CONNECT ON DATABASE %I TO dora_worker_app', current_database());
        GRANT USAGE ON SCHEMA agent TO dora_worker_app;
        GRANT SELECT ON agent.media_job_preview_v1_claimable TO dora_worker_app;
        GRANT EXECUTE ON FUNCTION agent.media_job_preview_v1_claim(uuid, text, uuid, uuid, integer) TO dora_worker_app;
        GRANT EXECUTE ON FUNCTION agent.media_job_preview_v1_renew(uuid, text, uuid, bigint, integer) TO dora_worker_app;
        GRANT EXECUTE ON FUNCTION agent.media_job_preview_v1_schedule_retry(uuid, text, uuid, bigint, integer, text) TO dora_worker_app;
        GRANT EXECUTE ON FUNCTION agent.media_job_preview_v1_mark_reconciling(uuid, text, uuid, bigint, text) TO dora_worker_app;
        GRANT EXECUTE ON FUNCTION agent.media_job_preview_v1_commit_terminal(uuid, text, uuid, bigint, uuid, text, text, text, jsonb) TO dora_worker_app;
        GRANT EXECUTE ON FUNCTION agent.media_job_preview_v1_get(uuid) TO dora_worker_app;
    END IF;
END
$$;
