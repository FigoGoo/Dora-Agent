CREATE TABLE agent.session_user_message_turn (
    turn_id uuid NOT NULL,
    input_id uuid NOT NULL,
    session_id uuid NOT NULL,
    message_id uuid NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    output_id uuid NOT NULL,
    model_call_id uuid NOT NULL,
    recovery_event_id uuid NOT NULL,
    terminal_event_id uuid NOT NULL,
    status varchar(16) NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    completed_at timestamptz NULL,
    CONSTRAINT pk_session_user_message_turn PRIMARY KEY (turn_id),
    CONSTRAINT uq_session_user_message_turn__input UNIQUE (input_id),
    CONSTRAINT uq_session_user_message_turn__output UNIQUE (output_id),
    CONSTRAINT uq_session_user_message_turn__model_call UNIQUE (model_call_id),
    CONSTRAINT uq_session_user_message_turn__recovery_event UNIQUE (recovery_event_id),
    CONSTRAINT uq_session_user_message_turn__terminal_event UNIQUE (terminal_event_id),
    CONSTRAINT ck_session_user_message_turn__status CHECK (status IN ('created', 'running', 'completed', 'failed')),
    CONSTRAINT ck_session_user_message_turn__version CHECK (version > 0),
    CONSTRAINT ck_session_user_message_turn__terminal_time CHECK (
        (status IN ('created', 'running') AND completed_at IS NULL)
        OR (status IN ('completed', 'failed') AND completed_at IS NOT NULL)
    )
);

COMMENT ON TABLE agent.session_user_message_turn IS '通用用户消息开发预览的稳定 Turn 身份与终态状态表';
COMMENT ON COLUMN agent.session_user_message_turn.turn_id IS '入队事务预分配且技术重试复用的 Turn UUIDv7';
COMMENT ON COLUMN agent.session_user_message_turn.input_id IS '关联首个用户消息 Input 的逻辑标识，不设置物理外键';
COMMENT ON COLUMN agent.session_user_message_turn.session_id IS '关联 Agent Session 的逻辑标识，不设置物理外键';
COMMENT ON COLUMN agent.session_user_message_turn.message_id IS '关联加密用户 Message 的逻辑标识，不设置物理外键';
COMMENT ON COLUMN agent.session_user_message_turn.user_id IS '入队时冻结的可信 Business User 逻辑标识';
COMMENT ON COLUMN agent.session_user_message_turn.project_id IS '入队时冻结的可信 Business Project 逻辑标识';
COMMENT ON COLUMN agent.session_user_message_turn.output_id IS '入队时预分配的 Output Receipt UUIDv7';
COMMENT ON COLUMN agent.session_user_message_turn.model_call_id IS '入队时预分配的唯一模型调用 UUIDv7';
COMMENT ON COLUMN agent.session_user_message_turn.recovery_event_id IS '模型结果未知时使用的稳定恢复阻断事件 UUIDv7';
COMMENT ON COLUMN agent.session_user_message_turn.terminal_event_id IS 'completed 或 failed 使用的稳定终态事件 UUIDv7';
COMMENT ON COLUMN agent.session_user_message_turn.status IS 'Turn 状态：created、running、completed 或 failed';
COMMENT ON COLUMN agent.session_user_message_turn.version IS 'Turn 乐观锁版本，从 1 开始单调递增';
COMMENT ON COLUMN agent.session_user_message_turn.created_at IS 'Turn 创建 UTC 时间';
COMMENT ON COLUMN agent.session_user_message_turn.updated_at IS 'Turn 最近状态变更 UTC 时间';
COMMENT ON COLUMN agent.session_user_message_turn.completed_at IS 'Turn 进入 completed 或 failed 的 UTC 时间';

CREATE INDEX idx_session_user_message_turn__session_created
    ON agent.session_user_message_turn (session_id, created_at, turn_id);

