ALTER TABLE agent.session_input
    DROP CONSTRAINT ck_session_input__status;

ALTER TABLE agent.session_input
    ADD CONSTRAINT ck_session_input__status CHECK (
        status IN ('pending', 'claimed', 'running', 'retry_wait', 'recovery_pending', 'resolved', 'dead')
    );

COMMENT ON COLUMN agent.session_input.status IS '输入状态：pending、claimed、running、retry_wait、recovery_pending、resolved 或 dead';

ALTER TABLE agent.session_input
    DROP CONSTRAINT ck_session_input__source_type;

ALTER TABLE agent.session_input
    ADD CONSTRAINT ck_session_input__source_type CHECK (
        source_type IN ('user_message', 'creation_spec_preview', 'analyze_materials_preview', 'plan_storyboard_preview', 'write_prompts_preview')
    );

COMMENT ON COLUMN agent.session_input.source_type IS '可信输入来源类型：普通用户消息、CreationSpec Preview、Analyze Materials Preview、Plan Storyboard Preview 或 Write Prompts Preview 结构化意图';

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

ALTER TABLE agent.session_event_log
    DROP CONSTRAINT ck_session_event_log__aggregate_type;

ALTER TABLE agent.session_event_log
    ADD CONSTRAINT ck_session_event_log__aggregate_type CHECK (
        aggregate_type IN ('session', 'session_input', 'creation_spec', 'session_turn', 'analyze_materials_preview', 'plan_storyboard_preview', 'write_prompts_preview')
    );

COMMENT ON COLUMN agent.session_event_log.aggregate_type IS '事件关联聚合类型：session、session_input、creation_spec、session_turn、analyze_materials_preview、plan_storyboard_preview 或 write_prompts_preview';

CREATE TABLE agent.write_prompts_preview_run (
    input_id uuid NOT NULL,
    request_id uuid NOT NULL,
    idempotency_key uuid NOT NULL,
    request_digest char(64) NOT NULL,
    session_id uuid NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    turn_id uuid NOT NULL,
    run_id uuid NOT NULL,
    tool_call_id uuid NOT NULL,
    business_command_id uuid NOT NULL,
    router_model_call_id uuid NOT NULL,
    graph_model_call_id uuid NOT NULL,
    accepted_event_id uuid NOT NULL,
    terminal_event_id uuid NOT NULL,
    owner_fence bigint NOT NULL DEFAULT 0,
    status varchar(24) NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    started_at timestamptz NULL,
    completed_at timestamptz NULL,
    CONSTRAINT pk_write_prompts_preview_run PRIMARY KEY (input_id),
    CONSTRAINT uq_write_prompts_preview_run__request UNIQUE (session_id, request_id),
    CONSTRAINT uq_write_prompts_preview_run__idempotency UNIQUE (session_id, idempotency_key),
    CONSTRAINT uq_write_prompts_preview_run__turn UNIQUE (turn_id),
    CONSTRAINT uq_write_prompts_preview_run__run UNIQUE (run_id),
    CONSTRAINT uq_write_prompts_preview_run__tool_call UNIQUE (tool_call_id),
    CONSTRAINT uq_write_prompts_preview_run__business_command UNIQUE (business_command_id),
    CONSTRAINT uq_write_prompts_preview_run__router_model UNIQUE (router_model_call_id),
    CONSTRAINT uq_write_prompts_preview_run__graph_model UNIQUE (graph_model_call_id),
    CONSTRAINT uq_write_prompts_preview_run__accepted_event UNIQUE (accepted_event_id),
    CONSTRAINT uq_write_prompts_preview_run__terminal_event UNIQUE (terminal_event_id),
    CONSTRAINT ck_write_prompts_preview_run__request_digest CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_write_prompts_preview_run__owner_fence CHECK (owner_fence >= 0),
    CONSTRAINT ck_write_prompts_preview_run__status CHECK (status IN ('created', 'running', 'recovery_pending', 'completed', 'failed')),
    CONSTRAINT ck_write_prompts_preview_run__version CHECK (version > 0),
    CONSTRAINT ck_write_prompts_preview_run__time CHECK (
        updated_at >= created_at
        AND (started_at IS NULL OR started_at >= created_at)
        AND (completed_at IS NULL OR completed_at >= created_at)
        AND ((status IN ('completed', 'failed') AND completed_at IS NOT NULL) OR (status NOT IN ('completed', 'failed') AND completed_at IS NULL))
    )
);

