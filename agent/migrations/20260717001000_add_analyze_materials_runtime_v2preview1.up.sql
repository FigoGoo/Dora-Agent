ALTER TABLE agent.session_input
    DROP CONSTRAINT ck_session_input__source_type;

ALTER TABLE agent.session_input
    ADD CONSTRAINT ck_session_input__source_type CHECK (
        source_type IN ('user_message', 'creation_spec_preview', 'analyze_materials_preview')
    );

COMMENT ON COLUMN agent.session_input.source_type IS '可信输入来源类型：普通用户消息、CreationSpec Preview 或 Analyze Materials Preview 结构化意图';

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
        'analyze_materials.preview.runtime_failed'
    ));

COMMENT ON COLUMN agent.session_event_log.event_type IS '事件类型：会话、输入、CreationSpec Preview、用户消息 Turn 或 Analyze Materials Preview 投影';

ALTER TABLE agent.session_event_log
    DROP CONSTRAINT ck_session_event_log__aggregate_type;

ALTER TABLE agent.session_event_log
    ADD CONSTRAINT ck_session_event_log__aggregate_type CHECK (
        aggregate_type IN ('session', 'session_input', 'creation_spec', 'session_turn', 'analyze_materials_preview')
    );

COMMENT ON COLUMN agent.session_event_log.aggregate_type IS '事件关联聚合类型：session、session_input、creation_spec、session_turn 或 analyze_materials_preview';

CREATE TABLE agent.analyze_materials_preview_run (
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
    router_model_call_id uuid NOT NULL,
    graph_model_call_id uuid NOT NULL,
    accepted_event_id uuid NOT NULL,
    terminal_event_id uuid NOT NULL,
    owner_fence bigint NOT NULL DEFAULT 0,
    status varchar(16) NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    started_at timestamptz NULL,
    completed_at timestamptz NULL,
    CONSTRAINT pk_analyze_materials_preview_run PRIMARY KEY (input_id),
    CONSTRAINT uq_analyze_materials_preview_run__request UNIQUE (session_id, request_id),
    CONSTRAINT uq_analyze_materials_preview_run__idempotency UNIQUE (session_id, idempotency_key),
    CONSTRAINT uq_analyze_materials_preview_run__turn UNIQUE (turn_id),
    CONSTRAINT uq_analyze_materials_preview_run__run UNIQUE (run_id),
    CONSTRAINT uq_analyze_materials_preview_run__tool_call UNIQUE (tool_call_id),
    CONSTRAINT uq_analyze_materials_preview_run__router_model UNIQUE (router_model_call_id),
    CONSTRAINT uq_analyze_materials_preview_run__graph_model UNIQUE (graph_model_call_id),
    CONSTRAINT uq_analyze_materials_preview_run__accepted_event UNIQUE (accepted_event_id),
    CONSTRAINT uq_analyze_materials_preview_run__terminal_event UNIQUE (terminal_event_id),
    CONSTRAINT ck_analyze_materials_preview_run__digest CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_analyze_materials_preview_run__fence CHECK (owner_fence >= 0),
    CONSTRAINT ck_analyze_materials_preview_run__status CHECK (status IN ('created', 'running', 'completed', 'failed')),
    CONSTRAINT ck_analyze_materials_preview_run__version CHECK (version > 0),
    CONSTRAINT ck_analyze_materials_preview_run__times CHECK (
        (status = 'created' AND started_at IS NULL AND completed_at IS NULL)
        OR (status = 'running' AND started_at IS NOT NULL AND completed_at IS NULL)
        OR (status IN ('completed', 'failed') AND started_at IS NOT NULL AND completed_at IS NOT NULL)
    )
);