CREATE TABLE agent.session_user_message_turn_context (
    turn_id uuid NOT NULL,
    schema_version varchar(64) NOT NULL,
    session_id uuid NOT NULL,
    input_id uuid NOT NULL,
    message_id uuid NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    message_cutoff_seq bigint NOT NULL,
    message_content_digest char(64) NOT NULL,
    skill_snapshot_ref varchar(128) NOT NULL,
    skill_snapshot_digest char(64) NOT NULL,
    prompt_ref varchar(128) NOT NULL,
    prompt_digest char(64) NOT NULL,
    tool_registry_ref varchar(128) NOT NULL,
    tool_registry_digest char(64) NOT NULL,
    runtime_policy_ref varchar(128) NOT NULL,
    runtime_policy_digest char(64) NOT NULL,
    model_route_ref varchar(128) NOT NULL,
    model_route_digest char(64) NOT NULL,
    budget_ref varchar(128) NOT NULL,
    budget_digest char(64) NOT NULL,
    access_scope_ref varchar(128) NOT NULL,
    access_scope_digest char(64) NOT NULL,
    context_digest char(64) NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_session_user_message_turn_context PRIMARY KEY (turn_id),
    CONSTRAINT uq_session_user_message_turn_context__input UNIQUE (input_id),
    CONSTRAINT ck_session_user_message_turn_context__schema CHECK (schema_version = 'user_message.turn_context.v2preview1'),
    CONSTRAINT ck_session_user_message_turn_context__cutoff CHECK (message_cutoff_seq > 0),
    CONSTRAINT ck_session_user_message_turn_context__digests CHECK (
        message_content_digest ~ '^[0-9a-f]{64}$'
        AND skill_snapshot_digest ~ '^[0-9a-f]{64}$'
        AND prompt_digest ~ '^[0-9a-f]{64}$'
        AND tool_registry_digest ~ '^[0-9a-f]{64}$'
        AND runtime_policy_digest ~ '^[0-9a-f]{64}$'
        AND model_route_digest ~ '^[0-9a-f]{64}$'
        AND budget_digest ~ '^[0-9a-f]{64}$'
        AND access_scope_digest ~ '^[0-9a-f]{64}$'
        AND context_digest ~ '^[0-9a-f]{64}$'
    )
);

COMMENT ON TABLE agent.session_user_message_turn_context IS 'user_message.runtime.v2preview1 入队时冻结的最小不可变上下文';
COMMENT ON COLUMN agent.session_user_message_turn_context.turn_id IS '关联开发预览 Turn 的逻辑标识，同时作为主键';
COMMENT ON COLUMN agent.session_user_message_turn_context.schema_version IS '最小上下文版本，固定 user_message.turn_context.v2preview1';
COMMENT ON COLUMN agent.session_user_message_turn_context.session_id IS '冻结的 Agent Session 逻辑标识';
COMMENT ON COLUMN agent.session_user_message_turn_context.input_id IS '冻结的 Session Input 逻辑标识';
COMMENT ON COLUMN agent.session_user_message_turn_context.message_id IS '冻结的 User Message 逻辑标识';
COMMENT ON COLUMN agent.session_user_message_turn_context.user_id IS '冻结的可信 Business User 逻辑标识';
COMMENT ON COLUMN agent.session_user_message_turn_context.project_id IS '冻结的可信 Business Project 逻辑标识';
COMMENT ON COLUMN agent.session_user_message_turn_context.message_cutoff_seq IS '模型历史截止 Message Seq，本 Profile 固定首条用户消息';
COMMENT ON COLUMN agent.session_user_message_turn_context.message_content_digest IS '规范用户消息明文 SHA-256 摘要';
COMMENT ON COLUMN agent.session_user_message_turn_context.skill_snapshot_ref IS '精确 Session Skill Snapshot 不可变引用';
COMMENT ON COLUMN agent.session_user_message_turn_context.skill_snapshot_digest IS 'Session Skill Snapshot canonical 摘要';
COMMENT ON COLUMN agent.session_user_message_turn_context.prompt_ref IS 'Direct Response Prompt 精确版本引用';
COMMENT ON COLUMN agent.session_user_message_turn_context.prompt_digest IS 'Direct Response Prompt canonical 摘要';
COMMENT ON COLUMN agent.session_user_message_turn_context.tool_registry_ref IS '空 Executable Tool Registry 精确版本引用';
COMMENT ON COLUMN agent.session_user_message_turn_context.tool_registry_digest IS '空 Executable Tool Registry canonical 摘要';
COMMENT ON COLUMN agent.session_user_message_turn_context.runtime_policy_ref IS '本地开发预览 Runtime Policy 精确版本引用';
COMMENT ON COLUMN agent.session_user_message_turn_context.runtime_policy_digest IS 'Runtime Policy canonical 摘要';
COMMENT ON COLUMN agent.session_user_message_turn_context.model_route_ref IS '本地确定性 Fake Model 精确版本引用';
COMMENT ON COLUMN agent.session_user_message_turn_context.model_route_digest IS 'Model Route canonical 摘要';
COMMENT ON COLUMN agent.session_user_message_turn_context.budget_ref IS '单模型调用 Budget 精确版本引用';
COMMENT ON COLUMN agent.session_user_message_turn_context.budget_digest IS 'Budget canonical 摘要';
COMMENT ON COLUMN agent.session_user_message_turn_context.access_scope_ref IS 'Ensure 命令冻结的可信访问范围引用';
COMMENT ON COLUMN agent.session_user_message_turn_context.access_scope_digest IS '访问范围 canonical 摘要';
COMMENT ON COLUMN agent.session_user_message_turn_context.context_digest IS '上述字段具名 canonical 编码的整体 SHA-256 摘要';
COMMENT ON COLUMN agent.session_user_message_turn_context.created_at IS '上下文与 Message/Input/Turn 同事务创建的 UTC 时间';