COMMENT ON TABLE agent.write_prompts_preview_run IS 'Write Prompts Preview 稳定执行身份、Session Fence 与 Run 状态';
COMMENT ON COLUMN agent.write_prompts_preview_run.input_id IS '无 Message Session Input UUIDv7，同时作为本表主键';
COMMENT ON COLUMN agent.write_prompts_preview_run.request_id IS '首次入队 HTTP 请求 UUIDv7，accepted 与 terminal Event 复用';
COMMENT ON COLUMN agent.write_prompts_preview_run.idempotency_key IS 'Session 内 typed Prompt Intent 同义重放键';
COMMENT ON COLUMN agent.write_prompts_preview_run.request_digest IS 'Storyboard Preview 精确引用与 canonical typed Intent 的 SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_run.session_id IS 'Agent Session 逻辑引用，不创建物理外键';
COMMENT ON COLUMN agent.write_prompts_preview_run.user_id IS '入队时冻结的可信 Business User 逻辑引用';
COMMENT ON COLUMN agent.write_prompts_preview_run.project_id IS '入队时冻结的可信 Business Project 逻辑引用';
COMMENT ON COLUMN agent.write_prompts_preview_run.turn_id IS '技术重试与恢复复用的稳定 Turn UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_run.run_id IS 'Lease takeover 复用的稳定 Run UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_run.tool_call_id IS 'Router 必须原样使用的稳定 write_prompts Tool Call UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_run.business_command_id IS 'Business Save、Query 与 Unknown Outcome 同键恢复复用的命令 UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_run.router_model_call_id IS '外层 Router Fake Model 稳定调用 UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_run.graph_model_call_id IS 'Graph Prompt Fake Model 稳定调用 UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_run.accepted_event_id IS '首次入队 accepted Event UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_run.terminal_event_id IS 'completed、failed 与 runtime_failed 互斥终态共用 Event UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_run.owner_fence IS '最近取得执行权的 Session Lane Fencing Token，未 Claim 时为零';
COMMENT ON COLUMN agent.write_prompts_preview_run.status IS 'Run 状态：created、running、recovery_pending、completed 或 failed';
COMMENT ON COLUMN agent.write_prompts_preview_run.version IS 'Run 状态变化的单调乐观锁版本';
COMMENT ON COLUMN agent.write_prompts_preview_run.created_at IS '首次入队事务的数据库 UTC 时间';
COMMENT ON COLUMN agent.write_prompts_preview_run.updated_at IS 'Run 最近变化的数据库 UTC 时间';
COMMENT ON COLUMN agent.write_prompts_preview_run.started_at IS '首次合法进入 running 的数据库 UTC 时间';
COMMENT ON COLUMN agent.write_prompts_preview_run.completed_at IS 'completed 或 failed 终态首写数据库 UTC 时间';

CREATE INDEX idx_write_prompts_preview_run__session_status
    ON agent.write_prompts_preview_run (session_id, status, created_at, input_id);

