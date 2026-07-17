ALTER TABLE agent.session_message
    DROP CONSTRAINT ck_session_message__source_kind;

ALTER TABLE agent.session_message
    ADD CONSTRAINT ck_session_message__source_kind CHECK (
        source_kind IN ('ensure_project_session', 'creation_spec_preview')
    );

COMMENT ON COLUMN agent.session_message.source_kind IS '稳定消息来源类型：建会话命令或 CreationSpec Preview 入队命令';

ALTER TABLE agent.session_input
    DROP CONSTRAINT ck_session_input__source_type;

ALTER TABLE agent.session_input
    ADD CONSTRAINT ck_session_input__source_type CHECK (
        source_type IN ('user_message', 'creation_spec_preview')
    );

COMMENT ON COLUMN agent.session_input.source_type IS '可信输入来源类型：普通用户消息或 CreationSpec Preview 结构化意图';

ALTER TABLE agent.session_input
    DROP CONSTRAINT ck_session_input__status;

ALTER TABLE agent.session_input
    ADD CONSTRAINT ck_session_input__status CHECK (status IN (
        'pending',
        'claimed',
        'running',
        'retry_wait',
        'recovery_pending',
        'resolved',
        'dead'
    ));

COMMENT ON COLUMN agent.session_input.status IS '输入状态：pending、claimed、running、retry_wait、recovery_pending、resolved 或 dead';

DROP INDEX agent.idx_session_input__claim;

CREATE INDEX idx_session_input__claim
    ON agent.session_input (status, available_at, session_id, enqueue_seq)
    WHERE status IN ('pending', 'retry_wait', 'recovery_pending', 'claimed', 'running');

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

ALTER TABLE agent.session_event_log
    DROP CONSTRAINT ck_session_event_log__schema_version;

ALTER TABLE agent.session_event_log
    ADD CONSTRAINT ck_session_event_log__schema_version CHECK (
        schema_version IN ('session.event.v1')
    );

COMMENT ON COLUMN agent.session_event_log.schema_version IS '前端投影载荷版本，V1 固定为 session.event.v1；Preview Card 在载荷内部自描述版本';

ALTER TABLE agent.session_event_log
    DROP CONSTRAINT ck_session_event_log__aggregate_type;

ALTER TABLE agent.session_event_log
    ADD CONSTRAINT ck_session_event_log__aggregate_type CHECK (
        aggregate_type IN ('session', 'session_input', 'creation_spec')
    );

COMMENT ON COLUMN agent.session_event_log.aggregate_type IS '事件关联聚合类型：session、session_input 或 Business creation_spec';

CREATE TABLE agent.creation_spec_preview_run (
    input_id uuid NOT NULL,
    request_id uuid NOT NULL,
    idempotency_key uuid NOT NULL,
    request_digest char(64) NOT NULL,
    session_id uuid NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    message_id uuid NOT NULL,
    turn_id uuid NOT NULL,
    run_id uuid NOT NULL,
    tool_call_id uuid NOT NULL,
    business_command_id uuid NOT NULL,
    terminal_event_id uuid NOT NULL,
    prompt_version varchar(64) NOT NULL,
    validator_version varchar(64) NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_creation_spec_preview_run PRIMARY KEY (input_id),
    CONSTRAINT uq_creation_spec_preview_run__idempotency UNIQUE (idempotency_key),
    CONSTRAINT uq_creation_spec_preview_run__message UNIQUE (message_id),
    CONSTRAINT uq_creation_spec_preview_run__turn UNIQUE (turn_id),
    CONSTRAINT uq_creation_spec_preview_run__run UNIQUE (run_id),
    CONSTRAINT uq_creation_spec_preview_run__tool_call UNIQUE (tool_call_id),
    CONSTRAINT uq_creation_spec_preview_run__business_command UNIQUE (business_command_id),
    CONSTRAINT uq_creation_spec_preview_run__terminal_event UNIQUE (terminal_event_id),
    CONSTRAINT ck_creation_spec_preview_run__digest CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_creation_spec_preview_run__prompt_version CHECK (length(prompt_version) BETWEEN 1 AND 64),
    CONSTRAINT ck_creation_spec_preview_run__validator_version CHECK (length(validator_version) BETWEEN 1 AND 64)
);