CREATE TABLE agent.session_user_message_run (
    run_id uuid NOT NULL,
    turn_id uuid NOT NULL,
    input_id uuid NOT NULL,
    session_id uuid NOT NULL,
    owner_fence bigint NOT NULL,
    status varchar(24) NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    started_at timestamptz NULL,
    completed_at timestamptz NULL,
    CONSTRAINT pk_session_user_message_run PRIMARY KEY (run_id),
    CONSTRAINT uq_session_user_message_run__turn UNIQUE (turn_id),
    CONSTRAINT uq_session_user_message_run__input UNIQUE (input_id),
    CONSTRAINT ck_session_user_message_run__fence CHECK (owner_fence > 0),
    CONSTRAINT ck_session_user_message_run__status CHECK (status IN ('created', 'running', 'recovery_pending', 'completed', 'failed')),
    CONSTRAINT ck_session_user_message_run__version CHECK (version > 0),
    CONSTRAINT ck_session_user_message_run__times CHECK (
        (status = 'created' AND started_at IS NULL AND completed_at IS NULL)
        OR (status IN ('running', 'recovery_pending') AND started_at IS NOT NULL AND completed_at IS NULL)
        OR (status IN ('completed', 'failed') AND started_at IS NOT NULL AND completed_at IS NOT NULL)
    )
);

COMMENT ON TABLE agent.session_user_message_run IS 'user_message.runtime.v2preview1 首次 Claim 创建并在恢复中复用的稳定 Run';
COMMENT ON COLUMN agent.session_user_message_run.run_id IS '首次 Claim 使用候选 UUIDv7 创建的稳定 Run 标识';
COMMENT ON COLUMN agent.session_user_message_run.turn_id IS '关联入队时冻结 Turn 的逻辑标识';
COMMENT ON COLUMN agent.session_user_message_run.input_id IS '关联严格 HOL Input 的逻辑标识';
COMMENT ON COLUMN agent.session_user_message_run.session_id IS '关联 Session Lane 的逻辑标识';
COMMENT ON COLUMN agent.session_user_message_run.owner_fence IS '最近取得执行权的 Session Lease Fence';
COMMENT ON COLUMN agent.session_user_message_run.status IS 'Run 状态：created、running、recovery_pending、completed 或 failed';
COMMENT ON COLUMN agent.session_user_message_run.version IS 'Run 乐观锁版本，从 1 开始单调递增';
COMMENT ON COLUMN agent.session_user_message_run.created_at IS 'Run 首次 Claim 创建 UTC 时间';
COMMENT ON COLUMN agent.session_user_message_run.started_at IS 'Runner 首次合法开始 UTC 时间';
COMMENT ON COLUMN agent.session_user_message_run.completed_at IS 'Run completed 或 failed UTC 时间';