COMMENT ON TABLE agent.analyze_materials_preview_run IS 'analyze_materials.runtime.v2preview1 的稳定执行身份、Fence 与 Run 状态真源';
COMMENT ON COLUMN agent.analyze_materials_preview_run.input_id IS '关联 Analyze Materials Preview Session Input 的逻辑标识，同时作为主键';
COMMENT ON COLUMN agent.analyze_materials_preview_run.request_id IS '首次入队 HTTP 请求 UUIDv7；Input 与 accepted/terminal Event 的 source_id 固定复用该值';
COMMENT ON COLUMN agent.analyze_materials_preview_run.idempotency_key IS 'HTTP Idempotency-Key UUIDv7，相同键只允许同义请求重放';
COMMENT ON COLUMN agent.analyze_materials_preview_run.request_digest IS '严格规范化 typed Intent 请求的 SHA-256 小写十六进制摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_run.session_id IS '关联 Agent Session 的逻辑标识，不设置物理外键';
COMMENT ON COLUMN agent.analyze_materials_preview_run.user_id IS '入队时冻结的可信 Business User 逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_run.project_id IS '入队时冻结的可信 Business Project 逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_run.turn_id IS '入队时预分配且技术重试复用的 Turn UUIDv7';
COMMENT ON COLUMN agent.analyze_materials_preview_run.run_id IS '入队时预分配且恢复重用的 Run UUIDv7';
COMMENT ON COLUMN agent.analyze_materials_preview_run.tool_call_id IS '入队时预分配且 Router 必须原样使用的 Tool Call UUIDv7';
COMMENT ON COLUMN agent.analyze_materials_preview_run.router_model_call_id IS '入队时预分配的本地 Router Model Call UUIDv7';
COMMENT ON COLUMN agent.analyze_materials_preview_run.graph_model_call_id IS '入队时预分配的 Graph 素材分析 Model Call UUIDv7';
COMMENT ON COLUMN agent.analyze_materials_preview_run.accepted_event_id IS '入队时预分配并与 typed accepted Event 绑定的 UUIDv7';
COMMENT ON COLUMN agent.analyze_materials_preview_run.terminal_event_id IS '入队时预分配且终态投影复用的 Event UUIDv7';
COMMENT ON COLUMN agent.analyze_materials_preview_run.owner_fence IS '最近取得执行权的 Session Lane Fence，未领取时为 0';
COMMENT ON COLUMN agent.analyze_materials_preview_run.status IS 'Run 状态：created、running、completed 或 failed';
COMMENT ON COLUMN agent.analyze_materials_preview_run.version IS 'Run 乐观锁版本，从 1 开始单调递增';
COMMENT ON COLUMN agent.analyze_materials_preview_run.created_at IS '稳定执行身份创建的数据库 UTC 时间';
COMMENT ON COLUMN agent.analyze_materials_preview_run.updated_at IS 'Run 最近状态变更的数据库 UTC 时间';
COMMENT ON COLUMN agent.analyze_materials_preview_run.started_at IS 'Run 首次由合法 Fence 开始的数据库 UTC 时间';
COMMENT ON COLUMN agent.analyze_materials_preview_run.completed_at IS 'Run 进入 completed 或 failed 的数据库 UTC 时间';

CREATE INDEX idx_analyze_materials_preview_run__session_status
    ON agent.analyze_materials_preview_run (session_id, status, created_at, input_id);