COMMENT ON TABLE agent.creation_spec_preview_run IS '创作规格预览输入的稳定执行身份表，保存入队时冻结的 Turn、Run、Tool Call 与 Business Command 标识';
COMMENT ON COLUMN agent.creation_spec_preview_run.input_id IS '关联 Session Input 的逻辑标识，同时作为本表主键，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.creation_spec_preview_run.request_id IS '内部身份断言携带的请求 UUIDv7，仅用于关联安全回执和事件';
COMMENT ON COLUMN agent.creation_spec_preview_run.idempotency_key IS 'HTTP Idempotency-Key UUIDv7；相同键只允许同义请求重放';
COMMENT ON COLUMN agent.creation_spec_preview_run.request_digest IS '严格规范化预览 Intent 的 SHA-256 小写十六进制摘要';
COMMENT ON COLUMN agent.creation_spec_preview_run.session_id IS '关联 Agent Session 的逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.creation_spec_preview_run.user_id IS '入队时冻结的 Business User 逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.creation_spec_preview_run.project_id IS '入队时冻结的 Business Project 逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.creation_spec_preview_run.message_id IS '保存预览 Intent 密文的 Message 逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.creation_spec_preview_run.turn_id IS '入队时冻结且技术重试复用的 Runner Turn UUIDv7';
COMMENT ON COLUMN agent.creation_spec_preview_run.run_id IS '入队时冻结且技术重试复用的 Agent Run UUIDv7';
COMMENT ON COLUMN agent.creation_spec_preview_run.tool_call_id IS '入队时冻结且技术重试复用的 Graph Tool Call UUIDv7';
COMMENT ON COLUMN agent.creation_spec_preview_run.business_command_id IS '入队时冻结且 Unknown Outcome 查询复用的 Business Command UUIDv7';
COMMENT ON COLUMN agent.creation_spec_preview_run.terminal_event_id IS '接受输入前预分配的终态 Event UUIDv7，投影恢复必须复用';
COMMENT ON COLUMN agent.creation_spec_preview_run.prompt_version IS '冻结的预览 Prompt 版本';
COMMENT ON COLUMN agent.creation_spec_preview_run.validator_version IS '冻结的独立确定性 Validator 版本';
COMMENT ON COLUMN agent.creation_spec_preview_run.created_at IS '稳定执行身份创建 UTC 时间';
COMMENT ON COLUMN agent.creation_spec_preview_run.updated_at IS '稳定执行身份最近更新 UTC 时间';

CREATE INDEX idx_creation_spec_preview_run__session_created
    ON agent.creation_spec_preview_run (session_id, created_at, input_id);

CREATE TABLE agent.creation_spec_preview_model_receipt (
    tool_call_id uuid NOT NULL,
    call_index integer NOT NULL,
    request_digest char(64) NOT NULL,
    status varchar(16) NOT NULL,
    response_ciphertext bytea NULL,
    response_key_version varchar(64) NULL,
    response_digest char(64) NULL,
    created_at timestamptz NOT NULL,
    completed_at timestamptz NULL,
    CONSTRAINT pk_creation_spec_preview_model_receipt PRIMARY KEY (tool_call_id, call_index),
    CONSTRAINT ck_creation_spec_preview_model_receipt__call_index CHECK (call_index > 0),
    CONSTRAINT ck_creation_spec_preview_model_receipt__request_digest CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_creation_spec_preview_model_receipt__status CHECK (status IN ('pending', 'completed')),
    CONSTRAINT ck_creation_spec_preview_model_receipt__response CHECK (
        (status = 'pending' AND response_ciphertext IS NULL AND response_key_version IS NULL AND response_digest IS NULL AND completed_at IS NULL)
        OR
        (status = 'completed' AND response_ciphertext IS NOT NULL AND response_key_version IS NOT NULL AND response_digest ~ '^[0-9a-f]{64}$' AND completed_at IS NOT NULL)
    )
);

COMMENT ON TABLE agent.creation_spec_preview_model_receipt IS '预览 Graph 模型调用 first-write-wins 回执，完成响应仅以 Agent 内容密钥密文保存';
COMMENT ON COLUMN agent.creation_spec_preview_model_receipt.tool_call_id IS '关联稳定 Graph Tool Call UUIDv7，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.creation_spec_preview_model_receipt.call_index IS 'Tool Call 内模型调用稳定序号，从 1 开始';
COMMENT ON COLUMN agent.creation_spec_preview_model_receipt.request_digest IS '模型输入规范化摘要，同键异义必须冲突';
COMMENT ON COLUMN agent.creation_spec_preview_model_receipt.status IS '回执状态：pending 或 completed';
COMMENT ON COLUMN agent.creation_spec_preview_model_receipt.response_ciphertext IS '模型响应的 DRAE v1 AEAD Envelope，pending 时为空';
COMMENT ON COLUMN agent.creation_spec_preview_model_receipt.response_key_version IS '模型响应内容密钥版本，pending 时为空';
COMMENT ON COLUMN agent.creation_spec_preview_model_receipt.response_digest IS '模型响应明文 SHA-256 摘要，pending 时为空';
COMMENT ON COLUMN agent.creation_spec_preview_model_receipt.created_at IS '模型回执首次占位 UTC 时间';
COMMENT ON COLUMN agent.creation_spec_preview_model_receipt.completed_at IS '模型响应冻结 UTC 时间，pending 时为空';