CREATE INDEX idx_session_user_message_run__session_status
    ON agent.session_user_message_run (session_id, status, created_at, run_id);

CREATE TABLE agent.session_user_message_model_receipt (
    model_call_id uuid NOT NULL,
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
    CONSTRAINT pk_session_user_message_model_receipt PRIMARY KEY (model_call_id),
    CONSTRAINT uq_session_user_message_model_receipt__run UNIQUE (run_id),
    CONSTRAINT ck_session_user_message_model_receipt__request_digest CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_session_user_message_model_receipt__execution_fence CHECK (execution_fence > 0),
    CONSTRAINT ck_session_user_message_model_receipt__status CHECK (status IN ('reserved', 'completed', 'failed')),
    CONSTRAINT ck_session_user_message_model_receipt__payload CHECK (
        (status = 'reserved' AND response_ciphertext IS NULL AND response_key_version IS NULL AND response_digest IS NULL AND error_code IS NULL AND completed_at IS NULL)
        OR (status = 'completed' AND response_ciphertext IS NOT NULL AND response_key_version IS NOT NULL AND response_digest ~ '^[0-9a-f]{64}$' AND error_code IS NULL AND completed_at IS NOT NULL)
        OR (status = 'failed' AND response_ciphertext IS NULL AND response_key_version IS NULL AND response_digest IS NULL AND error_code IS NOT NULL AND completed_at IS NOT NULL)
    )
);

COMMENT ON TABLE agent.session_user_message_model_receipt IS '本地 Fake Model 单次调用的 first-write-wins 请求与完成响应回执';
COMMENT ON COLUMN agent.session_user_message_model_receipt.model_call_id IS '入队时预分配的唯一模型调用 UUIDv7';
COMMENT ON COLUMN agent.session_user_message_model_receipt.run_id IS '关联稳定 Run 的逻辑标识';
COMMENT ON COLUMN agent.session_user_message_model_receipt.turn_id IS '关联稳定 Turn 的逻辑标识';
COMMENT ON COLUMN agent.session_user_message_model_receipt.input_id IS '关联严格 HOL Input 的逻辑标识';
COMMENT ON COLUMN agent.session_user_message_model_receipt.request_digest IS '冻结模型输入与 Route 的 canonical SHA-256 摘要';
COMMENT ON COLUMN agent.session_user_message_model_receipt.execution_fence IS '最近一次取得 reserved 本地 Fake 执行权的单调 Session Fence';
COMMENT ON COLUMN agent.session_user_message_model_receipt.status IS '模型回执状态：reserved、completed 或 failed';
COMMENT ON COLUMN agent.session_user_message_model_receipt.response_ciphertext IS 'completed 时保存的 DRAE v1 响应密文';
COMMENT ON COLUMN agent.session_user_message_model_receipt.response_key_version IS 'completed 响应密文的非秘密 Key 版本';
COMMENT ON COLUMN agent.session_user_message_model_receipt.response_digest IS 'completed 响应明文 SHA-256 摘要';
COMMENT ON COLUMN agent.session_user_message_model_receipt.error_code IS 'failed 时保存的稳定脱敏错误码';
COMMENT ON COLUMN agent.session_user_message_model_receipt.created_at IS '模型回执首次 reserve UTC 时间';
COMMENT ON COLUMN agent.session_user_message_model_receipt.completed_at IS '模型回执 completed 或 failed UTC 时间';