CREATE TABLE agent.write_prompts_preview_turn_context (
    turn_id uuid NOT NULL,
    profile varchar(64) NOT NULL,
    schema_version varchar(64) NOT NULL,
    request_id uuid NOT NULL,
    session_id uuid NOT NULL,
    input_id uuid NOT NULL,
    run_id uuid NOT NULL,
    tool_call_id uuid NOT NULL,
    business_command_id uuid NOT NULL,
    router_model_call_id uuid NOT NULL,
    graph_model_call_id uuid NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    intent_ciphertext bytea NOT NULL,
    intent_key_version varchar(64) NOT NULL,
    intent_digest char(64) NOT NULL,
    storyboard_preview_id uuid NOT NULL,
    storyboard_preview_version bigint NOT NULL,
    storyboard_preview_content_digest char(64) NOT NULL,
    access_scope_ref varchar(128) NOT NULL,
    access_scope_digest char(64) NOT NULL,
    tool_registry_ref varchar(128) NOT NULL,
    tool_registry_digest char(64) NOT NULL,
    tool_definition_ref varchar(128) NOT NULL,
    tool_definition_digest char(64) NOT NULL,
    intent_schema_ref varchar(128) NOT NULL,
    candidate_schema_ref varchar(128) NOT NULL,
    result_schema_ref varchar(128) NOT NULL,
    prompt_ref varchar(128) NOT NULL,
    prompt_digest char(64) NOT NULL,
    validator_ref varchar(128) NOT NULL,
    validator_digest char(64) NOT NULL,
    exact_set_validator_ref varchar(128) NOT NULL,
    exact_set_validator_digest char(64) NOT NULL,
    router_model_route_ref varchar(128) NOT NULL,
    router_model_route_digest char(64) NOT NULL,
    prompt_model_route_ref varchar(128) NOT NULL,
    prompt_model_route_digest char(64) NOT NULL,
    runtime_policy_ref varchar(128) NOT NULL,
    runtime_policy_digest char(64) NOT NULL,
    budget_ref varchar(128) NOT NULL,
    budget_digest char(64) NOT NULL,
    context_digest char(64) NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_write_prompts_preview_turn_context PRIMARY KEY (turn_id),
    CONSTRAINT uq_write_prompts_preview_turn_context__input UNIQUE (input_id),
    CONSTRAINT uq_write_prompts_preview_turn_context__request UNIQUE (session_id, request_id),
    CONSTRAINT uq_write_prompts_preview_turn_context__run UNIQUE (run_id),
    CONSTRAINT uq_write_prompts_preview_turn_context__tool_call UNIQUE (tool_call_id),
    CONSTRAINT uq_write_prompts_preview_turn_context__business_command UNIQUE (business_command_id),
    CONSTRAINT uq_write_prompts_preview_turn_context__router_model UNIQUE (router_model_call_id),
    CONSTRAINT uq_write_prompts_preview_turn_context__graph_model UNIQUE (graph_model_call_id),
    CONSTRAINT ck_write_prompts_preview_turn_context__profile CHECK (profile = 'write_prompts.runtime.v2preview1'),
    CONSTRAINT ck_write_prompts_preview_turn_context__schema CHECK (schema_version = 'write_prompts.turn_context.v2preview1'),
    CONSTRAINT ck_write_prompts_preview_turn_context__intent_ciphertext CHECK (octet_length(intent_ciphertext) > 0),
    CONSTRAINT ck_write_prompts_preview_turn_context__intent_key CHECK (length(intent_key_version) > 0),
    CONSTRAINT ck_write_prompts_preview_turn_context__storyboard_preview_version CHECK (storyboard_preview_version = 1),
    CONSTRAINT ck_write_prompts_preview_turn_context__refs CHECK (
        length(access_scope_ref) > 0 AND length(tool_registry_ref) > 0 AND length(tool_definition_ref) > 0
        AND length(intent_schema_ref) > 0 AND length(candidate_schema_ref) > 0 AND length(result_schema_ref) > 0
        AND length(prompt_ref) > 0 AND length(validator_ref) > 0 AND length(exact_set_validator_ref) > 0
        AND length(router_model_route_ref) > 0 AND length(prompt_model_route_ref) > 0
        AND length(runtime_policy_ref) > 0 AND length(budget_ref) > 0
    ),
    CONSTRAINT ck_write_prompts_preview_turn_context__digests CHECK (
        intent_digest ~ '^[0-9a-f]{64}$'
        AND storyboard_preview_content_digest ~ '^[0-9a-f]{64}$'
        AND access_scope_digest ~ '^[0-9a-f]{64}$'
        AND tool_registry_digest ~ '^[0-9a-f]{64}$'
        AND tool_definition_digest ~ '^[0-9a-f]{64}$'
        AND prompt_digest ~ '^[0-9a-f]{64}$'
        AND validator_digest ~ '^[0-9a-f]{64}$'
        AND exact_set_validator_digest ~ '^[0-9a-f]{64}$'
        AND router_model_route_digest ~ '^[0-9a-f]{64}$'
        AND prompt_model_route_digest ~ '^[0-9a-f]{64}$'
        AND runtime_policy_digest ~ '^[0-9a-f]{64}$'
        AND budget_digest ~ '^[0-9a-f]{64}$'
        AND context_digest ~ '^[0-9a-f]{64}$'
    )
);