CREATE TABLE agent.analyze_materials_preview_turn_context (
    turn_id uuid NOT NULL,
    profile varchar(64) NOT NULL,
    schema_version varchar(64) NOT NULL,
    session_id uuid NOT NULL,
    input_id uuid NOT NULL,
    run_id uuid NOT NULL,
    tool_call_id uuid NOT NULL,
    router_model_call_id uuid NOT NULL,
    graph_model_call_id uuid NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    intent_ciphertext bytea NOT NULL,
    intent_key_version varchar(64) NOT NULL,
    intent_digest char(64) NOT NULL,
    access_scope_ref varchar(128) NOT NULL,
    access_scope_digest char(64) NOT NULL,
    tool_registry_ref varchar(128) NOT NULL,
    tool_registry_digest char(64) NOT NULL,
    tool_definition_ref varchar(128) NOT NULL,
    tool_definition_digest char(64) NOT NULL,
    intent_schema_ref varchar(128) NOT NULL,
    result_schema_ref varchar(128) NOT NULL,
    prompt_ref varchar(128) NOT NULL,
    prompt_digest char(64) NOT NULL,
    validator_ref varchar(128) NOT NULL,
    validator_digest char(64) NOT NULL,
    evidence_policy_ref varchar(128) NOT NULL,
    evidence_policy_digest char(64) NOT NULL,
    router_model_route_ref varchar(128) NOT NULL,
    router_model_route_digest char(64) NOT NULL,
    analysis_model_route_ref varchar(128) NOT NULL,
    analysis_model_route_digest char(64) NOT NULL,
    runtime_policy_ref varchar(128) NOT NULL,
    runtime_policy_digest char(64) NOT NULL,
    budget_ref varchar(128) NOT NULL,
    budget_digest char(64) NOT NULL,
    context_digest char(64) NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_analyze_materials_preview_turn_context PRIMARY KEY (turn_id),
    CONSTRAINT uq_analyze_materials_preview_turn_context__input UNIQUE (input_id),
    CONSTRAINT uq_analyze_materials_preview_turn_context__run UNIQUE (run_id),
    CONSTRAINT uq_analyze_materials_preview_turn_context__tool_call UNIQUE (tool_call_id),
    CONSTRAINT uq_analyze_materials_preview_turn_context__router_model UNIQUE (router_model_call_id),
    CONSTRAINT uq_analyze_materials_preview_turn_context__graph_model UNIQUE (graph_model_call_id),
    CONSTRAINT ck_analyze_materials_preview_turn_context__profile CHECK (profile = 'analyze_materials.runtime.v2preview1'),
    CONSTRAINT ck_analyze_materials_preview_turn_context__schema CHECK (schema_version = 'analyze_materials.turn_context.v2preview1'),
    CONSTRAINT ck_analyze_materials_preview_turn_context__intent_key CHECK (length(intent_key_version) BETWEEN 1 AND 64),
    CONSTRAINT ck_analyze_materials_preview_turn_context__digests CHECK (
        intent_digest ~ '^[0-9a-f]{64}$'
        AND access_scope_digest ~ '^[0-9a-f]{64}$'
        AND tool_registry_digest ~ '^[0-9a-f]{64}$'
        AND tool_definition_digest ~ '^[0-9a-f]{64}$'
        AND prompt_digest ~ '^[0-9a-f]{64}$'
        AND validator_digest ~ '^[0-9a-f]{64}$'
        AND evidence_policy_digest ~ '^[0-9a-f]{64}$'
        AND router_model_route_digest ~ '^[0-9a-f]{64}$'
        AND analysis_model_route_digest ~ '^[0-9a-f]{64}$'
        AND runtime_policy_digest ~ '^[0-9a-f]{64}$'
        AND budget_digest ~ '^[0-9a-f]{64}$'
        AND context_digest ~ '^[0-9a-f]{64}$'
    )
);