CREATE TABLE agent.session_user_message_output_receipt (
    output_id uuid NOT NULL,
    run_id uuid NOT NULL,
    turn_id uuid NOT NULL,
    input_id uuid NOT NULL,
    projection_key varchar(128) NOT NULL,
    schema_version varchar(64) NOT NULL,
    status varchar(16) NOT NULL,
    result_ciphertext bytea NULL,
    result_key_version varchar(64) NULL,
    result_digest char(64) NULL,
    error_code varchar(64) NULL,
    created_at timestamptz NOT NULL,
    completed_at timestamptz NULL,
    CONSTRAINT pk_session_user_message_output_receipt PRIMARY KEY (output_id),
    CONSTRAINT uq_session_user_message_output_receipt__run UNIQUE (run_id),
    CONSTRAINT uq_session_user_message_output_receipt__turn UNIQUE (turn_id),
    CONSTRAINT ck_session_user_message_output_receipt__schema CHECK (schema_version IN ('session.turn.direct_response.card.v1', 'session.turn.failure.card.v1')),
    CONSTRAINT ck_session_user_message_output_receipt__status CHECK (status IN ('open', 'completed', 'failed')),
    CONSTRAINT ck_session_user_message_output_receipt__payload CHECK (
        (status = 'open' AND result_ciphertext IS NULL AND result_key_version IS NULL AND result_digest IS NULL AND error_code IS NULL AND completed_at IS NULL)
        OR (status = 'completed' AND schema_version = 'session.turn.direct_response.card.v1' AND result_ciphertext IS NOT NULL AND result_key_version IS NOT NULL AND result_digest ~ '^[0-9a-f]{64}$' AND error_code IS NULL AND completed_at IS NOT NULL)
        OR (status = 'failed' AND schema_version = 'session.turn.failure.card.v1' AND result_ciphertext IS NOT NULL AND result_key_version IS NOT NULL AND result_digest ~ '^[0-9a-f]{64}$' AND error_code IS NOT NULL AND completed_at IS NOT NULL)
    )
);

COMMENT ON TABLE agent.session_user_message_output_receipt IS '完整 Direct Response 或 Failure DTO 的加密 first-write-wins 输出回执';
COMMENT ON COLUMN agent.session_user_message_output_receipt.output_id IS '入队时预分配的稳定 Output UUIDv7';
COMMENT ON COLUMN agent.session_user_message_output_receipt.run_id IS '关联稳定 Run 的逻辑标识';
COMMENT ON COLUMN agent.session_user_message_output_receipt.turn_id IS '关联稳定 Turn 的逻辑标识';
COMMENT ON COLUMN agent.session_user_message_output_receipt.input_id IS '关联严格 HOL Input 的逻辑标识';
COMMENT ON COLUMN agent.session_user_message_output_receipt.projection_key IS 'Workspace 幂等投影键，固定绑定 Session 与 Turn';
COMMENT ON COLUMN agent.session_user_message_output_receipt.schema_version IS 'Direct Response 或 Failure Card 精确版本';
COMMENT ON COLUMN agent.session_user_message_output_receipt.status IS '输出回执状态：open、completed 或 failed';
COMMENT ON COLUMN agent.session_user_message_output_receipt.result_ciphertext IS '终态完整 DTO 的 DRAE v1 AEAD 密文';
COMMENT ON COLUMN agent.session_user_message_output_receipt.result_key_version IS '终态 DTO 密文的非秘密 Key 版本';
COMMENT ON COLUMN agent.session_user_message_output_receipt.result_digest IS '终态 DTO canonical SHA-256 摘要';
COMMENT ON COLUMN agent.session_user_message_output_receipt.error_code IS 'failed DTO 的稳定错误码';
COMMENT ON COLUMN agent.session_user_message_output_receipt.created_at IS '输出回执随 Run 创建的 UTC 时间';
COMMENT ON COLUMN agent.session_user_message_output_receipt.completed_at IS '输出 DTO 首次冻结 UTC 时间';