CREATE TABLE agent.creation_spec_preview_tool_receipt (
    tool_call_id uuid NOT NULL,
    request_digest char(64) NOT NULL,
    stage varchar(24) NOT NULL,
    business_command_id uuid NOT NULL,
    business_request_digest char(64) NULL,
	business_content_digest char(64) NULL,
    result_ciphertext bytea NULL,
    result_key_version varchar(64) NULL,
    result_digest char(64) NULL,
    error_code varchar(64) NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_creation_spec_preview_tool_receipt PRIMARY KEY (tool_call_id),
    CONSTRAINT ck_creation_spec_preview_tool_receipt__request_digest CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_creation_spec_preview_tool_receipt__stage CHECK (stage IN ('pending', 'business_prepared', 'business_unknown', 'completed', 'failed')),
    CONSTRAINT ck_creation_spec_preview_tool_receipt__business_digest CHECK (
		(business_request_digest IS NULL AND business_content_digest IS NULL)
		OR (business_request_digest ~ '^[0-9a-f]{64}$' AND business_content_digest ~ '^[0-9a-f]{64}$')
    ),
    CONSTRAINT ck_creation_spec_preview_tool_receipt__result CHECK (
		(stage = 'completed' AND business_request_digest IS NOT NULL AND business_content_digest IS NOT NULL AND result_ciphertext IS NOT NULL AND result_key_version IS NOT NULL AND result_digest ~ '^[0-9a-f]{64}$' AND error_code IS NULL)
        OR
        (stage = 'failed' AND result_ciphertext IS NOT NULL AND result_key_version IS NOT NULL AND result_digest ~ '^[0-9a-f]{64}$' AND error_code IS NOT NULL)
        OR
		(stage = 'pending' AND business_request_digest IS NULL AND business_content_digest IS NULL AND result_ciphertext IS NULL AND result_key_version IS NULL AND result_digest IS NULL AND error_code IS NULL)
		OR
		(stage IN ('business_prepared', 'business_unknown') AND business_request_digest IS NOT NULL AND business_content_digest IS NOT NULL AND result_ciphertext IS NULL AND result_key_version IS NULL AND result_digest IS NULL AND error_code IS NULL)
    )
);

COMMENT ON TABLE agent.creation_spec_preview_tool_receipt IS '预览 Graph Tool first-write-wins 回执，先冻结业务命令阶段，再冻结最终结果或稳定失败';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.tool_call_id IS '稳定 Tool Call UUIDv7，同时作为回执主键';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.request_digest IS 'Tool 参数规范化摘要，同键异义必须冲突';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.stage IS '执行阶段：pending、business_prepared、business_unknown、completed 或 failed';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.business_command_id IS 'Save 与 Query 复用的 Business Command UUIDv7';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.business_request_digest IS 'Save 请求语义摘要，首次构造后冻结';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.business_content_digest IS 'Save 内容摘要，恢复 Query completed 时无需重跑模型即可交叉校验资源';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.result_ciphertext IS 'completed 或确定 failed 的完整 Tool Result DRAE v1 AEAD Envelope，开放阶段为空';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.result_key_version IS '终态 Tool Result 的内容密钥版本，开放阶段为空';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.result_digest IS '终态 Tool Result 明文 SHA-256 摘要，开放阶段为空';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.error_code IS '稳定失败码，仅 failed 时存在且不包含 Provider 原文';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.created_at IS 'Tool 回执首次占位 UTC 时间';
COMMENT ON COLUMN agent.creation_spec_preview_tool_receipt.updated_at IS 'Tool 回执最近状态变更 UTC 时间';