COMMENT ON TABLE agent.analyze_materials_preview_turn_context IS 'Analyze Materials Preview 入队事务冻结且 append-only 的最小可信 Turn Context';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.turn_id IS '稳定 Turn UUIDv7，同时作为 Context 主键';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.profile IS '独立本地 Profile，固定 analyze_materials.runtime.v2preview1';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.schema_version IS '最小上下文版本，固定 analyze_materials.turn_context.v2preview1';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.session_id IS '冻结的 Agent Session 逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.input_id IS '冻结的无 Message Session Input 逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.run_id IS '冻结的稳定 Run UUIDv7';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.tool_call_id IS '冻结的稳定 Analyze Materials Tool Call UUIDv7';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.router_model_call_id IS '冻结的本地 Router Model Call UUIDv7';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.graph_model_call_id IS '冻结的 Graph 素材分析 Model Call UUIDv7';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.user_id IS '冻结的可信 Business User 逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.project_id IS '冻结的可信 Business Project 逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.intent_ciphertext IS '完整严格 typed Intent 的 DRAE v1 AEAD Envelope';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.intent_key_version IS 'Intent 密文使用的 Agent 内容密钥版本';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.intent_digest IS 'Intent 规范明文的 SHA-256 小写十六进制摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.access_scope_ref IS '入队时冻结的可信 Evidence 访问范围引用';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.access_scope_digest IS 'Evidence 访问范围 canonical SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.tool_registry_ref IS '恰含 analyze_materials 的 Executable Tool Registry 引用';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.tool_registry_digest IS 'Executable Tool Registry canonical SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.tool_definition_ref IS '固定 analyze_materials.v2preview1 Tool Definition 引用';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.tool_definition_digest IS 'Tool Definition canonical SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.intent_schema_ref IS '固定 analyze_materials.preview.intent.v1 Schema 引用';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.result_schema_ref IS '固定 analyze_materials.preview.result.v1 Schema 引用';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.prompt_ref IS 'Graph 分析 Prompt 的精确版本引用';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.prompt_digest IS 'Graph 分析 Prompt canonical SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.validator_ref IS '严格 Result Validator 的精确版本引用';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.validator_digest IS 'Result Validator canonical SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.evidence_policy_ref IS 'text/image Evidence exact-set Policy 引用';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.evidence_policy_digest IS 'Evidence Policy canonical SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.router_model_route_ref IS '本地确定性 Fake Router Route 引用';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.router_model_route_digest IS 'Router Model Route canonical SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.analysis_model_route_ref IS '本地确定性 Graph Fake Model Route 引用';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.analysis_model_route_digest IS 'Graph Model Route canonical SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.runtime_policy_ref IS '独立本地 Runtime Policy 精确版本引用';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.runtime_policy_digest IS 'Runtime Policy canonical SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.budget_ref IS '本批 Tool/Model/时间硬预算精确版本引用';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.budget_digest IS '硬预算 canonical SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.context_digest IS '上述具名字段 canonical 编码的整体 SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_turn_context.created_at IS 'Context 与 Input、Run、open Tool Receipt 同事务创建的数据库 UTC 时间';

CREATE TABLE agent.analyze_materials_preview_model_receipt (
    model_call_id uuid NOT NULL,
    call_kind varchar(16) NOT NULL,
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
    CONSTRAINT pk_analyze_materials_preview_model_receipt PRIMARY KEY (model_call_id),
    CONSTRAINT uq_analyze_materials_preview_model_receipt__run_kind UNIQUE (run_id, call_kind),
    CONSTRAINT ck_analyze_materials_preview_model_receipt__kind CHECK (call_kind IN ('router', 'graph_analysis')),
    CONSTRAINT ck_analyze_materials_preview_model_receipt__request_digest CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_analyze_materials_preview_model_receipt__execution_fence CHECK (execution_fence > 0),
    CONSTRAINT ck_analyze_materials_preview_model_receipt__status CHECK (status IN ('reserved', 'completed', 'failed')),
    CONSTRAINT ck_analyze_materials_preview_model_receipt__payload CHECK (
        (status = 'reserved' AND response_ciphertext IS NULL AND response_key_version IS NULL AND response_digest IS NULL AND error_code IS NULL AND completed_at IS NULL)
        OR (status = 'completed' AND response_ciphertext IS NOT NULL AND response_key_version IS NOT NULL AND response_digest ~ '^[0-9a-f]{64}$' AND error_code IS NULL AND completed_at IS NOT NULL)
        OR (status = 'failed' AND response_ciphertext IS NULL AND response_key_version IS NULL AND response_digest IS NULL AND error_code IS NOT NULL AND completed_at IS NOT NULL)
    )
);

COMMENT ON TABLE agent.analyze_materials_preview_model_receipt IS 'Router 与 Graph Fake Model 分层 first-write-wins 调用回执';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.model_call_id IS '入队时预分配的稳定 Model Call UUIDv7';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.call_kind IS '模型调用层：router 或 graph_analysis';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.run_id IS '关联稳定 Run 的逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.turn_id IS '关联稳定 Turn 的逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.input_id IS '关联严格 HOL Input 的逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.request_digest IS 'call kind、Context、Route 与 canonical Messages 的 SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.execution_fence IS '最近一次取得 reserved 本地 Fake 执行权的 Session Fence';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.status IS '模型回执状态：reserved、completed 或 failed';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.response_ciphertext IS 'completed 时完整模型响应的 DRAE v1 AEAD 密文';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.response_key_version IS 'completed 响应密文使用的 Agent 内容密钥版本';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.response_digest IS 'completed 响应规范明文的 SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.error_code IS 'failed 时冻结的稳定脱敏错误码';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.created_at IS '模型回执首次 reserve 的数据库 UTC 时间';
COMMENT ON COLUMN agent.analyze_materials_preview_model_receipt.completed_at IS '模型回执首次冻结 completed 或 failed 的数据库 UTC 时间';