CREATE TABLE agent.session_user_message_output_projection (
    session_id uuid NOT NULL,
    source_input_id uuid NOT NULL,
    source_enqueue_seq bigint NOT NULL,
    turn_id uuid NOT NULL,
    run_id uuid NOT NULL,
    schema_version varchar(64) NOT NULL,
    status varchar(24) NOT NULL,
    payload jsonb NOT NULL,
    projection_version bigint NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_session_user_message_output_projection PRIMARY KEY (session_id),
    CONSTRAINT uq_session_user_message_output_projection__turn UNIQUE (turn_id),
    CONSTRAINT ck_session_user_message_output_projection__source_seq CHECK (source_enqueue_seq > 0),
    CONSTRAINT ck_session_user_message_output_projection__schema CHECK (schema_version IN ('session.turn.direct_response.card.v1', 'session.turn.failure.card.v1')),
    CONSTRAINT ck_session_user_message_output_projection__status CHECK (status IN ('completed', 'failed', 'recovery_pending')),
    CONSTRAINT ck_session_user_message_output_projection__payload CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT ck_session_user_message_output_projection__version CHECK (projection_version > 0),
    CONSTRAINT ck_session_user_message_output_projection__union CHECK (
        (status = 'completed' AND schema_version = 'session.turn.direct_response.card.v1')
        OR (status IN ('failed', 'recovery_pending') AND schema_version = 'session.turn.failure.card.v1')
    )
);

COMMENT ON TABLE agent.session_user_message_output_projection IS 'Workspace Snapshot 与 SSE 共用的最新用户消息 Turn 安全卡片投影';
COMMENT ON COLUMN agent.session_user_message_output_projection.session_id IS '每个 Session 最新投影的逻辑标识和主键';
COMMENT ON COLUMN agent.session_user_message_output_projection.source_input_id IS '产生投影的严格 HOL Input UUIDv7';
COMMENT ON COLUMN agent.session_user_message_output_projection.source_enqueue_seq IS '产生投影的 Input 入队序号，防止旧结果覆盖新卡片';
COMMENT ON COLUMN agent.session_user_message_output_projection.turn_id IS '产生投影的稳定 Turn UUIDv7';
COMMENT ON COLUMN agent.session_user_message_output_projection.run_id IS '产生投影的稳定 Run UUIDv7';
COMMENT ON COLUMN agent.session_user_message_output_projection.schema_version IS '安全 Direct Response 或 Failure Card Schema 版本';
COMMENT ON COLUMN agent.session_user_message_output_projection.status IS '投影状态：completed、failed 或 recovery_pending';
COMMENT ON COLUMN agent.session_user_message_output_projection.payload IS '严格 Card JSON，不含 Prompt、密文、Provider Payload 或错误栈';
COMMENT ON COLUMN agent.session_user_message_output_projection.projection_version IS '同一 Turn 投影版本，从 1 开始单调递增';
COMMENT ON COLUMN agent.session_user_message_output_projection.updated_at IS '安全投影最近提交 UTC 时间';

CREATE TABLE agent.session_user_message_upgrade_ledger (
    input_id uuid NOT NULL,
    session_id uuid NOT NULL,
    stage varchar(16) NOT NULL,
    turn_id uuid NOT NULL,
    context_digest char(64) NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_session_user_message_upgrade_ledger PRIMARY KEY (input_id),
    CONSTRAINT uq_session_user_message_upgrade_ledger__turn UNIQUE (turn_id),
    CONSTRAINT ck_session_user_message_upgrade_ledger__stage CHECK (stage IN ('prepared', 'applied', 'verified')),
    CONSTRAINT ck_session_user_message_upgrade_ledger__digest CHECK (context_digest ~ '^[0-9a-f]{64}$')
);

COMMENT ON TABLE agent.session_user_message_upgrade_ledger IS '符合 pristine 条件的历史 user_message 最小 Context 升级状态账本';
COMMENT ON COLUMN agent.session_user_message_upgrade_ledger.input_id IS '待升级历史 Input UUIDv7，同时作为主键';
COMMENT ON COLUMN agent.session_user_message_upgrade_ledger.session_id IS '关联 Session 的逻辑标识';
COMMENT ON COLUMN agent.session_user_message_upgrade_ledger.stage IS '升级阶段：prepared、applied 或 verified';
COMMENT ON COLUMN agent.session_user_message_upgrade_ledger.turn_id IS '升级时预分配并与 Context 绑定的稳定 Turn UUIDv7';
COMMENT ON COLUMN agent.session_user_message_upgrade_ledger.context_digest IS '升级生成的最小不可变 Context canonical 摘要';
COMMENT ON COLUMN agent.session_user_message_upgrade_ledger.created_at IS 'Ledger 首次准备 UTC 时间';
COMMENT ON COLUMN agent.session_user_message_upgrade_ledger.updated_at IS 'Ledger 最近阶段变更 UTC 时间';