COMMENT ON TABLE agent.write_prompts_preview_turn_context IS 'Write Prompts Preview 入队事务 append-once 冻结的可信 Turn Context 与加密 Intent';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.turn_id IS 'Context 主键和稳定 Turn UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.profile IS '唯一批准本地 Profile：write_prompts.runtime.v2preview1';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.schema_version IS '最小不可变上下文版本：write_prompts.turn_context.v2preview1';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.request_id IS '首次入队冻结并由 accepted 与 terminal Event 复用的请求 UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.session_id IS '冻结的 Agent Session 逻辑引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.input_id IS '冻结的无 Message Session Input UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.run_id IS '冻结的稳定 Run UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.tool_call_id IS '冻结的 write_prompts Tool Call UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.business_command_id IS '冻结的 Business Save、Query 与恢复命令 UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.router_model_call_id IS '冻结的 Router Fake Model Call UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.graph_model_call_id IS '冻结的 Graph Prompt Fake Model Call UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.user_id IS '冻结的可信 Business User 逻辑引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.project_id IS '冻结的可信 Business Project 逻辑引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.intent_ciphertext IS '完整 canonical typed Intent 的 DRAE v1 AEAD 密文';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.intent_key_version IS 'Intent 密文使用的 Agent 内容密钥版本';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.intent_digest IS 'canonical Intent 明文 SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.storyboard_preview_id IS '本 Turn 唯一允许消费的 Business Storyboard Preview Draft UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.storyboard_preview_version IS '冻结的 Storyboard Preview Draft 精确版本，本 Profile 固定为一';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.storyboard_preview_content_digest IS '冻结的 Storyboard Preview 内容 SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.access_scope_ref IS 'Owner、Project 与 Storyboard Preview 读取权限的冻结策略引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.access_scope_digest IS '访问范围 canonical SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.tool_registry_ref IS '恰好包含 write_prompts 的可执行 Registry 引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.tool_registry_digest IS '可执行 Registry canonical SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.tool_definition_ref IS 'write_prompts.v2preview1 Tool Definition 引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.tool_definition_digest IS 'Tool Definition canonical SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.intent_schema_ref IS '严格 Tool Intent Schema 引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.candidate_schema_ref IS 'Graph Prompt Model 候选 Schema 引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.result_schema_ref IS '严格 Tool Result Schema 引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.prompt_ref IS 'Graph Prompt Prompt 精确版本引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.prompt_digest IS 'Graph Prompt Prompt canonical SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.validator_ref IS 'Prompt 候选 Validator 精确版本引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.validator_digest IS '候选 Validator canonical SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.exact_set_validator_ref IS '局部引用、Slot 归属与依赖 Exact-set Validator 引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.exact_set_validator_digest IS '依赖 Exact-set Validator canonical SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.router_model_route_ref IS '本地 Fake Router Model Route 引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.router_model_route_digest IS 'Router Model Route canonical SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.prompt_model_route_ref IS '本地 Graph Fake Prompt Model Route 引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.prompt_model_route_digest IS 'Graph Prompt Model Route canonical SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.runtime_policy_ref IS 'receipt-first、ReturnDirectly 与 Unknown Recovery Policy 引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.runtime_policy_digest IS 'Runtime Policy canonical SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.budget_ref IS 'Router、Tool、Graph Model 与 Business 同键重发硬预算引用';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.budget_digest IS '硬预算 canonical SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.context_digest IS '上述具名字段 canonical 编码的整体 SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_turn_context.created_at IS 'Context 与 Input、Run、open Tool Receipt 同事务创建的数据库 UTC 时间';