CREATE TABLE agent.analyze_materials_preview_tool_receipt (
    tool_call_id uuid NOT NULL,
    run_id uuid NOT NULL,
    turn_id uuid NOT NULL,
    input_id uuid NOT NULL,
    request_digest char(64) NOT NULL,
    execution_fence bigint NOT NULL DEFAULT 0,
    status varchar(16) NOT NULL,
    result_ciphertext bytea NULL,
    result_key_version varchar(64) NULL,
    result_digest char(64) NULL,
    result_code varchar(64) NULL,
    created_at timestamptz NOT NULL,
    completed_at timestamptz NULL,
    CONSTRAINT pk_analyze_materials_preview_tool_receipt PRIMARY KEY (tool_call_id),
    CONSTRAINT uq_analyze_materials_preview_tool_receipt__run UNIQUE (run_id),
    CONSTRAINT ck_analyze_materials_preview_tool_receipt__request_digest CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_analyze_materials_preview_tool_receipt__execution_fence CHECK (execution_fence >= 0),
    CONSTRAINT ck_analyze_materials_preview_tool_receipt__status CHECK (status IN ('open', 'completed', 'partial', 'failed')),
    CONSTRAINT ck_analyze_materials_preview_tool_receipt__payload CHECK (
        (status = 'open' AND result_ciphertext IS NULL AND result_key_version IS NULL AND result_digest IS NULL AND result_code IS NULL AND completed_at IS NULL)
        OR (status IN ('completed', 'partial', 'failed') AND execution_fence > 0 AND result_ciphertext IS NOT NULL AND result_key_version IS NOT NULL AND result_digest ~ '^[0-9a-f]{64}$' AND result_code IS NOT NULL AND completed_at IS NOT NULL)
    )
);

COMMENT ON TABLE agent.analyze_materials_preview_tool_receipt IS 'Analyze Materials Tool 完整严格 Result 的 receipt-first first-write-wins 回执';
COMMENT ON COLUMN agent.analyze_materials_preview_tool_receipt.tool_call_id IS '入队时预分配且 Router 必须原样使用的稳定 Tool Call UUIDv7';
COMMENT ON COLUMN agent.analyze_materials_preview_tool_receipt.run_id IS '关联稳定 Run 的逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_tool_receipt.turn_id IS '关联稳定 Turn 的逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_tool_receipt.input_id IS '关联严格 HOL Input 的逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_tool_receipt.request_digest IS 'Context、Tool Definition/Schema pins 与 canonical Intent 的 SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_tool_receipt.execution_fence IS '取得 open Tool 执行权的 Session Fence，入队占位时为 0';
COMMENT ON COLUMN agent.analyze_materials_preview_tool_receipt.status IS 'Tool 回执状态：open、completed、partial 或 failed';
COMMENT ON COLUMN agent.analyze_materials_preview_tool_receipt.result_ciphertext IS '终态完整严格 Tool Result 的 DRAE v1 AEAD 密文';
COMMENT ON COLUMN agent.analyze_materials_preview_tool_receipt.result_key_version IS 'Tool Result 密文使用的 Agent 内容密钥版本';
COMMENT ON COLUMN agent.analyze_materials_preview_tool_receipt.result_digest IS 'Tool Result 规范明文的 SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_tool_receipt.result_code IS 'Tool Result 内经 Validator 冻结的稳定结果码';
COMMENT ON COLUMN agent.analyze_materials_preview_tool_receipt.created_at IS 'open Tool Receipt 与 Input、Context、Run 同事务创建的数据库 UTC 时间';
COMMENT ON COLUMN agent.analyze_materials_preview_tool_receipt.completed_at IS 'Tool Receipt 首次冻结终态的数据库 UTC 时间';