CREATE TABLE agent.creation_spec_preview_projection (
    session_id uuid NOT NULL,
    source_input_id uuid NOT NULL,
    source_enqueue_seq bigint NOT NULL,
    schema_version varchar(64) NOT NULL,
    resource_id uuid NOT NULL,
    project_id uuid NOT NULL,
    resource_version bigint NOT NULL,
    content_digest char(64) NOT NULL,
    status varchar(16) NOT NULL,
    title varchar(80) NOT NULL,
    goal varchar(2000) NOT NULL,
    deliverable_type varchar(24) NOT NULL,
    audience varchar(500) NULL,
    locale varchar(16) NOT NULL,
    phases jsonb NOT NULL,
    constraints jsonb NOT NULL,
    acceptance_criteria jsonb NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_creation_spec_preview_projection PRIMARY KEY (session_id),
    CONSTRAINT uq_creation_spec_preview_projection__resource UNIQUE (resource_id),
	CONSTRAINT ck_creation_spec_preview_projection__source_seq CHECK (source_enqueue_seq > 0),
    CONSTRAINT ck_creation_spec_preview_projection__schema CHECK (schema_version = 'creation_spec.preview.card.v1'),
    CONSTRAINT ck_creation_spec_preview_projection__version CHECK (resource_version > 0),
    CONSTRAINT ck_creation_spec_preview_projection__content_digest CHECK (content_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_creation_spec_preview_projection__status CHECK (status = 'draft'),
    CONSTRAINT ck_creation_spec_preview_projection__deliverable CHECK (deliverable_type IN ('video', 'image_set', 'audio', 'mixed')),
    CONSTRAINT ck_creation_spec_preview_projection__locale CHECK (locale IN ('zh-CN', 'en-US')),
    CONSTRAINT ck_creation_spec_preview_projection__arrays CHECK (
        jsonb_typeof(phases) = 'array' AND
        jsonb_typeof(constraints) = 'array' AND
        jsonb_typeof(acceptance_criteria) = 'array'
    )
);

COMMENT ON TABLE agent.creation_spec_preview_projection IS 'Workspace 可直接消费的最新创作规格预览安全投影，每个 Session 至多一条';
COMMENT ON COLUMN agent.creation_spec_preview_projection.session_id IS '关联 Agent Session 的逻辑标识，同时作为投影主键，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.creation_spec_preview_projection.source_input_id IS '产生当前最新卡片的 Session Input UUIDv7，用于拒绝旧输入覆盖新投影';
COMMENT ON COLUMN agent.creation_spec_preview_projection.source_enqueue_seq IS '产生当前最新卡片的 Session HOL 入队序号，后续输入可替换为新的 CreationSpec 资源';
COMMENT ON COLUMN agent.creation_spec_preview_projection.schema_version IS '安全卡片版本，固定 creation_spec.preview.card.v1';
COMMENT ON COLUMN agent.creation_spec_preview_projection.resource_id IS 'Business 创作规格草稿资源 UUIDv7';
COMMENT ON COLUMN agent.creation_spec_preview_projection.project_id IS 'Business Project UUIDv7，用于 Snapshot 与身份绑定交叉校验';
COMMENT ON COLUMN agent.creation_spec_preview_projection.resource_version IS 'Business 草稿资源版本，必须大于 0';
COMMENT ON COLUMN agent.creation_spec_preview_projection.content_digest IS 'Business 冻结 CreationSpec 内容的 SHA-256 小写十六进制摘要';
COMMENT ON COLUMN agent.creation_spec_preview_projection.status IS 'V1 预览固定为 draft';
COMMENT ON COLUMN agent.creation_spec_preview_projection.title IS '经确定性 Validator 校验的预览标题';
COMMENT ON COLUMN agent.creation_spec_preview_projection.goal IS '经确定性 Validator 校验的预览目标';
COMMENT ON COLUMN agent.creation_spec_preview_projection.deliverable_type IS 'video、image_set、audio 或 mixed';
COMMENT ON COLUMN agent.creation_spec_preview_projection.audience IS '可选目标受众，空值以 NULL 保存';
COMMENT ON COLUMN agent.creation_spec_preview_projection.locale IS 'zh-CN 或 en-US';
COMMENT ON COLUMN agent.creation_spec_preview_projection.phases IS '1 至 6 个稳定 Phase 安全 JSON 数组';
COMMENT ON COLUMN agent.creation_spec_preview_projection.constraints IS '0 至 8 条稳定 Constraint 安全 JSON 数组';
COMMENT ON COLUMN agent.creation_spec_preview_projection.acceptance_criteria IS '1 至 8 条稳定验收条件安全 JSON 数组';
COMMENT ON COLUMN agent.creation_spec_preview_projection.updated_at IS '投影最近冻结 UTC 时间';