CREATE TABLE agent.write_prompts_preview_model_receipt (
    model_call_id uuid NOT NULL,
    call_kind varchar(24) NOT NULL,
    run_id uuid NOT NULL,
    turn_id uuid NOT NULL,
    input_id uuid NOT NULL,
    request_digest char(64) NOT NULL,
    execution_fence bigint NOT NULL,
    status varchar(16) NOT NULL,
    response_ciphertext bytea NULL,
    response_key_version varchar(64) NULL,
    response_digest char(64) NULL,
    error_code varchar(64) NULL,
    created_at timestamptz NOT NULL,
    completed_at timestamptz NULL,
    CONSTRAINT pk_write_prompts_preview_model_receipt PRIMARY KEY (model_call_id),
    CONSTRAINT uq_write_prompts_preview_model_receipt__run_kind UNIQUE (run_id, call_kind),
    CONSTRAINT ck_write_prompts_preview_model_receipt__kind CHECK (call_kind IN ('router', 'graph_prompt')),
    CONSTRAINT ck_write_prompts_preview_model_receipt__request_digest CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_write_prompts_preview_model_receipt__execution_fence CHECK (execution_fence > 0),
    CONSTRAINT ck_write_prompts_preview_model_receipt__status CHECK (status IN ('reserved', 'completed', 'failed')),
    CONSTRAINT ck_write_prompts_preview_model_receipt__payload CHECK (
        (status = 'reserved' AND response_ciphertext IS NULL AND response_key_version IS NULL AND response_digest IS NULL AND error_code IS NULL AND completed_at IS NULL)
        OR (status = 'completed' AND octet_length(response_ciphertext) > 0 AND response_key_version IS NOT NULL AND response_digest ~ '^[0-9a-f]{64}$' AND error_code IS NULL AND completed_at IS NOT NULL)
        OR (status = 'failed' AND response_ciphertext IS NULL AND response_key_version IS NULL AND response_digest IS NULL AND length(error_code) > 0 AND completed_at IS NOT NULL)
    )
);

COMMENT ON TABLE agent.write_prompts_preview_model_receipt IS 'Router 与 Graph Prompt Fake Model 分层 first-write-wins 调用回执';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.model_call_id IS '入队时预分配的稳定 Model Call UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.call_kind IS '模型调用层：router 或 graph_prompt';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.run_id IS '关联稳定 Run 的逻辑标识';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.turn_id IS '关联稳定 Turn 的逻辑标识';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.input_id IS '关联严格 HOL Input 的逻辑标识';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.request_digest IS 'call kind、Context、Route 与 canonical Messages 的 SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.execution_fence IS '最近一次取得 reserved Fake Model 执行权的 Session Fence';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.status IS '模型回执状态：reserved、completed 或 failed';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.response_ciphertext IS 'completed 时完整 classic Message 的 DRAE v1 AEAD 密文';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.response_key_version IS 'completed 响应密文使用的 Agent 内容密钥版本';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.response_digest IS 'completed 响应规范明文的 SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.error_code IS 'failed 时冻结的稳定脱敏错误码';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.created_at IS '模型回执首次 reserve 的数据库 UTC 时间';
COMMENT ON COLUMN agent.write_prompts_preview_model_receipt.completed_at IS '模型回执首次冻结 completed 或 failed 的数据库 UTC 时间';