CREATE TABLE agent.analyze_materials_preview_projection (
    source_input_id uuid NOT NULL,
    session_id uuid NOT NULL,
    source_enqueue_seq bigint NOT NULL,
    turn_id uuid NOT NULL,
    run_id uuid NOT NULL,
    tool_call_id uuid NOT NULL,
    schema_version varchar(64) NOT NULL,
    outcome_kind varchar(24) NOT NULL,
    status varchar(16) NOT NULL,
    result_digest char(64) NOT NULL,
    payload jsonb NOT NULL,
    projection_version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_analyze_materials_preview_projection PRIMARY KEY (source_input_id),
    CONSTRAINT uq_analyze_materials_preview_projection__turn UNIQUE (turn_id),
    CONSTRAINT uq_analyze_materials_preview_projection__run UNIQUE (run_id),
    CONSTRAINT uq_analyze_materials_preview_projection__tool_call UNIQUE (tool_call_id),
    CONSTRAINT ck_analyze_materials_preview_projection__source_seq CHECK (source_enqueue_seq > 0),
    CONSTRAINT ck_analyze_materials_preview_projection__schema CHECK (schema_version = 'analyze_materials.preview.card.v1'),
    CONSTRAINT ck_analyze_materials_preview_projection__outcome CHECK (outcome_kind IN ('tool_completed', 'tool_partial', 'tool_failed', 'runtime_failed')),
    CONSTRAINT ck_analyze_materials_preview_projection__status CHECK (status IN ('completed', 'partial', 'failed')),
    CONSTRAINT ck_analyze_materials_preview_projection__union CHECK (
        (outcome_kind = 'tool_completed' AND status = 'completed')
        OR (outcome_kind = 'tool_partial' AND status = 'partial')
        OR (outcome_kind IN ('tool_failed', 'runtime_failed') AND status = 'failed')
    ),
    CONSTRAINT ck_analyze_materials_preview_projection__digest CHECK (result_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_analyze_materials_preview_projection__payload CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT ck_analyze_materials_preview_projection__version CHECK (projection_version > 0)
);

COMMENT ON TABLE agent.analyze_materials_preview_projection IS 'Analyze Materials Preview 每个 Input 的 append-only 安全 Card 投影';
COMMENT ON COLUMN agent.analyze_materials_preview_projection.source_input_id IS '产生投影的严格 HOL Input UUIDv7，同时作为投影主键';
COMMENT ON COLUMN agent.analyze_materials_preview_projection.session_id IS '关联 Agent Session 的逻辑标识，用于按最新入队序号读取 Snapshot';
COMMENT ON COLUMN agent.analyze_materials_preview_projection.source_enqueue_seq IS '产生投影的 Session Input 入队序号，用于稳定选择最新卡片';
COMMENT ON COLUMN agent.analyze_materials_preview_projection.turn_id IS '关联稳定 Turn 的逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_projection.run_id IS '关联稳定 Run 的逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_projection.tool_call_id IS '关联稳定 Tool Call 的逻辑标识';
COMMENT ON COLUMN agent.analyze_materials_preview_projection.schema_version IS '安全 Card 版本，固定 analyze_materials.preview.card.v1';
COMMENT ON COLUMN agent.analyze_materials_preview_projection.outcome_kind IS '严格区分 tool_completed、tool_partial、tool_failed 与 runtime_failed';
COMMENT ON COLUMN agent.analyze_materials_preview_projection.status IS '浏览器安全状态：completed、partial 或 failed';
COMMENT ON COLUMN agent.analyze_materials_preview_projection.result_digest IS '冻结 Tool Result 或 Runtime Failure Card 的 SHA-256 摘要';
COMMENT ON COLUMN agent.analyze_materials_preview_projection.payload IS '严格版本化安全 Card JSON，不含 Intent、Evidence 正文、模型原文或内部错误';
COMMENT ON COLUMN agent.analyze_materials_preview_projection.projection_version IS 'Input 投影聚合版本，本 Profile 首次投影固定为 1';
COMMENT ON COLUMN agent.analyze_materials_preview_projection.created_at IS '投影与终态 Event、Run/Input 收尾同事务提交的数据库 UTC 时间';

CREATE INDEX idx_analyze_materials_preview_projection__session_latest
    ON agent.analyze_materials_preview_projection (session_id, source_enqueue_seq DESC, source_input_id DESC);

CREATE FUNCTION agent.reject_analyze_materials_context_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'analyze materials turn context cannot be updated or deleted';
END;
$$;

CREATE TRIGGER trg_analyze_materials_preview_turn_context__immutable
BEFORE UPDATE OR DELETE ON agent.analyze_materials_preview_turn_context
FOR EACH ROW EXECUTE FUNCTION agent.reject_analyze_materials_context_mutation();

CREATE FUNCTION agent.guard_analyze_materials_model_receipt_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'analyze materials model receipt cannot be deleted';
    END IF;
    IF NEW.model_call_id IS DISTINCT FROM OLD.model_call_id
       OR NEW.call_kind IS DISTINCT FROM OLD.call_kind
       OR NEW.run_id IS DISTINCT FROM OLD.run_id
       OR NEW.turn_id IS DISTINCT FROM OLD.turn_id
       OR NEW.input_id IS DISTINCT FROM OLD.input_id
       OR NEW.request_digest IS DISTINCT FROM OLD.request_digest
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'analyze materials model receipt identity and request are immutable';
    END IF;
    IF OLD.status <> 'reserved' THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'frozen analyze materials model receipt cannot be updated';
    END IF;
    IF NEW.status = 'reserved' THEN
        IF NEW.execution_fence <= OLD.execution_fence THEN
            RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'analyze materials model receipt execution fence must increase';
        END IF;
    ELSIF NEW.status IN ('completed', 'failed') THEN
        IF NEW.execution_fence IS DISTINCT FROM OLD.execution_fence THEN
            RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'terminal analyze materials model receipt cannot change execution fence';
        END IF;
    ELSE
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'analyze materials model receipt transition is invalid';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_analyze_materials_preview_model_receipt__guard
BEFORE UPDATE OR DELETE ON agent.analyze_materials_preview_model_receipt
FOR EACH ROW EXECUTE FUNCTION agent.guard_analyze_materials_model_receipt_mutation();

CREATE FUNCTION agent.guard_analyze_materials_tool_receipt_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'analyze materials tool receipt cannot be deleted';
    END IF;
    IF NEW.tool_call_id IS DISTINCT FROM OLD.tool_call_id
       OR NEW.run_id IS DISTINCT FROM OLD.run_id
       OR NEW.turn_id IS DISTINCT FROM OLD.turn_id
       OR NEW.input_id IS DISTINCT FROM OLD.input_id
       OR NEW.request_digest IS DISTINCT FROM OLD.request_digest
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'analyze materials tool receipt identity and request are immutable';
    END IF;
    IF OLD.status <> 'open' THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'frozen analyze materials tool receipt cannot be updated';
    END IF;
    IF NEW.status = 'open' THEN
        IF NEW.execution_fence <= OLD.execution_fence THEN
            RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'analyze materials tool receipt execution fence must increase';
        END IF;
    ELSIF NEW.status IN ('completed', 'partial', 'failed') THEN
        IF NEW.execution_fence IS DISTINCT FROM OLD.execution_fence OR NEW.execution_fence <= 0 THEN
            RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'terminal analyze materials tool receipt must keep its positive execution fence';
        END IF;
    ELSE
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'analyze materials tool receipt transition is invalid';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_analyze_materials_preview_tool_receipt__guard
BEFORE UPDATE OR DELETE ON agent.analyze_materials_preview_tool_receipt
FOR EACH ROW EXECUTE FUNCTION agent.guard_analyze_materials_tool_receipt_mutation();

CREATE FUNCTION agent.reject_analyze_materials_projection_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'completed analyze materials projection cannot be updated or deleted';
END;
$$;

CREATE TRIGGER trg_analyze_materials_preview_projection__immutable
BEFORE UPDATE OR DELETE ON agent.analyze_materials_preview_projection
FOR EACH ROW EXECUTE FUNCTION agent.reject_analyze_materials_projection_mutation();