CREATE FUNCTION agent.reject_user_message_context_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'user message runtime immutable fact cannot be updated or deleted';
END;
$$;

CREATE TRIGGER trg_session_user_message_turn_context__immutable
BEFORE UPDATE OR DELETE ON agent.session_user_message_turn_context
FOR EACH ROW EXECUTE FUNCTION agent.reject_user_message_context_mutation();

CREATE FUNCTION agent.guard_user_message_model_receipt_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'user message model receipt cannot be deleted';
    END IF;
    IF NEW.model_call_id IS DISTINCT FROM OLD.model_call_id
       OR NEW.run_id IS DISTINCT FROM OLD.run_id
       OR NEW.turn_id IS DISTINCT FROM OLD.turn_id
       OR NEW.input_id IS DISTINCT FROM OLD.input_id
       OR NEW.request_digest IS DISTINCT FROM OLD.request_digest
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'user message model receipt identity and request are immutable';
    END IF;
    IF OLD.status <> 'reserved' THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'frozen user message model receipt cannot be updated';
    END IF;
    IF NEW.status = 'reserved' THEN
        IF NEW.execution_fence <= OLD.execution_fence THEN
            RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'user message model receipt execution fence must increase';
        END IF;
    ELSIF NEW.status IN ('completed', 'failed') THEN
        IF NEW.execution_fence IS DISTINCT FROM OLD.execution_fence THEN
            RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'terminal user message model receipt cannot change execution fence';
        END IF;
    ELSE
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'user message model receipt transition is invalid';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_session_user_message_model_receipt__guard
BEFORE UPDATE OR DELETE ON agent.session_user_message_model_receipt
FOR EACH ROW EXECUTE FUNCTION agent.guard_user_message_model_receipt_mutation();

CREATE FUNCTION agent.guard_user_message_output_receipt_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'user message output receipt cannot be deleted';
    END IF;
    IF OLD.status <> 'open' OR NEW.status NOT IN ('completed', 'failed') THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'user message output receipt only permits one open to terminal transition';
    END IF;
    IF NEW.output_id IS DISTINCT FROM OLD.output_id
       OR NEW.run_id IS DISTINCT FROM OLD.run_id
       OR NEW.turn_id IS DISTINCT FROM OLD.turn_id
       OR NEW.input_id IS DISTINCT FROM OLD.input_id
       OR NEW.projection_key IS DISTINCT FROM OLD.projection_key
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'user message output receipt identity and projection are immutable';
    END IF;
    IF (NEW.status = 'completed' AND NEW.schema_version <> 'session.turn.direct_response.card.v1')
       OR (NEW.status = 'failed' AND NEW.schema_version <> 'session.turn.failure.card.v1') THEN
        RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'user message output receipt schema does not match terminal status';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_session_user_message_output_receipt__guard
BEFORE UPDATE OR DELETE ON agent.session_user_message_output_receipt
FOR EACH ROW EXECUTE FUNCTION agent.guard_user_message_output_receipt_mutation();

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

ALTER TABLE agent.session_event_log
    DROP CONSTRAINT ck_session_event_log__aggregate_type;
ALTER TABLE agent.session_event_log
    ADD CONSTRAINT ck_session_event_log__aggregate_type CHECK (
        aggregate_type IN ('session', 'session_input', 'creation_spec', 'session_turn')
    );
COMMENT ON COLUMN agent.session_event_log.aggregate_type IS '事件关联聚合类型：session、session_input、creation_spec 或 session_turn';