CREATE TABLE agent.write_prompts_preview_tool_receipt (
    tool_call_id uuid NOT NULL,
    run_id uuid NOT NULL,
    turn_id uuid NOT NULL,
    input_id uuid NOT NULL,
    business_command_id uuid NOT NULL,
    request_digest char(64) NOT NULL,
    execution_fence bigint NOT NULL DEFAULT 0,
    status varchar(24) NOT NULL,
    command_ciphertext bytea NULL,
    command_key_version varchar(64) NULL,
    command_digest char(64) NULL,
    expected_project_version bigint NULL,
    business_request_digest char(64) NULL,
    content_digest char(64) NULL,
    resend_attempts integer NOT NULL DEFAULT 0,
    resend_limit integer NOT NULL DEFAULT 0,
    result_ciphertext bytea NULL,
    result_key_version varchar(64) NULL,
    result_digest char(64) NULL,
    result_code varchar(64) NULL,
    created_at timestamptz NOT NULL,
    prepared_at timestamptz NULL,
    unknown_at timestamptz NULL,
    completed_at timestamptz NULL,
    CONSTRAINT pk_write_prompts_preview_tool_receipt PRIMARY KEY (tool_call_id),
    CONSTRAINT uq_write_prompts_preview_tool_receipt__run UNIQUE (run_id),
    CONSTRAINT uq_write_prompts_preview_tool_receipt__business_command UNIQUE (business_command_id),
    CONSTRAINT ck_write_prompts_preview_tool_receipt__request_digest CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_write_prompts_preview_tool_receipt__execution_fence CHECK (execution_fence >= 0),
    CONSTRAINT ck_write_prompts_preview_tool_receipt__status CHECK (status IN ('open', 'business_prepared', 'business_unknown', 'completed', 'failed')),
    CONSTRAINT ck_write_prompts_preview_tool_receipt__resend CHECK (
        resend_attempts >= 0 AND resend_limit >= 0 AND resend_attempts <= resend_limit
    ),
    CONSTRAINT ck_write_prompts_preview_tool_receipt__payload CHECK (
        (status = 'open'
            AND execution_fence >= 0
            AND command_ciphertext IS NULL AND command_key_version IS NULL AND command_digest IS NULL AND expected_project_version IS NULL
            AND business_request_digest IS NULL AND content_digest IS NULL
            AND resend_attempts = 0 AND resend_limit = 0
            AND result_ciphertext IS NULL AND result_key_version IS NULL AND result_digest IS NULL AND result_code IS NULL
            AND prepared_at IS NULL AND unknown_at IS NULL AND completed_at IS NULL)
        OR (status = 'business_prepared'
            AND execution_fence > 0
            AND octet_length(command_ciphertext) > 0 AND command_key_version IS NOT NULL
            AND command_digest ~ '^[0-9a-f]{64}$' AND expected_project_version > 0
            AND business_request_digest ~ '^[0-9a-f]{64}$'
            AND content_digest ~ '^[0-9a-f]{64}$' AND resend_attempts = 0
            AND result_ciphertext IS NULL AND result_key_version IS NULL AND result_digest IS NULL AND result_code IS NULL
            AND prepared_at IS NOT NULL AND unknown_at IS NULL AND completed_at IS NULL)
        OR (status = 'business_unknown'
            AND execution_fence > 0
            AND octet_length(command_ciphertext) > 0 AND command_key_version IS NOT NULL
            AND command_digest ~ '^[0-9a-f]{64}$' AND expected_project_version > 0
            AND business_request_digest ~ '^[0-9a-f]{64}$'
            AND content_digest ~ '^[0-9a-f]{64}$'
            AND result_ciphertext IS NULL AND result_key_version IS NULL AND result_digest IS NULL AND result_code IS NULL
            AND prepared_at IS NOT NULL AND unknown_at IS NOT NULL AND completed_at IS NULL)
        OR (status IN ('completed', 'failed')
            AND execution_fence > 0
            AND octet_length(result_ciphertext) > 0 AND result_key_version IS NOT NULL
            AND result_digest ~ '^[0-9a-f]{64}$' AND length(result_code) > 0 AND completed_at IS NOT NULL
            AND (
                (command_ciphertext IS NULL AND command_key_version IS NULL AND command_digest IS NULL AND expected_project_version IS NULL
                    AND business_request_digest IS NULL AND content_digest IS NULL
                    AND resend_attempts = 0 AND resend_limit = 0 AND prepared_at IS NULL AND unknown_at IS NULL)
                OR
                (octet_length(command_ciphertext) > 0 AND command_key_version IS NOT NULL
                    AND command_digest ~ '^[0-9a-f]{64}$' AND expected_project_version > 0
                    AND business_request_digest ~ '^[0-9a-f]{64}$'
                    AND content_digest ~ '^[0-9a-f]{64}$' AND prepared_at IS NOT NULL)
            ))
    )
);

COMMENT ON TABLE agent.write_prompts_preview_tool_receipt IS 'Write Prompts Tool Result 与 Business Save Unknown Outcome 的 receipt-first first-write-wins 回执';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.tool_call_id IS '入队时预分配且 Router 必须原样使用的稳定 Tool Call UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.run_id IS '关联稳定 Run 的逻辑标识';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.turn_id IS '关联稳定 Turn 的逻辑标识';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.input_id IS '关联严格 HOL Input 的逻辑标识';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.business_command_id IS 'Save、Query 与 Unknown Outcome 同键重发复用的 Business 命令 UUIDv7';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.request_digest IS 'Context、Tool Definition、Schema pins 与 canonical Intent 的外层 SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.execution_fence IS '当前 open、prepared 或 unknown Tool 执行权的 Session Fence，入队占位时为零';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.status IS 'Tool 回执状态：open、business_prepared、business_unknown、completed 或 failed';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.command_ciphertext IS 'Save RPC 前冻结的完整 Business Draft Command DRAE v1 AEAD 密文';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.command_key_version IS 'prepared Command 密文使用的 Agent 内容密钥版本';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.command_digest IS 'prepared Command canonical 明文 SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.expected_project_version IS 'prepared Save Command 保存时复验的 Business Project 乐观锁版本';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.business_request_digest IS 'Agent 与 Business 共同冻结的 Save 请求 SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.content_digest IS 'prepared Prompt Draft 完整内容 SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.resend_attempts IS 'Unknown Outcome 后已经持久化预留的同键重发次数';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.resend_limit IS '首次 prepared 时冻结且不可提高的同键重发上限';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.result_ciphertext IS 'completed 或 failed 完整严格 Tool Result 的 DRAE v1 AEAD 密文';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.result_key_version IS 'terminal Tool Result 密文使用的 Agent 内容密钥版本';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.result_digest IS 'terminal Tool Result canonical 明文 SHA-256 摘要';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.result_code IS 'terminal Tool Result 经 Validator 冻结的稳定结果码';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.created_at IS 'open Receipt 与 Input、Context、Run 同事务创建的数据库 UTC 时间';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.prepared_at IS 'Business Save RPC 前完整 Command 首次冻结的数据库 UTC 时间';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.unknown_at IS 'Business Save Unknown Outcome 首次冻结的数据库 UTC 时间';
COMMENT ON COLUMN agent.write_prompts_preview_tool_receipt.completed_at IS 'Tool Receipt 首次冻结 completed 或 failed 的数据库 UTC 时间';

CREATE INDEX idx_write_prompts_preview_tool_receipt__recovery
    ON agent.write_prompts_preview_tool_receipt (status, resend_attempts, resend_limit, created_at, tool_call_id)
    WHERE status IN ('business_prepared', 'business_unknown');

CREATE FUNCTION agent.reject_write_prompts_context_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'write prompts turn context cannot be updated or deleted';
END;
$$;

CREATE TRIGGER trg_write_prompts_preview_turn_context__immutable
BEFORE UPDATE OR DELETE ON agent.write_prompts_preview_turn_context
FOR EACH ROW EXECUTE FUNCTION agent.reject_write_prompts_context_mutation();

CREATE FUNCTION agent.guard_write_prompts_model_receipt_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'write prompts model receipt cannot be deleted';
    END IF;
    IF NEW.model_call_id IS DISTINCT FROM OLD.model_call_id
       OR NEW.call_kind IS DISTINCT FROM OLD.call_kind
       OR NEW.run_id IS DISTINCT FROM OLD.run_id
       OR NEW.turn_id IS DISTINCT FROM OLD.turn_id
       OR NEW.input_id IS DISTINCT FROM OLD.input_id
       OR NEW.request_digest IS DISTINCT FROM OLD.request_digest
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'write prompts model receipt identity and request are immutable';
    END IF;
    IF OLD.status <> 'reserved' THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'frozen write prompts model receipt cannot be updated';
    END IF;
    IF NEW.status = 'reserved' THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'reserved write prompts model receipt cannot be re-reserved';
    ELSIF NEW.status IN ('completed', 'failed') THEN
        IF NEW.execution_fence IS DISTINCT FROM OLD.execution_fence THEN
            RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'terminal write prompts model receipt cannot change execution fence';
        END IF;
    ELSE
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'write prompts model receipt transition is invalid';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_write_prompts_preview_model_receipt__guard
BEFORE UPDATE OR DELETE ON agent.write_prompts_preview_model_receipt
FOR EACH ROW EXECUTE FUNCTION agent.guard_write_prompts_model_receipt_mutation();

CREATE FUNCTION agent.guard_write_prompts_tool_receipt_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'write prompts tool receipt cannot be deleted';
    END IF;
    IF NEW.tool_call_id IS DISTINCT FROM OLD.tool_call_id
       OR NEW.run_id IS DISTINCT FROM OLD.run_id
       OR NEW.turn_id IS DISTINCT FROM OLD.turn_id
       OR NEW.input_id IS DISTINCT FROM OLD.input_id
       OR NEW.business_command_id IS DISTINCT FROM OLD.business_command_id
       OR NEW.request_digest IS DISTINCT FROM OLD.request_digest
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'write prompts tool receipt identity and request are immutable';
    END IF;
    IF OLD.status IN ('completed', 'failed') THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'frozen write prompts tool receipt cannot be updated';
    END IF;
    IF OLD.command_digest IS NOT NULL AND (
        NEW.command_ciphertext IS DISTINCT FROM OLD.command_ciphertext
        OR NEW.command_key_version IS DISTINCT FROM OLD.command_key_version
        OR NEW.command_digest IS DISTINCT FROM OLD.command_digest
        OR NEW.expected_project_version IS DISTINCT FROM OLD.expected_project_version
        OR NEW.business_request_digest IS DISTINCT FROM OLD.business_request_digest
        OR NEW.content_digest IS DISTINCT FROM OLD.content_digest
        OR NEW.resend_limit IS DISTINCT FROM OLD.resend_limit
        OR NEW.prepared_at IS DISTINCT FROM OLD.prepared_at
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'prepared write prompts business command is immutable';
    END IF;
    IF OLD.unknown_at IS NOT NULL AND NEW.unknown_at IS DISTINCT FROM OLD.unknown_at THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'write prompts unknown outcome timestamp is immutable';
    END IF;
    IF OLD.status = 'open' THEN
        IF NEW.status = 'open' THEN
            IF NEW.execution_fence <= OLD.execution_fence THEN
                RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'open write prompts tool receipt fence must increase';
            END IF;
        ELSIF NEW.status IN ('business_prepared', 'completed', 'failed') THEN
            IF NEW.execution_fence IS DISTINCT FROM OLD.execution_fence OR NEW.execution_fence <= 0 THEN
                RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'write prompts tool transition must keep its positive execution fence';
            END IF;
        ELSE
            RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'open write prompts tool receipt transition is invalid';
        END IF;
    ELSIF OLD.status = 'business_prepared' THEN
        IF NEW.status = 'business_prepared' THEN
            IF NEW.execution_fence <= OLD.execution_fence THEN
                RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'prepared write prompts tool receipt fence must increase';
            END IF;
        ELSIF NEW.status IN ('business_unknown', 'completed', 'failed') THEN
            IF NEW.execution_fence IS DISTINCT FROM OLD.execution_fence THEN
                RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'prepared write prompts transition cannot change execution fence';
            END IF;
        ELSE
            RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'prepared write prompts tool receipt transition is invalid';
        END IF;
    ELSIF OLD.status = 'business_unknown' THEN
        IF NEW.status = 'business_unknown' THEN
            IF NEW.execution_fence > OLD.execution_fence THEN
                IF NEW.resend_attempts IS DISTINCT FROM OLD.resend_attempts THEN
                    RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'takeover cannot consume write prompts resend budget';
                END IF;
            ELSIF NEW.execution_fence IS NOT DISTINCT FROM OLD.execution_fence THEN
                IF NEW.resend_attempts <> OLD.resend_attempts + 1 OR NEW.resend_attempts > NEW.resend_limit THEN
                    RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'write prompts resend budget must advance exactly once';
                END IF;
            ELSE
                RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'unknown write prompts tool receipt fence cannot decrease';
            END IF;
        ELSIF NEW.status IN ('completed', 'failed') THEN
            IF NEW.execution_fence IS DISTINCT FROM OLD.execution_fence OR NEW.resend_attempts IS DISTINCT FROM OLD.resend_attempts THEN
                RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'terminal write prompts recovery must keep fence and resend count';
            END IF;
        ELSE
            RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'unknown write prompts tool receipt transition is invalid';
        END IF;
    ELSE
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'write prompts tool receipt prior state is invalid';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_write_prompts_preview_tool_receipt__guard
BEFORE UPDATE OR DELETE ON agent.write_prompts_preview_tool_receipt
FOR EACH ROW EXECUTE FUNCTION agent.guard_write_prompts_tool_receipt_mutation();
